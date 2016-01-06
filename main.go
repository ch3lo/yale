package main

import (
	"github.com/ch3lo/yale/cli"
	_ "github.com/ch3lo/yale/scheduler/marathon"
	_ "github.com/ch3lo/yale/scheduler/swarm"
)

func main() {
	cli.RunApp()
}
