package cli

import "github.com/codegangsta/cli"

var commands = []cli.Command{
	{
		Name:    "deploy",
		Aliases: []string{"d"},
		Usage:   "despliega un servicio",
		Flags:   deployFlags(),
		Before:  deployBefore,
		Action:  deployCmd,
	},
	{
		Name:    "list",
		Aliases: []string{"l"},
		Usage:   "Lista contenedores",
		Flags:   listFlags(),
		Action:  listCmd,
	},
	{
		Name:    "filter",
		Aliases: []string{"f"},
		Usage:   "[WIP] Filtra contenedores",
		Flags:   filterFlags(),
		Action:  filterCmd,
	},
}
