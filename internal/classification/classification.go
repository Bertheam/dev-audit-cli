// Package classification derives conservative, evidence-backed resource
// categories after project correlation. It does not recommend or execute any
// cleanup action.
package classification

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"dev-environment-auditor/internal/domain"
)

const DefaultOldAfterDays = 180

var relativeLastUsePattern = regexp.MustCompile(`^([0-9]+) (second|minute|hour|day|week|month|year)s? ago$`)

type Config struct {
	EvaluatedAt  time.Time
	OldAfterDays int
}

type Result struct {
	Resources   []domain.InstalledResource
	Diagnostics []domain.Diagnostic
}

// Apply returns a copy of resources with classifications and conservative
// potentially-reclaimable estimates derived from recorded evidence.
func Apply(resources []domain.InstalledResource, config Config) Result {
	if config.OldAfterDays <= 0 {
		config.OldAfterDays = DefaultOldAfterDays
	}
	config.EvaluatedAt = config.EvaluatedAt.UTC()

	result := Result{
		Resources:   make([]domain.InstalledResource, len(resources)),
		Diagnostics: []domain.Diagnostic{},
	}
	for index, input := range resources {
		resource := cloneResource(input)
		resource.Classifications = nil
		resource.PotentiallyReclaimable = nil

		used := classifyUsed(&resource)
		reconstructible := classifyReconstructible(&resource)
		sensitive := classifySensitive(&resource)
		old, diagnostics := classifyOld(&resource, config)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		probableOrphan := classifyProbableOrphan(&resource, reconstructible, old, sensitive)
		if !used && !probableOrphan {
			classifyUnknown(&resource)
		}
		estimatePotentiallyReclaimable(&resource)
		result.Resources[index] = resource
	}
	return result
}

func classifyUsed(resource *domain.InstalledResource) bool {
	if resource.ReferenceStatus == domain.Referenced {
		resource.Classifications = append(resource.Classifications, domain.ResourceClassification{
			Category:  domain.CategoryUsed,
			Rationale: "At least one analyzed project requirement is correlated with this resource.",
			Evidence: []domain.Evidence{
				metadataEvidence("correlation", "reference_status", string(resource.ReferenceStatus), "classification.used.reference.v1"),
			},
		})
		return true
	}

	if resource.Component == "docker_container" {
		if state, ok := uniqueMetadata(resource.Metadata, "state"); ok &&
			(state == "running" || state == "paused" || state == "restarting") {
			resource.Classifications = append(resource.Classifications, domain.ResourceClassification{
				Category:  domain.CategoryUsed,
				Rationale: "Docker reports the container in an active runtime state.",
				Evidence: []domain.Evidence{
					metadataEvidence("docker container ls", "state", state, "classification.used.docker-container-state.v1"),
				},
			})
			return true
		}
	}
	return false
}

func classifyReconstructible(resource *domain.InstalledResource) bool {
	var rationale string
	var evidence []domain.Evidence

	source, _ := uniqueMetadata(resource.Metadata, "inventory_source")
	management, _ := uniqueMetadata(resource.Metadata, "management")
	switch {
	case resource.Ecosystem == "android" && resource.Component != "android_avd" &&
		management == "sdkmanager":
		packagePath, ok := uniqueMetadata(resource.Metadata, "package_path")
		if !ok || packagePath == "" {
			return false
		}
		rationale = "The exact Android SDK package identifier and its official manager were observed."
		evidence = []domain.Evidence{
			metadataEvidence("inventory", "management", management, "classification.reconstructible.android-package.v1"),
			metadataEvidence("inventory", "package_path", packagePath, "classification.reconstructible.android-package.v1"),
		}
	case resource.Ecosystem == "gradle" && resource.Component == "gradle" && source == "gradle_wrapper_cache":
		rationale = "The resource is a Gradle Wrapper distribution cache that can be downloaded again from a project wrapper declaration."
		evidence = []domain.Evidence{
			metadataEvidence("inventory", "inventory_source", source, "classification.reconstructible.gradle-wrapper-cache.v1"),
		}
	case (resource.Component == "android_gradle_plugin" || resource.Component == "kotlin_gradle_plugin") &&
		source == "gradle_module_cache":
		rationale = "The resource is a Gradle module cache entry that can be resolved again from project dependency declarations."
		evidence = []domain.Evidence{
			metadataEvidence("inventory", "inventory_source", source, "classification.reconstructible.gradle-module-cache.v1"),
		}
	case resource.Ecosystem == "flutter" && resource.Component == "flutter_sdk" &&
		source == "fvm_cache" && resource.Version != nil:
		rationale = "The exact Flutter SDK version was observed inside an FVM-managed cache."
		evidence = []domain.Evidence{
			metadataEvidence("inventory", "inventory_source", source, "classification.reconstructible.fvm-cache.v1"),
			metadataEvidence("inventory", "version", *resource.Version, "classification.reconstructible.fvm-cache.v1"),
		}
	case resource.Ecosystem == "docker" && resource.Component == "docker_build_cache" && management == "docker buildx":
		rationale = "The resource is a Docker BuildKit cache record that can be regenerated by future builds."
		evidence = []domain.Evidence{
			metadataEvidence("docker buildx du", "management", management, "classification.reconstructible.docker-build-cache.v1"),
		}
	default:
		return false
	}

	resource.Classifications = append(resource.Classifications, domain.ResourceClassification{
		Category:  domain.CategoryReconstructible,
		Rationale: rationale,
		Evidence:  evidence,
	})
	return true
}

func classifySensitive(resource *domain.InstalledResource) bool {
	sensitivity, ok := uniqueMetadata(resource.Metadata, "sensitivity")
	if !ok || sensitivity == "" {
		return false
	}
	resource.Classifications = append(resource.Classifications, domain.ResourceClassification{
		Category:  domain.CategorySensitive,
		Rationale: "Inventory metadata identifies mutable or user data that requires explicit human review.",
		Evidence: []domain.Evidence{
			metadataEvidence("inventory", "sensitivity", sensitivity, "classification.sensitive.metadata.v1"),
		},
	})
	return true
}

func classifyOld(resource *domain.InstalledResource, config Config) (bool, []domain.Diagnostic) {
	lastUsed, source, trusted := trustedLastUse(*resource)
	if !trusted {
		return false, nil
	}
	if config.EvaluatedAt.IsZero() {
		return false, []domain.Diagnostic{classificationDiagnostic(
			"CLASSIFICATION_TIME_UNAVAILABLE",
			"classification evaluation time is unavailable; age remains unknown",
			resource.Path,
		)}
	}
	parsed, err := time.Parse(time.RFC3339Nano, lastUsed)
	if err == nil {
		parsed = parsed.UTC()
		if parsed.After(config.EvaluatedAt) {
			return false, []domain.Diagnostic{classificationDiagnostic(
				"CLASSIFICATION_LAST_USED_IN_FUTURE",
				"the trusted last-use timestamp is later than scan completion; age remains unknown",
				resource.Path,
			)}
		}

		cutoff := config.EvaluatedAt.AddDate(0, 0, -config.OldAfterDays)
		if parsed.After(cutoff) {
			return false, nil
		}
		appendOldClassification(resource, source, parsed.Format(time.RFC3339Nano), fmt.Sprintf(
			"The last-use observation is at least %d days older than scan completion.",
			config.OldAfterDays,
		), config.OldAfterDays)
		return true, nil
	}

	minimumAgeDays, relative := conservativeRelativeAgeDays(lastUsed)
	if relative {
		if minimumAgeDays < int64(config.OldAfterDays) {
			return false, nil
		}
		appendOldClassification(resource, source, lastUsed, fmt.Sprintf(
			"Docker's relative last-use observation implies at least %d days without use, meeting the %d-day policy.",
			minimumAgeDays,
			config.OldAfterDays,
		), config.OldAfterDays)
		return true, nil
	}

	return false, []domain.Diagnostic{classificationDiagnostic(
		"CLASSIFICATION_LAST_USED_INVALID",
		"a trusted last-use field was neither RFC 3339 nor a supported conservative relative duration; age remains unknown",
		resource.Path,
	)}
}

func appendOldClassification(resource *domain.InstalledResource, source, observed, rationale string, oldAfterDays int) {
	resource.Classifications = append(resource.Classifications, domain.ResourceClassification{
		Category:  domain.CategoryOld,
		Rationale: rationale,
		Evidence: []domain.Evidence{
			metadataEvidence(source, "last_used_observed", observed, "classification.old.trusted-last-use.v1"),
			metadataEvidence("classification_policy", "old_after_days", strconv.Itoa(oldAfterDays), "classification.old.trusted-last-use.v1"),
		},
	})
}

func conservativeRelativeAgeDays(value string) (int64, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "less than a second ago", "about a second ago", "about a minute ago", "about an hour ago":
		return 0, true
	}
	match := relativeLastUsePattern.FindStringSubmatch(normalized)
	if match == nil {
		return 0, false
	}
	count, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, false
	}
	multiplier := int64(0)
	switch match[2] {
	case "second", "minute", "hour":
		return 0, true
	case "day":
		multiplier = 1
	case "week":
		multiplier = 7
	case "month":
		// Calendar months vary. Twenty-eight days is the conservative lower
		// bound; this intentionally leaves borderline observations unknown.
		multiplier = 28
	case "year":
		// Use 365 days rather than fabricating a calendar timestamp.
		multiplier = 365
	default:
		return 0, false
	}
	if count > (1<<63-1)/multiplier {
		return 0, false
	}
	return count * multiplier, true
}

func trustedLastUse(resource domain.InstalledResource) (string, string, bool) {
	if resource.Ecosystem != "docker" || resource.Component != "docker_build_cache" {
		return "", "", false
	}
	lastUsed, lastUsedOK := uniqueMetadata(resource.Metadata, "last_used_observed")
	source, sourceOK := uniqueMetadata(resource.Metadata, "last_used_source")
	if !lastUsedOK || !sourceOK || source != "docker_buildx_du" {
		return "", "", false
	}
	return lastUsed, "docker buildx du", true
}

func classifyProbableOrphan(resource *domain.InstalledResource, reconstructible, old, sensitive bool) bool {
	if resource.ReferenceStatus != domain.NoReferenceFound || !reconstructible || !old || sensitive {
		return false
	}
	resource.Classifications = append(resource.Classifications, domain.ResourceClassification{
		Category:  domain.CategoryProbableOrphan,
		Rationale: "No analyzed project references this resource, it is reconstructible, and reliable last-use evidence exceeds the age policy; this remains a probability, not a deletion recommendation.",
		Evidence: []domain.Evidence{
			metadataEvidence("correlation", "reference_status", string(resource.ReferenceStatus), "classification.probable-orphan.conservative.v1"),
			metadataEvidence("classification", "required_categories", "RECONSTRUCTIBLE,ANCIENNE", "classification.probable-orphan.conservative.v1"),
		},
	})
	return true
}

func classifyUnknown(resource *domain.InstalledResource) {
	rationale := "No reliable evidence establishes current use or probable orphan status."
	if resource.ReferenceStatus == domain.NoReferenceFound {
		rationale = "No project reference was found in the analyzed coverage, but that absence alone does not prove the resource is unused."
	}
	resource.Classifications = append(resource.Classifications, domain.ResourceClassification{
		Category:  domain.CategoryUnknown,
		Rationale: rationale,
		Evidence: []domain.Evidence{
			metadataEvidence("correlation", "reference_status", string(resource.ReferenceStatus), "classification.unknown.insufficient-evidence.v1"),
		},
	})
}

func estimatePotentiallyReclaimable(resource *domain.InstalledResource) {
	if resource.Ecosystem != "docker" || resource.Component != "docker_build_cache" || resource.SizeBytes == nil {
		return
	}
	reclaimable, reclaimableOK := uniqueMetadata(resource.Metadata, "reclaimable_by_docker")
	shared, sharedOK := uniqueMetadata(resource.Metadata, "shared")
	mutable, mutableOK := uniqueMetadata(resource.Metadata, "mutable")
	if !reclaimableOK || !sharedOK || !mutableOK || reclaimable != "true" || shared != "false" || mutable != "false" {
		return
	}
	resource.PotentiallyReclaimable = &domain.SpaceEstimate{
		Bytes:     *resource.SizeBytes,
		Rationale: "Docker marked this BuildKit cache record reclaimable, and reported it as neither shared nor mutable; the value is potential space only and is not cleanup authorization.",
		Evidence: []domain.Evidence{
			metadataEvidence("docker buildx du", "reclaimable_by_docker", reclaimable, "classification.space.docker-build-cache.v1"),
			metadataEvidence("docker buildx du", "shared", shared, "classification.space.docker-build-cache.v1"),
			metadataEvidence("docker buildx du", "mutable", mutable, "classification.space.docker-build-cache.v1"),
			metadataEvidence("docker buildx du", "size_bytes", strconv.FormatInt(*resource.SizeBytes, 10), "classification.space.docker-build-cache.v1"),
		},
	}
}

func cloneResource(input domain.InstalledResource) domain.InstalledResource {
	result := input
	result.Metadata = append([]domain.MetadataEntry{}, input.Metadata...)
	result.Warnings = append([]string{}, input.Warnings...)
	result.Classifications = append([]domain.ResourceClassification(nil), input.Classifications...)
	if input.PotentiallyReclaimable != nil {
		estimate := *input.PotentiallyReclaimable
		estimate.Evidence = append([]domain.Evidence(nil), input.PotentiallyReclaimable.Evidence...)
		result.PotentiallyReclaimable = &estimate
	}
	return result
}

func uniqueMetadata(metadata []domain.MetadataEntry, key string) (string, bool) {
	value := ""
	found := false
	for _, entry := range metadata {
		if entry.Key != key {
			continue
		}
		if !found {
			value = strings.TrimSpace(entry.Value)
			found = true
			continue
		}
		if strings.TrimSpace(entry.Value) != value {
			return "", false
		}
	}
	return value, found
}

func metadataEvidence(source, key, value, rule string) domain.Evidence {
	keyCopy := key
	valueCopy := value
	evidence := domain.Evidence{
		SourceType:    source,
		Key:           &keyCopy,
		ObservedValue: &valueCopy,
		RuleID:        rule,
	}
	if source == "docker buildx du" {
		evidence.Command = []string{"docker", "buildx", "du", "--format=json"}
	}
	return evidence
}

func classificationDiagnostic(code, message, path string) domain.Diagnostic {
	pathCopy := path
	return domain.Diagnostic{
		Code:     code,
		Severity: domain.SeverityInfo,
		Scope:    "classification",
		Message:  message,
		Path:     &pathCopy,
	}
}
