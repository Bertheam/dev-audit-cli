package report

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"dev-environment-auditor/internal/domain"
)

// Normalize returns a deep copy with every report collection in stable order.
func Normalize(document domain.ScanDocument) (domain.ScanDocument, error) {
	encoded, err := json.Marshal(document)
	if err != nil {
		return domain.ScanDocument{}, fmt.Errorf("copy document: %w", err)
	}
	var result domain.ScanDocument
	if err := json.Unmarshal(encoded, &result); err != nil {
		return domain.ScanDocument{}, fmt.Errorf("copy document: %w", err)
	}

	sort.Strings(result.Scan.Roots)
	sort.Strings(result.Scan.Exclusions)
	for projectIndex := range result.Projects {
		project := &result.Projects[projectIndex]
		sort.Strings(project.Warnings)
		for requirementIndex := range project.Requirements {
			requirement := &project.Requirements[requirementIndex]
			sort.Strings(requirement.Warnings)
			sortEvidence(requirement.Evidence)
		}
		sort.SliceStable(project.Requirements, func(left, right int) bool {
			return requirementKey(project.Requirements[left]) < requirementKey(project.Requirements[right])
		})
	}
	sort.SliceStable(result.Projects, func(left, right int) bool {
		return result.Projects[left].ID+"\x00"+result.Projects[left].Path <
			result.Projects[right].ID+"\x00"+result.Projects[right].Path
	})

	for resourceIndex := range result.InstalledResources {
		resource := &result.InstalledResources[resourceIndex]
		sort.Strings(resource.Warnings)
		sort.SliceStable(resource.Metadata, func(left, right int) bool {
			return resource.Metadata[left].Key+"\x00"+resource.Metadata[left].Value <
				resource.Metadata[right].Key+"\x00"+resource.Metadata[right].Value
		})
	}
	sort.SliceStable(result.InstalledResources, func(left, right int) bool {
		return resourceKey(result.InstalledResources[left]) < resourceKey(result.InstalledResources[right])
	})

	for relationIndex := range result.Relations {
		relation := &result.Relations[relationIndex]
		sort.Strings(relation.Warnings)
		sortEvidence(relation.Evidence)
	}
	sort.SliceStable(result.Relations, func(left, right int) bool {
		return relationKey(result.Relations[left]) < relationKey(result.Relations[right])
	})
	sort.SliceStable(result.Diagnostics, func(left, right int) bool {
		return diagnosticKey(result.Diagnostics[left]) < diagnosticKey(result.Diagnostics[right])
	})
	return result, nil
}

func requirementKey(requirement domain.Requirement) string {
	return strings.Join([]string{
		requirement.Ecosystem,
		requirement.Component,
		stringValue(requirement.VersionConstraint),
		string(requirement.Confidence),
		requirement.ID,
	}, "\x00")
}

func resourceKey(resource domain.InstalledResource) string {
	return strings.Join([]string{
		resource.Ecosystem,
		resource.Component,
		stringValue(resource.Version),
		resource.Path,
		resource.ID,
	}, "\x00")
}

func diagnosticKey(diagnostic domain.Diagnostic) string {
	return strings.Join([]string{
		diagnostic.Scope,
		diagnostic.Code,
		string(diagnostic.Severity),
		stringValue(diagnostic.Path),
		diagnostic.Message,
	}, "\x00")
}

func relationKey(relation domain.Relation) string {
	return strings.Join([]string{
		relation.ProjectID,
		relation.RequirementID,
		stringValue(relation.ResourceID),
		string(relation.MatchStatus),
		relation.Rationale,
	}, "\x00")
}

func sortEvidence(evidence []domain.Evidence) {
	sort.SliceStable(evidence, func(left, right int) bool {
		return evidenceKey(evidence[left]) < evidenceKey(evidence[right])
	})
}

func evidenceKey(evidence domain.Evidence) string {
	line := 0
	if evidence.LineHint != nil {
		line = *evidence.LineHint
	}
	return fmt.Sprintf("%s\x00%09d\x00%s\x00%s\x00%s\x00%s\x00%s",
		stringValue(evidence.FilePath),
		line,
		evidence.RuleID,
		evidence.SourceType,
		stringValue(evidence.Key),
		stringValue(evidence.ObservedValue),
		strings.Join(evidence.Command, "\x00"),
	)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
