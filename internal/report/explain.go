package report

import (
	"fmt"
	"strings"

	"dev-environment-auditor/internal/domain"
)

// Explain renders the evidence and rationale associated with one project,
// requirement or installed-resource identifier.
func Explain(document domain.ScanDocument, identifier string) ([]byte, bool, error) {
	document, err := Normalize(document)
	if err != nil {
		return nil, false, err
	}
	if err := document.Validate(); err != nil {
		return nil, false, fmt.Errorf("validate scan document: %w", err)
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, false, nil
	}

	for _, project := range document.Projects {
		if project.ID == identifier {
			return explainProject(document, project), true, nil
		}
		for _, requirement := range project.Requirements {
			if requirement.ID == identifier {
				return explainRequirement(document, project, requirement), true, nil
			}
		}
	}
	for _, resource := range document.InstalledResources {
		if resource.ID == identifier {
			return explainResource(document, resource), true, nil
		}
	}
	return nil, false, nil
}

func explainProject(document domain.ScanDocument, project domain.Project) []byte {
	var output strings.Builder
	fmt.Fprintf(&output, "Project %s\n", terminalValue(project.ID))
	fmt.Fprintf(&output, "  path: %s\n", terminalValue(project.Path))
	fmt.Fprintf(&output, "  kind: %s\n", project.Kind)
	fmt.Fprintf(&output, "  requirements (%d):\n", len(project.Requirements))
	for _, requirement := range project.Requirements {
		relation, _ := relationFor(document.Relations, project.ID, requirement.ID, domain.EnvironmentHost)
		dockerRelation, dockerExists := relationFor(document.Relations, project.ID, requirement.ID, domain.EnvironmentDocker)
		fmt.Fprintf(&output, "    - [HOST:%s", relation.MatchStatus)
		if dockerExists {
			fmt.Fprintf(&output, " DOCKER:%s", dockerRelation.MatchStatus)
		}
		fmt.Fprintf(&output, "] %s %s/%s constraint=%s\n",
			terminalValue(requirement.ID),
			terminalValue(requirement.Ecosystem),
			terminalValue(requirement.Component),
			optionalTerminalValue(requirement.VersionConstraint),
		)
	}
	appendWarnings(&output, project.Warnings, "  ")
	return []byte(output.String())
}

func explainRequirement(
	document domain.ScanDocument,
	project domain.Project,
	requirement domain.Requirement,
) []byte {
	var output strings.Builder
	fmt.Fprintf(&output, "Requirement %s\n", terminalValue(requirement.ID))
	fmt.Fprintf(&output, "  project: %s path=%s\n", terminalValue(project.ID), terminalValue(project.Path))
	fmt.Fprintf(&output, "  component: %s/%s\n", terminalValue(requirement.Ecosystem), terminalValue(requirement.Component))
	fmt.Fprintf(&output, "  constraint: %s\n", optionalTerminalValue(requirement.VersionConstraint))
	fmt.Fprintf(&output, "  confidence: %s\n", requirement.Confidence)
	for _, environment := range []domain.ExecutionEnvironment{domain.EnvironmentHost, domain.EnvironmentDocker} {
		relation, exists := relationFor(document.Relations, project.ID, requirement.ID, environment)
		if !exists && environment == domain.EnvironmentDocker {
			continue
		}
		if !exists {
			fmt.Fprintf(&output, "  %s match: UNKNOWN (relation absent from report)\n", environment)
			appendWarnings(&output, requirement.Warnings, "  ")
			appendEvidence(&output, requirement.Evidence, "  ")
			continue
		}
		fmt.Fprintf(&output, "  %s match: %s\n", environment, relation.MatchStatus)
		fmt.Fprintf(&output, "  %s rationale: %s\n", environment, terminalValue(relation.Rationale))
		if relation.ResourceID != nil {
			fmt.Fprintf(&output, "  %s resource: %s\n", environment, terminalValue(*relation.ResourceID))
		}
		appendWarnings(&output, relation.Warnings, "  ")
		appendEvidence(&output, relation.Evidence, "  ")
	}
	return []byte(output.String())
}

func explainResource(document domain.ScanDocument, resource domain.InstalledResource) []byte {
	var output strings.Builder
	fmt.Fprintf(&output, "Installed resource %s\n", terminalValue(resource.ID))
	fmt.Fprintf(&output, "  component: %s/%s\n", terminalValue(resource.Ecosystem), terminalValue(resource.Component))
	fmt.Fprintf(&output, "  version: %s\n", optionalTerminalValue(resource.Version))
	fmt.Fprintf(&output, "  path: %s\n", terminalValue(resource.Path))
	fmt.Fprintf(&output, "  logical size: %s\n", formatSize(resource.SizeBytes))
	fmt.Fprintf(&output, "  reference status: %s\n", resource.ReferenceStatus)
	if resource.ReferenceStatus == domain.NoReferenceFound {
		fmt.Fprintf(&output, "  safety: %s\n", safetyNotice)
	}
	output.WriteString("  metadata:\n")
	if len(resource.Metadata) == 0 {
		output.WriteString("    (none)\n")
	}
	for _, entry := range resource.Metadata {
		fmt.Fprintf(&output, "    - %s=%s\n", terminalValue(entry.Key), terminalValue(entry.Value))
	}
	appendWarnings(&output, resource.Warnings, "  ")

	output.WriteString("  matched relations:\n")
	matched := 0
	for _, relation := range document.Relations {
		if relation.ResourceID == nil || *relation.ResourceID != resource.ID {
			continue
		}
		matched++
		fmt.Fprintf(&output, "    - environment=%s project=%s requirement=%s status=%s\n",
			relationEnvironment(relation), terminalValue(relation.ProjectID), terminalValue(relation.RequirementID), relation.MatchStatus)
		fmt.Fprintf(&output, "      rationale=%s\n", terminalValue(relation.Rationale))
	}
	if matched == 0 {
		output.WriteString("    (none)\n")
	}
	return []byte(output.String())
}

func relationFor(
	relations []domain.Relation,
	projectID string,
	requirementID string,
	environment domain.ExecutionEnvironment,
) (domain.Relation, bool) {
	for _, relation := range relations {
		if relation.ProjectID == projectID && relation.RequirementID == requirementID &&
			relationEnvironment(relation) == environment {
			return relation, true
		}
	}
	return domain.Relation{MatchStatus: domain.MatchUnknown}, false
}

func appendWarnings(output *strings.Builder, warnings []string, indent string) {
	fmt.Fprintf(output, "%swarnings:\n", indent)
	if len(warnings) == 0 {
		fmt.Fprintf(output, "%s  (none)\n", indent)
		return
	}
	for _, warning := range warnings {
		fmt.Fprintf(output, "%s  - %s\n", indent, terminalValue(warning))
	}
}

func appendEvidence(output *strings.Builder, evidence []domain.Evidence, indent string) {
	fmt.Fprintf(output, "%sevidence:\n", indent)
	if len(evidence) == 0 {
		fmt.Fprintf(output, "%s  (none)\n", indent)
		return
	}
	for _, item := range evidence {
		fmt.Fprintf(output, "%s  - rule=%s source=%s", indent, terminalValue(item.RuleID), terminalValue(item.SourceType))
		if item.FilePath != nil {
			fmt.Fprintf(output, " file=%s", terminalValue(*item.FilePath))
		}
		if item.LineHint != nil {
			fmt.Fprintf(output, " line=%d", *item.LineHint)
		}
		if item.Key != nil {
			fmt.Fprintf(output, " key=%s", terminalValue(*item.Key))
		}
		if item.ObservedValue != nil {
			fmt.Fprintf(output, " observed=%s", terminalValue(*item.ObservedValue))
		}
		output.WriteByte('\n')
	}
}
