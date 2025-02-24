package exporter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unique"

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
	}, []string{"verb", "resource", "code", "user"})

	schedulingLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "binding_latency_seconds",
		Help: "Scheduling binding latency distribution in seconds",
		Buckets: []float64{
			0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9,
			1, 2, 3, 4, 5, 6, 7, 8, 9,
			10, 20, 30, 40, 50, 60, 70, 80, 90,
			100, 200, 300, 400, 500, 600},
	}, []string{})

	podCreationTimes = map[target]time.Time{}
)

// Target represents a Kubernetes resource target
type target struct {
	Name      string
	Namespace string
}

func init() {
	registry.MustRegister(apiRequests, schedulingLatency)
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

	log.Println("Service started on", addr)
	return http.ListenAndServe(addr, mux)
}

// processAuditEvents handles audit log file changes
func (p *Exporter) processAuditEvents() {
	ticker := time.NewTicker(time.Second)
	for range ticker.C {
		p.handleFileEvent(p.file)
		ticker.Reset(time.Second)
	}
}

// handleFileEvent processes filesystem events
func (p *Exporter) handleFileEvent(path string) {
	if err := p.processFileUpdate(path); err != nil {
		log.Printf("File processing error: %v", err)
	}
}

// processFileUpdate reads new log entries
func (p *Exporter) processFileUpdate(path string) error {

	fileInfo, err := os.Stat(path)
	if err != nil {
		return err
	}

	if fileInfo.Size() == p.offset {
		log.Println("Reload to offset", p.offset)
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("file open failed: %w", err)
	}
	defer file.Close()

	if _, err = file.Seek(p.offset, io.SeekStart); err != nil {
		return fmt.Errorf("file seek failed: %w", err)
	}

	reader := bufio.NewReader(file)

	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return fmt.Errorf("read error: %w", err)
		}

		// This means that we have mislocated the read and can no longer continue execution
		if !bytes.HasPrefix(line, []byte{'{'}) {
			return fmt.Errorf("failed get line data: %q", line)
		}

		// This means that we have reached the end of the file
		if !bytes.HasSuffix(line, []byte{'}'}) {
			return nil
		}

		var event auditv1.Event
		if err := json.Unmarshal(line, &event); err != nil {
			return fmt.Errorf("json decode failed: %w", err)
		}

		p.updateMetrics(event)
		p.offset += int64(len(line)) + 1
	}
}

// updateMetrics processes audit event and updates metrics
func (p *Exporter) updateMetrics(event auditv1.Event) {
	if event.Stage != auditv1.StageResponseComplete {
		return
	}
	if event.Level != auditv1.LevelMetadata {
		return
	}
	userAgent := p.extractUserAgent(event.UserAgent)

	resource := p.buildResourceName(event.ObjectRef)
	p.recordAPIMetrics(event, resource, userAgent)

	if event.Verb != "create" {
		return
	}

	target := target{
		Name:      event.ObjectRef.Name,
		Namespace: event.ObjectRef.Namespace,
	}

	p.handleResourceEvent(resource, target, event)
}

// extractUserAgent extracts clean user agent string
func (p *Exporter) extractUserAgent(ua string) string {
	parts := strings.SplitN(ua, "/", 2)
	return strings.SplitN(parts[0], " ", 2)[0]
}

// buildResourceName constructs resource identifier
func (p *Exporter) buildResourceName(ref *auditv1.ObjectReference) string {
	if ref == nil {
		return "NONE"
	}
	resource := ref.Resource
	if ref.Subresource != "" {
		resource += "/" + ref.Subresource
	}
	return unique.Make(resource).Value()
}

// handleResourceEvent processes resource-specific metrics
func (p *Exporter) handleResourceEvent(resource string, target target, event auditv1.Event) {
	switch resource {
	case "pods":
		podCreationTimes[target] = event.RequestReceivedTimestamp.Time
	case "pods/binding":
		if createTime, exists := podCreationTimes[target]; exists {
			latency := event.RequestReceivedTimestamp.Sub(createTime).Seconds()
			schedulingLatency.WithLabelValues().Observe(latency)
			delete(podCreationTimes, target)
		} else {
			log.Printf("Pod not found for binding: %v", target)
		}
	default:
		log.Printf("Unhandled resource type: %s (%v)", resource, target)
	}
}

func (p *Exporter) recordAPIMetrics(event auditv1.Event, resource, user string) {
	code := strconv.Itoa(int(event.ResponseStatus.Code))
	apiRequests.WithLabelValues(
		unique.Make(event.Verb).Value(),
		resource,
		code,
		unique.Make(user).Value(),
	).Inc()
}
