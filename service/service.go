package service

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"

	"github.com/Pallinder/go-randomdata"
	"github.com/ch3lo/yale/helper"
	"github.com/ch3lo/yale/util"
	"github.com/fsouza/go-dockerclient"
)

// Flow INIT -> CREATED -> READY/FAILED -> UNDEPLOYED
type Status int

const (
	INIT Status = 1 + iota
	CREATED
	READY
	FAILED
	UNDEPLOYED
	LOADED
)

var status = [...]string{
	"INIT",
	"CREATED",
	"READY",
	"FAILED",
	"UNDEPLOYED",
	"LOADED",
}

func (s Status) String() string {
	return status[s-1]
}

type ServiceConfig struct {
	ImageName      string
	Tag            string
	Envs           []string
	Healthy        string
	HealthyRetries int
	Publish        []string
}

func (s *ServiceConfig) Version() string {
	rp := regexp.MustCompile("(\\d|\\.)+-")
	result := rp.FindStringSubmatch(s.Tag)
	return result[0]
}

func (s *ServiceConfig) String() string {
	return fmt.Sprintf("ImageName: %s - Tag: %s - Envs - %s - Healthy: %s - HealthyRetries: %d - Publish: %#v", s.ImageName, s.Tag, util.MaskEnv(s.Envs), s.Healthy, s.HealthyRetries, s.Publish)
}

type DockerService struct {
	Id              string
	Status          Status
	statusChannel   chan<- string
	dockerApihelper *helper.DockerHelper
	container       *docker.Container
}

func NewDockerService(prefixId string, dh *helper.DockerHelper, sc chan<- string) *DockerService {
	ds := new(DockerService)
	ds.Id = prefixId + "_" + randomdata.SillyName()
	ds.dockerApihelper = dh
	ds.Status = INIT
	ds.statusChannel = sc

	util.Log.Infof("Setting Up Service %s", ds.Id)

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
		util.Log.Debugln("Processing Port", v)
		var dp docker.Port
		reflect.ValueOf(&dp).Elem().SetString(v)
		portBindings[dp] = []docker.PortBinding{docker.PortBinding{}}
	}

	util.Log.Debugf("PortBindings %#v", portBindings)

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
		util.Log.Errorf("Run error: %s", err)
		fmt.Printf("Container Run with error: %s", err)
		ds.SetStatus(FAILED)
		return
	}

	util.Log.Debugf("Service with ID %s has Registrator ID %s", ds.GetId(), ds.RegistratorId())

	ds.SetStatus(CREATED)
}

func (ds *DockerService) Undeploy() {
	if ds.container != nil && ds.container.ID != "" {
		err := ds.dockerCli().UndeployContainer(ds.container.ID, true, 10)
		if err != nil {
			util.Log.Errorln("No se pudo remover el contenedor", err)
		}
	} else {
		util.Log.Warnf("Container Instance not found %s", ds.Id)
	}
}

func (ds *DockerService) ContainerName() string {
	return ds.container.Name
}

func (ds *DockerService) ContainerImageName() string {
	return ds.container.Config.Image
}

func (ds *DockerService) ContainerNode() string {
	return ds.container.Node.Name
}

func (ds *DockerService) ContainerStatus() string {
	return ds.container.State.String()
}

func (ds *DockerService) PublicPorts() map[int64]int64 {
	ports := make(map[int64]int64)
	util.Log.Debugf("Api Ports %#v", ds.container.NetworkSettings.PortMappingAPI())
	for _, val := range ds.container.NetworkSettings.PortMappingAPI() {
		util.Log.Debugf("Private Port [%d] Public Port [%d]", val.PrivatePort, val.PublicPort)
		if val.PrivatePort != 0 && val.PublicPort != 0 {
			ports[val.PrivatePort] = val.PublicPort
		}
	}

	return ports
}

func (ds *DockerService) AddressAndPort(internalPort int64) (string, error) {

	util.Log.Debugf("Api Ports %#v", ds.container.NetworkSettings.PortMappingAPI())
	for _, val := range ds.container.NetworkSettings.PortMappingAPI() {
		util.Log.Debugln("Private Port", val.PrivatePort, "Public Port", val.PublicPort)
		if val.PrivatePort == internalPort {
			addr := val.IP + ":" + strconv.FormatInt(val.PublicPort, 10)
			util.Log.Debugf("Calculated Addr %s", addr)
			return addr, nil
		}
	}

	return "", errors.New(fmt.Sprintf("Unknown port %d", internalPort))
}
