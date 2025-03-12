package exporter

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

// Metric definitions
var (
	registry = prometheus.NewRegistry()

	apiRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "api_requests_total",
		Help: "Total number of API requests to the scheduler",
	}, []string{"cluster", "user", "verb", "resource", "code"})

	podSchedulingLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "pod_scheduling_latency_seconds",
		Help:    "Duration from pod creation to scheduled on node in seconds",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 20),
	}, []string{"cluster", "user"})

	batchJobCompleteLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "batchjob_completion_latency_seconds",
		Help:    "Time from job creation to complete condition in seconds",
		Buckets: prometheus.ExponentialBuckets(1, 2, 12),
	}, []string{"cluster", "user"})
)

func init() {
	registry.MustRegister(
		apiRequests,
		podSchedulingLatency,
		batchJobCompleteLatency,
	)
}

var (
	podCreationTimes      = map[target]*time.Time{}
	batchJobCreationTimes = map[target]*time.Time{}
)

// updateMetrics processes audit event and updates metrics
func updateMetrics(clusterLabel string, event auditv1.Event) {
	if event.ResponseStatus == nil ||
		(event.ResponseStatus.Code < 200 || event.ResponseStatus.Code >= 300) {
		return
	}

	if event.Stage == auditv1.StageResponseComplete {
		labels := []string{
			clusterLabel,
			extractUserAgent(event.UserAgent),
			event.Verb,
			extractResourceName(event),
			strconv.Itoa(int(event.ResponseStatus.Code)),
		}
		apiRequests.WithLabelValues(labels...).Inc()
	}

	if event.ObjectRef != nil {
		switch event.ObjectRef.Resource {
		case "pods":
			if event.ObjectRef.Subresource == "binding" && event.Verb == "create" {
				target := buildTarget(event.ObjectRef)
				createTime, exists := podCreationTimes[target]
				if !exists {
					slog.Warn("Pod not found", "target", target)
					return
				}

				if createTime == nil {
					return
				}
				latency := event.StageTimestamp.Sub(*createTime).Seconds()

				user := extractUserAgent(event.UserAgent)
				podSchedulingLatency.WithLabelValues(
					clusterLabel,
					user,
				).Observe(latency)
				podCreationTimes[target] = nil

			} else {
				if event.Verb == "create" {
					var pod Pod
					err := json.Unmarshal(event.ResponseObject.Raw, &pod)
					if err != nil {
						slog.Error("failed to unmarshal", "err", err)
						return
					}

					target := target{
						Name:      pod.Metadata.Name,
						Namespace: pod.Metadata.Namespace,
					}
					if pod.Spec.NodeName == "" {
						podCreationTimes[target] = &event.StageTimestamp.Time
					} else {
						podCreationTimes[target] = nil
					}
				} else if event.Verb == "delete" {
					delete(podCreationTimes, buildTarget(event.ObjectRef))
				}
			}

		case "jobs":
			if event.Verb == "create" && event.ResponseObject != nil {
				var job BatchJob
				err := json.Unmarshal(event.ResponseObject.Raw, &job)
				if err != nil {
					slog.Error("failed to unmarshal", "err", err)
					return
				}

				target := target{
					Name:      job.Metadata.Name,
					Namespace: job.Metadata.Namespace,
				}
				batchJobCreationTimes[target] = &event.StageTimestamp.Time
			} else if event.Verb == "delete" {
				target := buildTarget(event.ObjectRef)
				delete(batchJobCreationTimes, target)
			} else {
				target := buildTarget(event.ObjectRef)
				if createTime, ok := batchJobCreationTimes[target]; ok && createTime != nil && event.ResponseObject != nil {
					var job BatchJob
					err := json.Unmarshal(event.ResponseObject.Raw, &job)
					if err != nil {
						slog.Error("failed to unmarshal job", "err", err)
						return
					}
					if job.Status.IsCompleted() {
						latency := event.StageTimestamp.Sub(job.Metadata.CreationTimestamp).Seconds()
						user := extractUserAgent(event.UserAgent)
						batchJobCompleteLatency.WithLabelValues(
							clusterLabel,
							user,
						).Observe(latency)
						batchJobCreationTimes[target] = nil
					}
				}
			}
		}
	}
}
