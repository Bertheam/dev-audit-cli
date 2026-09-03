// Package domain defines the evidence-first data contract for Phase 0.
package domain

import (
	"errors"
	"fmt"
)

const SchemaVersion = "1.0"

type ProjectKind string

const (
	ProjectFlutter ProjectKind = "FLUTTER"
	ProjectAndroid ProjectKind = "ANDROID"
	ProjectHybrid  ProjectKind = "FLUTTER_ANDROID"
)

type Confidence string

const (
	RequiredExplicitly Confidence = "REQUIRED_EXPLICITLY"
	ProbablyRequired   Confidence = "PROBABLY_REQUIRED"
	ConfidenceUnknown  Confidence = "UNKNOWN"
)

type ReferenceStatus string

const (
	Referenced       ReferenceStatus = "REFERENCED"
	NoReferenceFound ReferenceStatus = "NO_REFERENCE_FOUND"
	ReferenceUnknown ReferenceStatus = "UNKNOWN"
)

type MatchStatus string

const (
	Matched      MatchStatus = "MATCHED"
	Missing      MatchStatus = "MISSING"
	Ambiguous    MatchStatus = "AMBIGUOUS"
	MatchUnknown MatchStatus = "UNKNOWN"
)

type ExecutionEnvironment string

const (
	EnvironmentHost   ExecutionEnvironment = "HOST"
	EnvironmentDocker ExecutionEnvironment = "DOCKER"
)

type DiagnosticSeverity string

const (
	SeverityInfo    DiagnosticSeverity = "INFO"
	SeverityWarning DiagnosticSeverity = "WARNING"
	SeverityError   DiagnosticSeverity = "ERROR"
)

type Evidence struct {
	SourceType    string   `json:"source_type"`
	FilePath      *string  `json:"file_path,omitempty"`
	Key           *string  `json:"key,omitempty"`
	LineHint      *int     `json:"line_hint,omitempty"`
	Command       []string `json:"command,omitempty"`
	ObservedValue *string  `json:"observed_value,omitempty"`
	RuleID        string   `json:"rule_id"`
}

type Requirement struct {
	ID                string     `json:"id"`
	Ecosystem         string     `json:"ecosystem"`
	Component         string     `json:"component"`
	VersionConstraint *string    `json:"version_constraint,omitempty"`
	Confidence        Confidence `json:"confidence"`
	Evidence          []Evidence `json:"evidence"`
	Warnings          []string   `json:"warnings"`
}

type Project struct {
	ID             string        `json:"id"`
	Path           string        `json:"path"`
	Kind           ProjectKind   `json:"kind"`
	LastModifiedAt *string       `json:"last_modified_at,omitempty"`
	Requirements   []Requirement `json:"requirements"`
	Warnings       []string      `json:"warnings"`
}

type MetadataEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type InstalledResource struct {
	ID              string          `json:"id"`
	Ecosystem       string          `json:"ecosystem"`
	Component       string          `json:"component"`
	Version         *string         `json:"version,omitempty"`
	Path            string          `json:"path"`
	SizeBytes       *int64          `json:"size_bytes,omitempty"`
	ReferenceStatus ReferenceStatus `json:"reference_status"`
	Metadata        []MetadataEntry `json:"metadata"`
	Warnings        []string        `json:"warnings"`
}

type Relation struct {
	ProjectID     string `json:"project_id"`
	RequirementID string `json:"requirement_id"`
	// Environment is optional in JSON so reports emitted before environment
	// awareness remain readable. An omitted value means HOST.
	Environment ExecutionEnvironment `json:"environment,omitempty"`
	ResourceID  *string              `json:"resource_id,omitempty"`
	MatchStatus MatchStatus          `json:"match_status"`
	Rationale   string               `json:"rationale"`
	Evidence    []Evidence           `json:"evidence"`
	Warnings    []string             `json:"warnings"`
}

type Diagnostic struct {
	Code     string             `json:"code"`
	Severity DiagnosticSeverity `json:"severity"`
	Scope    string             `json:"scope"`
	Message  string             `json:"message"`
	Path     *string            `json:"path,omitempty"`
}

type ScanMetadata struct {
	StartedAt   string   `json:"started_at"`
	CompletedAt string   `json:"completed_at"`
	Roots       []string `json:"roots"`
	Exclusions  []string `json:"exclusions"`
	ReadOnly    bool     `json:"read_only"`
}

type ScanDocument struct {
	SchemaVersion      string              `json:"schema_version"`
	ToolVersion        string              `json:"tool_version"`
	Scan               ScanMetadata        `json:"scan"`
	Projects           []Project           `json:"projects"`
	InstalledResources []InstalledResource `json:"installed_resources"`
	Relations          []Relation          `json:"relations"`
	Diagnostics        []Diagnostic        `json:"diagnostics"`
}

func (document ScanDocument) Validate() error {
	if document.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %q", SchemaVersion)
	}
	if document.ToolVersion == "" {
		return errors.New("tool_version is required")
	}
	if !document.Scan.ReadOnly {
		return errors.New("Phase 0 scans must be read-only")
	}
	if len(document.Scan.Roots) == 0 {
		return errors.New("at least one scan root is required")
	}
	if document.Scan.StartedAt == "" || document.Scan.CompletedAt == "" {
		return errors.New("scan timestamps are required")
	}
	for index, relation := range document.Relations {
		environment := relation.Environment
		if environment == "" {
			environment = EnvironmentHost
		}
		if environment != EnvironmentHost && environment != EnvironmentDocker {
			return fmt.Errorf("relations[%d].environment is invalid", index)
		}
		if environment == EnvironmentDocker && relation.ResourceID != nil {
			return fmt.Errorf("relations[%d]: Docker declarations cannot reference installed HOST resources", index)
		}
	}
	return nil
}
