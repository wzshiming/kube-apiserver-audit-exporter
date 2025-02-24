package exporter

type Pod struct {
	Kind     string   `json:"kind"`
	Metadata Metadata `json:"metadata"`
	Spec     PodSpec  `json:"spec"`
}

type Metadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}
type PodSpec struct {
	NodeName string `json:"nodeName"`
}
