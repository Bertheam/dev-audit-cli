package report

import (
	"fmt"
	"strconv"
	"strings"

	"dev-environment-auditor/internal/domain"
)

const safetyNotice = "NO_REFERENCE_FOUND means no reference was found in the analyzed coverage; it never means safe to delete."

// RenderTerminal returns a deterministic, escape-safe human-readable report.
func RenderTerminal(document domain.ScanDocument) ([]byte, error) {
	document, err := Normalize(document)
	if err != nil {
		return nil, err
	}
	if err := document.Validate(); err != nil {
		return nil, fmt.Errorf("validate scan document: %w", err)
	}

	relations := make(map[string]domain.Relation, len(document.Relations))
	resources := make(map[string]domain.InstalledResource, len(document.InstalledResources))
	for _, relation := range document.Relations {
		relations[relationMapKey(relation.ProjectID, relation.RequirementID, relationEnvironment(relation))] = relation
	}
	for _, resource := range document.InstalledResources {
		resources[resource.ID] = resource
	}

	var output strings.Builder
	fmt.Fprintf(&output, "Dev Environment Auditor %s\n", terminalValue(document.ToolVersion))
	fmt.Fprintf(&output, "Read-only scan: %s -> %s\n", terminalValue(document.Scan.StartedAt), terminalValue(document.Scan.CompletedAt))
	fmt.Fprintf(&output, "Roots (%d):\n", len(document.Scan.Roots))
	for _, root := range document.Scan.Roots {
		fmt.Fprintf(&output, "  - %s\n", terminalValue(root))
	}

	requirementCount := 0
	matchCounts := map[domain.ExecutionEnvironment]map[domain.MatchStatus]int{
		domain.EnvironmentHost:   {},
		domain.EnvironmentDocker: {},
	}
	resourceCounts := map[domain.ReferenceStatus]int{}
	for _, project := range document.Projects {
		requirementCount += len(project.Requirements)
	}
	for _, relation := range document.Relations {
		matchCounts[relationEnvironment(relation)][relation.MatchStatus]++
	}
	for _, resource := range document.InstalledResources {
		resourceCounts[resource.ReferenceStatus]++
	}
	fmt.Fprintf(&output,
		"Summary: %d projects, %d requirements, %d resources, %d diagnostics\n",
		len(document.Projects), requirementCount, len(document.InstalledResources), len(document.Diagnostics),
	)
	writeRelationCounts(&output, domain.EnvironmentHost, matchCounts[domain.EnvironmentHost])
	writeRelationCounts(&output, domain.EnvironmentDocker, matchCounts[domain.EnvironmentDocker])
	fmt.Fprintf(&output,
		"Resources: REFERENCED=%d NO_REFERENCE_FOUND=%d UNKNOWN=%d\n",
		resourceCounts[domain.Referenced], resourceCounts[domain.NoReferenceFound],
		resourceCounts[domain.ReferenceUnknown],
	)

	fmt.Fprintf(&output, "\nProjects (%d):\n", len(document.Projects))
	if len(document.Projects) == 0 {
		output.WriteString("  (none)\n")
	}
	for _, project := range document.Projects {
		fmt.Fprintf(&output, "- [%s] %s (id=%s)\n", project.Kind, terminalValue(project.Path), terminalValue(project.ID))
		if len(project.Requirements) == 0 {
			output.WriteString("    requirements: none observed\n")
			continue
		}
		for _, requirement := range project.Requirements {
			relation, exists := relations[relationMapKey(project.ID, requirement.ID, domain.EnvironmentHost)]
			if !exists {
				relation.MatchStatus = domain.MatchUnknown
			}
			dockerRelation, dockerExists := relations[relationMapKey(project.ID, requirement.ID, domain.EnvironmentDocker)]
			fmt.Fprintf(&output, "    - [HOST:%s", relation.MatchStatus)
			if dockerExists {
				fmt.Fprintf(&output, " DOCKER:%s", dockerRelation.MatchStatus)
			}
			fmt.Fprintf(&output, "] %s/%s constraint=%s confidence=%s",
				terminalValue(requirement.Ecosystem),
				terminalValue(requirement.Component),
				optionalTerminalValue(requirement.VersionConstraint),
				requirement.Confidence,
			)
			if relation.ResourceID != nil {
				if resource, exists := resources[*relation.ResourceID]; exists {
					fmt.Fprintf(&output, " -> %s", terminalValue(resource.Path))
				} else {
					fmt.Fprintf(&output, " -> resource=%s", terminalValue(*relation.ResourceID))
				}
			}
			output.WriteByte('\n')
		}
	}

	fmt.Fprintf(&output, "\nLocal inventory resources (%d):\n", len(document.InstalledResources))
	if len(document.InstalledResources) == 0 {
		output.WriteString("  (none)\n")
	}
	for _, resource := range document.InstalledResources {
		fmt.Fprintf(&output, "- [%s] %s/%s version=%s size=%s\n",
			resource.ReferenceStatus,
			terminalValue(resource.Ecosystem),
			terminalValue(resource.Component),
			optionalTerminalValue(resource.Version),
			formatSize(resource.SizeBytes),
		)
		fmt.Fprintf(&output, "    path=%s id=%s\n", terminalValue(resource.Path), terminalValue(resource.ID))
	}

	fmt.Fprintf(&output, "\nDiagnostics (%d):\n", len(document.Diagnostics))
	if len(document.Diagnostics) == 0 {
		output.WriteString("  (none)\n")
	}
	for _, diagnostic := range document.Diagnostics {
		fmt.Fprintf(&output, "- [%s] %s scope=%s: %s",
			diagnostic.Severity,
			terminalValue(diagnostic.Code),
			terminalValue(diagnostic.Scope),
			terminalValue(diagnostic.Message),
		)
		if diagnostic.Path != nil {
			fmt.Fprintf(&output, " path=%s", terminalValue(*diagnostic.Path))
		}
		output.WriteByte('\n')
	}

	fmt.Fprintf(&output, "\nSafety: %s\n", safetyNotice)
	return []byte(output.String()), nil
}

func writeRelationCounts(
	output *strings.Builder,
	environment domain.ExecutionEnvironment,
	counts map[domain.MatchStatus]int,
) {
	fmt.Fprintf(output,
		"Relations %s: MATCHED=%d MISSING=%d AMBIGUOUS=%d UNKNOWN=%d\n",
		environment,
		counts[domain.Matched], counts[domain.Missing],
		counts[domain.Ambiguous], counts[domain.MatchUnknown],
	)
}

func relationMapKey(projectID, requirementID string, environment domain.ExecutionEnvironment) string {
	return projectID + "\x00" + requirementID + "\x00" + string(environment)
}

func terminalValue(value string) string {
	return strconv.QuoteToASCII(value)
}

func optionalTerminalValue(value *string) string {
	if value == nil {
		return "unknown"
	}
	return terminalValue(*value)
}

func formatSize(size *int64) string {
	if size == nil {
		return "unknown"
	}
	const unit = int64(1024)
	if *size < unit {
		return fmt.Sprintf("%d B", *size)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	value := float64(*size)
	unitIndex := -1
	for value >= float64(unit) && unitIndex < len(units)-1 {
		value /= float64(unit)
		unitIndex++
	}
	return fmt.Sprintf("%.1f %s", value, units[unitIndex])
}
