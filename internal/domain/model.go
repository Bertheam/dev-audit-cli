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
	ProjectDocker  ProjectKind = "DOCKER"
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

type ResourceCategory string

const (
	CategoryUsed            ResourceCategory = "UTILISEE"
	CategoryReconstructible ResourceCategory = "RECONSTRUCTIBLE"
	CategoryOld             ResourceCategory = "ANCIENNE"
	CategoryProbableOrphan  ResourceCategory = "ORPHELINE_PROBABLE"
	CategorySensitive       ResourceCategory = "SENSIBLE"
	CategoryUnknown         ResourceCategory = "INCONNUE"
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
	EnvironmentHost         ExecutionEnvironment = "HOST"
	EnvironmentDocker       ExecutionEnvironment = "DOCKER"
	EnvironmentDockerDaemon ExecutionEnvironment = "DOCKER_DAEMON"
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

type ResourceClassification struct {
	Category  ResourceCategory `json:"category"`
	Rationale string           `json:"rationale"`
	Evidence  []Evidence       `json:"evidence"`
}

type SpaceEstimate struct {
	Bytes     int64      `json:"bytes"`
	Rationale string     `json:"rationale"`
	Evidence  []Evidence `json:"evidence"`
}

type InstalledResource struct {
	ID                     string                   `json:"id"`
	Ecosystem              string                   `json:"ecosystem"`
	Component              string                   `json:"component"`
	Version                *string                  `json:"version,omitempty"`
	Path                   string                   `json:"path"`
	SizeBytes              *int64                   `json:"size_bytes,omitempty"`
	PotentiallyReclaimable *SpaceEstimate           `json:"potentially_reclaimable,omitempty"`
	ReferenceStatus        ReferenceStatus          `json:"reference_status"`
	Classifications        []ResourceClassification `json:"classifications,omitempty"`
	Metadata               []MetadataEntry          `json:"metadata"`
	Warnings               []string                 `json:"warnings"`
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
	StartedAt            string                `json:"started_at"`
	CompletedAt          string                `json:"completed_at"`
	Roots                []string              `json:"roots"`
	Exclusions           []string              `json:"exclusions"`
	ReadOnly             bool                  `json:"read_only"`
	ClassificationPolicy *ClassificationPolicy `json:"classification_policy,omitempty"`
}

type ClassificationPolicy struct {
	OldAfterDays int `json:"old_after_days"`
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
	if document.Scan.ClassificationPolicy != nil && document.Scan.ClassificationPolicy.OldAfterDays <= 0 {
		return errors.New("classification_policy.old_after_days must be greater than zero")
	}
	for resourceIndex, resource := range document.InstalledResources {
		if resource.PotentiallyReclaimable != nil {
			if resource.PotentiallyReclaimable.Bytes < 0 {
				return fmt.Errorf("installed_resources[%d].potentially_reclaimable.bytes must not be negative", resourceIndex)
			}
			if resource.PotentiallyReclaimable.Rationale == "" {
				return fmt.Errorf("installed_resources[%d].potentially_reclaimable.rationale is required", resourceIndex)
			}
			if len(resource.PotentiallyReclaimable.Evidence) == 0 {
				return fmt.Errorf("installed_resources[%d].potentially_reclaimable.evidence is required", resourceIndex)
			}
			if resource.SizeBytes != nil && resource.PotentiallyReclaimable.Bytes > *resource.SizeBytes {
				return fmt.Errorf("installed_resources[%d].potentially_reclaimable.bytes exceeds size_bytes", resourceIndex)
			}
		}
		seenCategories := make(map[ResourceCategory]struct{}, len(resource.Classifications))
		for classificationIndex, classification := range resource.Classifications {
			if !validResourceCategory(classification.Category) {
				return fmt.Errorf("installed_resources[%d].classifications[%d].category is invalid", resourceIndex, classificationIndex)
			}
			if classification.Rationale == "" {
				return fmt.Errorf("installed_resources[%d].classifications[%d].rationale is required", resourceIndex, classificationIndex)
			}
			if len(classification.Evidence) == 0 {
				return fmt.Errorf("installed_resources[%d].classifications[%d].evidence is required", resourceIndex, classificationIndex)
			}
			if _, exists := seenCategories[classification.Category]; exists {
				return fmt.Errorf("installed_resources[%d].classifications contains duplicate category %q", resourceIndex, classification.Category)
			}
			seenCategories[classification.Category] = struct{}{}
		}
		if _, used := seenCategories[CategoryUsed]; used {
			if _, unknown := seenCategories[CategoryUnknown]; unknown {
				return fmt.Errorf("installed_resources[%d] cannot be both UTILISEE and INCONNUE", resourceIndex)
			}
		}
		if _, old := seenCategories[CategoryOld]; old && document.Scan.ClassificationPolicy == nil {
			return fmt.Errorf("installed_resources[%d].ANCIENNE requires classification_policy", resourceIndex)
		}
		if _, orphan := seenCategories[CategoryProbableOrphan]; orphan {
			if resource.ReferenceStatus != NoReferenceFound {
				return fmt.Errorf("installed_resources[%d].ORPHELINE_PROBABLE requires NO_REFERENCE_FOUND", resourceIndex)
			}
			if _, reconstructible := seenCategories[CategoryReconstructible]; !reconstructible {
				return fmt.Errorf("installed_resources[%d].ORPHELINE_PROBABLE requires RECONSTRUCTIBLE", resourceIndex)
			}
			if _, old := seenCategories[CategoryOld]; !old {
				return fmt.Errorf("installed_resources[%d].ORPHELINE_PROBABLE requires ANCIENNE", resourceIndex)
			}
			if _, sensitive := seenCategories[CategorySensitive]; sensitive {
				return fmt.Errorf("installed_resources[%d] cannot be both ORPHELINE_PROBABLE and SENSIBLE", resourceIndex)
			}
			if _, used := seenCategories[CategoryUsed]; used {
				return fmt.Errorf("installed_resources[%d] cannot be both ORPHELINE_PROBABLE and UTILISEE", resourceIndex)
			}
			if _, unknown := seenCategories[CategoryUnknown]; unknown {
				return fmt.Errorf("installed_resources[%d] cannot be both ORPHELINE_PROBABLE and INCONNUE", resourceIndex)
			}
		}
		if resource.PotentiallyReclaimable != nil {
			if _, sensitive := seenCategories[CategorySensitive]; sensitive {
				return fmt.Errorf("installed_resources[%d].SENSIBLE cannot expose potentially reclaimable space", resourceIndex)
			}
		}
	}
	for index, relation := range document.Relations {
		environment := relation.Environment
		if environment == "" {
			environment = EnvironmentHost
		}
		if environment != EnvironmentHost && environment != EnvironmentDocker && environment != EnvironmentDockerDaemon {
			return fmt.Errorf("relations[%d].environment is invalid", index)
		}
		if environment == EnvironmentDocker && relation.ResourceID != nil {
			return fmt.Errorf("relations[%d]: Docker declarations cannot reference installed HOST resources", index)
		}
	}
	return nil
}

func validResourceCategory(category ResourceCategory) bool {
	switch category {
	case CategoryUsed, CategoryReconstructible, CategoryOld, CategoryProbableOrphan, CategorySensitive, CategoryUnknown:
		return true
	default:
		return false
	}
}
