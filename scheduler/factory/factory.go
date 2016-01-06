package factory

import (
	"fmt"

	"github.com/ch3lo/yale/scheduler"
	"github.com/ch3lo/yale/util"
)

// schedulerFactories almacena una mapeo entre un identificador de scheduler y su constructor
var schedulerFactories = make(map[string]SchedulerFactory)

// SchedulerFactory es una interfaz para crear un Scheduler
// Cada Scheduler debe implementar estar interfaz y además llamar el metodo Register
// para registrar el constructor de la implementacion
type SchedulerFactory interface {
	Create(parameters map[string]interface{}) (scheduler.Scheduler, error)
}

// Register permite registrar a una implementación de Scheduler, de esta
// manera estara disponible mediante su ID para poder ser instanciado
func Register(name string, factory SchedulerFactory) {
	if factory == nil {
		util.Log.Fatal("Se debe pasar como argumento un SchedulerFactory")
	}
	_, registered := schedulerFactories[name]
	if registered {
		util.Log.Fatalf("SchedulerFactory %s ya está registrado", name)
	}

	schedulerFactories[name] = factory
}

// Create crea un Scheduler a partir de un ID y retorna la implementacion asociada a él.
// Si el Scheduler no estaba registrado se retornará un InvalidScheduler
func Create(name string, parameters map[string]interface{}) (scheduler.Scheduler, error) {
	schedulerFactory, ok := schedulerFactories[name]
	if !ok {
		return nil, InvalidScheduler{name}
	}
	return schedulerFactory.Create(parameters)
}

// InvalidScheduler es una estructura de error utilizada cuando se instenta
// crear un Scheduler no registrado
type InvalidScheduler struct {
	Name string
}

func (err InvalidScheduler) Error() string {
	return fmt.Sprintf("Scheduler no esta registrado: %s", err.Name)
}
