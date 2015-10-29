package cluster

import (
	"fmt"

	"github.com/Pallinder/go-randomdata"
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
	monitor               monitor.Monitor
}

func NewStack(stackKey string, stackNofitication chan<- StackStatus, dh *helper.DockerHelper) *Stack {
	sm := new(Stack)
	sm.id = stackKey + "_" + randomdata.Country(randomdata.TwoCharCountry)
	sm.stackNofitication = stackNofitication
	sm.dockerApiHelper = dh

	sm.serviceIdNotification = make(chan string)

	return sm
}

func createMonitor(healthyPath string, healthyRetries int) monitor.Monitor {
	var mon monitor.Monitor

	util.Log.Infoln("Creating monitor")
	mon = new(monitor.HttpMonitor)
	mon.SetExpect(".*")
	mon.SetEndpoint(healthyPath)
	mon.SetRetries(healthyRetries)

	return mon
}

func (sm *Stack) DeployCheckAndNotify(serviceConfig service.ServiceConfig, instances int, tolerance float64) {
	sm.monitor = createMonitor(serviceConfig.Healthy, serviceConfig.HealthyRetries)

	for i := 1; i <= instances; i++ {
		util.Log.Debugf("Deploying instance number %d in stack %s", i, sm.id)
		sm.deployOneInstance(serviceConfig)
	}

	if sm.checkInstances(serviceConfig, instances, tolerance) {
		sm.SetStatus(STACK_READY)
		return
	}

	sm.SetStatus(STACK_FAILED)
}

func (sm *Stack) SetStatus(status StackStatus) {
	sm.stackNofitication <- status
}

func (sm *Stack) addNewService(dockerService *service.DockerService) {
	sm.services = append(sm.services, dockerService)
}

func (sm *Stack) deployOneInstance(serviceConfig service.ServiceConfig) {
	dockerService := service.NewDockerService(sm.id, sm.dockerApiHelper, sm.serviceIdNotification)
	sm.addNewService(dockerService)

	util.PrintfAndLogInfof("Deploying Service with ID %s in background", dockerService.GetId())
	go dockerService.Run(serviceConfig)
}

func (sm *Stack) undeployInstance(serviceId string) {
	dockerService := sm.getService(serviceId)
	util.PrintfAndLogInfof("Undeploying Service %s", serviceId)
	dockerService.Undeploy()
}

func (sm *Stack) Rollback() {
	for _, srv := range sm.services {
		if srv.Status != service.LOADED {
			sm.undeployInstance(srv.GetId())
		}
	}
}

func (sm *Stack) UndeployInstances(total int) {
	undeployed := 0
	for _, srv := range sm.services {
		if undeployed == total {
			return
		}
		sm.undeployInstance(srv.GetId())
		undeployed++
	}
}

func (sm *Stack) getService(serviceId string) *service.DockerService {
	for key, _ := range sm.services {
		if sm.services[key].GetId() == serviceId {
			return sm.services[key]
		}
	}

	return nil
}

func (sm *Stack) ServicesWithStatus(status service.Status) []*service.DockerService {
	var services []*service.DockerService
	for k, v := range sm.services {
		if v.Status == status {
			services = append(services, sm.services[k])
		}
	}
	return services
}

func (sm *Stack) countServicesWithStatus(status service.Status) int {
	return len(sm.ServicesWithStatus(status))
}

func (sm *Stack) checkInstances(serviceConfig service.ServiceConfig, totalInstances int, tolerance float64) bool {
	for {
		util.Log.Infoln("Waiting for signal")
		serviceId := <-sm.serviceIdNotification
		util.Log.Infoln("Signal received from", serviceId)

		dockerService := sm.getService(serviceId) // que pasa si dockerService es nil?
		util.PrintfAndLogInfof("Service %s with status %s", serviceId, dockerService.Status)

		okInstances := sm.countServicesWithStatus(service.READY)

		if dockerService.Status == service.CREATED {
			util.Log.Debugf("Service %s created, checking healthy", dockerService.GetId())
			go sm.checkHealthy(dockerService)
		} else if dockerService.Status == service.READY {
			util.Log.Debugf("Service %s ready", dockerService.GetId())
		} else if dockerService.Status == service.FAILED {
			util.Log.Debugf("Service %s failed", dockerService.GetId())
			sm.undeployInstance(dockerService.GetId())

			failedInstances := sm.countServicesWithStatus(service.FAILED)

			maxFailedServices := float64(totalInstances) * tolerance
			util.Log.Debugf("Failed Services %f", float64(failedInstances))
			util.Log.Debugf("Fail Tolerance %f", maxFailedServices)
			if float64(failedInstances) < maxFailedServices {
				util.Log.Debugf("Accepted Tolerance")
				sm.deployOneInstance(serviceConfig)
			} else {
				fmt.Printf("Services resume: success %d - failed %d - total %d (tolerance %f)\n", okInstances, failedInstances, totalInstances, tolerance)
				return false
			}
		}

		util.PrintfAndLogInfof("Services resume %d/%d", okInstances, totalInstances)
		if okInstances == totalInstances {
			return true
		}
	}

	okInstances := sm.countServicesWithStatus(service.READY)
	failedInstances := sm.countServicesWithStatus(service.FAILED)

	fmt.Printf("Services resume: success %d - failed %d - total %d (tolerance %f)\n", okInstances, failedInstances, totalInstances, tolerance)
	return false
}

func (sm *Stack) checkHealthy(ds *service.DockerService) {
	var err error
	var addr string

	// TODO check a puertos que no sean 8080
	addr, err = ds.AddressAndPort(8080)
	if err != nil {
		ds.SetStatus(service.FAILED)
		return
	}

	result := sm.monitor.Check(addr)

	util.Log.Infof("Service %s, Healthy Check status %t", ds.GetId(), result)

	if result {
		ds.SetStatus(service.READY)
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
