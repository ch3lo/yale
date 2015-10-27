package cli

import (
	"errors"
	"fmt"
	"os"
	"path"

	log "github.com/Sirupsen/logrus"
	"github.com/ch3lo/yale/cluster"
	"github.com/ch3lo/yale/helper"
	"github.com/ch3lo/yale/util"
	"github.com/ch3lo/yale/version"
	"github.com/codegangsta/cli"
)

var stackManager *cluster.StackManager
var logFile *os.File = nil

type logConfig struct {
	LogLevel     string
	LogFormatter string
	LogColored   bool
	LogOutput    string
}

func dockerCfgPath() string {
	p := path.Join(os.Getenv("HOME"), ".docker", "config.json")
	if err := util.FileExists(p); err != nil {
		p = path.Join(os.Getenv("HOME"), ".dockercfg")
	}

	return p
}

func setupLogger(config logConfig) error {
	var err error

	if util.Log.Level, err = log.ParseLevel(config.LogLevel); err != nil {
		return err
	}

	switch config.LogFormatter {
	case "text":
		formatter := new(log.TextFormatter)
		formatter.ForceColors = config.LogColored
		formatter.FullTimestamp = true
		util.Log.Formatter = formatter
		break
	case "json":
		formatter := new(log.JSONFormatter)
		util.Log.Formatter = formatter
		break
	default:
		return errors.New("Formato de lo log desconocido")
	}

	switch config.LogOutput {
	case "stdout":
		util.Log.Out = os.Stdout
		break
	case "stderr":
		util.Log.Out = os.Stderr
		break
	case "file":
		util.Log.Out = logFile
		break
	default:
		return errors.New("Output de logs desconocido")
	}

	return nil
}

func globalFlags() []cli.Flag {
	flags := []cli.Flag{
		cli.StringSliceFlag{
			Name:   "endpoint, ep",
			Usage:  "Endpoint de la API de Docker",
			EnvVar: "DOCKER_HOST",
		},
		cli.BoolFlag{
			Name:  "tls",
			Usage: "Utiliza TLS en la comunicacion con los Endpoints",
		},
		cli.StringFlag{
			Name:   "cert_path",
			Usage:  "Directorio con los certificados",
			EnvVar: "DOCKER_CERT_PATH",
		},
		cli.StringFlag{
			Name:   "cert",
			Value:  "cert.pem",
			Usage:  "Ruta relativa del arhivo con el certificado cliente",
			EnvVar: "DEPLOYER_CERT_CERT",
		},
		cli.StringFlag{
			Name:   "key",
			Value:  "key.pem",
			Usage:  "Ruta relativa del arhivo con la llave del certificado cliente",
			EnvVar: "DEPLOYER_CERT_KEY",
		},
		cli.StringFlag{
			Name:   "auth-file",
			Value:  dockerCfgPath(),
			Usage:  "Archivo de configuracion de la autenticacion",
			EnvVar: "DEPLOYER_AUTH_CONFIG",
		},
		cli.StringFlag{
			Name:   "log-level",
			Value:  "debug",
			Usage:  "Nivel de verbosidad de log",
			EnvVar: "DEPLOYER_LOG_LEVEL",
		},
		cli.StringFlag{
			Name:   "log-formatter",
			Value:  "text",
			Usage:  "Formato de log",
			EnvVar: "DEPLOYER_LOG_FORMATTER",
		},
		cli.BoolFlag{
			Name:   "log-colored",
			Usage:  "Coloreo de log :D",
			EnvVar: "DEPLOYER_LOG_COLORED",
		},
		cli.StringFlag{
			Name:   "log-output",
			Value:  "file",
			Usage:  "Output de los logs",
			EnvVar: "DEPLOYER_LOG_OUTPUT",
		},
	}

	return flags
}

func buildCertPath(certPath string, file string) string {
	if file == "" {
		return ""
	}

	if certPath != "" {
		return certPath + "/" + file
	}

	return file
}

func setupGlobalFlags(c *cli.Context) error {
	var config logConfig = logConfig{}
	config.LogLevel = c.String("log-level")
	config.LogFormatter = c.String("log-formatter")
	config.LogColored = c.Bool("log-colored")
	config.LogOutput = c.String("log-output")

	var err error

	if err = setupLogger(config); err != nil {
		fmt.Println("Nivel de log invalido")
		return err
	}

	stackManager = cluster.NewStackManager()

	for _, ep := range c.StringSlice("endpoint") {
		util.Log.Infof("Configuring Docker Endpoint %s", ep)
		var dh *helper.DockerHelper
		if c.Bool("tls") {
			cert := buildCertPath(c.String("cert_path"), c.String("cert"))
			key := buildCertPath(c.String("cert_path"), c.String("key"))
			dh, err = helper.NewDockerTlsHelper(ep, c.String("auth-file"), cert, key)
		} else {
			dh, err = helper.NewDockerHelper(ep, c.String("auth-file"))
		}
		if err != nil {
			fmt.Println("Endpoint de Docker invalido")
			return err
		}
		stackManager.AddDockerApiEndpoint(dh)
	}

	util.Log.Debugf("%#v", config)

	return nil
}

func RunApp() {

	app := cli.NewApp()
	app.Name = "yale"
	app.Usage = "Despliegue de App Docker con esteroides"
	app.Version = version.VERSION + " (" + version.GITCOMMIT + ")"

	app.Flags = globalFlags()

	app.Before = func(c *cli.Context) error {
		return setupGlobalFlags(c)
	}

	app.Commands = commands

	var err error
	logFile, err = os.OpenFile("yale.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		util.Log.Warnln("Error al abrir el archivo")
	} else {
		defer logFile.Close()
	}

	err = app.Run(os.Args)
	if err != nil {
		util.Log.Fatalln(err)
	}
}