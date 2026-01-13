package main

import (
	"context"
	"os"

	l "github.com/sprisa/x/log"
	"github.com/sprisa/x/sig"
	"github.com/urfave/cli/v3"
)

func main() {
	ctx := sig.ShutdownContext(context.Background())
	err := MainCommand.Run(ctx, os.Args)
	if err != nil {
		l.Log.Error().Msg(err.Error())
		defer os.Exit(1)
	}
}

var MainCommand = &cli.Command{
	Name:     "npmreleaser",
	Usage:    "Publish your Go package on NPM",
	Commands: []*cli.Command{
		BuildCommand,
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		return cli.ShowAppHelp(cmd)
	},
}
