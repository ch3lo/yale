package cluster

import (
	"fmt"

	"github.com/Pallinder/go-randomdata"
	log "github.com/Sirupsen/logrus"
	"github.com/ch3lo/yale/configuration"
	"github.com/ch3lo/yale/monitor"
	"github.com/ch3lo/yale/scheduler"
	"github.com/ch3lo/yale/scheduler/factory"
	"github.com/ch3lo/yale/service"
	"github.com/ch3lo/yale/util"
)

type StackStatus int

const (
	StackReady StackStatus = 1 + iota
	StackFailed
)

var stackStatus = [...]string{
	"StackReady",
	"StackFailed",
}

func (s StackStatus) String() string {
	return stackStatus[s-1]
}

type Stack struct {
	id                    string
	clusterScheduler      scheduler.Scheduler
	services              []*service.DockerService
	serviceIdNotification chan string
	stackNofitication     chan<- StackStatus
	smokeTestMonitor      monitor.Monitor
	warmUpMonitor         monitor.Monitor
	log                   *log.Entry
}

func NewStack(stackKey string, stackNofitication chan<- StackStatus, config configuration.Cluster) (*Stack, error) {
	if config.Disabled {
		return nil, &ClusterDisabled{Name: stackKey}
	}

	clusterScheduler, err := factory.Create(config.Scheduler.Type(), config.Scheduler.Parameters())
	if err != nil {
		return nil, fmt.Errorf("Error al crear el scheduler %s en %s. %s", config.Scheduler.Type(), stackKey, err.Error())
	}

	s := new(Stack)
	s.id = stackKey
	s.stackNofitication = stackNofitication
	s.clusterScheduler = clusterScheduler
	s.serviceIdNotification = make(chan string, 1000)

	util.Log.WithFields(log.Fields{
		"stack": stackKey,
	}).Infof("Se creo un nuevo scheduler %s", config.Scheduler.Type())

	return s, nil
}

func (s *Stack) createID() string {
	for {
		key := s.id + "_" + randomdata.Adjective()
		exist := false

		for _, srv := range s.services {
			if srv.GetId() == key {
				exist = true
			}
		}

		if !exist {
			return key
		}
	}
}

func (s *Stack) createMonitor(config monitor.MonitorConfig) monitor.Monitor {
	var mon monitor.Monitor

	s.log.Infof("Creando monitor con mode [%s] y request [%s]", config.Type, config.Request)
	if config.Type == monitor.TCP {
		mon = new(monitor.TcpMonitor)
	} else {
		mon = new(monitor.HttpMonitor)
	}

	mon.SetRetries(config.Retries)
	mon.SetRequest(config.Request)
	mon.SetExpected(config.Expected)

	return mon
}

func (s *Stack) DeployCheckAndNotify(serviceConfig scheduler.ServiceConfig, smokeConfig monitor.MonitorConfig, warmConfig monitor.MonitorConfig, instances int, tolerance float64) {
	currentContainers := s.countServicesWithState(service.RUNNING)

	if currentContainers == instances {
		s.log.Infoln("El Stack ya estaba desplegado. Omitiendo...")
		s.setStatus(StackReady)
	} else if currentContainers < instances {
		diff := instances - currentContainers
		s.log.Printf("El Stack tenia %d instancias. Se desplegaran %d instancias más.", currentContainers, diff)
		s.smokeTestMonitor = s.createMonitor(smokeConfig)
		s.warmUpMonitor = s.createMonitor(warmConfig)

		for i := 1; i <= diff; i++ {
			s.log.Debugf("Desplegando instancia número %d", i)
			s.deployOneInstance(serviceConfig)
		}

		if s.checkInstances(serviceConfig, diff, tolerance) {
			s.setStatus(StackReady)
			return
		}

		s.setStatus(StackFailed)
	} else {
		diff := currentContainers - instances
		s.log.Printf("El Stack tenia más instancias de las necesarias (%d from %d). Comenzando el undeploy...", currentContainers, instances)
		s.UndeployInstances(diff)
		s.setStatus(StackReady)
	}
}

func (s *Stack) setStatus(status StackStatus) {
	s.stackNofitication <- status
}

func (s *Stack) addNewService(dockerService *service.DockerService) {
	s.services = append(s.services, dockerService)
}

func (s *Stack) deployOneInstance(serviceConfig scheduler.ServiceConfig) {
	dockerService := service.NewDockerService(s.createID(), s.clusterScheduler, s.serviceIdNotification)
	s.addNewService(dockerService)
	dockerService.Run(serviceConfig)
}

func (s *Stack) undeployInstance(serviceID string) {
	dockerService := s.getService(serviceID)
	dockerService.Undeploy()
}

func (s *Stack) Rollback() {
	s.log.Infof("Comenzando Rollback en el Stack")
	for _, srv := range s.services {
		if !srv.Loaded() {
			s.undeployInstance(srv.GetId())
		}
	}
}

func (s *Stack) UndeployInstances(total int) {
	undeployed := 0
	for _, srv := range s.services {
		if undeployed == total {
			return
		}
		s.undeployInstance(srv.GetId())
		undeployed++
	}
}

func (s *Stack) getService(serviceID string) *service.DockerService {
	for key := range s.services {
		if s.services[key].GetId() == serviceID {
			return s.services[key]
		}
	}

	return nil
}

func (s *Stack) ServicesWithStep(step service.Step) []*service.DockerService {
	var services []*service.DockerService
	for k, v := range s.services {
		if v.GetStep() == step {
			services = append(services, s.services[k])
		}
	}
	return services
}

func (s *Stack) ServicesWithState(state service.State) []*service.DockerService {
	var services []*service.DockerService
	for k, v := range s.services {
		if v.CheckState(state) {
			services = append(services, s.services[k])
		}
	}
	return services
}

func (s *Stack) countServicesWithStep(step service.Step) int {
	return len(s.ServicesWithStep(step))
}

func (s *Stack) countServicesWithState(state service.State) int {
	return len(s.ServicesWithState(state))
}

func (s *Stack) checkInstances(serviceConfig scheduler.ServiceConfig, totalInstances int, tolerance float64) bool {
	for {
		s.log.Infoln("Esperando notificación de los servicios")
		serviceID := <-s.serviceIdNotification
		s.log.Infoln("Notificación recibida del Servicio", serviceID)

		dockerService := s.getService(serviceID) // que pasa si dockerService es nil?
		s.log.Infof("Notificación del Servicio %s tiene estado %s", serviceID, dockerService.GetStep())

		okInstances := s.countServicesWithStep(service.STEP_WARM_READY)

		if dockerService.GetStep() == service.STEP_CREATED {
			s.log.Debugf("Notificación de Servicio %s creado... Iniciando Healthy Check", dockerService.GetId())
			go dockerService.RunSmokeTest(s.smokeTestMonitor)
		} else if dockerService.GetStep() == service.STEP_SMOKE_READY {
			s.log.Debugf("Notificación de Servicio %s con Smoke Test exitoso... Iniciando Warm UP", dockerService.GetId())
			go dockerService.RunWarmUp(s.warmUpMonitor)
		} else if dockerService.GetStep() == service.STEP_WARM_READY {
			s.log.Debugf("Notificación de Servicio %s Listo", dockerService.GetId())
		} else if dockerService.GetStep() == service.STEP_FAILED {
			s.log.Errorf("Notificación de Servicio %s con errores", dockerService.GetId())
			s.undeployInstance(dockerService.GetId())

			failedInstances := s.countServicesWithStep(service.STEP_FAILED)

			maxFailedServices := float64(totalInstances) * tolerance
			s.log.Infof("Resumen de Servicios: OK %d - ERROR %d - TOTAL %d (TOLERANCIA %f)", okInstances, failedInstances, totalInstances, tolerance)
			s.log.Debugf("La tolerancia de fallo es %f servicios, superado este valor el deploy fallará", maxFailedServices)
			if float64(failedInstances) < maxFailedServices {
				s.log.Debugf("Tolerancia aceptada... Iniciando el despliegue de una nueva instancia")
				s.deployOneInstance(serviceConfig)
			} else {
				return false
			}
		}

		s.log.Infof("Resumen de Servicios: %d/%d", okInstances, totalInstances)
		if okInstances == totalInstances {
			return true
		}
	}

	okInstances := s.countServicesWithStep(service.STEP_WARM_READY)
	failedInstances := s.countServicesWithStep(service.STEP_FAILED)

	s.log.Infof("Resumen de Servicios: OK %d - ERROR %d - TOTAL %d (TOLERANCIA %f)", okInstances, failedInstances, totalInstances, tolerance)
	return false
}

func (s *Stack) LoadFilteredContainers(imageNameFilter string, tagFilter string, containerNameFilter string) error {
	util.Log.Debugf("Cargando contenedores con filtros: imagen %s - nombre contenedor %s", imageNameFilter, containerNameFilter)
	filter := scheduler.NewContainerFilter()
	//filter.GetStep() = []string{"running"}
	filter.ImageRegexp = imageNameFilter
	filter.TagRegexp = tagFilter
	filter.NameRegexp = containerNameFilter

	containers, err := s.clusterScheduler.ListContainers(filter)
	if err != nil {
		return err
	}

	for k := range containers {
		/*c, err := s.clusterScheduler.ContainerInspect(containers[k].ID)
		if err != nil {
			return err
		}*/
		s.services = append(s.services, service.NewFromContainer(s.createID(), s.clusterScheduler, containers[k], s.serviceIdNotification))
	}

	return nil
}

func (s *Stack) LoadTaggedContainers(imageName string, tag string) error {
	util.Log.Debugf("Cargando contenedores filtrando por TAG con filtros: imagen %s - tag %s", imageName, tag)

	containers, err := s.clusterScheduler.ListTaggedContainers(imageName, tag)
	if err != nil {
		return err
	}

	for k := range containers {
		/*c, err := s.clusterScheduler.ContainerInspect(containers[k].ID)
		if err != nil {
			return err
		}*/
		s.services = append(s.services, service.NewFromContainer(s.createID(), s.clusterScheduler, containers[k], s.serviceIdNotification))
	}

	return nil
}
