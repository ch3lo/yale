package cluster

import "fmt"

// ClusterDisabled error generado cuando un cluster no esta habilitado
type ClusterDisabled struct {
	Name string
}

func (err ClusterDisabled) Error() string {
	return fmt.Sprintf("El cluster no esta habilitado: %s", err.Name)
}
