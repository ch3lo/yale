package service

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"

	log "github.com/Sirupsen/logrus"
	"github.com/ch3lo/yale/helper"
	"github.com/ch3lo/yale/monitor"
	"github.com/ch3lo/yale/util"
	"github.com/fsouza/go-dockerclient"
)

// Flow CREATED -> SMOKE_READY -> WARM_READY -> [FAILED]
// STEP_CREATED     Contenedor creado y corriendo pero aún no verificado
// STEP_SMOKE_READY Contenedor ha pasado las pruebas de humo
// STEP_WARM_READY  Contenedor que paso exitoso el despliegue
// STEP_FAILED      Contenedor que fallo en el despliegue
type Step int

const (
	STEP_CREATED Step = 1 + iota
	STEP_SMOKE_READY
	STEP_WARM_READY
	STEP_FAILED
)

var step = [...]string{
	"STEP_CREATED",
	"STEP_SMOKE_READY",
	"STEP_WARM_UP",
	"STEP_FAILED",
}

func (s Step) String() string {
	return step[s-1]
}

// RUNNING     Contenedor corriendo. Se omiten aquellos en estado de Restarting
// UNDEPLOYED  Contenedor removido
type State int

const (
	RUNNING State = 1 + iota
	UNDEPLOYED
)

var state = [...]string{
	"RUNNING",
	"UNDEPLOYED",
}

func (s State) String() string {
	return state[s-1]
}

type ServiceConfig struct {
	CpuShares int
	Envs      []string
	ImageName string
	Memory    int64
	Publish   []string
	Tag       string
}

func (s *ServiceConfig) Version() string {
	rp := regexp.MustCompile("^([\\d\\.]+)-")
	result := rp.FindStringSubmatch(s.Tag)
	if result == nil {
		util.Log.Fatalln("Formato de TAG invalido")
	}
	return result[1]
}

func (s *ServiceConfig) String() string {
	return fmt.Sprintf("ImageName: %s - Tag: %s - CpuShares: %d - Memory: %s - Publish: %#v - Envs: %s", s.ImageName, s.Tag, s.CpuShares, s.Memory, s.Publish, util.MaskEnv(s.Envs))
}

type DockerService struct {
	id              string
	loaded          bool
	state           State
	step            Step
	statusChannel   chan<- string
	dockerApihelper *helper.DockerHelper
	container       *docker.Container
	log             *log.Entry
}

func NewDockerService(id string, dh *helper.DockerHelper, sc chan<- string) *DockerService {
	ds := new(DockerService)
	ds.id = id
	ds.dockerApihelper = dh
	ds.loaded = false
	ds.statusChannel = sc

	ds.log = util.Log.WithFields(log.Fields{
		"ds": ds.id,
	})

	ds.log.Infof("Se configuró el Servicio de Docker")

	return ds
}

func NewFromContainer(id string, dh *helper.DockerHelper, container *docker.Container, sc chan<- string) *DockerService {
	ds := NewDockerService(id, dh, sc)
	ds.container = container
	ds.loaded = true

	ds.log.Infof("Se configuró desde el contenedor %s", ds.container.Name)
	return ds
}

func (ds *DockerService) GetId() string {
	return ds.id
}

func (ds *DockerService) RegistratorId() string {
	return ds.container.Node.Name + ":" + ds.container.Name[1:] + ":8080"
}

func (ds *DockerService) dockerCli() *helper.DockerHelper {
	dh := ds.dockerApihelper
	if dh == nil {
		ds.setStep(STEP_FAILED)
		return nil
	}

	return dh
}

func (ds *DockerService) bindPort(publish []string) map[docker.Port][]docker.PortBinding {
	portBindings := map[docker.Port][]docker.PortBinding{}

	for _, v := range publish {
		ds.log.Debugln("Procesando el bindeo del puerto", v)
		var dp docker.Port
		reflect.ValueOf(&dp).Elem().SetString(v)
		portBindings[dp] = []docker.PortBinding{docker.PortBinding{}}
	}

	ds.log.Debugf("PortBindings %#v", portBindings)

	return portBindings
}

func (ds *DockerService) setStep(status Step) {
	ds.step = status
	ds.statusChannel <- ds.id
}

func (ds *DockerService) GetStep() Step {
	return ds.step
}

func (ds *DockerService) setState(state State) {
	ds.state = state
	//ds.statusChannel <- ds.id
}

func (ds *DockerService) CheckState(state State) bool {
	if ds.container == nil {
		return false
	}

	if state == RUNNING {
		return (ds.container.State.Running &&
			!ds.container.State.Paused &&
			!ds.container.State.Restarting)
	}

	if state == UNDEPLOYED && ds.state == UNDEPLOYED {
		return true
	}

	return false
}

func (ds *DockerService) Run(serviceConfig ServiceConfig) {
	ds.log.Infoln("Iniciando el despliegue del servicio")
	labels := map[string]string{
		"image_name": serviceConfig.ImageName,
		"image_tag":  serviceConfig.Tag,
	}

	dockerConfig := docker.Config{
		Image:  serviceConfig.ImageName + ":" + serviceConfig.Tag,
		Env:    serviceConfig.Envs,
		Labels: labels,
	}

	dockerHostConfig := docker.HostConfig{
		Binds:           []string{"/var/log/service/:/var/log/service/"},
		CPUShares:       int64(serviceConfig.CpuShares),
		PortBindings:    ds.bindPort(serviceConfig.Publish),
		PublishAllPorts: false,
		Privileged:      false,
	}

	if serviceConfig.Memory != 0 {
		dockerHostConfig.Memory = serviceConfig.Memory
	}

	opts := docker.CreateContainerOptions{
		Config:     &dockerConfig,
		HostConfig: &dockerHostConfig}

	var err error
	ds.container, err = ds.dockerCli().CreateAndRun(opts)

	if err != nil {
		ds.log.Errorf("Se produjo un error al arrancar el contenedor: %s", err)
		ds.setStep(STEP_FAILED)
		return
	}

	ds.log.Debugln("El contenedor arrancó exitosamente")
	ds.log.Debugf("El contenedor esta asociado al ID de Registrator %s", ds.RegistratorId())

	ds.setStep(STEP_CREATED)
}

func (ds *DockerService) Undeploy() {
	ds.log.Infoln("Iniciando el proceso de Undeploy")
	if ds.CheckState(UNDEPLOYED) {
		ds.log.Infoln("El servicio ya se habia removido (undeployed)")
		return
	}

	if ds.container == nil || ds.container.ID == "" {
		ds.log.Warnln("El servicio no esta asociado a un contenedor")
		return
	}

	err := ds.dockerCli().UndeployContainer(ds.container.ID, true, 10)
	if err != nil {
		ds.log.Warnln("No se pudo remover el contenedor", err)
		return
	}
	ds.log.Infoln("Proceso de undeploy exitoso")
	ds.setState(UNDEPLOYED)
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

func (ds *DockerService) ContainerState() string {
	return ds.container.State.String()
}

func (ds *DockerService) Loaded() bool {
	return ds.loaded
}

func (ds *DockerService) PublicPorts() map[int64]int64 {
	ports := make(map[int64]int64)
	ds.log.Debugf("Api Ports %#v", ds.container.NetworkSettings.PortMappingAPI())
	for _, val := range ds.container.NetworkSettings.PortMappingAPI() {
		ds.log.Debugf("Puerto privado [%d] Puerto publico [%d]", val.PrivatePort, val.PublicPort)
		if val.PrivatePort != 0 && val.PublicPort != 0 {
			ports[val.PrivatePort] = val.PublicPort
		}
	}

	return ports
}

func (ds *DockerService) AddressAndPort(internalPort int64) (string, error) {

	ds.log.Debugf("Api Ports %#v", ds.container.NetworkSettings.PortMappingAPI())
	for _, val := range ds.container.NetworkSettings.PortMappingAPI() {
		ds.log.Debugf("Puerto privado [%d] Puerto publico [%d]", val.PrivatePort, val.PublicPort)
		if val.PrivatePort == internalPort {
			addr := val.IP + ":" + strconv.FormatInt(val.PublicPort, 10)
			ds.log.Debugf("La dirección calculada del servicio es %s", addr)
			return addr, nil
		}
	}

	return "", errors.New(fmt.Sprintf("Puerto %s desconocido", internalPort))
}

func (ds *DockerService) RunSmokeTest(monitor monitor.Monitor) {
	var err error
	var addr string

	// TODO check a puertos que no sean 8080
	addr, err = ds.AddressAndPort(8080)
	if err != nil {
		ds.setStep(STEP_FAILED)
		return
	}

	result := monitor.Check(ds.GetId(), addr)

	ds.log.Infof("Se terminó el Smoke Test con estado %t", result)

	if result {
		ds.setStep(STEP_SMOKE_READY)
	} else {
		ds.setStep(STEP_FAILED)
	}
}

func (ds *DockerService) RunWarmUp(monitor monitor.Monitor) {
	if !monitor.Configured() {
		ds.log.Infoln("El servicio no tiene configurado Warm UP. Se saltará esta validación")
		ds.setStep(STEP_WARM_READY)
		return
	}

	var err error
	var addr string

	// TODO check a puertos que no sean 8080
	addr, err = ds.AddressAndPort(8080)
	if err != nil {
		ds.setStep(STEP_FAILED)
		return
	}

	result := monitor.Check(ds.GetId(), addr)

	ds.log.Infof("Se terminó el Warm UP con estado %t", result)

	if result {
		ds.setStep(STEP_WARM_READY)
	} else {
		ds.setStep(STEP_FAILED)
	}
}
