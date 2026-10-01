package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"dev-environment-auditor/internal/domain"
)

// RenderTerminal returns a deterministic, escape-safe, plain-text report.
func RenderTerminal(document domain.ScanDocument) ([]byte, error) {
	return RenderTerminalWithOptions(document, TerminalOptions{})
}

// RenderTerminalWithOptions returns a human-first terminal report while
// preserving deterministic ordering and an ANSI-free plain mode.
func RenderTerminalWithOptions(document domain.ScanDocument, options TerminalOptions) ([]byte, error) {
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
	style := newTerminalStyle(options)

	requirementCount := 0
	matchCounts := map[domain.ExecutionEnvironment]map[domain.MatchStatus]int{
		domain.EnvironmentHost:         {},
		domain.EnvironmentDocker:       {},
		domain.EnvironmentDockerDaemon: {},
	}
	resourceCounts := map[domain.ReferenceStatus]int{}
	classificationCounts := map[domain.ResourceCategory]int{}
	for _, project := range document.Projects {
		requirementCount += len(project.Requirements)
	}
	for _, relation := range document.Relations {
		matchCounts[relationEnvironment(relation)][relation.MatchStatus]++
	}
	for _, resource := range document.InstalledResources {
		resourceCounts[resource.ReferenceStatus]++
		for _, classification := range resource.Classifications {
			classificationCounts[classification.Category]++
		}
	}

	var output strings.Builder
	fmt.Fprintf(&output, "%s  %s\n", style.title("◆ dev-audit"), style.heading("scan"))
	fmt.Fprintf(&output, "  %s · %s · %s\n",
		style.success("READ ONLY"), style.muted(document.ToolVersion), style.muted(scanElapsed(document.Scan.StartedAt, document.Scan.CompletedAt)))
	fmt.Fprintf(&output, "  Scope: %s", plural(len(document.Scan.Roots), "root", "roots"))
	if document.Scan.ClassificationPolicy != nil {
		fmt.Fprintf(&output, " · old after %d days", document.Scan.ClassificationPolicy.OldAfterDays)
	}
	output.WriteByte('\n')

	output.WriteString(style.section("OVERVIEW"))
	fmt.Fprintf(&output, "  %s · %s · %s · %s\n",
		plural(len(document.Projects), "project", "projects"),
		plural(requirementCount, "requirement", "requirements"),
		plural(len(document.InstalledResources), "resource", "resources"),
		plural(len(document.Diagnostics), "diagnostic", "diagnostics"),
	)
	totalMatches := sumMatchStatus(matchCounts, domain.Matched)
	totalMissing := sumMatchStatus(matchCounts, domain.Missing)
	totalAmbiguous := sumMatchStatus(matchCounts, domain.Ambiguous)
	totalUnknown := sumMatchStatus(matchCounts, domain.MatchUnknown)
	fmt.Fprintf(&output, "  Relations  %s %d matched  %s %d missing  %s %d ambiguous  %s %d unknown\n",
		statusSymbol(style, "MATCHED"), totalMatches,
		statusSymbol(style, "MISSING"), totalMissing,
		statusSymbol(style, "AMBIGUOUS"), totalAmbiguous,
		statusSymbol(style, "UNKNOWN"), totalUnknown,
	)
	fmt.Fprintf(&output, "  Resources  %s %d used  %s %d unknown  %s %d sensitive  %s %d probable orphan\n",
		statusSymbol(style, "UTILISEE"), classificationCounts[domain.CategoryUsed],
		statusSymbol(style, "INCONNUE"), classificationCounts[domain.CategoryUnknown],
		statusSymbol(style, "SENSIBLE"), classificationCounts[domain.CategorySensitive],
		statusSymbol(style, "ORPHELINE_PROBABLE"), classificationCounts[domain.CategoryProbableOrphan],
	)

	output.WriteString(style.section("ENVIRONMENTS"))
	writeEnvironmentSummary(&output, style, domain.EnvironmentHost, matchCounts[domain.EnvironmentHost])
	writeEnvironmentSummary(&output, style, domain.EnvironmentDocker, matchCounts[domain.EnvironmentDocker])
	writeEnvironmentSummary(&output, style, domain.EnvironmentDockerDaemon, matchCounts[domain.EnvironmentDockerDaemon])
	fmt.Fprintf(&output, "  Resources        %s %d referenced  %s %d no reference  %s %d unknown\n",
		statusSymbol(style, "REFERENCED"), resourceCounts[domain.Referenced],
		statusSymbol(style, "ORPHELINE_PROBABLE"), resourceCounts[domain.NoReferenceFound],
		statusSymbol(style, "UNKNOWN"), resourceCounts[domain.ReferenceUnknown])

	if options.Verbose {
		output.WriteString(style.section(fmt.Sprintf("ROOTS  %d", len(document.Scan.Roots))))
		for _, root := range document.Scan.Roots {
			fmt.Fprintf(&output, "  %s %s\n", style.muted("•"), terminalText(root))
		}
	}

	output.WriteString(style.section(fmt.Sprintf("PROJECTS  %d", len(document.Projects))))
	if len(document.Projects) == 0 {
		fmt.Fprintf(&output, "  %s No projects found\n", style.muted("—"))
	}
	visibleProjects := len(document.Projects)
	if !options.Verbose && visibleProjects > 12 {
		visibleProjects = 12
	}
	for _, project := range document.Projects[:visibleProjects] {
		matched, missing, ambiguous, unknown := projectRelationCounts(project, relations)
		projectStatus := "MATCHED"
		if missing > 0 {
			projectStatus = "MISSING"
		} else if ambiguous > 0 {
			projectStatus = "AMBIGUOUS"
		} else if unknown > 0 {
			projectStatus = "UNKNOWN"
		}
		fmt.Fprintf(&output, "  %s %s\n", statusSymbol(style, projectStatus), terminalText(project.Path))
		fmt.Fprintf(&output, "    %s · %s · %d matched · %d missing · %d unknown",
			project.Kind, plural(len(project.Requirements), "requirement", "requirements"), matched, missing, unknown)
		if ambiguous > 0 {
			fmt.Fprintf(&output, " · %d ambiguous", ambiguous)
		}
		if options.Verbose {
			fmt.Fprintf(&output, " · %s", style.muted(terminalText(project.ID)))
		}
		output.WriteByte('\n')
		if len(project.Requirements) == 0 {
			continue
		}
		if !options.Verbose {
			continue
		}
		for _, requirement := range project.Requirements {
			requirementRelations := reportRelationsForRequirement(relations, project.ID, requirement.ID)
			fmt.Fprintf(&output, "    %s ", relationSetSymbol(style, requirementRelations))
			for index, relation := range requirementRelations {
				if index > 0 {
					output.WriteString("  ")
				}
				fmt.Fprintf(&output, "%s:%s", relationEnvironment(relation), relation.MatchStatus)
			}
			fmt.Fprintf(&output, " · %s/%s · %s · %s · %s",
				terminalText(requirement.Ecosystem),
				terminalText(requirement.Component),
				optionalTerminalText(requirement.VersionConstraint),
				requirement.Confidence,
				style.muted(terminalText(requirement.ID)),
			)
			for _, relation := range requirementRelations {
				if relation.ResourceID != nil {
					if resource, exists := resources[*relation.ResourceID]; exists {
						fmt.Fprintf(&output, "\n      %s %s", style.muted("→"), terminalText(resource.Path))
					} else {
						fmt.Fprintf(&output, "\n      %s %s", style.muted("→"), terminalText(*relation.ResourceID))
					}
					break
				}
			}
			output.WriteByte('\n')
		}
	}
	if visibleProjects < len(document.Projects) {
		fmt.Fprintf(&output, "  %s %d more projects hidden · use --verbose to show all\n",
			style.muted("…"), len(document.Projects)-visibleProjects)
	}

	writeDockerVolumes(&output, style, document.InstalledResources)

	orderedResources := append([]domain.InstalledResource(nil), document.InstalledResources...)
	sort.SliceStable(orderedResources, func(left, right int) bool {
		leftSize := int64(-1)
		rightSize := int64(-1)
		if orderedResources[left].SizeBytes != nil {
			leftSize = *orderedResources[left].SizeBytes
		}
		if orderedResources[right].SizeBytes != nil {
			rightSize = *orderedResources[right].SizeBytes
		}
		if leftSize != rightSize {
			return leftSize > rightSize
		}
		return orderedResources[left].ID < orderedResources[right].ID
	})
	visibleResources := len(orderedResources)
	if !options.Verbose && visibleResources > 12 {
		visibleResources = 12
	}
	output.WriteString(style.section(fmt.Sprintf("LARGEST RESOURCES  %d", len(document.InstalledResources))))
	if len(document.InstalledResources) == 0 {
		fmt.Fprintf(&output, "  %s No local resources found\n", style.muted("—"))
	}
	for _, resource := range orderedResources[:visibleResources] {
		fmt.Fprintf(&output, "  %s %-10s %s/%s · %s\n",
			resourceSymbol(style, resource), formatSize(resource.SizeBytes),
			terminalText(resource.Ecosystem), terminalText(resource.Component), optionalTerminalText(resource.Version))
		fmt.Fprintf(&output, "    %s\n", terminalText(resource.Path))
		fmt.Fprintf(&output, "    %s · %s · %s", formatCategories(resource.Classifications), resource.ReferenceStatus, style.muted(terminalText(resource.ID)))
		if resource.PotentiallyReclaimable != nil {
			fmt.Fprintf(&output, " · reclaimable %s", formatSpaceEstimate(resource.PotentiallyReclaimable))
		}
		output.WriteByte('\n')
	}
	if visibleResources < len(orderedResources) {
		fmt.Fprintf(&output, "  %s %d more resources hidden · use --verbose to show all\n",
			style.muted("…"), len(orderedResources)-visibleResources)
	}

	diagnostics := document.Diagnostics
	if !options.Verbose {
		diagnostics = diagnosticsWithAttention(document.Diagnostics)
	}
	output.WriteString(style.section(fmt.Sprintf("DIAGNOSTICS  %d", len(document.Diagnostics))))
	if len(diagnostics) == 0 {
		fmt.Fprintf(&output, "  %s No warnings or errors", statusSymbol(style, "OK"))
		if len(document.Diagnostics) > 0 {
			fmt.Fprintf(&output, " · %d informational hidden", len(document.Diagnostics))
		}
		output.WriteByte('\n')
	}
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(&output, "  %s %s · %s\n", statusSymbol(style, string(diagnostic.Severity)), terminalText(diagnostic.Code), terminalText(diagnostic.Message))
		fmt.Fprintf(&output, "    scope %s", terminalText(diagnostic.Scope))
		if diagnostic.Path != nil {
			fmt.Fprintf(&output, " · %s", terminalText(*diagnostic.Path))
		}
		output.WriteByte('\n')
	}

	output.WriteString(style.section("NEXT"))
	output.WriteString("  Export evidence   dev-audit scan --format json --output audit.json\n")
	output.WriteString("  Inspect an item   dev-audit explain --report audit.json <id>\n")
	if !options.Verbose {
		output.WriteString("  Show everything   dev-audit scan --verbose\n")
	}

	writeSafetyNotice(&output, style)
	return []byte(output.String()), nil
}

func writeDockerVolumes(output *strings.Builder, style terminalStyle, resources []domain.InstalledResource) {
	type volumeGroup struct {
		name      string
		resources []domain.InstalledResource
		size      int64
	}
	groupsByName := map[string]*volumeGroup{}
	volumeCount := 0
	for _, resource := range resources {
		if resource.Ecosystem != "docker" || resource.Component != "docker_volume" {
			continue
		}
		volumeCount++
		groupName := reportMetadata(resource.Metadata, "compose_project")
		if groupName == "" {
			if reportMetadata(resource.Metadata, "anonymous") == "true" {
				groupName = "anonymous"
			} else {
				groupName = "unassigned"
			}
		}
		group := groupsByName[groupName]
		if group == nil {
			group = &volumeGroup{name: groupName}
			groupsByName[groupName] = group
		}
		group.resources = append(group.resources, resource)
		if resource.SizeBytes != nil {
			group.size += *resource.SizeBytes
		}
	}
	if volumeCount == 0 {
		return
	}
	groups := make([]*volumeGroup, 0, len(groupsByName))
	for _, group := range groupsByName {
		sort.Slice(group.resources, func(left, right int) bool {
			return group.resources[left].Path < group.resources[right].Path
		})
		groups = append(groups, group)
	}
	sort.Slice(groups, func(left, right int) bool { return groups[left].name < groups[right].name })
	output.WriteString(style.section(fmt.Sprintf("DOCKER VOLUMES  %d", volumeCount)))
	for _, group := range groups {
		fmt.Fprintf(output, "  %s · %s · %s\n", terminalText(group.name),
			plural(len(group.resources), "volume", "volumes"), formatSize(&group.size))
		for _, resource := range group.resources {
			role := reportMetadata(resource.Metadata, "volume_role")
			if role == "" {
				role = strings.TrimPrefix(resource.Path, "docker://volume/")
			}
			usage := "unused"
			if reportMetadata(resource.Metadata, "in_use") == "true" {
				usage = "active"
			}
			kind := reportMetadata(resource.Metadata, "storage_kind")
			fmt.Fprintf(output, "    %s %s · %s · %s · %s\n", resourceSymbol(style, resource),
				terminalText(role), usage, terminalText(kind), formatSize(resource.SizeBytes))
		}
	}
}

func reportMetadata(metadata []domain.MetadataEntry, key string) string {
	for _, item := range metadata {
		if item.Key == key {
			return item.Value
		}
	}
	return ""
}

func scanElapsed(startedAt, completedAt string) string {
	started, startErr := time.Parse(time.RFC3339Nano, startedAt)
	completed, completeErr := time.Parse(time.RFC3339Nano, completedAt)
	if startErr != nil || completeErr != nil || completed.Before(started) {
		return "completed"
	}
	duration := completed.Sub(started).Round(time.Millisecond)
	if duration < time.Second {
		return fmt.Sprintf("completed in %dms", duration.Milliseconds())
	}
	return fmt.Sprintf("completed in %.1fs", duration.Seconds())
}

func sumMatchStatus(counts map[domain.ExecutionEnvironment]map[domain.MatchStatus]int, status domain.MatchStatus) int {
	total := 0
	for _, byStatus := range counts {
		total += byStatus[status]
	}
	return total
}

func writeEnvironmentSummary(output *strings.Builder, style terminalStyle, environment domain.ExecutionEnvironment, counts map[domain.MatchStatus]int) {
	fmt.Fprintf(output, "  %-16s %s %d matched  %s %d missing  %s %d ambiguous  %s %d unknown\n",
		environment,
		statusSymbol(style, "MATCHED"), counts[domain.Matched],
		statusSymbol(style, "MISSING"), counts[domain.Missing],
		statusSymbol(style, "AMBIGUOUS"), counts[domain.Ambiguous],
		statusSymbol(style, "UNKNOWN"), counts[domain.MatchUnknown])
}

func projectRelationCounts(project domain.Project, relations map[string]domain.Relation) (matched, missing, ambiguous, unknown int) {
	for _, requirement := range project.Requirements {
		for _, relation := range reportRelationsForRequirement(relations, project.ID, requirement.ID) {
			switch relation.MatchStatus {
			case domain.Matched:
				matched++
			case domain.Missing:
				missing++
			case domain.Ambiguous:
				ambiguous++
			default:
				unknown++
			}
		}
	}
	return matched, missing, ambiguous, unknown
}

func relationSetSymbol(style terminalStyle, relations []domain.Relation) string {
	status := "MATCHED"
	for _, relation := range relations {
		if relation.MatchStatus == domain.Missing {
			return statusSymbol(style, "MISSING")
		}
		if relation.MatchStatus == domain.Ambiguous {
			status = "AMBIGUOUS"
		} else if relation.MatchStatus == domain.MatchUnknown && status == "MATCHED" {
			status = "UNKNOWN"
		}
	}
	return statusSymbol(style, status)
}

func resourceSymbol(style terminalStyle, resource domain.InstalledResource) string {
	for _, category := range []domain.ResourceCategory{domain.CategorySensitive, domain.CategoryUnknown, domain.CategoryProbableOrphan, domain.CategoryUsed} {
		for _, classification := range resource.Classifications {
			if classification.Category == category {
				return statusSymbol(style, string(category))
			}
		}
	}
	return statusSymbol(style, string(resource.ReferenceStatus))
}

func diagnosticsWithAttention(diagnostics []domain.Diagnostic) []domain.Diagnostic {
	result := make([]domain.Diagnostic, 0)
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == domain.SeverityWarning || diagnostic.Severity == domain.SeverityError {
			result = append(result, diagnostic)
		}
	}
	return result
}

func reportRelationsForRequirement(
	relations map[string]domain.Relation,
	projectID string,
	requirementID string,
) []domain.Relation {
	result := []domain.Relation{}
	for _, environment := range []domain.ExecutionEnvironment{
		domain.EnvironmentHost,
		domain.EnvironmentDocker,
		domain.EnvironmentDockerDaemon,
	} {
		if relation, exists := relations[relationMapKey(projectID, requirementID, environment)]; exists {
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

func relationMapKey(projectID, requirementID string, environment domain.ExecutionEnvironment) string {
	return projectID + "\x00" + requirementID + "\x00" + string(environment)
}

// terminalText escapes control characters without surrounding ordinary values
// with quotes. It is intended for the human-first terminal renderer only.
func terminalText(value string) string {
	quoted := strconv.QuoteToASCII(value)
	if len(quoted) >= 2 {
		return quoted[1 : len(quoted)-1]
	}
	return quoted
}

func optionalTerminalText(value *string) string {
	if value == nil {
		return "unknown"
	}
	return terminalText(*value)
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

func writeSafetyNotice(output *strings.Builder, style terminalStyle) {
	output.WriteString(style.section("SAFETY"))
	fmt.Fprintf(output, "  %s\n", style.muted("NO_REFERENCE_FOUND means no reference was found in the analyzed coverage."))
	fmt.Fprintf(output, "  %s\n", style.muted("It never means safe to delete. Classifications and reclaimable space are observations, not cleanup recommendations."))
}

func formatSpaceEstimate(estimate *domain.SpaceEstimate) string {
	if estimate == nil {
		return "unknown"
	}
	return formatSize(&estimate.Bytes)
}

func formatCategories(classifications []domain.ResourceClassification) string {
	if len(classifications) == 0 {
		return "not-recorded"
	}
	categories := make([]string, 0, len(classifications))
	for _, classification := range classifications {
		categories = append(categories, string(classification.Category))
	}
	return strings.Join(categories, ",")
}
