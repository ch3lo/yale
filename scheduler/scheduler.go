package scheduler

// Scheduler es una interfaz que debe implementar cualquier scheduler de Servicios
// Para un ejemplo ir a swarm.Scheduler
type Scheduler interface {
	ID() string
	ListContainers(ContainerFilter) ([]*ServiceInformation, error)
	ListTaggedContainers(string, string) ([]*ServiceInformation, error)
	PullImage(string) error
	CreateAndRun(ServiceConfig) (*ServiceInformation, error)
	ContainerInspect(string) (*ServiceInformation, error)
	//ContainerAddress(string, int64) (string, error)
	UndeployContainer(string, bool, uint) error
}
