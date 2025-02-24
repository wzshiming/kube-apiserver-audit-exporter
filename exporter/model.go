package exporter

import (
	"strings"
)

type Pod struct {
	Kind     string   `json:"kind"`
	Metadata Metadata `json:"metadata"`
	Spec     PodSpec  `json:"spec"`
}

type Metadata struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}
type PodSpec struct {
	NodeName string `json:"nodeName"`
}

type BatchJob struct {
	Metadata Metadata       `json:"metadata"`
	Status   BatchJobStatus `json:"status"`
}

type BatchJobStatus struct {
	Conditions []Condition `json:"conditions"`
}

type Condition struct {
	Type string `json:"type"`
}

// IsCompleted checks if job has a Complete condition
func (b BatchJobStatus) IsCompleted() bool {
	for _, condition := range b.Conditions {
		if strings.HasPrefix(condition.Type, "Complete") {
			return true
		}
	}
	return false
}
