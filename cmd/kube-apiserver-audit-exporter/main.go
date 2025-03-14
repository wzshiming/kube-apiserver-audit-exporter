package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/pflag"
	"github.com/wzshiming/kube-apiserver-audit-exporter/exporter"
)

var (
	auditLogPath = []string{"./audit.log"}
	address      = ":8080"
	cluster      = ""
	replay       = false
)

func init() {
	pflag.StringArrayVar(&auditLogPath, "audit-log-path", auditLogPath, "Path to audit log files, path[:clusterName]")
	pflag.StringVar(&address, "address", address, "Address to listen on")
	pflag.StringVar(&cluster, "cluster-label", cluster, "Default cluster label of metrics")
	pflag.BoolVar(&replay, "replay", replay, "replay the audit log")
	pflag.Parse()
}

func main() {
	for _, p := range auditLogPath {
		ns := strings.SplitN(p, ":", 2)
		path := ns[0]
		clusterLabel := cluster
		if len(ns) > 1 {
			clusterLabel = ns[1]
		}

		e := exporter.NewExporter(
			exporter.WithReplay(replay),
			exporter.WithFile(path),
			exporter.WithClusterLabel(clusterLabel),
		)
		e.Start()
	}

	if err := exporter.ListenAndServe(address); err != nil {
		slog.Error("Failed", "err", err)
		os.Exit(1)
	}
}
