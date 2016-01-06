package scheduler

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ch3lo/yale/util"
)

// ServiceInformationStatus define el estado de un servicio
type ServiceInformationStatus int

const (
	// ServiceUp Estado de un servicio que está OK
	ServiceUp ServiceInformationStatus = 1 + iota
	// ServiceDown Estado de un servicio que esta caido
	ServiceDown
)

var statuses = [...]string{
	"Up",
	"Down",
}

func (s ServiceInformationStatus) String() string {
	return statuses[s-1]
}

// ServicePortType Tipo de protocolo de un puerto
type ServicePortType string

const (
	// TCP Puerto de protocolo TCP
	TCP ServicePortType = "TCP"
	// UDP Puerto de protocolo UDP
	UDP ServicePortType = "UDP"
)

// NewServicePortType retorna un ServicePortTyep basado en el string pasado como parametro
func NewServicePortType(t string) ServicePortType {
	if strings.ToUpper(t) == "UDP" {
		return UDP
	}
	return TCP
}

// ServicePort estructura que encapsula la información relacionada a un puerto de un contenedor
type ServicePort struct {
	Advertise string
	Internal  int64
	Publics   []int64
	Type      ServicePortType
}

// ServiceInformation define una estructura con la informacion basica de un servicio
// Esta estructura sirve para la comunicacion con los consumidores de schedulers
type ServiceInformation struct {
	ID            string
	ImageName     string
	ImageTag      string
	Host          string
	ContainerName string
	Status        ServiceInformationStatus
	Ports         map[string]ServicePort
}

// Healthy es una funcion que retorna si un servicio esta saludable o no
func (si ServiceInformation) Healthy() bool {
	return si.Status == ServiceUp
}

type ContainerFilter struct {
	NameRegexp  string
	Status      []string
	ImageRegexp string
	TagRegexp   string
}

func NewContainerFilter() ContainerFilter {
	return ContainerFilter{
		Status:      []string{"restarting", "running", "paused", "exited"},
		NameRegexp:  ".*",
		ImageRegexp: ".*",
		TagRegexp:   ".*",
	}
}

type ServiceConfig struct {
	ServiceID string
	CPUShares int
	Envs      []string
	ImageName string
	Memory    int64
	Publish   []string
	Tag       string
}

func (s *ServiceConfig) Version() string {
	rp := regexp.MustCompile("^([\\d\\.]+)-")
	result := rp.FindStringSubmatch(s.Tag)
	if result == nil {
		util.Log.Fatalln("Formato de TAG invalido")
	}
	return result[1]
}

func (s *ServiceConfig) String() string {
	return fmt.Sprintf("ImageName: %s - Tag: %s - CpuShares: %d - Memory: %s - Publish: %#v - Envs: %s", s.ImageName, s.Tag, s.CPUShares, s.Memory, s.Publish, util.MaskEnv(s.Envs))
}
