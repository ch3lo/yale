package service

import (
	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/ch3lo/yale/monitor"
	"github.com/ch3lo/yale/scheduler"
	"github.com/ch3lo/yale/util"
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
	"STEP_WARM_READY",
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

type DockerService struct {
	id               string
	loaded           bool
	state            State
	step             Step
	statusChannel    chan<- string
	clusterScheduler scheduler.Scheduler
	serviceInfo      *scheduler.ServiceInformation
	log              *log.Entry
}

func NewDockerService(id string, clusterScheduler scheduler.Scheduler, sc chan<- string) *DockerService {
	ds := new(DockerService)
	ds.id = id
	ds.clusterScheduler = clusterScheduler
	ds.loaded = false
	ds.statusChannel = sc

	ds.log = util.Log.WithFields(log.Fields{
		"ds": ds.id,
	})

	ds.log.Infof("Se configuró el Servicio de Docker")

	return ds
}

func NewFromContainer(id string, scheduler scheduler.Scheduler, info *scheduler.ServiceInformation, sc chan<- string) *DockerService {
	ds := NewDockerService(id, scheduler, sc)
	ds.serviceInfo = info
	ds.loaded = true

	ds.log.Infof("Se configuró desde el contenedor %s", ds.serviceInfo.ID)
	return ds
}

func (ds *DockerService) GetId() string {
	return ds.id
}

func (ds *DockerService) ServiceInformation() *scheduler.ServiceInformation {
	return ds.serviceInfo
}

func (ds *DockerService) RegistratorId() string {
	return ds.ContainerSwarmNode() + ":" + ds.ContainerName() + ":8080"
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
	if state == RUNNING {
		return ds.serviceInfo.Status == scheduler.ServiceUp
	}

	if state == UNDEPLOYED && ds.state == UNDEPLOYED {
		return true
	}

	return false
}

func (ds *DockerService) Run(serviceConfig scheduler.ServiceConfig) {
	ds.log.Infoln("Iniciando el despliegue del servicio")

	var err error
	ds.serviceInfo, err = ds.clusterScheduler.CreateAndRun(serviceConfig)

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

	if ds.serviceInfo == nil || ds.serviceInfo.ID == "" {
		ds.log.Warnln("El servicio no esta asociado a un contenedor")
		return
	}

	err := ds.clusterScheduler.UndeployContainer(ds.serviceInfo.ID, true, 10)
	if err != nil {
		ds.log.Warnln("No se pudo remover el contenedor", err)
		return
	}
	ds.log.Infoln("Proceso de undeploy exitoso")
	ds.setState(UNDEPLOYED)
}

func (ds *DockerService) ContainerName() string {
	return ds.serviceInfo.ContainerName
}

func (ds *DockerService) ContainerImageName() string {
	return ds.serviceInfo.ImageName + ":" + ds.serviceInfo.ImageTag
}

func (ds *DockerService) ContainerSwarmNode() string {
	return ds.serviceInfo.Host
}

func (ds *DockerService) ContainerState() string {
	return ds.serviceInfo.Status.String()
}

func (ds *DockerService) Loaded() bool {
	return ds.loaded
}

func (ds *DockerService) AddressAndPort(internalPort int64) (string, error) {
	for _, port := range ds.serviceInfo.Ports {
		if internalPort == port.Internal {
			for _, pub := range port.Publics {
				if pub != 0 {
					return fmt.Sprintf("%s:%d", port.Advertise, pub), nil
				}
			}
			return "", fmt.Errorf("El puerto %d, no tiene un puerto publico", internalPort)
		}
	}

	return "", fmt.Errorf("Puerto %s desconocido", internalPort)
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
