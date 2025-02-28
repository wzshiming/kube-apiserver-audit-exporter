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
)

func init() {
	pflag.StringVar(&auditLogPath, "audit-log-path", auditLogPath, "Path to audit log file")
	pflag.StringVar(&address, "address", address, "Address to listen on")
	pflag.Parse()
}

func main() {
	e := exporter.NewExporter(auditLogPath, 0)

	if err := e.ListenAndServe(address); err != nil {
		slog.Error("Failed", "err", err)
		os.Exit(1)
	}
}
