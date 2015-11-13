package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/ch3lo/yale/cluster"
	"github.com/ch3lo/yale/monitor"
	"github.com/ch3lo/yale/service"
	"github.com/ch3lo/yale/util"
	"github.com/codegangsta/cli"
	"github.com/pivotal-golang/bytefmt"
)

func handleDeploySigTerm(sm *cluster.StackManager) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGTERM)
	go func() {
		<-c
		sm.Rollback()
		os.Exit(1)
	}()
}

func deployFlags() []cli.Flag {
	return []cli.Flag{
		cli.StringFlag{
			Name:  "image",
			Usage: "Nombre de la imagen",
		},
		cli.StringFlag{
			Name:  "tag",
			Usage: "TAG de la imagen",
		},
		cli.StringSliceFlag{
			Name:  "port",
			Value: &cli.StringSlice{"8080"},
			Usage: "Puerto interno del contenedor a exponer en el Host",
		},
		cli.IntFlag{
			Name:  "cpu",
			Value: 0,
			Usage: "Cantidad de CPU reservadas para el servicio.",
		},
		cli.StringFlag{
			Name:  "memory",
			Usage: "Cantidad de memoria principal (Unidades: M, m, MB, mb, GB, G) que puede utilizar el servicio. Mas info 'man docker-run' memory.",
		},
		cli.StringSliceFlag{
			Name:  "env-file",
			Usage: "Archivo con variables de entorno",
		},
		cli.StringSliceFlag{
			Name:  "env",
			Usage: "Variables de entorno en formato KEY=VALUE",
		},
		cli.IntFlag{
			Name:  "instances",
			Value: 1,
			Usage: "Total de servicios que se quieren obtener en cada uno de los stack.",
		},
		cli.Float64Flag{
			Name:  "tolerance",
			Value: 0.5,
			Usage: "Porcentaje de servicios que pueden fallar en el proceso de deploy por cada enpoint entregado." +
				"Este valor es respecto al total de instancias." +
				"Por ejemplo, si se despliegan 5 servicios y fallan ",
		},
		cli.StringFlag{
			Name:   "callback-url",
			Value:  "https://jenkinsdomain.com/buildByToken/buildWithParameters",
			Usage:  "URL donde se realizará el callback en caso de despliegue exitoso",
			EnvVar: "DEPLOYER_CALLBACK_URL",
		},
		cli.StringFlag{
			Name:  "callback-job",
			Usage: "Nombre del job de Jenkins donde se realizará la notificacion",
		},
		cli.StringFlag{
			Name:  "callback-token",
			Usage: "Token de seguridad que se utilizará en el callback",
		},
		cli.IntFlag{
			Name:  "smoke-retries",
			Value: 10,
			Usage: "Cantidad de smoke test que se realizarán antes de declarar el servicio con fallo de despliegue",
		},
		cli.StringFlag{
			Name:  "smoke-type",
			Value: "http",
			Usage: "Define si el smoke test es TCP o HTTP",
		},
		cli.StringFlag{
			Name:  "smoke-request",
			Usage: "Información necesaria para el request",
		},
		cli.StringFlag{
			Name:  "smoke-expected",
			Value: ".*",
			Usage: "Valor esperado en el smoke test para definir la prueba como exitosa. Es una expresión regular.",
		},
		cli.StringFlag{
			Name:  "warmup-request",
			Usage: "Enpoint que se utilizará para hacer el calentamiento del servicio",
		},
		cli.StringFlag{
			Name:  "warmup-expected",
			Value: ".*",
			Usage: "Valor esperado del resultado del calentamiento. Si se cumple el valor pasado, se asume un calentamiento exitoso",
		},
	}
}

func deployBefore(c *cli.Context) error {
	if c.String("image") == "" {
		return errors.New("El nombre de la imagen esta vacio")
	}

	if c.String("tag") == "" {
		return errors.New("El TAG de la imagen esta vacio")
	}

	if c.String("smoke-request") == "" {
		return errors.New("El endpoint de Smoke Test esta vacio")
	}

	if c.String("memory") != "" {
		if _, err := bytefmt.ToMegabytes(c.String("memory")); err != nil {
			return errors.New("Valor del parámetro memory invalido")
		}

	}

	for _, file := range c.StringSlice("env-file") {
		if err := util.FileExists(file); err != nil {
			return errors.New(fmt.Sprintf("El archivo %s con variables de entorno no existe", file))
		}
	}

	return nil
}

func callbackNotification(callbackUrl string, callbackJob string, token string, resume []callbackResume) {
	util.Log.Infof("Se enviará una notificacion a %s", callbackUrl)

	jsonResume, err := json.Marshal(resume)
	if err != nil {
		util.Log.Errorf("No se pudo procesar %s", resume, err)
		return
	}

	data := url.Values{}
	data.Set("job", callbackJob)
	data.Set("token", token)
	data.Set("services", string(jsonResume))

	util.Log.Debugf("Datos de la notificación: %s", data.Encode())

	req, err := http.NewRequest("POST", callbackUrl, bytes.NewBufferString(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		util.Log.Warnf("Request de la notificación tuvo el error %s", err)
		return
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	util.Log.Infof("Notificacion con estatus %d y respuesta %s", resp.StatusCode, string(body))
}

type callbackResume struct {
	RegisterId string `json:"RegisterId"`
	Address    string `json:"Address"`
}

func deployCmd(c *cli.Context) {

	envs, err := util.ParseMultiFileLinesToArray(c.StringSlice("env-file"))
	if err != nil {
		util.Log.Fatalln("No se pudo procesar el archivo con variables de entorno", err)
	}

	for _, v := range c.StringSlice("env") {
		envs = append(envs, v)
	}

	serviceConfig := service.ServiceConfig{
		CpuShares: c.Int("cpu"),
		Envs:      envs,
		ImageName: c.String("image"),
		Publish:   []string{"8080/tcp"}, // TODO desplegar puertos que no sean 8080
		Tag:       c.String("tag"),
	}

	if c.String("memory") != "" {
		mebabytes, _ := bytefmt.ToMegabytes(c.String("memory"))
		memory := mebabytes * 1024
		serviceConfig.Memory = int64(memory)
	}

	smokeConfig := monitor.MonitorConfig{
		Retries:  c.Int("smoke-retries"),
		Type:     monitor.GetMonitor(c.String("smoke-type")),
		Request:  c.String("smoke-request"),
		Expected: c.String("smoke-expected"),
	}

	warmUpConfig := monitor.MonitorConfig{
		Retries:  1,
		Type:     monitor.HTTP,
		Request:  c.String("warmup-request"),
		Expected: c.String("warmup-expected"),
	}

	util.Log.Debugf("La configuración del servicio es: %#v", serviceConfig.String())

	handleDeploySigTerm(stackManager)
	if stackManager.Deploy(serviceConfig, smokeConfig, warmUpConfig, c.Int("instances"), c.Float64("tolerance")) {
		fmt.Println("Proceso de deploy OK")
		services := stackManager.DeployedContainers()
		var resume []callbackResume

		for k := range services {
			if addr, err := services[k].AddressAndPort(8080); err != nil {
				util.Log.Errorln(err)
			} else {
				util.Log.Infof("Se desplegó %s con el tag de registrator %s y dirección %s", services[k].GetId(), services[k].RegistratorId(), addr)
				containerInfo := callbackResume{
					RegisterId: services[k].RegistratorId(),
					Address:    addr,
				}
				resume = append(resume, containerInfo)
			}
		}

		if c.String("callback-job") != "" && c.String("callback-token") != "" {
			callbackNotification(c.String("callback-url"), c.String("callback-job"), c.String("callback-token"), resume)
		} else {
			util.Log.Warnln("No existen parametros de callback")
		}
	} else {
		util.Log.Fatalln("Proceso de deploy con errores")
	}
}
