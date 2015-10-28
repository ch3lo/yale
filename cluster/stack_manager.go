package cluster

import (
	"github.com/Pallinder/go-randomdata"
	"github.com/ch3lo/yale/helper"
	"github.com/ch3lo/yale/service"
	"github.com/ch3lo/yale/util"
)

type StackManager struct {
	stacks            map[string]*Stack
	stackNotification chan StackStatus
}

func NewStackManager() *StackManager {
	sm := new(StackManager)
	sm.stacks = make(map[string]*Stack)
	sm.stackNotification = make(chan StackStatus, 100)

	return sm
}

func (sm *StackManager) existStack(stackKey string) bool {
	for stack := range sm.stacks {
		if stack == stackKey {
			return true
		}
	}
	return false
}

func (sm *StackManager) AppendStack(dh *helper.DockerHelper) {
	for {
		key := randomdata.Country(randomdata.TwoCharCountry)
		if !sm.existStack(key) {
			util.Log.Infof("API configured and mapped to %s", key)
			sm.stacks[key] = NewStack(key, sm.stackNotification, dh)
			break
		}
	}
}

func (sm *StackManager) Deploy(serviceConfig service.ServiceConfig, replace bool, instances int, tolerance float64) bool {

	if replace {
		sm.replaceContainers(serviceConfig.ImageName)
	}

	for stackKey, _ := range sm.stacks {
		util.Log.Debugf("Creating Stack with ID: %s", stackKey)
		go sm.stacks[stackKey].DeployInstances(serviceConfig, instances, tolerance)
	}

	for i := 0; i < len(sm.stacks); i++ {
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

	for key, _ := range sm.stacks {
		containers = append(containers, sm.stacks[key].ServicesWithStatus(service.READY)...)
	}

	return containers
}

func (sm *StackManager) SearchContainers(imageNameFilter string, containerNameFilter string) (map[string][]*service.DockerService, error) {
	filter := helper.NewContainerFilter()
	//filter.Status = []string{"running"}
	filter.ImageRegexp = imageNameFilter
	filter.NameRegexp = containerNameFilter

	containers := make(map[string][]*service.DockerService)

	for stackKey, _ := range sm.stacks {
		if err := sm.stacks[stackKey].LoadContainers(imageNameFilter, containerNameFilter); err != nil {
			return nil, err
		}
		containers[stackKey] = append(containers[stackKey], sm.stacks[stackKey].services...)
	}

	return containers, nil
}

func (sm *StackManager) replaceContainers(imageName string) error {
	util.Log.Debugf("Init Replace %s", imageName)

	for stackKey, _ := range sm.stacks {
		if err := sm.stacks[stackKey].LoadContainers(imageName, ".*"); err != nil {
			return err
		}
	}

	sm.UndeployAll()

	return nil
}

func (pm *StackManager) UndeployAll() {
	for stack, _ := range pm.stacks {
		pm.stacks[stack].UndeployAll()
	}
}
