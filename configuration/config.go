package configuration

import (
	"fmt"
	"strings"
)

type Cluster struct {
	Disabled  bool      `yaml:"disabled"`
	Scheduler Scheduler `yaml:"scheduler"`
}

type Configuration struct {
	Clusters map[string]Cluster `yaml:"cluster"`
}

type Parameters map[string]interface{}

type Scheduler map[string]Parameters

func (scheduler *Scheduler) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var schedulerMap map[string]Parameters
	err := unmarshal(&schedulerMap)
	if err == nil {
		if len(schedulerMap) > 1 {
			types := make([]string, 0, len(schedulerMap))
			for k := range schedulerMap {
				types = append(types, k)
			}

			if len(types) > 1 {
				return fmt.Errorf("Se debe configurar sólo un Scheduler. Schedulers: %v", types)
			}
		}
		*scheduler = schedulerMap
		return nil
	}

	var schedulerType string
	err = unmarshal(&schedulerType)
	if err == nil {
		*scheduler = Scheduler{schedulerType: Parameters{}}
		return nil
	}

	return err
}

func (scheduler Scheduler) MarshalYAML() (interface{}, error) {
	if scheduler.Parameters() == nil {
		return scheduler.Type(), nil
	}
	return map[string]Parameters(scheduler), nil
}

func (scheduler Scheduler) Parameters() Parameters {
	return scheduler[scheduler.Type()]
}

func (scheduler Scheduler) Type() string {
	var schedulerType []string

	for k := range scheduler {
		schedulerType = append(schedulerType, k)
	}
	if len(schedulerType) > 1 {
		panic("multiple schedulers definidos: " + strings.Join(schedulerType, ", "))
	}
	if len(schedulerType) == 1 {
		return schedulerType[0]
	}
	return ""
}
