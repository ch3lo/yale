package cluster

import (
	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/ch3lo/yale/helper"
	"github.com/ch3lo/yale/monitor"
	"github.com/ch3lo/yale/service"
	"github.com/ch3lo/yale/util"
)

type StackStatus int

const (
	STACK_READY StackStatus = 1 + iota
	STACK_FAILED
)

var stackStatus = [...]string{
	"STACK_READY",
	"STACK_FAILED",
}

func (s StackStatus) String() string {
	return stackStatus[s-1]
}

type Stack struct {
	id                    string
	dockerApiHelper       *helper.DockerHelper
	services              []*service.DockerService // refactorizar a interfaz service
	serviceIdNotification chan string
	stackNofitication     chan<- StackStatus
	smokeTestMonitor      monitor.Monitor
	warmUpMonitor         monitor.Monitor
	log                   *log.Entry
}

func NewStack(stackKey string, stackNofitication chan<- StackStatus, dh *helper.DockerHelper) *Stack {
	s := new(Stack)
	s.id = stackKey
	s.stackNofitication = stackNofitication
	s.dockerApiHelper = dh
	s.serviceIdNotification = make(chan string, 100)

	s.log = util.Log.WithFields(log.Fields{
		"stack": stackKey,
	})

	return s
}

func (s *Stack) createMonitor(config monitor.MonitorConfig) monitor.Monitor {
	var mon monitor.Monitor

	s.log.Infof("Creating monitor with: mode=%s and ping=%s", config.Type, config.Ping)
	if config.Type == monitor.TCP {
		mon = new(monitor.TcpMonitor)
	} else {
		mon = new(monitor.HttpMonitor)
	}

	mon.SetRetries(config.Retries)
	mon.SetEndpoint(config.Ping)
	mon.SetExpect(config.Pong)

	return mon
}

func (s *Stack) DeployCheckAndNotify(serviceConfig service.ServiceConfig, smokeConfig monitor.MonitorConfig, warmConfig monitor.MonitorConfig, instances int, tolerance float64) {
	s.smokeTestMonitor = s.createMonitor(smokeConfig)
	s.warmUpMonitor = s.createMonitor(warmConfig)

	for i := 1; i <= instances; i++ {
		s.log.Debugf("Deploying instance number %d", i)
		s.deployOneInstance(serviceConfig)
	}

	if s.checkInstances(serviceConfig, instances, tolerance) {
		s.SetStatus(STACK_READY)
		return
	}

	s.SetStatus(STACK_FAILED)
}

func (s *Stack) SetStatus(status StackStatus) {
	s.stackNofitication <- status
}

func (s *Stack) addNewService(dockerService *service.DockerService) {
	s.services = append(s.services, dockerService)
}

func (s *Stack) deployOneInstance(serviceConfig service.ServiceConfig) {
	dockerService := service.NewDockerService(s.id, s.dockerApiHelper, s.serviceIdNotification)
	s.addNewService(dockerService)

	s.log.Infof("Deploying Service with ID %s in background", dockerService.GetId())
	fmt.Printf("Deploying Service with ID %s in background", dockerService.GetId())
	go dockerService.Run(serviceConfig)
}

func (s *Stack) undeployInstance(serviceId string) {
	dockerService := s.getService(serviceId)
	s.log.Infof("Undeploying Service %s", serviceId)
	fmt.Printf("Undeploying Service %s", serviceId)
	dockerService.Undeploy()
}

func (s *Stack) Rollback() {
	for _, srv := range s.services {
		if srv.Status != service.LOADED {
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

func (s *Stack) getService(serviceId string) *service.DockerService {
	for key, _ := range s.services {
		if s.services[key].GetId() == serviceId {
			return s.services[key]
		}
	}

	return nil
}

func (s *Stack) ServicesWithStatus(status service.Status) []*service.DockerService {
	var services []*service.DockerService
	for k, v := range s.services {
		if v.Status == status {
			services = append(services, s.services[k])
		}
	}
	return services
}

func (s *Stack) countServicesWithStatus(status service.Status) int {
	return len(s.ServicesWithStatus(status))
}

func (s *Stack) checkInstances(serviceConfig service.ServiceConfig, totalInstances int, tolerance float64) bool {
	for {
		s.log.Infoln("Waiting for signal")
		serviceId := <-s.serviceIdNotification
		s.log.Infoln("Signal received from", serviceId)

		dockerService := s.getService(serviceId) // que pasa si dockerService es nil?
		s.log.Infof("Service %s with status %s", serviceId, dockerService.Status)
		fmt.Printf("Service %s with status %s", serviceId, dockerService.Status)

		okInstances := s.countServicesWithStatus(service.READY)

		if dockerService.Status == service.CREATED {
			s.log.Debugf("Service %s created, checking healthy", dockerService.GetId())
			go s.smokeTest(dockerService)
		} else if dockerService.Status == service.SMOKE_READY {
			s.log.Debugf("Service %s smoke test ready", dockerService.GetId())
			go s.warmUp(dockerService)
		} else if dockerService.Status == service.WARM_READY {
			s.log.Debugf("Service %s warm up ready", dockerService.GetId())
			dockerService.SetStatus(service.READY)
		} else if dockerService.Status == service.READY {
			s.log.Debugf("Service %s ready", dockerService.GetId())
		} else if dockerService.Status == service.FAILED {
			s.log.Debugf("Service %s failed", dockerService.GetId())
			s.undeployInstance(dockerService.GetId())

			failedInstances := s.countServicesWithStatus(service.FAILED)

			maxFailedServices := float64(totalInstances) * tolerance
			s.log.Debugf("Failed Services %f", float64(failedInstances))
			s.log.Debugf("Fail Tolerance %f", maxFailedServices)
			if float64(failedInstances) < maxFailedServices {
				s.log.Debugf("Accepted Tolerance")
				s.deployOneInstance(serviceConfig)
			} else {
				fmt.Printf("Services resume: success %d - failed %d - total %d (tolerance %f)\n", okInstances, failedInstances, totalInstances, tolerance)
				return false
			}
		}

		s.log.Infof("Services resume %d/%d", okInstances, totalInstances)
		fmt.Printf("Services resume %d/%d", okInstances, totalInstances)
		if okInstances == totalInstances {
			return true
		}
	}

	okInstances := s.countServicesWithStatus(service.READY)
	failedInstances := s.countServicesWithStatus(service.FAILED)

	fmt.Printf("Services resume: success %d - failed %d - total %d (tolerance %f)\n", okInstances, failedInstances, totalInstances, tolerance)
	return false
}

func (s *Stack) smokeTest(ds *service.DockerService) {
	var err error
	var addr string

	// TODO check a puertos que no sean 8080
	addr, err = ds.AddressAndPort(8080)
	if err != nil {
		ds.SetStatus(service.FAILED)
		return
	}

	result := s.smokeTestMonitor.Check(addr)

	s.log.Infof("Service %s, Smoke Test status %t", ds.GetId(), result)

	if result {
		ds.SetStatus(service.SMOKE_READY)
	} else {
		ds.SetStatus(service.FAILED)
	}
}

func (s *Stack) warmUp(ds *service.DockerService) {
	if !s.warmUpMonitor.Configured() {
		s.log.Infof("Service %s, doesn't have Warm Up. Skiping", ds.GetId())
		fmt.Printf("Service %s, doesn't have Warm Up. Skiping", ds.GetId())
		ds.SetStatus(service.WARM_READY)
		return
	}

	var err error
	var addr string

	// TODO check a puertos que no sean 8080
	addr, err = ds.AddressAndPort(8080)
	if err != nil {
		ds.SetStatus(service.FAILED)
		return
	}

	result := s.warmUpMonitor.Check(addr)

	s.log.Infof("Service %s, Warm Up status %t", ds.GetId(), result)

	if result {
		ds.SetStatus(service.WARM_READY)
	} else {
		ds.SetStatus(service.FAILED)
	}
}

func (s *Stack) LoadContainers(imageNameFilter string, containerNameFilter string) error {
	filter := helper.NewContainerFilter()
	//filter.Status = []string{"running"}
	filter.ImageRegexp = imageNameFilter
	filter.NameRegexp = containerNameFilter

	containers, err := s.dockerApiHelper.ListContainers(filter)
	if err != nil {
		return err
	}

	for k := range containers {
		c, err := s.dockerApiHelper.ContainerInspect(containers[k].ID)
		if err != nil {
			return err
		}
		s.services = append(s.services, service.NewFromContainer(s.id, s.dockerApiHelper, c, s.serviceIdNotification))
	}

	return nil
}
