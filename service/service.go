package service

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

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
)

var status = [...]string{
	"INIT",
	"CREATED",
	"READY",
	"FAILED",
	"UNDEPLOYED",
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

type DockerService struct {
	Id              string
	Status          Status
	statusChannel   chan<- string
	dockerApihelper *helper.DockerHelper
	container       *docker.Container
	config          docker.CreateContainerOptions
}

func NewDockerService(prefixId string, dh *helper.DockerHelper, serviceConfig ServiceConfig, sc chan<- string) *DockerService {
	ds := new(DockerService)
	ds.Id = prefixId + "_" + randomdata.SillyName()
	ds.dockerApihelper = dh
	ds.Status = INIT
	ds.statusChannel = sc

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

	ds.config = opts

	util.PrintfAndLogInfof("Setting Up Service %s:", ds.Id)
	util.Log.Debugf("Image: %s", dockerConfig.Image)
	util.Log.Debugf("Envs: %s", maskEnv(dockerConfig.Env))
	util.Log.Debugf("Publish: %s", dockerHostConfig.PortBindings)

	return ds
}

func maskEnv(unmaskedEnvs []string) []string {
	var maskedEnvs []string
	for _, val := range unmaskedEnvs {
		kv := strings.Split(val, "=")
		if strings.Contains(kv[0], "pass") {
			maskedEnvs = append(maskedEnvs, kv[0]+"="+"*****")
		} else {
			maskedEnvs = append(maskedEnvs, val)
		}
	}

	return maskedEnvs
}

func (ds *DockerService) GetId() string {
	return ds.Id
}

func (ds *DockerService) RegistratorId() string {
	ds.container, _ = ds.dockerCli().ContainerInspect(ds.container.ID)
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

func (ds *DockerService) Run() {
	var err error

	ds.container, err = ds.dockerCli().CreateAndRun(ds.config)

	if err != nil {
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
