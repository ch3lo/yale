package cluster

import (
	"github.com/ch3lo/yale/configuration"
	"github.com/ch3lo/yale/monitor"
	"github.com/ch3lo/yale/scheduler"
	"github.com/ch3lo/yale/service"
	"github.com/ch3lo/yale/util"
)

type StackManager struct {
	stacks            map[string]*Stack
	stackNotification chan StackStatus
}

func NewStackManager(config *configuration.Configuration) *StackManager {
	sm := &StackManager{
		stacks:            make(map[string]*Stack),
		stackNotification: make(chan StackStatus, 100),
	}

	sm.setupStacks(config.Clusters)
	return sm
}

// setupClusters inicia el cluster, mapeando el cluster el id del cluster como key
func (sm *StackManager) setupStacks(config map[string]configuration.Cluster) {
	for key := range config {
		s, err := NewStack(key, sm.stackNotification, config[key])
		if err != nil {
			switch err.(type) {
			case *ClusterDisabled:
				util.Log.Warnln(err.Error())
				continue
			default:
				util.Log.Fatalln(err.Error())
			}
		}

		sm.stacks[key] = s
		util.Log.Infof("Se configuro el cluster %s", key)
	}

	if len(sm.stacks) == 0 {
		util.Log.Fatalln("Al menos debe existir un cluster")
	}
}

func (sm *StackManager) Deploy(serviceConfig scheduler.ServiceConfig, smokeConfig monitor.MonitorConfig, warmConfig monitor.MonitorConfig, instances int, tolerance float64) bool {
	for stackKey := range sm.stacks {
		if err := sm.stacks[stackKey].LoadFilteredContainers(serviceConfig.ImageName, serviceConfig.Tag, ".*"); err != nil {
			return false
		}
	}

	for stackKey := range sm.stacks {
		go sm.stacks[stackKey].DeployCheckAndNotify(serviceConfig, smokeConfig, warmConfig, instances, tolerance)
	}

	for i := 0; i < len(sm.stacks); i++ {
		stackStatus := <-sm.stackNotification
		util.Log.Infoln("Se recibió notificación del Stack con estado", stackStatus)
		if stackStatus == StackFailed {
			util.Log.Errorln("Fallo el stack, se procederá a realizar Rollback")
			sm.Rollback()
			return false
		}
	}
	util.Log.Infoln("Proceso de deploy OK")
	return true
}

func (sm *StackManager) DeployedContainers() []*service.DockerService {
	var containers []*service.DockerService

	for stackKey := range sm.stacks {
		containers = append(containers, sm.stacks[stackKey].ServicesWithStep(service.STEP_WARM_READY)...)
	}

	return containers
}

func (sm *StackManager) SearchContainers(imageNameFilter string, tagFilter string, containerNameFilter string) (map[string][]*service.DockerService, error) {
	for stackKey := range sm.stacks {
		if err := sm.stacks[stackKey].LoadFilteredContainers(imageNameFilter, tagFilter, containerNameFilter); err != nil {
			return nil, err
		}
	}

	containers := make(map[string][]*service.DockerService)
	for stackKey := range sm.stacks {
		containers[stackKey] = append(containers[stackKey], sm.stacks[stackKey].services...)
	}

	return containers, nil
}

func (sm *StackManager) Tagged(image string, tag string) (map[string][]*service.DockerService, error) {
	for stackKey := range sm.stacks {
		if err := sm.stacks[stackKey].LoadTaggedContainers(image, tag); err != nil {
			return nil, err
		}
	}

	containers := make(map[string][]*service.DockerService)
	for stackKey := range sm.stacks {
		containers[stackKey] = append(containers[stackKey], sm.stacks[stackKey].services...)
	}

	return containers, nil
}

func (sm *StackManager) Rollback() {
	util.Log.Infoln("Iniciando el Rollback")
	for stack := range sm.stacks {
		sm.stacks[stack].Rollback()
	}
}
