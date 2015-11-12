package cli

import (
	"os"
	"strconv"

	"github.com/ch3lo/yale/util"
	"github.com/codegangsta/cli"
	"github.com/olekukonko/tablewriter"
)

func listFlags() []cli.Flag {
	return []cli.Flag{
		cli.StringFlag{
			Name:  "image-filter, if",
			Value: ".*",
			Usage: "Expresion regular para filtrar contenedores por el nombre de la imagen",
		},
		cli.StringFlag{
			Name:  "tag-filter, tf",
			Value: ".*",
			Usage: "Expresion regular para filtrar contenedores por el tag",
		},
		cli.StringFlag{
			Name:  "cname-filter, cf",
			Value: ".*",
			Usage: "Expresion regultar para filtrar contenedores por el nombre del contenedor",
		},
		cli.StringSliceFlag{
			Name:  "status-filter, sf",
			Value: &cli.StringSlice{"restarting", "running", "paused", "exited"},
			Usage: "Expresion regultar para filtrar contenedores por el estado del contenedor",
		},
	}
}

func listCmd(c *cli.Context) {
	data := [][]string{}
	stackMap, err := stackManager.SearchContainers(c.String("if"), c.String("tf"), c.String("cf"))
	if err != nil {
		util.Log.Fatalln(err)
	}

	for stackKey, containers := range stackMap {
		for _, c := range containers {
			var ports string
			for key, val := range c.PublicPorts() {
				ports = ports + strconv.FormatInt(val, 10) + "->" + strconv.FormatInt(key, 10) + " "
			}
			data = append(data, []string{stackKey, c.ContainerSwarmNode(), c.ContainerName(), c.ContainerImageName(), c.ContainerState(), ports})
		}
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Stack", "Node", "Name", "Image", "Status", "Ports"})

	for _, v := range data {
		table.Append(v)
	}
	table.Render()
}
