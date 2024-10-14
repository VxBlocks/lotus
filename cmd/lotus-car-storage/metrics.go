package main

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	Version, _ = tag.NewKey("version")
	Commit, _  = tag.NewKey("commit")
)

var (
	LotusStoreInfo = stats.Int64("info", "Arbitrary counter to tag lotus info to", stats.UnitDimensionless)
)

var (
	InfoView = &view.View{
		Name:        "info",
		Description: "Lotus worker c2 node information",
		Measure:     LotusStoreInfo,
		Aggregation: view.LastValue(),
		TagKeys:     []tag.Key{Version, Commit},
	}
)

var DefaultViews = []*view.View{
	InfoView,
}
