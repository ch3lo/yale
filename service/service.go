package service

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"

	"github.com/Pallinder/go-randomdata"
	log "github.com/Sirupsen/logrus"
	"github.com/ch3lo/yale/helper"
	"github.com/ch3lo/yale/util"
	"github.com/fsouza/go-dockerclient"
)

// Flow INIT -> CREATED -> SMOKE_READY -> [WARM_READY] -> READY/FAILED -> UNDEPLOYED
type Status int

const (
	INIT        Status = 1 + iota // Contenedor configurado pero aun no se crea ni corre
	CREATED                       // Contenedor creado y corriendo pero aún no verificado
	SMOKE_READY                   // Contenedor ha pasado las pruebas de humo
	WARM_READY                    // Contenedor ha pasado el calentamiento
	READY                         // Contenedor que paso exitoso el despliegue
	FAILED                        // Contenedor que fallo en el despliegue
	UNDEPLOYED                    // Contenedor removido
	LOADED                        // Contanedor cargado desde la API
)

var status = [...]string{
	"INIT",
	"CREATED",
	"SMOKE_READY",
	"WARM_READY",
	"READY",
	"FAILED",
	"UNDEPLOYED",
	"LOADED",
}

func (s Status) String() string {
	return status[s-1]
}

type ServiceConfig struct {
	ImageName string
	Tag       string
	Envs      []string
	Publish   []string
}

func (s *ServiceConfig) Version() string {
	rp := regexp.MustCompile("^([\\d\\.]+)-")
	result := rp.FindStringSubmatch(s.Tag)
	if result == nil {
		util.Log.Fatalln("Invalid TAG format")
	}
	return result[1]
}

func (s *ServiceConfig) String() string {
	return fmt.Sprintf("ImageName: %s - Tag: %s - Envs - %s - Publish: %#v", s.ImageName, s.Tag, util.MaskEnv(s.Envs), s.Publish)
}

type DockerService struct {
	Id              string
	Status          Status
	statusChannel   chan<- string
	dockerApihelper *helper.DockerHelper
	container       *docker.Container
	log             *log.Entry
}

func NewDockerService(prefixId string, dh *helper.DockerHelper, sc chan<- string) *DockerService {
	ds := new(DockerService)
	ds.Id = prefixId + "_" + randomdata.SillyName()
	ds.dockerApihelper = dh
	ds.Status = INIT
	ds.statusChannel = sc

	ds.log = util.Log.WithFields(log.Fields{
		"ds": ds.Id,
	})

	ds.log.Infof("Setting Up Service")

	return ds
}

func NewFromContainer(prefixId string, dh *helper.DockerHelper, container *docker.Container, sc chan<- string) *DockerService {
	ds := NewDockerService(prefixId, dh, sc)
	ds.container = container
	ds.Status = LOADED

	return ds
}

func (ds *DockerService) GetId() string {
	return ds.Id
}

func (ds *DockerService) RegistratorId() string {
	return ds.container.Node.Name + ":" + ds.container.Name[1:] + ":8080"
}

func (ds *DockerService) dockerCli() *helper.DockerHelper {
	dh := ds.dockerApihelper
	if dh == nil {
		ds.SetStatus(FAILED)
		return nil
	}

	return dh
}

func (ds *DockerService) bindPort(publish []string) map[docker.Port][]docker.PortBinding {
	portBindings := map[docker.Port][]docker.PortBinding{}

	for _, v := range publish {
		ds.log.Debugln("Processing Port", v)
		var dp docker.Port
		reflect.ValueOf(&dp).Elem().SetString(v)
		portBindings[dp] = []docker.PortBinding{docker.PortBinding{}}
	}

	ds.log.Debugf("PortBindings %#v", portBindings)

	return portBindings
}

func (ds *DockerService) SetStatus(status Status) {
	ds.Status = status
	ds.statusChannel <- ds.Id
}

func (ds *DockerService) Run(serviceConfig ServiceConfig) {
	dockerConfig := docker.Config{
		Image: serviceConfig.ImageName + ":" + serviceConfig.Tag,
		Env:   serviceConfig.Envs,
	}

	dockerHostConfig := docker.HostConfig{
		Binds:           []string{"/var/log/service/:/var/log/service/"},
		PortBindings:    ds.bindPort(serviceConfig.Publish),
		PublishAllPorts: false,
		Privileged:      false,
	}

	opts := docker.CreateContainerOptions{
		Config:     &dockerConfig,
		HostConfig: &dockerHostConfig}

	var err error
	ds.container, err = ds.dockerCli().CreateAndRun(opts)

	if err != nil {
		ds.log.Errorf("Run error: %s", err)
		fmt.Printf("Container Run with error: %s", err)
		ds.SetStatus(FAILED)
		return
	}

	ds.log.Debugf("Service Registrator ID %s", ds.RegistratorId())

	ds.SetStatus(CREATED)
}

func (ds *DockerService) Undeploy() {
	if ds.container != nil && ds.container.ID != "" {
		err := ds.dockerCli().UndeployContainer(ds.container.ID, true, 10)
		if err != nil {
			ds.log.Errorln("No se pudo remover el contenedor", err)
		}
	} else {
		ds.log.Warnf("Container Instance not found")
	}
}

func (ds *DockerService) ContainerName() string {
	return ds.container.Name
}

func (ds *DockerService) ContainerImageName() string {
	return ds.container.Config.Image
}

func (ds *DockerService) ContainerSwarmNode() string {

	if ds.container.Node == nil {
		return ""
	}

	return ds.container.Node.Name
}

func (ds *DockerService) ContainerStatus() string {
	return ds.container.State.String()
}

func (ds *DockerService) PublicPorts() map[int64]int64 {
	ports := make(map[int64]int64)
	ds.log.Debugf("Api Ports %#v", ds.container.NetworkSettings.PortMappingAPI())
	for _, val := range ds.container.NetworkSettings.PortMappingAPI() {
		ds.log.Debugf("Private Port [%d] Public Port [%d]", val.PrivatePort, val.PublicPort)
		if val.PrivatePort != 0 && val.PublicPort != 0 {
			ports[val.PrivatePort] = val.PublicPort
		}
	}

	return ports
}

func (ds *DockerService) AddressAndPort(internalPort int64) (string, error) {

	ds.log.Debugf("Api Ports %#v", ds.container.NetworkSettings.PortMappingAPI())
	for _, val := range ds.container.NetworkSettings.PortMappingAPI() {
		ds.log.Debugln("Private Port", val.PrivatePort, "Public Port", val.PublicPort)
		if val.PrivatePort == internalPort {
			addr := val.IP + ":" + strconv.FormatInt(val.PublicPort, 10)
			ds.log.Debugf("Calculated Addr %s", addr)
			return addr, nil
		}
	}

	return "", errors.New(fmt.Sprintf("Unknown port %d", internalPort))
}
