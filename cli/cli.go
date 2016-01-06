package cli

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"

	log "github.com/Sirupsen/logrus"
	"github.com/ch3lo/yale/cluster"
	"github.com/ch3lo/yale/configuration"
	"github.com/ch3lo/yale/util"
	"github.com/ch3lo/yale/version"
	"github.com/codegangsta/cli"
)

var stackManager *cluster.StackManager
var logFile *os.File

type logConfig struct {
	level     string
	Formatter string
	colored   bool
	output    string
	debug     bool
}

func setupLogger(config logConfig) error {
	var err error

	if util.Log.Level, err = log.ParseLevel(config.level); err != nil {
		return err
	}

	if config.debug {
		util.Log.Level = log.DebugLevel
	}

	switch config.Formatter {
	case "text":
		formatter := new(log.TextFormatter)
		formatter.ForceColors = config.colored
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

	switch config.output {
	case "console":
		util.Log.Out = os.Stdout
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
		cli.BoolFlag{
			Name:  "debug",
			Usage: "Modo de verbosidad debug",
		},
		cli.StringFlag{
			Name:  "config",
			Value: "yale.yml",
			Usage: "Ruta del archivo de configuración",
		},
		cli.StringFlag{
			Name:   "log-level",
			Value:  "info",
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
			Value:  "console",
			Usage:  "Output de los logs. console | file",
			EnvVar: "DEPLOYER_LOG_OUTPUT",
		},
	}

	return flags
}

func setupConfiguration(configFile string) (*configuration.Configuration, error) {
	_, err := os.Stat(configFile)
	if os.IsNotExist(err) {
		return nil, err
	}

	configFile, err = filepath.Abs(configFile)
	if err != nil {
		return nil, err
	}

	var yamlFile []byte
	if yamlFile, err = ioutil.ReadFile(configFile); err != nil {
		return nil, err
	}

	var config configuration.Configuration
	if err = yaml.Unmarshal(yamlFile, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func setupApplication(c *cli.Context) error {
	logConfig := logConfig{}
	logConfig.level = c.String("log-level")
	logConfig.Formatter = c.String("log-formatter")
	logConfig.colored = c.Bool("log-colored")
	logConfig.output = c.String("log-output")
	logConfig.debug = c.Bool("debug")

	err := setupLogger(logConfig)
	if err != nil {
		return err
	}

	var appConfig *configuration.Configuration
	if appConfig, err = setupConfiguration(c.String("config")); err != nil {
		return err
	}

	stackManager = cluster.NewStackManager(appConfig)
	return nil
}

// RunApp Entrypoint de la Aplicacion.
// Procesa los comandos y sus argumentos
func RunApp() {

	app := cli.NewApp()
	app.Name = "yale"
	app.Usage = "Despliegue de App Docker con esteroides"
	app.Version = version.VERSION + " (" + version.GITCOMMIT + ")"

	app.Flags = globalFlags()

	app.Before = func(c *cli.Context) error {
		return setupApplication(c)
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
		fmt.Println(err)
		util.Log.Fatalln(err)
	}
}
