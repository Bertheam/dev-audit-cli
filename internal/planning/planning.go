// Package planning turns a validated read-only scan into a deterministic,
// simulation-only cleanup plan. It never executes the commands it describes.
package planning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"dev-environment-auditor/internal/domain"
)

var (
	dockerObjectPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:+-]{0,255}$`)
	androidPackagePattern = regexp.MustCompile(`^[A-Za-z0-9._+:-]+(?:;[A-Za-z0-9._+:-]+)+$`)
	avdNamePattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	safeVersionPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+*-]{0,79}$`)
	gradleCacheKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)
)

type Config struct {
	ToolVersion        string
	SourceReportSHA256 string
	ResourceIDs        []string
}

type cleanupAction struct {
	Command  []string
	Impact   string
	Warnings []string
}

// Build creates a stable plan tied to the exact bytes of a source report.
// An empty ResourceIDs list uses the conservative default policy; otherwise
// only the explicitly named resources are considered.
func Build(document domain.ScanDocument, config Config) (domain.CleanupPlan, error) {
	if err := document.Validate(); err != nil {
		return domain.CleanupPlan{}, fmt.Errorf("validate source scan: %w", err)
	}
	if strings.TrimSpace(config.ToolVersion) == "" {
		return domain.CleanupPlan{}, errors.New("tool version is required")
	}
	if !isLowerSHA256(config.SourceReportSHA256) {
		return domain.CleanupPlan{}, errors.New("source report SHA-256 must contain 64 lowercase hexadecimal characters")
	}

	requested := sortedUnique(config.ResourceIDs)
	selectionMode := domain.SelectionDefaultSafe
	if len(requested) > 0 {
		selectionMode = domain.SelectionExplicitResourceIDs
	}
	plan := domain.CleanupPlan{
		SchemaVersion:       domain.CleanupPlanSchemaVersion,
		ToolVersion:         strings.TrimSpace(config.ToolVersion),
		Mode:                domain.PlanModeSimulationOnly,
		SourceReportSHA256:  config.SourceReportSHA256,
		SourceScanCompleted: document.Scan.CompletedAt,
		Selection: domain.PlanSelection{
			Mode:                 selectionMode,
			RequestedResourceIDs: requested,
		},
		Items:    []domain.CleanupPlanItem{},
		Excluded: []domain.CleanupPlanExclusion{},
		Warnings: []string{
			"Simulation only: no command in this plan was executed.",
			"Targeted commands are stored as argument arrays for review and are not shell scripts.",
			"The reclaimable total is a sum of known estimates and is not a guaranteed post-cleanup disk gain.",
		},
	}

	resources := make(map[string]domain.InstalledResource, len(document.InstalledResources))
	for _, resource := range document.InstalledResources {
		resources[resource.ID] = resource
	}
	affectedProjects := projectsByResource(document.Relations)
	if selectionMode == domain.SelectionExplicitResourceIDs {
		for _, resourceID := range requested {
			resource, found := resources[resourceID]
			if !found {
				plan.Excluded = append(plan.Excluded, domain.CleanupPlanExclusion{
					ResourceID: resourceID,
					Reasons:    []domain.PlanExclusionReason{domain.ExclusionResourceNotFound},
				})
				continue
			}
			action, supported := actionForResource(resource)
			if !supported {
				plan.Excluded = append(plan.Excluded, domain.CleanupPlanExclusion{
					ResourceID: resource.ID,
					Reasons:    []domain.PlanExclusionReason{domain.ExclusionUnsupportedAction},
				})
				continue
			}
			plan.Items = append(plan.Items, planItem(resource, affectedProjects[resource.ID], domain.SelectedByManual, action))
		}
	} else {
		orderedResources := append([]domain.InstalledResource(nil), document.InstalledResources...)
		sort.SliceStable(orderedResources, func(left, right int) bool {
			return orderedResources[left].ID < orderedResources[right].ID
		})
		for _, resource := range orderedResources {
			action, supported := actionForResource(resource)
			reasons := defaultExclusionReasons(resource, supported)
			if len(reasons) > 0 {
				plan.Excluded = append(plan.Excluded, domain.CleanupPlanExclusion{
					ResourceID: resource.ID,
					Reasons:    reasons,
				})
				continue
			}
			plan.Items = append(plan.Items, planItem(resource, affectedProjects[resource.ID], domain.SelectedByDefaultSafe, action))
		}
	}

	normalizePlan(&plan)
	plan.Summary = summarize(plan)
	planID, err := domain.ComputeCleanupPlanID(plan)
	if err != nil {
		return domain.CleanupPlan{}, fmt.Errorf("compute cleanup plan ID: %w", err)
	}
	plan.PlanID = planID
	if err := plan.Validate(); err != nil {
		return domain.CleanupPlan{}, fmt.Errorf("validate cleanup plan: %w", err)
	}
	return plan, nil
}

func defaultExclusionReasons(resource domain.InstalledResource, supported bool) []domain.PlanExclusionReason {
	categories := categorySet(resource.Classifications)
	reasons := []domain.PlanExclusionReason{}
	if _, ok := categories[domain.CategorySensitive]; ok {
		reasons = append(reasons, domain.ExclusionSensitive)
	}
	if _, ok := categories[domain.CategoryUnknown]; ok {
		reasons = append(reasons, domain.ExclusionUnknownUsage)
	}
	if _, ok := categories[domain.CategoryUsed]; ok {
		reasons = append(reasons, domain.ExclusionUsed)
	}
	if _, ok := categories[domain.CategoryProbableOrphan]; !ok {
		reasons = append(reasons, domain.ExclusionNotProbableOrphan)
	}
	if resource.PotentiallyReclaimable == nil {
		reasons = append(reasons, domain.ExclusionNoConservativeEstimate)
	}
	if !supported {
		reasons = append(reasons, domain.ExclusionUnsupportedAction)
	}
	return reasons
}

func planItem(
	resource domain.InstalledResource,
	projectIDs []string,
	selectedBy domain.PlanSelectedBy,
	action cleanupAction,
) domain.CleanupPlanItem {
	categories := make([]domain.ResourceCategory, 0, len(resource.Classifications))
	evidence := []domain.Evidence{selectionEvidence(resource.ID)}
	for _, classification := range resource.Classifications {
		categories = append(categories, classification.Category)
		evidence = append(evidence, classification.Evidence...)
	}
	var estimate *int64
	if resource.PotentiallyReclaimable != nil {
		value := resource.PotentiallyReclaimable.Bytes
		estimate = &value
		evidence = append(evidence, resource.PotentiallyReclaimable.Evidence...)
	}
	observedSize := cloneInt64(resource.SizeBytes)
	warnings := append([]string(nil), resource.Warnings...)
	warnings = append(warnings, action.Warnings...)
	if selectedBy == domain.SelectedByManual {
		if len(categories) == 0 {
			warnings = append(warnings, "The source report has no classification for this resource; usage risk remains unknown.")
		}
		if containsCategory(categories, domain.CategorySensitive) {
			warnings = append(warnings, "Manual selection includes a sensitive resource and may cause permanent data loss if executed in the future.")
		}
		if containsCategory(categories, domain.CategoryUnknown) {
			warnings = append(warnings, "Manual selection includes a resource whose usage is unknown.")
		}
		if containsCategory(categories, domain.CategoryUsed) {
			warnings = append(warnings, "Manual selection includes a resource observed as used by an analyzed project.")
		}
	}
	rationale := "Selected by the conservative default policy because the resource is a probable orphan with a conservative estimate and a supported official command."
	if selectedBy == domain.SelectedByManual {
		rationale = "Included only because its resource ID was explicitly selected; this simulation is not authorization to execute the command."
	}
	return domain.CleanupPlanItem{
		ID:                           stableItemID(resource.ID, action.Command),
		ResourceID:                   resource.ID,
		Ecosystem:                    resource.Ecosystem,
		Component:                    resource.Component,
		Path:                         resource.Path,
		SelectedBy:                   selectedBy,
		ObservedSizeBytes:            observedSize,
		EstimatedReclaimableBytes:    estimate,
		AffectedProjectIDs:           append([]string{}, projectIDs...),
		RiskCategories:               categories,
		Impact:                       action.Impact,
		OfficialCommand:              append([]string(nil), action.Command...),
		RequiresExplicitConfirmation: true,
		Rationale:                    rationale,
		Evidence:                     evidence,
		Warnings:                     warnings,
	}
}

func actionForResource(resource domain.InstalledResource) (cleanupAction, bool) {
	management, managementOK := uniqueMetadata(resource.Metadata, "management")
	switch {
	case resource.Ecosystem == "docker" && resource.Component == "docker_build_cache" && managementOK && management == "docker buildx":
		objectID, ok := dockerObjectID(resource.Path, "build_cache", "build-cache")
		if !ok {
			return cleanupAction{}, false
		}
		return cleanupAction{
			Command: []string{"docker", "buildx", "prune", "--filter", "id=" + objectID},
			Impact:  "Removes one BuildKit cache record; a future build may need to rebuild layers or download dependencies again.",
		}, true
	case resource.Ecosystem == "docker" && resource.Component == "docker_image" && managementOK && management == "docker image":
		objectID, ok := dockerObjectID(resource.Path, "image")
		if !ok {
			return cleanupAction{}, false
		}
		return cleanupAction{
			Command:  []string{"docker", "image", "rm", objectID},
			Impact:   "Removes the local image reference and unshared layers when Docker permits it; the image may need to be pulled or rebuilt again.",
			Warnings: []string{"Docker image layers may be shared, so observed image size is not a guaranteed reclaimable amount."},
		}, true
	case resource.Ecosystem == "docker" && resource.Component == "docker_container" && managementOK && management == "docker container":
		objectID, ok := dockerObjectID(resource.Path, "container")
		if !ok {
			return cleanupAction{}, false
		}
		return cleanupAction{
			Command: []string{"docker", "container", "rm", objectID},
			Impact:  "Removes the container and its writable layer; mutable data stored only in that layer can be permanently lost, while named volumes are not requested for removal.",
		}, true
	case resource.Ecosystem == "android" && managementOK && (management == "android sdk" || management == "sdkmanager"):
		packagePath, ok := uniqueMetadata(resource.Metadata, "package_path")
		if !ok || !androidPackagePattern.MatchString(packagePath) {
			return cleanupAction{}, false
		}
		command, ok := androidRemovalCommand(resource, management, packagePath)
		if !ok {
			return cleanupAction{}, false
		}
		return cleanupAction{
			Command: command,
			Impact:  "Uninstalls this Android SDK package; affected builds may fail until the same package is installed again.",
		}, true
	case resource.Ecosystem == "android" && resource.Component == "android_avd" && managementOK && management == "avdmanager":
		name, ok := uniqueMetadata(resource.Metadata, "avd_name")
		if !ok || !avdNamePattern.MatchString(name) {
			return cleanupAction{}, false
		}
		return cleanupAction{
			Command:  []string{"avdmanager", "delete", "avd", "-n", name},
			Impact:   "Deletes this Android Virtual Device and its mutable emulator data; the loss can be permanent.",
			Warnings: []string{"avdmanager is deprecated by current Android documentation; the command is retained because it is the manager recorded by the scan."},
		}, true
	case resource.Ecosystem == "gradle" && resource.Component == "gradle" &&
		managementOK && management == "filesystem":
		if !isTargetedGradleWrapperCache(resource) {
			return cleanupAction{}, false
		}
		return cleanupAction{
			Command: []string{"/bin/rm", "-R", resource.Path},
			Impact:  "Removes one exact Gradle Wrapper distribution cache; a project that needs this version will download it again on its next build.",
			Warnings: []string{
				"This is a permanent targeted filesystem deletion, not a Gradle package-manager operation.",
				"Stop Gradle builds and daemons that may be using this distribution before executing the command.",
			},
		}, true
	case resource.Ecosystem == "apple" && resource.Component == "xcode_derived_data" &&
		managementOK && management == "filesystem":
		if !isTargetedXcodeDerivedData(resource) {
			return cleanupAction{}, false
		}
		return cleanupAction{
			Command: []string{"/bin/rm", "-R", resource.Path},
			Impact:  "Removes one exact Xcode DerivedData entry; build products, indexes and module caches in it will be regenerated.",
			Warnings: []string{
				"This is a permanent targeted filesystem deletion, not an Xcode project clean operation.",
				"Close Xcode and stop xcodebuild processes before executing the command.",
			},
		}, true
	default:
		return cleanupAction{}, false
	}
}

func androidRemovalCommand(resource domain.InstalledResource, management, packagePath string) ([]string, bool) {
	executable := ""
	if managerPath, ok := uniqueMetadata(resource.Metadata, "manager_path"); ok {
		sdkRoot, rootOK := uniqueMetadata(resource.Metadata, "sdk_root")
		executableName := "android"
		if management == "sdkmanager" {
			executableName = "sdkmanager"
		}
		if !rootOK || !isAndroidManagerPath(sdkRoot, managerPath, executableName) {
			return nil, false
		}
		executable = managerPath
	} else if management == "android sdk" {
		executable = "android"
	} else {
		executable = "sdkmanager"
	}
	if management == "android sdk" {
		return []string{executable, "sdk", "remove", packagePath}, true
	}
	return []string{executable, "--uninstall", packagePath}, true
}

func isAndroidManagerPath(sdkRoot, managerPath, executableName string) bool {
	if !isCleanAbsolutePath(sdkRoot) || !isCleanAbsolutePath(managerPath) || filepath.Base(managerPath) != executableName {
		return false
	}
	relative, err := filepath.Rel(sdkRoot, managerPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if executableName == "android" {
		return len(parts) == 4 && parts[0] == "cmdline-tools" && safeVersionPattern.MatchString(parts[1]) && parts[2] == "bin"
	}
	return (len(parts) == 4 && parts[0] == "cmdline-tools" && safeVersionPattern.MatchString(parts[1]) && parts[2] == "bin") ||
		(len(parts) == 3 && parts[0] == "tools" && parts[1] == "bin")
}

func isTargetedGradleWrapperCache(resource domain.InstalledResource) bool {
	source, sourceOK := uniqueMetadata(resource.Metadata, "inventory_source")
	strategy, strategyOK := uniqueMetadata(resource.Metadata, "cleanup_strategy")
	distributionType, typeOK := uniqueMetadata(resource.Metadata, "distribution_type")
	installationPath, installationOK := uniqueMetadata(resource.Metadata, "installation_path")
	if !sourceOK || source != "gradle_wrapper_cache" || !strategyOK || strategy != "targeted_directory_removal" ||
		!typeOK || (distributionType != "all" && distributionType != "bin") || !installationOK ||
		resource.Version == nil || !safeVersionPattern.MatchString(*resource.Version) || !isCleanAbsolutePath(resource.Path) {
		return false
	}
	if !gradleCacheKeyPattern.MatchString(filepath.Base(resource.Path)) {
		return false
	}
	distributionDirectory := filepath.Dir(resource.Path)
	if filepath.Base(distributionDirectory) != "gradle-"+*resource.Version+"-"+distributionType ||
		filepath.Base(filepath.Dir(distributionDirectory)) != "dists" ||
		filepath.Base(filepath.Dir(filepath.Dir(distributionDirectory))) != "wrapper" {
		return false
	}
	return installationPath == filepath.Join(resource.Path, "gradle-"+*resource.Version)
}

func isTargetedXcodeDerivedData(resource domain.InstalledResource) bool {
	source, sourceOK := uniqueMetadata(resource.Metadata, "inventory_source")
	strategy, strategyOK := uniqueMetadata(resource.Metadata, "cleanup_strategy")
	resourceName, nameOK := uniqueMetadata(resource.Metadata, "resource_name")
	if !sourceOK || source != "xcode_derived_data" || !strategyOK || strategy != "targeted_directory_removal" ||
		!nameOK || resourceName == "." || resourceName == ".." || !isCleanAbsolutePath(resource.Path) ||
		filepath.Base(resource.Path) != resourceName {
		return false
	}
	derivedDataDirectory := filepath.Dir(resource.Path)
	return filepath.Base(derivedDataDirectory) == "DerivedData" && filepath.Base(filepath.Dir(derivedDataDirectory)) == "Xcode"
}

func isCleanAbsolutePath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && path != string(filepath.Separator)
}

func dockerObjectID(locator string, expectedHosts ...string) (string, bool) {
	parsed, err := url.Parse(locator)
	if err != nil || parsed.Scheme != "docker" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	hostOK := false
	for _, expected := range expectedHosts {
		if parsed.Host == expected {
			hostOK = true
			break
		}
	}
	if !hostOK {
		return "", false
	}
	encoded := strings.TrimPrefix(parsed.EscapedPath(), "/")
	objectID, err := url.PathUnescape(encoded)
	if err != nil || strings.Contains(objectID, "/") || !dockerObjectPattern.MatchString(objectID) {
		return "", false
	}
	return objectID, true
}

func uniqueMetadata(entries []domain.MetadataEntry, key string) (string, bool) {
	value := ""
	found := false
	for _, entry := range entries {
		if entry.Key != key {
			continue
		}
		if found && entry.Value != value {
			return "", false
		}
		value = entry.Value
		found = true
	}
	return value, found && value != ""
}

func projectsByResource(relations []domain.Relation) map[string][]string {
	sets := make(map[string]map[string]struct{})
	for _, relation := range relations {
		if relation.ResourceID == nil {
			continue
		}
		if sets[*relation.ResourceID] == nil {
			sets[*relation.ResourceID] = make(map[string]struct{})
		}
		sets[*relation.ResourceID][relation.ProjectID] = struct{}{}
	}
	result := make(map[string][]string, len(sets))
	for resourceID, projects := range sets {
		for projectID := range projects {
			result[resourceID] = append(result[resourceID], projectID)
		}
		sort.Strings(result[resourceID])
	}
	return result
}

func selectionEvidence(resourceID string) domain.Evidence {
	key := "installed_resources.id"
	value := resourceID
	return domain.Evidence{
		SourceType:    "scan report",
		Key:           &key,
		ObservedValue: &value,
		RuleID:        "planning.resource-selection.v1",
	}
}

func normalizePlan(plan *domain.CleanupPlan) {
	for index := range plan.Items {
		item := &plan.Items[index]
		sort.Strings(item.AffectedProjectIDs)
		sort.SliceStable(item.RiskCategories, func(left, right int) bool {
			return item.RiskCategories[left] < item.RiskCategories[right]
		})
		sort.Strings(item.Warnings)
		sort.SliceStable(item.Evidence, func(left, right int) bool {
			leftJSON, _ := json.Marshal(item.Evidence[left])
			rightJSON, _ := json.Marshal(item.Evidence[right])
			return string(leftJSON) < string(rightJSON)
		})
	}
	sort.SliceStable(plan.Items, func(left, right int) bool {
		return plan.Items[left].ResourceID < plan.Items[right].ResourceID
	})
	sort.SliceStable(plan.Excluded, func(left, right int) bool {
		return plan.Excluded[left].ResourceID < plan.Excluded[right].ResourceID
	})
}

func summarize(plan domain.CleanupPlan) domain.CleanupPlanSummary {
	summary := domain.CleanupPlanSummary{
		SelectedCount: len(plan.Items),
		ExcludedCount: len(plan.Excluded),
	}
	for _, item := range plan.Items {
		if item.EstimatedReclaimableBytes == nil {
			summary.SelectedItemsWithoutEstimate++
			continue
		}
		summary.KnownPotentiallyReclaimableBytesSum += *item.EstimatedReclaimableBytes
	}
	return summary
}

func stableItemID(resourceID string, command []string) string {
	digest := sha256.Sum256([]byte(resourceID + "\x00" + strings.Join(command, "\x00")))
	return "plan-item-" + hex.EncodeToString(digest[:8])
}

func categorySet(classifications []domain.ResourceClassification) map[domain.ResourceCategory]struct{} {
	result := make(map[domain.ResourceCategory]struct{}, len(classifications))
	for _, classification := range classifications {
		result[classification.Category] = struct{}{}
	}
	return result
}

func containsCategory(categories []domain.ResourceCategory, expected domain.ResourceCategory) bool {
	for _, category := range categories {
		if category == expected {
			return true
		}
	}
	return false
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func sortedUnique(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func isLowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}
