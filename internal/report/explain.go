package report

import (
	"fmt"
	"strings"

	"dev-environment-auditor/internal/domain"
)

// Explain renders the evidence and rationale associated with one project,
// requirement or installed-resource identifier.
func Explain(document domain.ScanDocument, identifier string) ([]byte, bool, error) {
	return ExplainWithOptions(document, identifier, TerminalOptions{})
}

// ExplainWithOptions renders one report item using the human-first terminal
// presentation. Options affect presentation only, never report semantics.
func ExplainWithOptions(document domain.ScanDocument, identifier string, options TerminalOptions) ([]byte, bool, error) {
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
	style := newTerminalStyle(options)

	for _, project := range document.Projects {
		if project.ID == identifier {
			return explainProject(document, project, style), true, nil
		}
		for _, requirement := range project.Requirements {
			if requirement.ID == identifier {
				return explainRequirement(document, project, requirement, style), true, nil
			}
		}
	}
	for _, resource := range document.InstalledResources {
		if resource.ID == identifier {
			return explainResource(document, resource, style), true, nil
		}
	}
	return nil, false, nil
}

func explainProject(document domain.ScanDocument, project domain.Project, style terminalStyle) []byte {
	var output strings.Builder
	writeExplainHeader(&output, style, "PROJECT", project.ID)

	output.WriteString(style.section("DETAILS"))
	fmt.Fprintf(&output, "  Path  %s\n", terminalText(project.Path))
	fmt.Fprintf(&output, "  Kind  %s\n", project.Kind)

	output.WriteString(style.section(fmt.Sprintf("REQUIREMENTS  %d", len(project.Requirements))))
	if len(project.Requirements) == 0 {
		fmt.Fprintf(&output, "  %s No requirements found\n", style.muted("—"))
	}
	for _, requirement := range project.Requirements {
		relations := explanationRelationsForRequirement(document.Relations, project.ID, requirement.ID)
		fmt.Fprintf(&output, "  %s %s/%s · %s\n",
			relationSetSymbol(style, relations), terminalText(requirement.Ecosystem),
			terminalText(requirement.Component), optionalTerminalText(requirement.VersionConstraint))
		fmt.Fprintf(&output, "    %s · ", style.muted(terminalText(requirement.ID)))
		for index, relation := range relations {
			if index > 0 {
				output.WriteString("  ")
			}
			fmt.Fprintf(&output, "%s:%s", relationEnvironment(relation), relation.MatchStatus)
		}
		output.WriteByte('\n')
	}
	appendWarnings(&output, style, project.Warnings)
	writeExplainFooter(&output, style)
	return []byte(output.String())
}

func explainRequirement(
	document domain.ScanDocument,
	project domain.Project,
	requirement domain.Requirement,
	style terminalStyle,
) []byte {
	var output strings.Builder
	writeExplainHeader(&output, style, "REQUIREMENT", requirement.ID)

	output.WriteString(style.section("DETAILS"))
	fmt.Fprintf(&output, "  Project     %s\n", terminalText(project.ID))
	fmt.Fprintf(&output, "  Path        %s\n", terminalText(project.Path))
	fmt.Fprintf(&output, "  Component   %s/%s\n", terminalText(requirement.Ecosystem), terminalText(requirement.Component))
	fmt.Fprintf(&output, "  Constraint  %s\n", optionalTerminalText(requirement.VersionConstraint))
	fmt.Fprintf(&output, "  Confidence  %s\n", requirement.Confidence)

	foundRelation := false
	output.WriteString(style.section("MATCHES"))
	for _, environment := range []domain.ExecutionEnvironment{
		domain.EnvironmentHost,
		domain.EnvironmentDocker,
		domain.EnvironmentDockerDaemon,
	} {
		relation, exists := relationFor(document.Relations, project.ID, requirement.ID, environment)
		if !exists {
			continue
		}
		foundRelation = true
		fmt.Fprintf(&output, "  %s %s  %s\n", statusSymbol(style, string(relation.MatchStatus)), environment, relation.MatchStatus)
		fmt.Fprintf(&output, "    %s\n", terminalText(relation.Rationale))
		if relation.ResourceID != nil {
			fmt.Fprintf(&output, "    Resource  %s\n", style.muted(terminalText(*relation.ResourceID)))
		}
		appendInlineWarnings(&output, style, relation.Warnings)
		appendEvidence(&output, style, relation.Evidence)
	}
	if !foundRelation {
		fmt.Fprintf(&output, "  %s HOST  UNKNOWN\n", statusSymbol(style, "UNKNOWN"))
		output.WriteString("    Relation absent from report\n")
		appendInlineWarnings(&output, style, requirement.Warnings)
		appendEvidence(&output, style, requirement.Evidence)
	}
	writeExplainFooter(&output, style)
	return []byte(output.String())
}

func explanationRelationsForRequirement(
	relations []domain.Relation,
	projectID string,
	requirementID string,
) []domain.Relation {
	result := []domain.Relation{}
	for _, environment := range []domain.ExecutionEnvironment{
		domain.EnvironmentHost,
		domain.EnvironmentDocker,
		domain.EnvironmentDockerDaemon,
	} {
		if relation, exists := relationFor(relations, projectID, requirementID, environment); exists {
			result = append(result, relation)
		}
	}
	if len(result) == 0 {
		result = append(result, domain.Relation{
			Environment: domain.EnvironmentHost,
			MatchStatus: domain.MatchUnknown,
		})
	}
	return result
}

func explainResource(document domain.ScanDocument, resource domain.InstalledResource, style terminalStyle) []byte {
	var output strings.Builder
	writeExplainHeader(&output, style, "RESOURCE", resource.ID)

	output.WriteString(style.section("DETAILS"))
	fmt.Fprintf(&output, "  Component    %s/%s\n", terminalText(resource.Ecosystem), terminalText(resource.Component))
	fmt.Fprintf(&output, "  Version      %s\n", optionalTerminalText(resource.Version))
	fmt.Fprintf(&output, "  Path         %s\n", terminalText(resource.Path))
	fmt.Fprintf(&output, "  Size         %s observed\n", formatSize(resource.SizeBytes))
	fmt.Fprintf(&output, "  Reclaimable  %s potential\n", formatSpaceEstimate(resource.PotentiallyReclaimable))
	fmt.Fprintf(&output, "  Reference    %s %s\n", statusSymbol(style, string(resource.ReferenceStatus)), resource.ReferenceStatus)
	if resource.PotentiallyReclaimable != nil {
		fmt.Fprintf(&output, "  Estimate     %s\n", terminalText(resource.PotentiallyReclaimable.Rationale))
		appendEvidence(&output, style, resource.PotentiallyReclaimable.Evidence)
	}

	output.WriteString(style.section(fmt.Sprintf("CLASSIFICATIONS  %d", len(resource.Classifications))))
	if len(resource.Classifications) == 0 {
		fmt.Fprintf(&output, "  %s Not recorded in this report\n", style.muted("—"))
	}
	for _, classification := range resource.Classifications {
		fmt.Fprintf(&output, "  %s %s\n", statusSymbol(style, string(classification.Category)), classification.Category)
		fmt.Fprintf(&output, "    %s\n", terminalText(classification.Rationale))
		appendEvidence(&output, style, classification.Evidence)
	}

	output.WriteString(style.section(fmt.Sprintf("MATCHED RELATIONS  %d", resourceRelationCount(document.Relations, resource.ID))))
	matched := 0
	for _, relation := range document.Relations {
		if relation.ResourceID == nil || *relation.ResourceID != resource.ID {
			continue
		}
		matched++
		fmt.Fprintf(&output, "  %s %s · project %s · requirement %s\n",
			statusSymbol(style, string(relation.MatchStatus)), relationEnvironment(relation),
			terminalText(relation.ProjectID), terminalText(relation.RequirementID))
		fmt.Fprintf(&output, "    %s\n", terminalText(relation.Rationale))
	}
	if matched == 0 {
		fmt.Fprintf(&output, "  %s None\n", style.muted("—"))
	}

	output.WriteString(style.section(fmt.Sprintf("METADATA  %d", len(resource.Metadata))))
	if len(resource.Metadata) == 0 {
		fmt.Fprintf(&output, "  %s None\n", style.muted("—"))
	}
	for _, entry := range resource.Metadata {
		fmt.Fprintf(&output, "  %s  %s\n", terminalText(entry.Key), terminalText(entry.Value))
	}
	appendWarnings(&output, style, resource.Warnings)
	writeExplainFooter(&output, style)
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

func writeExplainHeader(output *strings.Builder, style terminalStyle, kind, identifier string) {
	fmt.Fprintf(output, "%s  %s\n", style.title("◆ dev-audit"), style.heading("explain"))
	fmt.Fprintf(output, "  %s · %s\n", style.accent(kind), style.muted(terminalText(identifier)))
}

func writeExplainFooter(output *strings.Builder, style terminalStyle) {
	writeSafetyNotice(output, style)
}

func appendWarnings(output *strings.Builder, style terminalStyle, warnings []string) {
	output.WriteString(style.section(fmt.Sprintf("WARNINGS  %d", len(warnings))))
	if len(warnings) == 0 {
		fmt.Fprintf(output, "  %s None\n", statusSymbol(style, "OK"))
		return
	}
	for _, warning := range warnings {
		fmt.Fprintf(output, "  %s %s\n", style.warning("!"), terminalText(warning))
	}
}

func appendInlineWarnings(output *strings.Builder, style terminalStyle, warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintf(output, "    %s %s\n", style.warning("!"), terminalText(warning))
	}
}

func appendEvidence(output *strings.Builder, style terminalStyle, evidence []domain.Evidence) {
	if len(evidence) == 0 {
		return
	}
	fmt.Fprintf(output, "    %s\n", style.heading("Evidence"))
	for _, item := range evidence {
		fmt.Fprintf(output, "      %s %s · %s", style.muted("•"), terminalText(item.RuleID), terminalText(item.SourceType))
		if item.FilePath != nil {
			fmt.Fprintf(output, " · %s", terminalText(*item.FilePath))
		}
		if item.LineHint != nil {
			fmt.Fprintf(output, ":%d", *item.LineHint)
		}
		if item.Key != nil {
			fmt.Fprintf(output, " · %s", terminalText(*item.Key))
		}
		if item.ObservedValue != nil {
			fmt.Fprintf(output, "=%s", terminalText(*item.ObservedValue))
		}
		if len(item.Command) > 0 {
			fmt.Fprintf(output, " · %s", terminalText(strings.Join(item.Command, " ")))
		}
		output.WriteByte('\n')
	}
}

func resourceRelationCount(relations []domain.Relation, resourceID string) int {
	count := 0
	for _, relation := range relations {
		if relation.ResourceID != nil && *relation.ResourceID == resourceID {
			count++
		}
	}
	return count
}
