package main

import (
	"log"
	"os"

	"github.com/spf13/pflag"
	"github.com/wzshiming/kube-apiserver-audit-exporter/exporter"
)

var (
	auditLogPath = "./audit.log"
)

func init() {
	pflag.StringVar(&auditLogPath, "audit-log-path", auditLogPath, "Path to audit log file")
	pflag.Parse()
}

func main() {
	e := exporter.NewExporter(auditLogPath, 0)

	if err := e.ListenAndServe(":8080"); err != nil {
		log.Printf("Application failed: %v", err)
		os.Exit(1)
	}
}
