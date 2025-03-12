package main

import (
	"log/slog"
	"os"

	"github.com/spf13/pflag"
	"github.com/wzshiming/kube-apiserver-audit-exporter/exporter"
)

var (
	auditLogPath = "./audit.log"
	address      = ":8080"
	cluster      = ""
	replay       = false
)

func init() {
	pflag.StringVar(&auditLogPath, "audit-log-path", auditLogPath, "Path to audit log file")
	pflag.StringVar(&address, "address", address, "Address to listen on")
	pflag.StringVar(&cluster, "cluster-label", cluster, "cluster label of metrics")
	pflag.BoolVar(&replay, "replay", replay, "replay the audit log")
	pflag.Parse()
}

func main() {
	e := exporter.NewExporter(
		exporter.WithFile(auditLogPath),
		exporter.WithReplay(replay),
		exporter.WithClusterLabel(cluster),
	)

	if err := e.ListenAndServe(address); err != nil {
		slog.Error("Failed", "err", err)
		os.Exit(1)
	}
}
