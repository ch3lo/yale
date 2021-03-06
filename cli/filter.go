package cli

import (
	"os"
	"strconv"

	"github.com/ch3lo/yale/util"
	"github.com/codegangsta/cli"
	"github.com/olekukonko/tablewriter"
)

func filterFlags() []cli.Flag {
	return []cli.Flag{
		cli.StringFlag{
			Name:  "image",
			Value: ".*",
		},
		cli.StringFlag{
			Name:  "tag",
			Value: ".*",
		},
	}
}

func filterCmd(c *cli.Context) {
	data := [][]string{}
	stackMap, err := stackManager.Tagged(c.String("image"), c.String("tag"))
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
