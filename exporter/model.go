package exporter

import (
	"time"
)

type Pod struct {
	Kind     string   `json:"kind"`
	Metadata Metadata `json:"metadata"`
	Spec     PodSpec  `json:"spec"`
}

type Metadata struct {
	UID               string    `json:"uid"`
	CreationTimestamp time.Time `json:"creationTimestamp"`
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
}

type PodSpec struct {
	NodeName string `json:"nodeName"`
}

type BatchJob struct {
	Metadata Metadata       `json:"metadata"`
	Status   BatchJobStatus `json:"status"`
}

type BatchJobStatus struct {
	// For batch job
	CompletionTime time.Time `json:"completionTime"`

	// for volcano batch job
	State VolcanoBatchJobState `json:"state"`
}

type VolcanoBatchJobState struct {
	Phase string `json:"phase"`
}

// IsCompleted checks if job has a Complete condition
func (b BatchJobStatus) IsCompleted() bool {
	if b.State.Phase == "Completed" {
		return true
	}

	if !b.CompletionTime.IsZero() {
		return true
	}
	return false
}
