package cluster

import (
	"github.com/ch3lo/yale/helper"
	"github.com/ch3lo/yale/monitor"
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
	i := 0
	for {
		key := util.Letter(i)
		if !sm.existStack(key) {
			util.Log.Infof("API configured and mapped to %s", key)
			sm.stacks[key] = NewStack(key, sm.stackNotification, dh)
			break
		}
		i++
	}
}

func (sm *StackManager) Deploy(serviceConfig service.ServiceConfig, smokeConfig monitor.MonitorConfig, warmConfig monitor.MonitorConfig, instances int, tolerance float64) bool {
	for stackKey, _ := range sm.stacks {
		if err := sm.stacks[stackKey].LoadFilteredContainers(serviceConfig.ImageName, serviceConfig.Tag, ".*"); err != nil {
			return false
		}
	}

	for stackKey, _ := range sm.stacks {
		go sm.stacks[stackKey].DeployCheckAndNotify(serviceConfig, smokeConfig, warmConfig, instances, tolerance)
	}

	for i := 0; i < len(sm.stacks); i++ {
		stackStatus := <-sm.stackNotification
		util.Log.Infoln("Stack notification received with status", stackStatus)
		if stackStatus == STACK_FAILED {
			util.Log.Errorln("Fallo el stack, se procederá a realizar Rollback")
			sm.Rollback()
			return false
		}
	}
	return true
}

func (sm *StackManager) DeployedContainers() []*service.DockerService {
	var containers []*service.DockerService

	for stackKey, _ := range sm.stacks {
		containers = append(containers, sm.stacks[stackKey].ServicesWithStep(service.STEP_WARM_READY)...)
	}

	return containers
}

func (sm *StackManager) SearchContainers(imageNameFilter string, tagFilter string, containerNameFilter string) (map[string][]*service.DockerService, error) {
	for stackKey, _ := range sm.stacks {
		if err := sm.stacks[stackKey].LoadFilteredContainers(imageNameFilter, tagFilter, containerNameFilter); err != nil {
			return nil, err
		}
	}

	containers := make(map[string][]*service.DockerService)
	for stackKey, _ := range sm.stacks {
		containers[stackKey] = append(containers[stackKey], sm.stacks[stackKey].services...)
	}

	return containers, nil
}

func (sm *StackManager) Tagged(image string, tag string) (map[string][]*service.DockerService, error) {
	for stackKey, _ := range sm.stacks {
		if err := sm.stacks[stackKey].LoadTaggedContainers(image, tag); err != nil {
			return nil, err
		}
	}

	containers := make(map[string][]*service.DockerService)
	for stackKey, _ := range sm.stacks {
		containers[stackKey] = append(containers[stackKey], sm.stacks[stackKey].services...)
	}

	return containers, nil
}

func (sm *StackManager) Rollback() {
	util.Log.Infoln("Starting Rollback")
	for stack, _ := range sm.stacks {
		sm.stacks[stack].Rollback()
	}
}
