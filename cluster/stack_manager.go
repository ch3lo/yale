package cluster

import (
	"github.com/Pallinder/go-randomdata"
	"github.com/ch3lo/yale/helper"
	"github.com/ch3lo/yale/service"
	"github.com/ch3lo/yale/util"
	"github.com/fsouza/go-dockerclient"
)

type StackManager struct {
	dockerApiEndpoints map[string]*helper.DockerHelper
	services           map[string]*Stack
	stackNotification  chan StackStatus
}

func NewStackManager() *StackManager {
	sm := new(StackManager)
	sm.dockerApiEndpoints = make(map[string]*helper.DockerHelper)
	sm.services = make(map[string]*Stack)
	sm.stackNotification = make(chan StackStatus, 100)

	return sm
}

func (sm *StackManager) existStack(stackKey string) bool {
	for stack := range sm.dockerApiEndpoints {
		if stack == stackKey {
			return true
		}
	}
	return false
}

func (sm *StackManager) AddDockerApiEndpoint(dh *helper.DockerHelper) {
	if sm.dockerApiEndpoints == nil {
		sm.dockerApiEndpoints = make(map[string]*helper.DockerHelper)
	}

	for {
		key := randomdata.Country(randomdata.TwoCharCountry)
		if !sm.existStack(key) {
			util.Log.Infof("API configured and mapped to %s", key)
			sm.dockerApiEndpoints[key] = dh
			break
		}
	}
}

func (sm *StackManager) Deploy(serviceConfig service.ServiceConfig, replace bool, instances int, tolerance float64) bool {

	if replace {
		sm.replaceContainers(serviceConfig.ImageName)
	}

	for stackKey, _ := range sm.dockerApiEndpoints {
		util.Log.Debugf("Creating Stack with ID: %s", stackKey)
		stack := NewStack(stackKey, serviceConfig, sm.stackNotification, sm.dockerApiEndpoints[stackKey])
		stack.TotalInstances = instances
		stack.Tolerance = tolerance
		sm.services[stackKey] = stack
		go stack.DeployInstances()
	}

	for i := 0; i < len(sm.dockerApiEndpoints); i++ {
		stackStatus := <-sm.stackNotification
		util.Log.Infoln("POD STATUS RECEIVED", stackStatus)
		if stackStatus == STACK_FAILED {
			sm.UndeployAll()
			return false
		}
	}
	return true
}

func (sm *StackManager) DeployedContainers() []*service.DockerService {
	var containers []*service.DockerService

	for key, _ := range sm.services {
		containers = append(containers, sm.services[key].ServicesWithStatus(service.READY)...)
	}

	return containers
}

func (sm *StackManager) SearchContainers(imageNameFilter string, containerNameFilter string) (map[string][]docker.APIContainers, error) {
	filter := helper.NewContainerFilter()
	//filter.Status = []string{"running"}
	filter.ImageRegexp = imageNameFilter
	filter.NameRegexp = containerNameFilter

	containers := make(map[string][]docker.APIContainers)
	var err error

	for stackKey, _ := range sm.dockerApiEndpoints {
		var epContainers []docker.APIContainers
		epContainers, err = sm.dockerApiEndpoints[stackKey].ListContainers(filter)
		if err != nil {
			return nil, err
		}
		containers[stackKey] = append(containers[stackKey], epContainers...)
	}

	return containers, nil
}

func (sm *StackManager) replaceContainers(imageName string) error {
	imgReg := imageName
	util.Log.Debugf("Init Replace %s", imgReg)
	containerFilter := helper.NewContainerFilter()
	containerFilter.ImageRegexp = imgReg

	for stackKey, _ := range sm.dockerApiEndpoints {
		containers, err := sm.dockerApiEndpoints[stackKey].ListContainers(containerFilter)
		if err != nil {
			return err
		}

		for _, container := range containers {
			util.PrintfAndLogInfof("Stack %s - Replace in process... removing container %s", stackKey, container.Names[0])
			err := sm.dockerApiEndpoints[stackKey].UndeployContainer(container.ID, true, 10)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (pm *StackManager) UndeployAll() {
	for stack, _ := range pm.services {
		pm.services[stack].UndeployAll()
	}
}
