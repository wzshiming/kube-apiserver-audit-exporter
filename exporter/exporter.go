package exporter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

// Metric definitions
var (
	registry = prometheus.NewRegistry()

	apiRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "api_requests_total",
		Help: "Total number of API requests to the scheduler",
	}, []string{"user", "verb", "resource", "code"})

	podSchedulingLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "pod_scheduling_latency_seconds",
		Help: "Duration from pod creation to scheduled on node in seconds",
		Buckets: []float64{
			0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9,
			1, 2, 3, 4, 5, 6, 7, 8, 9,
			10, 20, 30, 40, 50, 60, 70, 80, 90,
			100, 200, 300, 400, 500, 600,
		},
	}, []string{"user"})

	batchJobCompleteLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "batchjob_completion_latency_seconds",
		Help: "Time from job creation to complete condition in seconds",
		Buckets: []float64{
			1, 2, 3, 4, 5, 6, 7, 8, 9,
			10, 20, 30, 40, 50, 60, 70, 80, 90,
			100, 200, 300, 400, 500, 600,
		},
	}, []string{"user"})

	podCreationTimes = map[Target]*time.Time{}
	batchJobMark     = map[string]struct{}{}
)

// Target represents a Kubernetes resource Target
type Target struct {
	Name      string
	Namespace string
}

func init() {
	registry.MustRegister(
		apiRequests,
		podSchedulingLatency,
		batchJobCompleteLatency,
	)
}

func NewExporter(file string, offset int64) *Exporter {
	return &Exporter{
		file:   file,
		offset: offset,
	}
}

type Exporter struct {
	file   string
	offset int64
}

// run initializes the application components
func (p *Exporter) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
	mux.Handle("/metrics", handler)

	// Process audit events
	go p.processAuditEvents()

	slog.Info("Service started", "address", addr)
	return http.ListenAndServe(addr, mux)
}

// processAuditEvents handles audit log file changes
func (p *Exporter) processAuditEvents() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		p.handleFileEvent(p.file)
		ticker.Reset(time.Second)
	}
}

// handleFileEvent processes filesystem events
func (p *Exporter) handleFileEvent(path string) {
	if err := p.processFileUpdate(path); err != nil {
		slog.Error("Error processing file", "error", err)
	}
}

// processFileUpdate reads new log entries
func (p *Exporter) processFileUpdate(path string) error {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if size := fileInfo.Size(); size < p.offset {
		slog.Info("Log file truncated, resetting offset")
		p.offset = 0
	} else if size == p.offset {
		slog.Info("No new updates in log file", "offset", p.offset)
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	if _, err = file.Seek(p.offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek failed: %w", err)
	}

	start := time.Now()
	defer func() {
		slog.Info("File processing complete", "new_offset", p.offset, "duration", time.Since(start))
	}()

	reader := bufio.NewReaderSize(file, 1<<20) // 1MB buffer
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return fmt.Errorf("read error: %w", err)
		}

		// This means that we have mislocated the read and can no longer continue execution
		if !bytes.HasPrefix(line, []byte{'{'}) || !bytes.HasSuffix(line, []byte{'}', '\n'}) {
			return fmt.Errorf("malformed log entry: %q", line)
		}

		var event auditv1.Event
		if err := json.Unmarshal(line, &event); err != nil {
			return fmt.Errorf("json decode error: %w", err)
		}

		p.updateMetrics(event)
		p.offset += int64(len(line))
	}
}

// updateMetrics processes audit event and updates metrics
func (p *Exporter) updateMetrics(event auditv1.Event) {
	switch {
	case event.Level == auditv1.LevelMetadata &&
		event.Stage == auditv1.StageResponseComplete:
		p.recordAPICall(event)
	case event.Level == auditv1.LevelRequestResponse &&
		event.Stage == auditv1.StageResponseComplete &&
		event.ObjectRef.Resource == "pods" &&
		event.ResponseStatus.Code >= 200 && event.ResponseStatus.Code < 300:
		p.recordPodScheduling(event)
	case event.Level == auditv1.LevelRequestResponse &&
		event.Stage == auditv1.StageResponseComplete &&
		event.ObjectRef.Resource == "jobs" &&
		event.ResponseStatus.Code >= 200 && event.ResponseStatus.Code < 300:
		p.recordBatchJobScheduling(event)
	}
}

func extractUserAgent(ua string) string {
	parts := strings.SplitN(ua, "/", 2)
	name := strings.SplitN(parts[0], " ", 2)[0]
	if name == "" {
		name = "unknown"
	}
	return name
}

func extractResourceName(event auditv1.Event) string {
	ref := event.ObjectRef
	if ref == nil {
		return "None"
	}

	var builder strings.Builder
	builder.WriteString(ref.Resource)

	if ref.APIGroup != "" {
		builder.WriteString(".")
		builder.WriteString(ref.APIGroup)
	}
	if ref.Subresource != "" {
		builder.WriteString("/")
		builder.WriteString(ref.Subresource)
	}
	return builder.String()
}

func (p *Exporter) buildTarget(ref *auditv1.ObjectReference) Target {
	if ref == nil || ref.Name == "" {
		return Target{}
	}

	return Target{
		Name:      ref.Name,
		Namespace: ref.Namespace,
	}
}

func (p *Exporter) recordBatchJobScheduling(event auditv1.Event) {
	var job BatchJob
	err := json.Unmarshal(event.ResponseObject.Raw, &job)
	if err != nil {
		slog.Error("failed to unmarshal job", "err", err)
		return
	}

	if _, ok := batchJobMark[job.Metadata.UID]; !ok {
		if job.Status.IsCompleted() {
			latency := event.StageTimestamp.Sub(job.Metadata.CreationTimestamp).Seconds()
			user := extractUserAgent(event.UserAgent)
			batchJobCompleteLatency.WithLabelValues(
				user,
			).Observe(latency)
			batchJobMark[job.Metadata.UID] = struct{}{}
		}
	}
	if event.Verb == "delete" {
		delete(batchJobMark, job.Metadata.UID)
	}
}

func (p *Exporter) recordPodScheduling(event auditv1.Event) {
	switch event.Verb {
	case "delete":
		delete(podCreationTimes, p.buildTarget(event.ObjectRef))
	case "create":
		var pod Pod
		err := json.Unmarshal(event.ResponseObject.Raw, &pod)
		if err != nil {
			slog.Error("failed to unmarshal", "err", err)
			return
		}

		target := Target{
			Name:      pod.Metadata.Name,
			Namespace: pod.Metadata.Namespace,
		}
		if pod.Spec.NodeName == "" {
			podCreationTimes[target] = &event.StageTimestamp.Time
		} else {
			podCreationTimes[target] = nil
		}

	case "patch", "update":
		target := p.buildTarget(event.ObjectRef)
		createTime, exists := podCreationTimes[target]
		if !exists {
			slog.Warn("Pod not found", "target", target)
			return
		}

		if createTime == nil {
			return
		}

		var pod Pod
		err := json.Unmarshal(event.ResponseObject.Raw, &pod)
		if err != nil {
			slog.Error("failed to unmarshal", "err", err)
			return
		}
		if pod.Spec.NodeName != "" {
			latency := event.RequestReceivedTimestamp.Sub(*createTime).Seconds()
			user := extractUserAgent(event.UserAgent)
			podSchedulingLatency.WithLabelValues(
				user,
			).Observe(latency)
			podCreationTimes[target] = nil
		}

	default:
	}
}

func (p *Exporter) recordAPICall(event auditv1.Event) {
	labels := []string{
		extractUserAgent(event.UserAgent),
		event.Verb,
		extractResourceName(event),
		strconv.Itoa(int(event.ResponseStatus.Code)),
	}
	apiRequests.WithLabelValues(labels...).Inc()
}
