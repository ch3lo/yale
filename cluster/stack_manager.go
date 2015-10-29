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

func (sm *StackManager) Deploy(serviceConfig service.ServiceConfig, instances int, tolerance float64) bool {
	sm.loadContainers(serviceConfig.ImageName+":"+serviceConfig.Version(), ".*")

	for stackKey, _ := range sm.stacks {
		currentContainers := sm.stacks[stackKey].countServicesWithStatus(service.LOADED)

		if currentContainers == instances {
			util.PrintfAndLogInfof("Stack %s was deployed", stackKey)
			sm.stacks[stackKey].SetStatus(STACK_READY)
		} else if currentContainers < instances {
			diff := instances - currentContainers
			util.PrintfAndLogInfof("Stack %s has %d from %d containers ", stackKey, currentContainers, instances)
			go sm.stacks[stackKey].DeployCheckAndNotify(serviceConfig, diff, tolerance)
		} else {
			diff := currentContainers - instances
			util.PrintfAndLogInfof("Stack %s has more containers than needed (%d from %d), undeploying...", stackKey, currentContainers, instances)
			sm.stacks[stackKey].UndeployInstances(diff)
			sm.stacks[stackKey].SetStatus(STACK_READY)
		}
	}

	for i := 0; i < len(sm.stacks); i++ {
		stackStatus := <-sm.stackNotification
		util.Log.Infoln("POD STATUS RECEIVED", stackStatus)
		if stackStatus == STACK_FAILED {
			sm.Rollback()
			return false
		}
	}
	return true
}

func (sm *StackManager) DeployedContainers() []*service.DockerService {
	var containers []*service.DockerService

	for stackKey, _ := range sm.stacks {
		containers = append(containers, sm.stacks[stackKey].ServicesWithStatus(service.READY)...)
	}

	return containers
}

func (sm *StackManager) SearchContainers(imageNameFilter string, containerNameFilter string) (map[string][]*service.DockerService, error) {
	if err := sm.loadContainers(imageNameFilter, containerNameFilter); err != nil {
		return nil, err
	}

	containers := make(map[string][]*service.DockerService)
	for stackKey, _ := range sm.stacks {
		containers[stackKey] = append(containers[stackKey], sm.stacks[stackKey].services...)
	}

	return containers, nil
}

/*
func (sm *StackManager) replaceContainers(imageNameFilter string) error {
	util.Log.Debugf("Init Replace %s", imageNameFilter)

	if err := sm.loadContainers(imageNameFilter, ".*"); err != nil {
		return err
	}
	sm.UndeployAll()

	return nil
}*/

func (sm *StackManager) loadContainers(imageNameFilter string, containerNameFilter string) error {
	util.Log.Debugf("Loading containers with image name: %s", imageNameFilter)

	for stackKey, _ := range sm.stacks {
		if err := sm.stacks[stackKey].LoadContainers(imageNameFilter, containerNameFilter); err != nil {
			return err
		}
	}

	return nil
}

func (sm *StackManager) Rollback() {
	for stack, _ := range sm.stacks {
		sm.stacks[stack].Rollback()
	}
}
