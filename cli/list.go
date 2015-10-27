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
			Usage: "Expresion regultar para filtrar contenedores por el nombre de la imagen",
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
	stackMap, err := stackManager.SearchContainers(c.String("if"), c.String("cf"))

	if err != nil {
		util.Log.Errorln(err)
	}

	for stackKey, containers := range stackMap {
		for _, c := range containers {
			var ports string
			for _, port := range c.Ports {
				if port.PublicPort != 0 {
					ports = ports + strconv.FormatInt(port.PublicPort, 10) + "->" + strconv.FormatInt(port.PrivatePort, 10) + "/" + port.Type + " "
				}
			}
			data = append(data, []string{stackKey, c.Names[0], c.Image, c.Status, ports})
		}
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Endpoint", "Name", "Image", "Status", "Ports"})

	for _, v := range data {
		table.Append(v)
	}
	table.Render()
}
