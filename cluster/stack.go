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
	serviceConfig         service.ServiceConfig
	dockerApiHelper       *helper.DockerHelper
	services              []*service.DockerService // refactorizar a interfaz service
	serviceIdNotification chan string
	stackNofitication     chan<- StackStatus
	monitor               monitor.Monitor
	TotalInstances        int
	Tolerance             float64
}

func NewStack(stackKey string, serviceConfig service.ServiceConfig, stackNofitication chan<- StackStatus, dh *helper.DockerHelper) *Stack {
	sm := new(Stack)
	sm.id = stackKey + "_" + randomdata.Country(randomdata.TwoCharCountry)
	sm.serviceConfig = serviceConfig
	sm.stackNofitication = stackNofitication
	sm.dockerApiHelper = dh

	sm.serviceIdNotification = make(chan string)
	sm.TotalInstances = 1
	sm.Tolerance = 0.5
	sm.monitor = createMonitor(serviceConfig.Healthy, serviceConfig.HealthyRetries)

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

func (sm *Stack) DeployInstances() {

	for i := 0; i < sm.TotalInstances; i++ {
		util.Log.Debugf("Deploying instance number %d in stack %s", i, sm.id)
		sm.deployOneInstance()
	}

	if !sm.checkInstances() {
		sm.stackNofitication <- STACK_FAILED
		return
	}

	sm.stackNofitication <- STACK_READY
}

func (sm *Stack) addNewService(dockerService *service.DockerService) {
	sm.services = append(sm.services, dockerService)
}

func (sm *Stack) deployOneInstance() {
	dockerService := service.NewDockerService(sm.id, sm.dockerApiHelper, sm.serviceConfig, sm.serviceIdNotification)
	sm.addNewService(dockerService)

	util.PrintfAndLogInfof("Deploying Service with ID %s in background", dockerService.GetId())
	go dockerService.Run()
}

func (sm *Stack) undeployInstance(serviceId string) {
	dockerService := sm.getService(serviceId)
	util.PrintfAndLogInfof("Undeploying Service %s", serviceId)
	dockerService.Undeploy()
}
func (sm *Stack) UndeployAll() {
	for _, srv := range sm.services {
		sm.undeployInstance(srv.GetId())
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

func (sm *Stack) checkInstances() bool {
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

			maxFailedServices := float64(sm.TotalInstances) * sm.Tolerance
			util.Log.Debugf("Failed Services %f", float64(failedInstances))
			util.Log.Debugf("Fail Tolerance %f", maxFailedServices)
			if float64(failedInstances) < maxFailedServices {
				util.Log.Debugf("Accepted Tolerance")
				sm.deployOneInstance()
			} else {
				fmt.Printf("Services resume: success %d - failed %d - total %d (tolerance %f)\n", okInstances, failedInstances, sm.TotalInstances, sm.Tolerance)
				return false
			}
		}

		util.PrintfAndLogInfof("Services resume %d/%d", okInstances, sm.TotalInstances)
		if okInstances == sm.TotalInstances {
			return true
		}
	}

	okInstances := sm.countServicesWithStatus(service.READY)
	failedInstances := sm.countServicesWithStatus(service.FAILED)

	fmt.Printf("Services resume: success %d - failed %d - total %d (tolerance %f)\n", okInstances, failedInstances, sm.TotalInstances, sm.Tolerance)
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