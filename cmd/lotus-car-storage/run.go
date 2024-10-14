package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/lotus/build"
	"github.com/gorilla/mux"
	"github.com/urfave/cli/v2"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var runCmd = &cli.Command{
	Name:  "run",
	Usage: "Start lotus-car-storage",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "address",
			Usage: "locally reachable address",
			Value: "0.0.0.0:5678",
		},
		&cli.StringFlag{
			Name:    "paths",
			Usage:   "lotus storage car file path",
			EnvVars: []string{"CAR_STORAGE_PATHS"},
		},
	},

	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		unSetEnv()

		mux := mux.NewRouter()

		log.Infof("store version: %s", build.MinerUserVersion())

		// Register all metric views
		if err := view.Register(
			DefaultViews...,
		); err != nil {
			log.Fatalf("Cannot register the view: %v", err)
		}

		lAddr := cctx.String("address")
		machineName, err := os.Hostname()
		if err != nil {
			machineName = lAddr
		}

		log.Infof("car paths: %s", cctx.String("paths"))
		carPaths := strings.Split(cctx.String("paths"), ";")
		if len(carPaths) == 0 {
			return fmt.Errorf("car data path len 0")
		}
		SetEnv(CAR_STORAGE_PATHS, cctx.String("paths"))
		showEnv()

		stat := new(status)
		stat.SectorPath = make(map[string]string)

		h := handler{
			CarDataPath: carPaths,
			MachineName: machineName,
			StoreStatus: stat,
		}

		log.Info("scan path start")
		start := time.Now()
		if err := h.scanPath(); err != nil {
			log.Infof("scan path err: %v", err)
		}
		log.Infof("scan path len: %d end time: %s", h.StoreStatus.getLen(), time.Since(start).String())

		mux.PathPrefix("/remote").HandlerFunc((&h).ServeHTTP)

		ah := &auth.Handler{
			Verify: func(ctx context.Context, token string) ([]auth.Permission, error) {
				return []auth.Permission{"admin"}, nil
			},
			Next: mux.ServeHTTP,
		}

		srv := &http.Server{
			Handler: ah,
			BaseContext: func(listener net.Listener) context.Context {
				return ctx
			},
		}

		tctx, _ := tag.New(context.Background(), tag.Insert(Version, build.MinerBuildVersion), tag.Insert(Commit, build.CurrentCommit))
		stats.Record(tctx, LotusStoreInfo.M(1))

		nl, err := net.Listen("tcp", lAddr)
		if err != nil {
			return err
		}
		log.Infof("lotus store listen: %s", lAddr)

		return srv.Serve(nl)
	},
}
