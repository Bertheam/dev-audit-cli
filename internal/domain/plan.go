package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"time"
)

const CleanupPlanSchemaVersion = "1.0"

var (
	cleanupPlanIDPattern = regexp.MustCompile(`^plan-sha256-[0-9a-f]{64}$`)
	sha256Pattern        = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type CleanupPlanMode string

const PlanModeSimulationOnly CleanupPlanMode = "SIMULATION_ONLY"

type PlanSelectionMode string

const (
	SelectionDefaultSafe         PlanSelectionMode = "DEFAULT_SAFE"
	SelectionExplicitResourceIDs PlanSelectionMode = "EXPLICIT_RESOURCE_IDS"
)

type PlanSelectedBy string

const (
	SelectedByDefaultSafe PlanSelectedBy = "DEFAULT_SAFE"
	SelectedByManual      PlanSelectedBy = "MANUAL"
)

type PlanExclusionReason string

const (
	ExclusionSensitive              PlanExclusionReason = "SENSITIVE"
	ExclusionUnknownUsage           PlanExclusionReason = "UNKNOWN_USAGE"
	ExclusionUsed                   PlanExclusionReason = "USED"
	ExclusionNotProbableOrphan      PlanExclusionReason = "NOT_PROBABLE_ORPHAN"
	ExclusionNoConservativeEstimate PlanExclusionReason = "NO_CONSERVATIVE_ESTIMATE"
	ExclusionUnsupportedAction      PlanExclusionReason = "UNSUPPORTED_ACTION"
	ExclusionResourceNotFound       PlanExclusionReason = "RESOURCE_NOT_FOUND"
)

type PlanSelection struct {
	Mode                 PlanSelectionMode `json:"mode"`
	RequestedResourceIDs []string          `json:"requested_resource_ids"`
}

type CleanupPlanItem struct {
	ID                           string             `json:"id"`
	ResourceID                   string             `json:"resource_id"`
	Ecosystem                    string             `json:"ecosystem"`
	Component                    string             `json:"component"`
	Path                         string             `json:"path"`
	SelectedBy                   PlanSelectedBy     `json:"selected_by"`
	ObservedSizeBytes            *int64             `json:"observed_size_bytes,omitempty"`
	EstimatedReclaimableBytes    *int64             `json:"estimated_reclaimable_bytes,omitempty"`
	AffectedProjectIDs           []string           `json:"affected_project_ids"`
	RiskCategories               []ResourceCategory `json:"risk_categories"`
	Impact                       string             `json:"impact"`
	OfficialCommand              []string           `json:"official_command"`
	RequiresExplicitConfirmation bool               `json:"requires_explicit_confirmation"`
	Rationale                    string             `json:"rationale"`
	Evidence                     []Evidence         `json:"evidence"`
	Warnings                     []string           `json:"warnings"`
}

type CleanupPlanExclusion struct {
	ResourceID string                `json:"resource_id"`
	Reasons    []PlanExclusionReason `json:"reasons"`
}

type CleanupPlanSummary struct {
	SelectedCount                       int   `json:"selected_count"`
	ExcludedCount                       int   `json:"excluded_count"`
	KnownPotentiallyReclaimableBytesSum int64 `json:"known_potentially_reclaimable_bytes_sum"`
	SelectedItemsWithoutEstimate        int   `json:"selected_items_without_estimate"`
}

type CleanupPlan struct {
	SchemaVersion       string                 `json:"schema_version"`
	ToolVersion         string                 `json:"tool_version"`
	PlanID              string                 `json:"plan_id"`
	Mode                CleanupPlanMode        `json:"mode"`
	SourceReportSHA256  string                 `json:"source_report_sha256"`
	SourceScanCompleted string                 `json:"source_scan_completed_at"`
	Selection           PlanSelection          `json:"selection"`
	Items               []CleanupPlanItem      `json:"items"`
	Excluded            []CleanupPlanExclusion `json:"excluded"`
	Summary             CleanupPlanSummary     `json:"summary"`
	Warnings            []string               `json:"warnings"`
}

func (plan CleanupPlan) Validate() error {
	if plan.SchemaVersion != CleanupPlanSchemaVersion {
		return fmt.Errorf("schema_version must be %q", CleanupPlanSchemaVersion)
	}
	if plan.ToolVersion == "" {
		return errors.New("tool_version is required")
	}
	if !cleanupPlanIDPattern.MatchString(plan.PlanID) {
		return errors.New("plan_id must be a content-addressed SHA-256 identifier")
	}
	expectedPlanID, err := ComputeCleanupPlanID(plan)
	if err != nil {
		return fmt.Errorf("compute plan_id: %w", err)
	}
	if plan.PlanID != expectedPlanID {
		return errors.New("plan_id does not match the plan content")
	}
	if plan.Mode != PlanModeSimulationOnly {
		return errors.New("cleanup plans must be simulation-only")
	}
	if !sha256Pattern.MatchString(plan.SourceReportSHA256) {
		return errors.New("source_report_sha256 must be a lowercase SHA-256 digest")
	}
	if _, err := time.Parse(time.RFC3339, plan.SourceScanCompleted); err != nil {
		return errors.New("source_scan_completed_at must be an RFC3339 timestamp")
	}
	if err := validatePlanSelection(plan.Selection); err != nil {
		return err
	}

	itemResources := make(map[string]struct{}, len(plan.Items))
	itemIDs := make(map[string]struct{}, len(plan.Items))
	knownEstimateSum := int64(0)
	withoutEstimate := 0
	for index, item := range plan.Items {
		if item.ID == "" || item.ResourceID == "" || item.Ecosystem == "" || item.Component == "" || item.Path == "" {
			return fmt.Errorf("items[%d] identity fields are required", index)
		}
		if _, exists := itemIDs[item.ID]; exists {
			return fmt.Errorf("items contains duplicate id %q", item.ID)
		}
		itemIDs[item.ID] = struct{}{}
		if _, exists := itemResources[item.ResourceID]; exists {
			return fmt.Errorf("items contains duplicate resource_id %q", item.ResourceID)
		}
		itemResources[item.ResourceID] = struct{}{}
		if item.SelectedBy != SelectedByDefaultSafe && item.SelectedBy != SelectedByManual {
			return fmt.Errorf("items[%d].selected_by is invalid", index)
		}
		if plan.Selection.Mode == SelectionDefaultSafe && item.SelectedBy != SelectedByDefaultSafe {
			return fmt.Errorf("items[%d] selection does not match default-safe mode", index)
		}
		if plan.Selection.Mode == SelectionExplicitResourceIDs && item.SelectedBy != SelectedByManual {
			return fmt.Errorf("items[%d] selection does not match explicit mode", index)
		}
		if item.ObservedSizeBytes != nil && *item.ObservedSizeBytes < 0 {
			return fmt.Errorf("items[%d].observed_size_bytes must not be negative", index)
		}
		if item.EstimatedReclaimableBytes != nil {
			if *item.EstimatedReclaimableBytes < 0 {
				return fmt.Errorf("items[%d].estimated_reclaimable_bytes must not be negative", index)
			}
			if *item.EstimatedReclaimableBytes > math.MaxInt64-knownEstimateSum {
				return errors.New("known potentially reclaimable estimate sum overflows int64")
			}
			knownEstimateSum += *item.EstimatedReclaimableBytes
		} else {
			withoutEstimate++
		}
		if item.Impact == "" || item.Rationale == "" || len(item.OfficialCommand) == 0 || len(item.Evidence) == 0 {
			return fmt.Errorf("items[%d] must explain impact, command, rationale and evidence", index)
		}
		for _, argument := range item.OfficialCommand {
			if argument == "" {
				return fmt.Errorf("items[%d].official_command contains an empty argument", index)
			}
		}
		if !item.RequiresExplicitConfirmation {
			return fmt.Errorf("items[%d] must require explicit confirmation", index)
		}
		categories, err := validatePlanCategories(item.RiskCategories)
		if err != nil {
			return fmt.Errorf("items[%d]: %w", index, err)
		}
		if item.SelectedBy == SelectedByDefaultSafe {
			if _, ok := categories[CategoryProbableOrphan]; !ok {
				return fmt.Errorf("items[%d] default-safe selection requires ORPHELINE_PROBABLE", index)
			}
			for _, forbidden := range []ResourceCategory{CategorySensitive, CategoryUnknown, CategoryUsed} {
				if _, ok := categories[forbidden]; ok {
					return fmt.Errorf("items[%d] default-safe selection cannot include %s", index, forbidden)
				}
			}
			if item.EstimatedReclaimableBytes == nil {
				return fmt.Errorf("items[%d] default-safe selection requires a conservative estimate", index)
			}
		}
		if err := validateUniqueNonEmptyStrings(item.AffectedProjectIDs, fmt.Sprintf("items[%d].affected_project_ids", index)); err != nil {
			return err
		}
	}

	excludedResources := make(map[string]struct{}, len(plan.Excluded))
	for index, excluded := range plan.Excluded {
		if excluded.ResourceID == "" || len(excluded.Reasons) == 0 {
			return fmt.Errorf("excluded[%d] must identify a resource and at least one reason", index)
		}
		if _, exists := itemResources[excluded.ResourceID]; exists {
			return fmt.Errorf("resource_id %q cannot be both selected and excluded", excluded.ResourceID)
		}
		if _, exists := excludedResources[excluded.ResourceID]; exists {
			return fmt.Errorf("excluded contains duplicate resource_id %q", excluded.ResourceID)
		}
		excludedResources[excluded.ResourceID] = struct{}{}
		seenReasons := make(map[PlanExclusionReason]struct{}, len(excluded.Reasons))
		for _, reason := range excluded.Reasons {
			if !validPlanExclusionReason(reason) {
				return fmt.Errorf("excluded[%d] contains invalid reason %q", index, reason)
			}
			if _, exists := seenReasons[reason]; exists {
				return fmt.Errorf("excluded[%d] contains duplicate reason %q", index, reason)
			}
			seenReasons[reason] = struct{}{}
		}
	}

	if plan.Selection.Mode == SelectionExplicitResourceIDs {
		for _, resourceID := range plan.Selection.RequestedResourceIDs {
			_, selected := itemResources[resourceID]
			_, excluded := excludedResources[resourceID]
			if !selected && !excluded {
				return fmt.Errorf("requested resource_id %q has no plan result", resourceID)
			}
		}
	}
	if plan.Summary.SelectedCount < 0 || plan.Summary.ExcludedCount < 0 ||
		plan.Summary.KnownPotentiallyReclaimableBytesSum < 0 || plan.Summary.SelectedItemsWithoutEstimate < 0 {
		return errors.New("summary values must not be negative")
	}
	if plan.Summary.SelectedCount != len(plan.Items) || plan.Summary.ExcludedCount != len(plan.Excluded) ||
		plan.Summary.KnownPotentiallyReclaimableBytesSum != knownEstimateSum ||
		plan.Summary.SelectedItemsWithoutEstimate != withoutEstimate {
		return errors.New("summary does not match plan items and exclusions")
	}
	return nil
}

// ComputeCleanupPlanID returns the content address of every plan field except
// PlanID itself. Any later change therefore invalidates the identifier.
func ComputeCleanupPlanID(plan CleanupPlan) (string, error) {
	plan.PlanID = ""
	content, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(content)
	return "plan-sha256-" + hex.EncodeToString(digest[:]), nil
}

func validatePlanSelection(selection PlanSelection) error {
	switch selection.Mode {
	case SelectionDefaultSafe:
		if len(selection.RequestedResourceIDs) != 0 {
			return errors.New("default-safe selection cannot contain requested resource IDs")
		}
	case SelectionExplicitResourceIDs:
		if len(selection.RequestedResourceIDs) == 0 {
			return errors.New("explicit selection requires at least one resource ID")
		}
		if err := validateUniqueNonEmptyStrings(selection.RequestedResourceIDs, "selection.requested_resource_ids"); err != nil {
			return err
		}
	default:
		return errors.New("selection.mode is invalid")
	}
	return nil
}

func validatePlanCategories(categories []ResourceCategory) (map[ResourceCategory]struct{}, error) {
	seen := make(map[ResourceCategory]struct{}, len(categories))
	for _, category := range categories {
		if !validResourceCategory(category) {
			return nil, fmt.Errorf("invalid risk category %q", category)
		}
		if _, exists := seen[category]; exists {
			return nil, fmt.Errorf("duplicate risk category %q", category)
		}
		seen[category] = struct{}{}
	}
	return seen, nil
}

func validateUniqueNonEmptyStrings(values []string, field string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("%s contains an empty value", field)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s contains duplicate value %q", field, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validPlanExclusionReason(reason PlanExclusionReason) bool {
	switch reason {
	case ExclusionSensitive, ExclusionUnknownUsage, ExclusionUsed, ExclusionNotProbableOrphan,
		ExclusionNoConservativeEstimate, ExclusionUnsupportedAction, ExclusionResourceNotFound:
		return true
	default:
		return false
	}
}
