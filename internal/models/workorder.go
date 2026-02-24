package models

type WorkOrder struct {
	Title              string   `yaml:"title"`
	TargetModule       string   `yaml:"target_module"`
	ReferenceModule    string   `yaml:"reference_module"`
	Type               string   `yaml:"type"`
	KnownFiles         []string `yaml:"known_files"`
	AcceptanceCriteria []string `yaml:"acceptance_criteria"`
}
