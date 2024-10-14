package main

import (
	"os"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/lib/lotuslog"
	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
)

var log = logging.Logger("lotus-car-storage")

func main() {
	lotuslog.SetupLogLevels()

	local := []*cli.Command{
		runCmd,
	}

	app := &cli.App{
		Name:    "lotus-car-storage",
		Usage:   "Remote lotus-car-storage node",
		Version: string(build.MinerUserVersion()),
		Flags:   []cli.Flag{},

		Commands: local,
	}
	app.Setup()

	if err := app.Run(os.Args); err != nil {
		log.Warnf("%+v", err)
		return
	}
}
