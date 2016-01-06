package swarm

// basado en https://github.com/docker/distribution/blob/603ffd58e18a9744679f741f2672dd9aea6babe0/registry/storage/driver/rados/rados.go

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"reflect"
	"regexp"

	"github.com/ch3lo/yale/scheduler"
	"github.com/ch3lo/yale/scheduler/factory"
	"github.com/ch3lo/yale/util"
	"github.com/fsouza/go-dockerclient"
)

const schedulerID = "swarm"

func init() {
	factory.Register(schedulerID, &swarmCreator{})
}

// swarmCreator implementa la interfaz factory.SchedulerFactory
type swarmCreator struct{}

func (factory *swarmCreator) Create(parameters map[string]interface{}) (scheduler.Scheduler, error) {
	return NewFromParameters(parameters)
}

// parameters encapsula los parametros de configuracion de Swarm
type parameters struct {
	address   string
	authfile  string
	tlsverify bool
	tlscacert string
	tlscert   string
	tlskey    string
}

func dockerCfgPath() string {
	p := path.Join(os.Getenv("HOME"), ".docker", "config.json")

	if _, err := os.Stat(p); os.IsNotExist(err) {
		p = path.Join(os.Getenv("HOME"), ".dockercfg")
	}

	return p
}

// NewFromParameters construye un Scheduler a partir de un mapeo de parámetros
// Al menos se debe pasar como parametro address, ya que si no existe se retornara un error
// Si se pasa tlsverify como true los parametros tlscacert, tlscert y tlskey también deben existir
func NewFromParameters(params map[string]interface{}) (*Scheduler, error) {

	address, ok := params["address"]
	if !ok || fmt.Sprint(address) == "" {
		return nil, errors.New("Parametro address no existe")
	}

	authfile := dockerCfgPath()
	if af, ok := params["authfile"]; !ok || fmt.Sprint(af) == "" {
		util.Log.Warnln("Parametro authfile no existe o está vacio, utilizando su valor por defecto", authfile)
	} else {
		authfile = fmt.Sprint(af)
	}

	tlsverify := false
	if tlsv, ok := params["tlsverify"]; ok {
		tlsverify, ok = tlsv.(bool)
		if !ok {
			return nil, fmt.Errorf("El parametro tlsverify debe ser un boolean")
		}
	}

	var tlscacert interface{}
	var tlscert interface{}
	var tlskey interface{}

	if tlsverify {
		tlscacert, ok = params["tlscacert"]
		if !ok || fmt.Sprint(tlscacert) == "" {
			return nil, errors.New("Parametro tlscacert no existe")
		}

		tlscert, ok = params["tlscert"]
		if !ok || fmt.Sprint(tlscert) == "" {
			return nil, errors.New("Parametro tlscert no existe")
		}

		tlskey, ok = params["tlskey"]
		if !ok || fmt.Sprint(tlskey) == "" {
			return nil, errors.New("Parametro tlskey no existe")
		}
	}

	p := parameters{
		address:   fmt.Sprint(address),
		authfile:  authfile,
		tlsverify: tlsverify,
		tlscacert: fmt.Sprint(tlscacert),
		tlscert:   fmt.Sprint(tlscert),
		tlskey:    fmt.Sprint(tlskey),
	}

	return New(p)
}

func authConfig(authConfigPath string, registry string) (docker.AuthConfiguration, error) {
	var r io.Reader
	var err error

	util.Log.Infof("Obteniendo los parámetros de autenticación para el registro %s del archivo %s", registry, authConfigPath)
	if r, err = os.Open(authConfigPath); err != nil {
		return docker.AuthConfiguration{}, err
	}

	var authConfigs *docker.AuthConfigurations

	if authConfigs, err = docker.NewAuthConfigurations(r); err != nil {
		return docker.AuthConfiguration{}, err
	}

	for key := range authConfigs.Configs {
		if key == registry {
			return authConfigs.Configs[registry], nil
		}
	}

	return docker.AuthConfiguration{}, errors.New("No se encontraron las credenciales de autenticación")
}

// New instancia un nuevo cliente de Swarm
func New(params parameters) (*Scheduler, error) {
	swarm := &Scheduler{
		authConfigs: make(map[string]docker.AuthConfiguration),
	}

	var err error
	util.Log.Debugf("Configurando Swarm con los parametros %+v", params)
	if params.tlsverify {
		swarm.client, err = docker.NewTLSClient(params.address, params.tlscert, params.tlskey, params.tlscacert)
	} else {
		swarm.client, err = docker.NewClient(params.address)
	}
	if err != nil {
		return nil, err
	}

	registriesURL := []string{"https://registry.it.lan.com", "https://registry.dev.lan.com"}
	for _, v := range registriesURL {
		auth, err := authConfig(params.authfile, v)
		if err != nil {
			return nil, err
		}
		swarm.authConfigs[v] = auth
	}

	return swarm, nil
}

// Scheduler es una implementacion de scheduler.Scheduler
// Permite el la comunicación con la API de Swarm
type Scheduler struct {
	client      *docker.Client
	authConfigs map[string]docker.AuthConfiguration
}

// ID retorna el identificador del scheduler Swarm
func (s *Scheduler) ID() string {
	return schedulerID
}

func getStatus(s string) scheduler.ServiceInformationStatus {
	upRegexp := regexp.MustCompile("^[u|U]p")
	status := scheduler.ServiceDown
	if upRegexp.MatchString(s) {
		status = scheduler.ServiceUp
	}
	return status
}

func getStatusFromState(state docker.State) scheduler.ServiceInformationStatus {
	status := scheduler.ServiceDown
	if state.Running &&
		!state.Paused &&
		!state.Restarting {
		status = scheduler.ServiceUp
	}
	return status
}

func hostAndContainerName(fullName string) (string, string) {
	hostAndContainerName := regexp.MustCompile("^(?:/([\\w|_-]+))?/([\\w|_-]+)$")
	result := hostAndContainerName.FindStringSubmatch(fullName)
	host := "unknown"
	if result[1] != "" {
		host = result[1]
	}
	containerName := result[2]
	return host, containerName
}

func imageAndTag(fullImageName string) (string, string) {
	imageAndTagRegexp := regexp.MustCompile("^([\\w./_-]+)(?::([\\w._-]+))?$")
	result := imageAndTagRegexp.FindStringSubmatch(fullImageName)
	imageName := result[1]
	imageTag := "latest"
	if result[1] != "" {
		imageTag = result[2]
	}
	return imageName, imageTag
}

func mapPorts(apiPorts []docker.APIPort) map[string]scheduler.ServicePort {
	ports := make(map[string]scheduler.ServicePort)
	for _, v := range apiPorts {
		id := fmt.Sprintf("%d/%s", v.PrivatePort, v.Type)
		p, ok := ports[id]
		if !ok {
			ip := "127.0.0.1"
			if v.IP != "0.0.0.0" {
				ip = v.IP
			}
			p = scheduler.ServicePort{
				Advertise: ip,
				Internal:  v.PrivatePort,
				Type:      scheduler.NewServicePortType(v.Type),
			}
		}
		p.Publics = append(p.Publics, v.PublicPort)
		ports[id] = p
	}
	return ports
}

func (s *Scheduler) ListContainers(filter scheduler.ContainerFilter) ([]*scheduler.ServiceInformation, error) {
	util.Log.Debugln("Obteniendo el listado de contenedores")

	containers, err := s.client.ListContainers(docker.ListContainersOptions{Filters: map[string][]string{"status": filter.Status}})
	if err != nil {
		return nil, err
	}

	var validName = regexp.MustCompile(filter.NameRegexp)
	var validImage = regexp.MustCompile(filter.ImageRegexp + ":" + filter.TagRegexp)

	var filteredContainers []*scheduler.ServiceInformation
	for _, container := range containers {
		util.Log.Debugf("Filtrando el contenedor %+v", container)

		if validName.MatchString(container.Names[0]) && validImage.MatchString(container.Image) {
			host, containerName := hostAndContainerName(container.Names[0])
			imageName, imageTag := imageAndTag(container.Image)

			c := &scheduler.ServiceInformation{
				ID:            container.ID,
				ImageName:     imageName,
				ImageTag:      imageTag,
				Status:        getStatus(container.Status),
				Host:          host,
				ContainerName: containerName,
				Ports:         mapPorts(container.Ports),
			}

			util.Log.Debugf("Mapeando contenedor a %+v", c)
			filteredContainers = append(filteredContainers, c)
		}
	}

	return filteredContainers, nil
}

func (s *Scheduler) ListTaggedContainers(image string, tag string) ([]*scheduler.ServiceInformation, error) {
	filter := map[string][]string{"label": []string{"image_name=" + image}} // no funciona con 2 tags
	util.Log.Debugf("Obteniendo el listado de contenedores con filtro %#v", filter)
	containers, err := s.client.ListContainers(docker.ListContainersOptions{All: true, Filters: filter})

	if err != nil {
		return nil, err
	}

	var mappedContainers []*scheduler.ServiceInformation
	for _, container := range containers {
		host, containerName := hostAndContainerName(container.Names[0])
		imageName, imageTag := imageAndTag(container.Image)
		c := &scheduler.ServiceInformation{
			ID:            container.ID,
			ImageName:     imageName,
			ImageTag:      imageTag,
			Status:        getStatus(container.Status),
			Host:          host,
			ContainerName: containerName,
			Ports:         mapPorts(container.Ports),
		}
		mappedContainers = append(mappedContainers, c)
	}

	return mappedContainers, nil
}

func (s *Scheduler) PullImage(imageName string) error {
	util.Log.Infoln("Realizando el pulling de la imagen", imageName)
	var buf bytes.Buffer
	pullImageOpts := docker.PullImageOptions{Repository: imageName, OutputStream: &buf}
	err := s.client.PullImage(pullImageOpts, s.authConfigs["https://registry.it.lan.com"])
	if err != nil {
		return err
	}

	util.Log.Debugln(buf.String())

	if invalidOut := regexp.MustCompile("Pulling .+ Error"); invalidOut.MatchString(buf.String()) {
		return errors.New("Problema al descargar la imagen")
	}

	return nil
}

func bindPort(publish []string) map[docker.Port][]docker.PortBinding {
	portBindings := map[docker.Port][]docker.PortBinding{}

	for _, v := range publish {
		util.Log.Debugln("Procesando el bindeo del puerto", v)
		var dp docker.Port
		reflect.ValueOf(&dp).Elem().SetString(v)
		portBindings[dp] = []docker.PortBinding{docker.PortBinding{}}
	}

	util.Log.Debugf("PortBindings %#v", portBindings)

	return portBindings
}

func (s *Scheduler) CreateAndRun(serviceConfig scheduler.ServiceConfig) (*scheduler.ServiceInformation, error) {
	labels := map[string]string{
		"image_name": serviceConfig.ImageName,
		"image_tag":  serviceConfig.Tag,
	}

	dockerConfig := docker.Config{
		Image:  serviceConfig.ImageName + ":" + serviceConfig.Tag,
		Env:    serviceConfig.Envs,
		Labels: labels,
	}

	sourcetype := "{{.Name}}"
	if serviceConfig.ServiceID != "" {
		sourcetype = serviceConfig.ServiceID
	}

	dockerHostConfig := docker.HostConfig{
		Binds:           []string{"/var/log/service/:/var/log/service/"},
		CPUShares:       int64(serviceConfig.CPUShares),
		PortBindings:    bindPort(serviceConfig.Publish),
		PublishAllPorts: false,
		Privileged:      false,
		LogConfig: docker.LogConfig{
			Type: "syslog",
			Config: map[string]string{
				"tag":             fmt.Sprintf("{{.ImageName}}|%s|{{.ID}}", sourcetype),
				"syslog-facility": "local1",
			},
		},
	}

	if serviceConfig.Memory != 0 {
		dockerHostConfig.Memory = serviceConfig.Memory
	}

	opts := docker.CreateContainerOptions{
		Config:     &dockerConfig,
		HostConfig: &dockerHostConfig}

	err := s.PullImage(opts.Config.Image)
	if err != nil {
		return nil, err
	}

	util.Log.Infoln("Creando el contenedor con imagen", opts.Config.Image)
	container, err := s.client.CreateContainer(opts)
	if err != nil {
		return nil, err
	}

	util.Log.Infoln("Contenedor creado... Se inicia el proceso de arranque", container.ID)
	err = s.client.StartContainer(container.ID, nil)
	if err != nil {
		switch err.(type) {
		case *docker.NoSuchContainer:
			return nil, err
		case *docker.ContainerAlreadyRunning:
			util.Log.Infof("El contenedor %s ya estaba corriendo", container.ID)
			break
		default:
			return nil, err
		}
	}

	util.Log.Infoln("Contenedor corriendo... Inspeccionando sus datos", container.Name)
	return s.ContainerInspect(container.ID)
}

func (s *Scheduler) ContainerInspect(containerID string) (*scheduler.ServiceInformation, error) {
	container, err := s.client.InspectContainer(containerID)
	if err != nil {
		return nil, err
	}
	util.Log.Debugf("Inspeccionando contenedor %#v", container)

	host, containerName := hostAndContainerName(container.Name)
	return &scheduler.ServiceInformation{
		ID:            container.ID,
		ImageName:     container.Config.Image,
		ImageTag:      "",
		Status:        getStatusFromState(container.State),
		Host:          host,          // container.Node.Name
		ContainerName: containerName, // container.Name
		//Ports:         make(map[string]scheduler.ServicePort{}),
	}, nil
}

/*
func (s *Scheduler) ContainerAddress(containerID string, internalPort int64) (string, error) {
	container, err := s.client.InspectContainer(containerID)
	if err != nil {
		return "", err
	}

	util.Log.Debugf("Api Ports %#v", container.NetworkSettings.PortMappingAPI())
	for _, val := range container.NetworkSettings.PortMappingAPI() {
		util.Log.Debugf("Puerto privado %d - Puerto Publico %d", val.PrivatePort, val.PublicPort)
		if val.PrivatePort == internalPort {
			addr := val.IP + ":" + strconv.FormatInt(val.PublicPort, 10)
			util.Log.Debugf("La dirección calculada es %s", addr)
			return addr, nil
		}
	}

	return "", errors.New("No se encontró el puerto interno del contenedor")
}*/

func (s *Scheduler) UndeployContainer(containerID string, remove bool, timeout uint) error {
	util.Log.Infoln("Se está iniciando el proceso de undeploy del contenedor", containerID)

	// Un valor de 0 sera interpretado como por defecto
	if timeout == 0 {
		timeout = 10
	}

	util.Log.Infoln("Deteniendo el contenedor", containerID)
	err := s.client.StopContainer(containerID, timeout)

	if err != nil {
		switch err.(type) {
		case *docker.NoSuchContainer:
			util.Log.Infoln("No se encontró el contenedor", containerID)
			return nil
		case *docker.ContainerNotRunning:
			util.Log.Infof("El contenedor %s no estaba corriendo", containerID)
			break
		default:
			return err
		}
	}

	if remove {
		util.Log.Infoln("Se inició el proceso de remover el contenedor", containerID)
		opts := docker.RemoveContainerOptions{ID: containerID}
		err = s.client.RemoveContainer(opts)
		if err != nil {
			return err
		}
	}

	return nil
}
