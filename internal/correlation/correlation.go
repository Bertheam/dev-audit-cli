// Package correlation links statically observed project requirements to
// explicitly inventoried local resources without inferring that an unmatched
// resource is safe to remove.
package correlation

import (
	"fmt"
	"sort"
	"strings"

	"dev-environment-auditor/internal/domain"
)

// InventoryScope identifies a normalized resource type whose configured roots
// were completely inventoried. Omitted scopes are treated as incomplete.
type InventoryScope struct {
	Ecosystem string
	Component string
}

// Coverage records the observations that were complete for this correlation
// run. Project and requirement coverage are intentionally separate from the
// per-resource inventory coverage used to prove MISSING.
type Coverage struct {
	ProjectDiscoveryComplete    bool
	RequirementAnalysisComplete bool
	CompleteInventoryScopes     []InventoryScope
}

type Result struct {
	Projects    []domain.Project
	Resources   []domain.InstalledResource
	Relations   []domain.Relation
	Diagnostics []domain.Diagnostic
}

type versionStrategy int

const (
	strategyExact versionStrategy = iota
	strategyFlutter
	strategyDart
	strategyJDKFeature
	strategyDockerImage
)

type requirementTarget struct {
	ecosystem   string
	component   string
	description string
	strategy    versionStrategy
	environment domain.ExecutionEnvironment
}

type candidateEvaluation int

const (
	candidateNoMatch candidateEvaluation = iota
	candidateMatch
	candidateUnknown
)

// Correlate returns deep-copied, deterministically ordered domain objects. It
// never mutates analyzer or inventory results supplied by callers.
func Correlate(projects []domain.Project, resources []domain.InstalledResource, coverage Coverage) Result {
	result := Result{
		Projects:    cloneProjects(projects),
		Resources:   cloneResources(resources),
		Relations:   []domain.Relation{},
		Diagnostics: []domain.Diagnostic{},
	}
	sortProjects(result.Projects)
	sortResources(result.Resources)

	completeScopes := make(map[string]struct{}, len(coverage.CompleteInventoryScopes))
	for _, scope := range coverage.CompleteInventoryScopes {
		key := resourceKey(scope.Ecosystem, scope.Component)
		if key == "\x00" {
			continue
		}
		completeScopes[key] = struct{}{}
	}

	resourceIndexes := make(map[string][]int)
	for index := range result.Resources {
		resource := &result.Resources[index]
		resource.ReferenceStatus = domain.ReferenceUnknown
		key := resourceKey(resource.Ecosystem, resource.Component)
		resourceIndexes[key] = append(resourceIndexes[key], index)
	}

	referenced := make([]bool, len(result.Resources))
	possiblyReferenced := make([]bool, len(result.Resources))
	for projectIndex := range result.Projects {
		project := &result.Projects[projectIndex]
		for requirementIndex := range project.Requirements {
			requirement := &project.Requirements[requirementIndex]
			relation, definite, possible, diagnostics := correlateRequirement(
				*project,
				*requirement,
				result.Resources,
				resourceIndexes,
				completeScopes,
			)
			result.Relations = append(result.Relations, relation)
			result.Diagnostics = append(result.Diagnostics, diagnostics...)
			for _, index := range definite {
				referenced[index] = true
			}
			for _, index := range possible {
				possiblyReferenced[index] = true
			}
		}
	}

	completeReferenceCoverage := coverage.ProjectDiscoveryComplete && coverage.RequirementAnalysisComplete
	for index := range result.Resources {
		switch {
		case referenced[index]:
			result.Resources[index].ReferenceStatus = domain.Referenced
		case possiblyReferenced[index]:
			result.Resources[index].ReferenceStatus = domain.ReferenceUnknown
		case completeReferenceCoverage && isCorrelatableResource(result.Resources[index]):
			result.Resources[index].ReferenceStatus = domain.NoReferenceFound
		default:
			result.Resources[index].ReferenceStatus = domain.ReferenceUnknown
		}
	}

	sortRelations(result.Relations)
	sortDiagnostics(result.Diagnostics)
	return result
}

func correlateRequirement(
	project domain.Project,
	requirement domain.Requirement,
	resources []domain.InstalledResource,
	resourceIndexes map[string][]int,
	completeScopes map[string]struct{},
) (domain.Relation, []int, []int, []domain.Diagnostic) {
	relation := domain.Relation{
		ProjectID:     project.ID,
		RequirementID: requirement.ID,
		Environment:   domain.EnvironmentHost,
		MatchStatus:   domain.MatchUnknown,
		Rationale:     "the requirement could not be correlated safely",
		Evidence:      cloneEvidence(requirement.Evidence),
		Warnings:      cloneStrings(requirement.Warnings),
	}
	diagnostics := []domain.Diagnostic{}
	target, supported := targetFor(requirement)
	if !supported {
		relation.Rationale = "the requirement component has no Phase 0 inventory mapping"
		relation.Warnings = appendUnique(relation.Warnings,
			"unsupported requirement component; no installed resource was inferred")
		diagnostics = append(diagnostics, correlationDiagnostic(
			"CORRELATION_COMPONENT_NOT_INVENTORIED",
			domain.SeverityInfo,
			project,
			requirement,
			"requirement component has no Phase 0 inventory mapping",
		))
		return relation, nil, nil, diagnostics
	}
	relation.Environment = target.environment

	candidates := append([]int(nil), resourceIndexes[resourceKey(target.ecosystem, target.component)]...)
	if requirement.Confidence != domain.RequiredExplicitly {
		relation.Rationale = fmt.Sprintf(
			"confidence %s does not prove an explicit resource requirement",
			requirement.Confidence,
		)
		relation.Warnings = appendUnique(relation.Warnings,
			"non-explicit requirement left unresolved")
		return relation, nil, candidates, diagnostics
	}
	if requirement.VersionConstraint == nil || strings.TrimSpace(*requirement.VersionConstraint) == "" {
		relation.Rationale = "the explicit requirement has no static version constraint"
		relation.Warnings = appendUnique(relation.Warnings,
			"missing static version constraint; candidate resources remain possible")
		return relation, nil, candidates, diagnostics
	}

	constraint := strings.TrimSpace(*requirement.VersionConstraint)
	evaluator, supportedConstraint := compileEvaluator(target, constraint)
	if !supportedConstraint {
		relation.Rationale = "the static version constraint is outside the supported conservative subset"
		relation.Warnings = appendUnique(relation.Warnings,
			"unsupported version constraint; candidate resources remain possible")
		diagnostics = append(diagnostics, correlationDiagnostic(
			"CORRELATION_CONSTRAINT_UNSUPPORTED",
			domain.SeverityWarning,
			project,
			requirement,
			"version constraint is outside the supported conservative subset",
		))
		return relation, nil, candidates, diagnostics
	}

	matches := []int{}
	unknowns := []int{}
	for _, index := range candidates {
		switch evaluator(resources[index]) {
		case candidateMatch:
			matches = append(matches, index)
		case candidateUnknown:
			unknowns = append(unknowns, index)
		}
	}

	switch {
	case len(matches) == 1 && len(unknowns) == 0:
		resourceID := resources[matches[0]].ID
		relation.ResourceID = &resourceID
		relation.MatchStatus = domain.Matched
		relation.Rationale = fmt.Sprintf(
			"one inventoried %s satisfies the explicit constraint %q",
			target.description,
			constraint,
		)
		return relation, matches, nil, diagnostics
	case len(matches) > 1 || (len(matches) == 1 && len(unknowns) > 0):
		possible := append(append([]int(nil), matches...), unknowns...)
		relation.MatchStatus = domain.Ambiguous
		relation.Rationale = fmt.Sprintf(
			"%d inventoried candidates satisfy or may satisfy the explicit constraint %q",
			len(possible),
			constraint,
		)
		relation.Warnings = appendUnique(relation.Warnings,
			"multiple installed candidates prevent a unique association")
		return relation, nil, possible, diagnostics
	case len(unknowns) > 0:
		relation.Rationale = fmt.Sprintf(
			"%d candidate resources lack enough version metadata to evaluate constraint %q",
			len(unknowns),
			constraint,
		)
		relation.Warnings = appendUnique(relation.Warnings,
			"candidate version metadata is incomplete")
		return relation, nil, unknowns, diagnostics
	default:
		if _, complete := completeScopes[resourceKey(target.ecosystem, target.component)]; complete {
			relation.MatchStatus = domain.Missing
			relation.Rationale = fmt.Sprintf(
				"no inventoried %s satisfies constraint %q in the declared complete inventory scope",
				target.description,
				constraint,
			)
			return relation, nil, nil, diagnostics
		}
		relation.Rationale = fmt.Sprintf(
			"no matching %s was observed, but its inventory scope is incomplete",
			target.description,
		)
		relation.Warnings = appendUnique(relation.Warnings,
			"incomplete inventory coverage prevents a missing-resource conclusion")
		return relation, nil, nil, diagnostics
	}
}

func targetFor(requirement domain.Requirement) (requirementTarget, bool) {
	key := resourceKey(requirement.Ecosystem, requirement.Component)
	switch key {
	case resourceKey("android", "compile_sdk"), resourceKey("android", "android_sdk_platform"):
		return requirementTarget{"android", "android_sdk_platform", "Android SDK platform", strategyExact, domain.EnvironmentHost}, true
	case resourceKey("android", "ndk"):
		return requirementTarget{"android", "ndk", "Android NDK", strategyExact, domain.EnvironmentHost}, true
	case resourceKey("android", "cmake"):
		return requirementTarget{"android", "cmake", "CMake package", strategyExact, domain.EnvironmentHost}, true
	case resourceKey("android", "android_gradle_plugin"):
		return requirementTarget{"android", "android_gradle_plugin", "Android Gradle Plugin", strategyExact, domain.EnvironmentHost}, true
	case resourceKey("flutter", "flutter_sdk"):
		return requirementTarget{"flutter", "flutter_sdk", "Flutter SDK", strategyFlutter, domain.EnvironmentHost}, true
	case resourceKey("dart", "dart_sdk"):
		return requirementTarget{"flutter", "flutter_sdk", "Flutter-bundled Dart SDK", strategyDart, domain.EnvironmentHost}, true
	case resourceKey("gradle", "gradle"):
		return requirementTarget{"gradle", "gradle", "Gradle distribution", strategyExact, domain.EnvironmentHost}, true
	case resourceKey("kotlin", "kotlin_gradle_plugin"):
		return requirementTarget{"kotlin", "kotlin_gradle_plugin", "Kotlin Gradle Plugin", strategyExact, domain.EnvironmentHost}, true
	case resourceKey("java", "jdk"):
		return requirementTarget{"java", "jdk", "JDK", strategyJDKFeature, domain.EnvironmentHost}, true
	case resourceKey("docker", "image"):
		return requirementTarget{"docker", "docker_image", "Docker daemon image", strategyDockerImage, domain.EnvironmentDockerDaemon}, true
	default:
		return requirementTarget{}, false
	}
}

func isCorrelatableResource(resource domain.InstalledResource) bool {
	key := resourceKey(resource.Ecosystem, resource.Component)
	switch key {
	case resourceKey("android", "android_sdk_platform"),
		resourceKey("android", "ndk"),
		resourceKey("android", "cmake"),
		resourceKey("android", "android_gradle_plugin"),
		resourceKey("flutter", "flutter_sdk"),
		resourceKey("gradle", "gradle"),
		resourceKey("kotlin", "kotlin_gradle_plugin"),
		resourceKey("java", "jdk"),
		resourceKey("docker", "docker_image"):
		return true
	default:
		return false
	}
}

func resourceKey(ecosystem, component string) string {
	return strings.ToLower(strings.TrimSpace(ecosystem)) + "\x00" +
		strings.ToLower(strings.TrimSpace(component))
}

func correlationDiagnostic(
	code string,
	severity domain.DiagnosticSeverity,
	project domain.Project,
	requirement domain.Requirement,
	message string,
) domain.Diagnostic {
	path := project.Path
	return domain.Diagnostic{
		Code:     code,
		Severity: severity,
		Scope:    project.ID + "/" + requirement.ID,
		Message:  message,
		Path:     &path,
	}
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func sortProjects(projects []domain.Project) {
	for index := range projects {
		sort.SliceStable(projects[index].Requirements, func(left, right int) bool {
			return requirementSortKey(projects[index].Requirements[left]) <
				requirementSortKey(projects[index].Requirements[right])
		})
	}
	sort.SliceStable(projects, func(left, right int) bool {
		return projects[left].ID+"\x00"+projects[left].Path < projects[right].ID+"\x00"+projects[right].Path
	})
}

func requirementSortKey(requirement domain.Requirement) string {
	constraint := ""
	if requirement.VersionConstraint != nil {
		constraint = *requirement.VersionConstraint
	}
	return strings.Join([]string{
		requirement.Ecosystem,
		requirement.Component,
		constraint,
		string(requirement.Confidence),
		requirement.ID,
	}, "\x00")
}

func sortResources(resources []domain.InstalledResource) {
	sort.SliceStable(resources, func(left, right int) bool {
		return strings.Join([]string{
			resources[left].Ecosystem,
			resources[left].Component,
			pointerValue(resources[left].Version),
			resources[left].Path,
			resources[left].ID,
		}, "\x00") < strings.Join([]string{
			resources[right].Ecosystem,
			resources[right].Component,
			pointerValue(resources[right].Version),
			resources[right].Path,
			resources[right].ID,
		}, "\x00")
	})
}

func sortRelations(relations []domain.Relation) {
	sort.SliceStable(relations, func(left, right int) bool {
		return relations[left].ProjectID+"\x00"+relations[left].RequirementID+"\x00"+string(relations[left].Environment) <
			relations[right].ProjectID+"\x00"+relations[right].RequirementID+"\x00"+string(relations[right].Environment)
	})
}

func sortDiagnostics(diagnostics []domain.Diagnostic) {
	sort.SliceStable(diagnostics, func(left, right int) bool {
		return strings.Join([]string{
			diagnostics[left].Scope,
			diagnostics[left].Code,
			pointerValue(diagnostics[left].Path),
			diagnostics[left].Message,
		}, "\x00") < strings.Join([]string{
			diagnostics[right].Scope,
			diagnostics[right].Code,
			pointerValue(diagnostics[right].Path),
			diagnostics[right].Message,
		}, "\x00")
	})
}

func cloneProjects(projects []domain.Project) []domain.Project {
	result := make([]domain.Project, len(projects))
	for index, project := range projects {
		result[index] = project
		result[index].LastModifiedAt = cloneString(project.LastModifiedAt)
		result[index].Warnings = cloneStrings(project.Warnings)
		result[index].Requirements = make([]domain.Requirement, len(project.Requirements))
		for requirementIndex, requirement := range project.Requirements {
			result[index].Requirements[requirementIndex] = requirement
			result[index].Requirements[requirementIndex].VersionConstraint = cloneString(requirement.VersionConstraint)
			result[index].Requirements[requirementIndex].Evidence = cloneEvidence(requirement.Evidence)
			result[index].Requirements[requirementIndex].Warnings = cloneStrings(requirement.Warnings)
		}
	}
	return result
}

func cloneResources(resources []domain.InstalledResource) []domain.InstalledResource {
	result := make([]domain.InstalledResource, len(resources))
	for index, resource := range resources {
		result[index] = resource
		result[index].Version = cloneString(resource.Version)
		result[index].SizeBytes = cloneInt64(resource.SizeBytes)
		result[index].Metadata = cloneMetadata(resource.Metadata)
		result[index].Warnings = cloneStrings(resource.Warnings)
	}
	return result
}

func cloneEvidence(evidence []domain.Evidence) []domain.Evidence {
	result := make([]domain.Evidence, len(evidence))
	for index, item := range evidence {
		result[index] = item
		result[index].FilePath = cloneString(item.FilePath)
		result[index].Key = cloneString(item.Key)
		result[index].LineHint = cloneInt(item.LineHint)
		result[index].ObservedValue = cloneString(item.ObservedValue)
		result[index].Command = cloneStrings(item.Command)
	}
	return result
}

func cloneStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func cloneMetadata(metadata []domain.MetadataEntry) []domain.MetadataEntry {
	result := make([]domain.MetadataEntry, len(metadata))
	copy(result, metadata)
	return result
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
