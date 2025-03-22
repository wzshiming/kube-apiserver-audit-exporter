package main

import (
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/wzshiming/kube-apiserver-audit-exporter/exporter"
)

var (
	auditLogPath = []string{"./audit.log"}
	address      = ":8080"
	cluster      = ""
	replay       = false
	delay        time.Duration
)

func init() {
	pflag.StringArrayVar(&auditLogPath, "audit-log-path", auditLogPath, "Path to audit log files, path[:clusterName]")
	pflag.StringVar(&address, "address", address, "Address to listen on")
	pflag.StringVar(&cluster, "cluster-label", cluster, "Default cluster label of metrics")
	pflag.BoolVar(&replay, "replay", replay, "replay the audit log")
	pflag.DurationVar(&delay, "delay", 0, "delay to start")
	pflag.Parse()
}

func monitorAndStartExporters() {
	for !validAuditLogs() {
		time.Sleep(time.Second)
	}

	if delay > 0 {
		time.Sleep(delay)
	}

	for _, path := range auditLogPath {
		startExporterForPath(path)
	}
}

func validAuditLogs() bool {
	for _, p := range auditLogPath {
		info, err := os.Stat(p)
		if err != nil {
			slog.Warn("Failed to stat audit log", "path", p, "err", err)
			return false
		}
		if info.Size() == 0 {
			slog.Info("Audit log is empty, waiting for content", "path", p)
			return false
		}
	}
	return true
}

func startExporterForPath(pathWithLabel string) {
	parts := strings.SplitN(pathWithLabel, ":", 2)
	path := parts[0]
	clusterLabel := cluster
	if len(parts) > 1 {
		clusterLabel = parts[1]
	}

	e := exporter.NewExporter(
		exporter.WithReplay(replay),
		exporter.WithFile(path),
		exporter.WithClusterLabel(clusterLabel),
	)
	e.Start()
}

func main() {
	go monitorAndStartExporters()

	if err := exporter.ListenAndServe(address); err != nil {
		slog.Error("Failed to start metrics server", "err", err)
		os.Exit(1)
	}
}
