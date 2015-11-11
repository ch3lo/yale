package helper

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"

	"github.com/ch3lo/yale/util"
	"github.com/fsouza/go-dockerclient"
)

type containerFilter struct {
	NameRegexp  string
	Status      []string
	ImageRegexp string
}

func NewContainerFilter() *containerFilter {
	return &containerFilter{
		Status:      []string{"restarting", "running", "paused", "exited"},
		NameRegexp:  ".*",
		ImageRegexp: ".*"}
}

type DockerHelper struct {
	client         *docker.Client
	authConfigPath string
}

func NewDockerHelper(apiEndpoint string, authCfg string) (*DockerHelper, error) {
	dh := new(DockerHelper)
	dh.authConfigPath = authCfg

	var err error
	dh.client, err = docker.NewClient(apiEndpoint)
	if err != nil {
		return nil, err
	}

	return dh, nil
}

func NewDockerTlsVerifyHelper(apiEndpoint string, authCfg string, cert string, key string, ca string) (*DockerHelper, error) {
	dh := new(DockerHelper)
	dh.authConfigPath = authCfg
	var err error
	dh.client, err = docker.NewTLSClient(apiEndpoint, cert, key, ca)
	if err != nil {
		return nil, err
	}

	return dh, nil
}

func NewDockerTlsHelper(apiEndpoint string, authCfg string, cert string, key string) (*DockerHelper, error) {
	dh := new(DockerHelper)
	dh.authConfigPath = authCfg

	certPEMBlock, err := ioutil.ReadFile(cert)
	if err != nil {
		return nil, err
	}
	keyPEMBlock, err := ioutil.ReadFile(key)
	if err != nil {
		return nil, err
	}

	var caPEMCert []byte

	dh.client, err = docker.NewTLSClientFromBytes(apiEndpoint, certPEMBlock, keyPEMBlock, caPEMCert)
	if err != nil {
		return nil, err
	}

	return dh, nil
}

func (dh *DockerHelper) authConfig(registry string) (docker.AuthConfiguration, error) {
	var r io.Reader
	var err error

	util.Log.Infoln("Retrieving authenticated user")
	util.Log.Debugf("GetDockerHelper %#v", dh)
	if r, err = os.Open(dh.authConfigPath); err != nil {
		return docker.AuthConfiguration{}, err
	}

	var authConfigs *docker.AuthConfigurations

	if authConfigs, err = docker.NewAuthConfigurations(r); err != nil {
		return docker.AuthConfiguration{}, err
	}

	for key, _ := range authConfigs.Configs {
		if key == registry {
			return authConfigs.Configs[registry], nil
		}
	}

	return docker.AuthConfiguration{}, errors.New("Auth not found")
}

func (dh *DockerHelper) ListContainers(filter *containerFilter) ([]docker.APIContainers, error) {
	util.Log.Debugln("Retrieving containers")

	containers, err := dh.client.ListContainers(docker.ListContainersOptions{Filters: map[string][]string{"status": filter.Status}})

	if err != nil {
		return nil, err
	}

	var validName = regexp.MustCompile(filter.NameRegexp)
	var validImage = regexp.MustCompile(filter.ImageRegexp)

	var filteredContainers []docker.APIContainers
	for _, container := range containers {
		util.Log.Debugf("Container %#v", container)

		if validName.MatchString(container.Names[0]) && validImage.MatchString(container.Image) {
			filteredContainers = append(filteredContainers, container)
		}
	}

	return filteredContainers, nil
}

func (dh *DockerHelper) ListTaggedContainers(image string, tag string) ([]docker.APIContainers, error) {
	filter := map[string][]string{"label": []string{"image_name=" + image}} // no funciona con 2 tags
	util.Log.Debugf("Retrieving containers with filter %#v", filter)
	containers, err := dh.client.ListContainers(docker.ListContainersOptions{All: true, Filters: filter})

	if err != nil {
		return nil, err
	}

	return containers, nil
}

func (dh *DockerHelper) PullImage(imageName string) error {

	auth, aErr := dh.authConfig("https://registry.it.lan.com")
	if aErr != nil {
		return aErr
	}

	util.Log.Infoln("Pulling image", imageName)
	var buf bytes.Buffer
	pullImageOpts := docker.PullImageOptions{Repository: imageName, OutputStream: &buf}
	err := dh.client.PullImage(pullImageOpts, auth)
	if err != nil {
		return err
	}

	util.Log.Debugln(buf.String())

	if invalidOut := regexp.MustCompile("Pulling .+ Error"); invalidOut.MatchString(buf.String()) {
		return errors.New("Problema al descargar la imagen")
	}

	return nil
}

func (dh *DockerHelper) CreateAndRun(containerOpts docker.CreateContainerOptions) (*docker.Container, error) {

	util.Log.Infoln("Pulling image")
	err := dh.PullImage(containerOpts.Config.Image)
	if err != nil {
		return nil, err
	}

	util.Log.Infoln("Deploying image", containerOpts.Config.Image)
	container, err := dh.client.CreateContainer(containerOpts)
	if err != nil {
		return nil, err
	}

	util.Log.Infoln("Starting container", container.ID)
	err = dh.client.StartContainer(container.ID, nil)
	if err != nil {
		return nil, err
	}

	util.Log.Infoln("Inspecting container", container.Name)
	container, err = dh.ContainerInspect(container.ID)
	if err != nil {
		return nil, err
	}

	return container, nil
}

func (dh *DockerHelper) ContainerInspect(containerId string) (*docker.Container, error) {
	container, err := dh.client.InspectContainer(containerId)
	if err != nil {
		return nil, err
	}

	return container, nil
}

func (dh *DockerHelper) ContainerAddress(containerId string, internalPort int64) (string, error) {
	container, err := dh.client.InspectContainer(containerId)
	if err != nil {
		return "", err
	}

	util.Log.Debugf("Api Ports %#v", container.NetworkSettings.PortMappingAPI())
	for _, val := range container.NetworkSettings.PortMappingAPI() {
		util.Log.Debugf("Private Port %d - Public Port %d", val.PrivatePort, val.PublicPort)
		if val.PrivatePort == internalPort {
			addr := val.IP + ":" + strconv.FormatInt(val.PublicPort, 10)
			util.Log.Debugf("Calculated Addr %s", addr)
			return addr, nil
		}
	}

	return "", errors.New("Internal Container Port not found")
}

func (dh *DockerHelper) UndeployContainer(containerId string, remove bool, timeout uint) error {

	util.Log.Infoln("Undeploying container", containerId)

	// Un valor de 0 sera interpretado como por defecto
	if timeout == 0 {
		timeout = 10
	}

	var err error

	util.Log.Infoln("Stopping container", containerId)
	err = dh.client.StopContainer(containerId, timeout)
	if err != nil {
		return err
	}

	if remove {
		util.Log.Infoln("Removing container", containerId)
		opts := docker.RemoveContainerOptions{ID: containerId}
		err = dh.client.RemoveContainer(opts)
		if err != nil {
			return err
		}
	}

	return nil
}

/*
func ContainerStatus(containerId string) docker.State {
	container, err := client.InspectContainer(containerId)
	if err != nil {
		return docker.State.Error
	}

	return container.State
}
*/
