package classification

import (
	"testing"
	"time"

	"dev-environment-auditor/internal/domain"
)

func TestApplyClassifiesPositiveUseAndReconstructibility(t *testing.T) {
	version := "8.10.2"
	result := Apply([]domain.InstalledResource{{
		ID:              "gradle",
		Ecosystem:       "gradle",
		Component:       "gradle",
		Version:         &version,
		Path:            "/cache/gradle",
		ReferenceStatus: domain.Referenced,
		Metadata: []domain.MetadataEntry{
			{Key: "inventory_source", Value: "gradle_wrapper_cache"},
		},
	}}, Config{EvaluatedAt: mustTime(t, "2026-09-05T12:00:00Z"), OldAfterDays: 180})

	resource := result.Resources[0]
	assertCategories(t, resource, domain.CategoryUsed, domain.CategoryReconstructible)
	if hasCategory(resource, domain.CategoryUnknown) || hasCategory(resource, domain.CategoryProbableOrphan) {
		t.Fatalf("positive use was weakened by an unknown or orphan classification: %#v", resource.Classifications)
	}
}

func TestApplyClassifiesDockerVolumesFromUsageAndStorageKind(t *testing.T) {
	result := Apply([]domain.InstalledResource{
		{
			ID: "active-db", Ecosystem: "docker", Component: "docker_volume", Path: "docker://volume/acme_postgres",
			ReferenceStatus: domain.ReferenceUnknown,
			Metadata: []domain.MetadataEntry{
				{Key: "in_use", Value: "true"},
				{Key: "sensitivity", Value: "sensitive_mutable_data"},
				{Key: "storage_kind", Value: "mutable_project_data"},
			},
		},
		{
			ID: "unused-cache", Ecosystem: "docker", Component: "docker_volume", Path: "docker://volume/acme_gradle_cache",
			ReferenceStatus: domain.ReferenceUnknown,
			Metadata: []domain.MetadataEntry{
				{Key: "in_use", Value: "false"},
				{Key: "storage_kind", Value: "reconstructible_cache"},
			},
		},
	}, Config{EvaluatedAt: mustTime(t, "2026-10-01T12:00:00Z")})

	assertCategories(t, result.Resources[0], domain.CategoryUsed, domain.CategorySensitive)
	assertCategories(t, result.Resources[1], domain.CategoryReconstructible, domain.CategoryUnknown)
	if result.Resources[1].PotentiallyReclaimable != nil {
		t.Fatal("volume usage alone must not produce a reclaimable-space recommendation")
	}
}

func TestApplyKeepsSensitiveAVDUsageUnknown(t *testing.T) {
	result := Apply([]domain.InstalledResource{{
		ID:              "avd",
		Ecosystem:       "android",
		Component:       "android_avd",
		Path:            "/avd/pixel.avd",
		ReferenceStatus: domain.ReferenceUnknown,
		Metadata: []domain.MetadataEntry{
			{Key: "sensitivity", Value: "sensitive_mutable_user_data"},
			{Key: "management", Value: "avdmanager"},
		},
	}}, Config{EvaluatedAt: mustTime(t, "2026-09-05T12:00:00Z")})

	assertCategories(t, result.Resources[0], domain.CategorySensitive, domain.CategoryUnknown)
	if result.Resources[0].PotentiallyReclaimable != nil {
		t.Fatal("sensitive AVD must not expose potentially reclaimable space")
	}
}

func TestApplyRecognizesModernAndroidCLIAsReconstructible(t *testing.T) {
	result := Apply([]domain.InstalledResource{{
		ID: "platform", Ecosystem: "android", Component: "android_sdk_platform", Path: "/sdk/platforms/android-36",
		ReferenceStatus: domain.ReferenceUnknown,
		Metadata: []domain.MetadataEntry{
			{Key: "management", Value: "android sdk"},
			{Key: "package_path", Value: "platforms;android-36"},
		},
	}}, Config{EvaluatedAt: mustTime(t, "2026-09-05T12:00:00Z")})

	assertCategories(t, result.Resources[0], domain.CategoryReconstructible, domain.CategoryUnknown)
}

func TestApplyClassifiesXcodeDerivedDataAndProtectsSimulatorData(t *testing.T) {
	result := Apply([]domain.InstalledResource{
		{
			ID: "derived", Ecosystem: "apple", Component: "xcode_derived_data", Path: "/DerivedData/App",
			ReferenceStatus: domain.ReferenceUnknown,
			Metadata:        []domain.MetadataEntry{{Key: "inventory_source", Value: "xcode_derived_data"}},
		},
		{
			ID: "simulator", Ecosystem: "apple", Component: "apple_simulator_device", Path: "/CoreSimulator/Devices/id",
			ReferenceStatus: domain.ReferenceUnknown,
			Metadata:        []domain.MetadataEntry{{Key: "sensitivity", Value: "sensitive_mutable_user_data"}},
		},
	}, Config{EvaluatedAt: mustTime(t, "2026-09-05T12:00:00Z")})

	assertCategories(t, result.Resources[0], domain.CategoryReconstructible, domain.CategoryUnknown)
	assertCategories(t, result.Resources[1], domain.CategorySensitive, domain.CategoryUnknown)
}

func TestApplyUsesOnlyTrustedLastUseAndConservativeDockerSpace(t *testing.T) {
	size := int64(829_889_526)
	resource := domain.InstalledResource{
		ID:              "cache",
		Ecosystem:       "docker",
		Component:       "docker_build_cache",
		Path:            "docker://build-cache/cache",
		SizeBytes:       &size,
		ReferenceStatus: domain.ReferenceUnknown,
		Metadata: []domain.MetadataEntry{
			{Key: "management", Value: "docker buildx"},
			{Key: "last_used_observed", Value: "2025-08-05T09:24:09Z"},
			{Key: "last_used_source", Value: "docker_buildx_du"},
			{Key: "reclaimable_by_docker", Value: "true"},
			{Key: "shared", Value: "false"},
			{Key: "mutable", Value: "false"},
		},
	}
	result := Apply([]domain.InstalledResource{resource}, Config{
		EvaluatedAt:  mustTime(t, "2026-09-05T12:00:00Z"),
		OldAfterDays: 180,
	})

	classified := result.Resources[0]
	assertCategories(t, classified, domain.CategoryReconstructible, domain.CategoryOld, domain.CategoryUnknown)
	if classified.PotentiallyReclaimable == nil || classified.PotentiallyReclaimable.Bytes != size ||
		len(classified.PotentiallyReclaimable.Evidence) != 4 {
		t.Fatalf("unexpected reclaimable estimate: %#v", classified.PotentiallyReclaimable)
	}
	if resource.PotentiallyReclaimable != nil || len(resource.Classifications) != 0 {
		t.Fatal("Apply mutated its input")
	}
}

func TestProbableOrphanRequiresAllConservativeConditions(t *testing.T) {
	size := int64(100)
	base := domain.InstalledResource{
		ID:              "cache",
		Ecosystem:       "docker",
		Component:       "docker_build_cache",
		Path:            "docker://build-cache/cache",
		SizeBytes:       &size,
		ReferenceStatus: domain.NoReferenceFound,
		Metadata: []domain.MetadataEntry{
			{Key: "management", Value: "docker buildx"},
			{Key: "last_used_observed", Value: "2025-01-01T00:00:00Z"},
			{Key: "last_used_source", Value: "docker_buildx_du"},
		},
	}
	result := Apply([]domain.InstalledResource{base}, Config{
		EvaluatedAt:  mustTime(t, "2026-09-05T12:00:00Z"),
		OldAfterDays: 180,
	})
	assertCategories(t, result.Resources[0], domain.CategoryReconstructible, domain.CategoryOld, domain.CategoryProbableOrphan)
	if hasCategory(result.Resources[0], domain.CategoryUnknown) {
		t.Fatal("probable orphan should not also be usage-unknown")
	}

	directJDK := base
	directJDK.ID = "jdk"
	directJDK.Ecosystem = "java"
	directJDK.Component = "jdk"
	directJDK.Metadata = nil
	withoutProof := Apply([]domain.InstalledResource{directJDK}, Config{
		EvaluatedAt:  mustTime(t, "2026-09-05T12:00:00Z"),
		OldAfterDays: 180,
	})
	assertCategories(t, withoutProof.Resources[0], domain.CategoryUnknown)
	if hasCategory(withoutProof.Resources[0], domain.CategoryProbableOrphan) {
		t.Fatal("NO_REFERENCE_FOUND alone must never imply probable orphan")
	}
}

func TestAgePolicyIsConfigurableAndRejectsUnreliableTime(t *testing.T) {
	resource := domain.InstalledResource{
		ID:              "cache",
		Ecosystem:       "docker",
		Component:       "docker_build_cache",
		Path:            "docker://build-cache/cache",
		ReferenceStatus: domain.ReferenceUnknown,
		Metadata: []domain.MetadataEntry{
			{Key: "management", Value: "docker buildx"},
			{Key: "last_used_observed", Value: "2026-07-01T00:00:00Z"},
			{Key: "last_used_source", Value: "docker_buildx_du"},
		},
	}
	now := mustTime(t, "2026-09-05T12:00:00Z")
	if hasCategory(Apply([]domain.InstalledResource{resource}, Config{EvaluatedAt: now, OldAfterDays: 180}).Resources[0], domain.CategoryOld) {
		t.Fatal("resource should be recent under a 180-day policy")
	}
	if !hasCategory(Apply([]domain.InstalledResource{resource}, Config{EvaluatedAt: now, OldAfterDays: 30}).Resources[0], domain.CategoryOld) {
		t.Fatal("resource should be old under a 30-day policy")
	}

	resource.Metadata[1].Value = "not-a-time"
	invalid := Apply([]domain.InstalledResource{resource}, Config{EvaluatedAt: now, OldAfterDays: 30})
	if hasCategory(invalid.Resources[0], domain.CategoryOld) || len(invalid.Diagnostics) != 1 ||
		invalid.Diagnostics[0].Code != "CLASSIFICATION_LAST_USED_INVALID" {
		t.Fatalf("invalid trusted timestamp was not kept unknown: %#v", invalid)
	}

	resource.Metadata[1].Value = "2027-01-01T00:00:00Z"
	future := Apply([]domain.InstalledResource{resource}, Config{EvaluatedAt: now, OldAfterDays: 30})
	if hasCategory(future.Resources[0], domain.CategoryOld) || len(future.Diagnostics) != 1 ||
		future.Diagnostics[0].Code != "CLASSIFICATION_LAST_USED_IN_FUTURE" {
		t.Fatalf("future trusted timestamp was not kept unknown: %#v", future)
	}
}

func TestSharedOrMutableCacheHasNoPotentiallyReclaimableEstimate(t *testing.T) {
	size := int64(100)
	for _, metadata := range [][]domain.MetadataEntry{
		{
			{Key: "management", Value: "docker buildx"},
			{Key: "reclaimable_by_docker", Value: "true"},
			{Key: "shared", Value: "true"},
			{Key: "mutable", Value: "false"},
		},
		{
			{Key: "management", Value: "docker buildx"},
			{Key: "reclaimable_by_docker", Value: "true"},
			{Key: "shared", Value: "false"},
			{Key: "mutable", Value: "true"},
		},
	} {
		result := Apply([]domain.InstalledResource{{
			ID: "cache", Ecosystem: "docker", Component: "docker_build_cache",
			Path: "docker://build-cache/cache", SizeBytes: &size, Metadata: metadata,
		}}, Config{EvaluatedAt: mustTime(t, "2026-09-05T12:00:00Z")})
		if result.Resources[0].PotentiallyReclaimable != nil {
			t.Fatalf("shared or mutable cache got reclaimable estimate: %#v", result.Resources[0])
		}
	}
}

func TestRelativeBuildxLastUseUsesConservativeLowerBound(t *testing.T) {
	resource := domain.InstalledResource{
		ID:              "cache",
		Ecosystem:       "docker",
		Component:       "docker_build_cache",
		Path:            "docker://build-cache/cache",
		ReferenceStatus: domain.ReferenceUnknown,
		Metadata: []domain.MetadataEntry{
			{Key: "management", Value: "docker buildx"},
			{Key: "last_used_observed", Value: "2 days ago"},
			{Key: "last_used_source", Value: "docker_buildx_du"},
		},
	}
	config := Config{EvaluatedAt: mustTime(t, "2026-09-05T12:00:00Z"), OldAfterDays: 180}
	recent := Apply([]domain.InstalledResource{resource}, config)
	if hasCategory(recent.Resources[0], domain.CategoryOld) || len(recent.Diagnostics) != 0 {
		t.Fatalf("supported recent relative value should remain recent without diagnostics: %#v", recent)
	}

	resource.Metadata[1].Value = "7 months ago"
	old := Apply([]domain.InstalledResource{resource}, config)
	if !hasCategory(old.Resources[0], domain.CategoryOld) || len(old.Diagnostics) != 0 {
		t.Fatalf("conservatively old relative value was not classified: %#v", old)
	}

	resource.Metadata[1].Value = "6 months ago"
	borderline := Apply([]domain.InstalledResource{resource}, config)
	if hasCategory(borderline.Resources[0], domain.CategoryOld) || len(borderline.Diagnostics) != 0 {
		t.Fatalf("borderline relative month value should remain not old: %#v", borderline)
	}
}

func assertCategories(t *testing.T, resource domain.InstalledResource, expected ...domain.ResourceCategory) {
	t.Helper()
	if len(resource.Classifications) != len(expected) {
		t.Fatalf("categories = %#v, want %#v", resource.Classifications, expected)
	}
	for _, category := range expected {
		if !hasCategory(resource, category) {
			t.Errorf("missing category %s in %#v", category, resource.Classifications)
		}
	}
}

func hasCategory(resource domain.InstalledResource, category domain.ResourceCategory) bool {
	for _, classification := range resource.Classifications {
		if classification.Category == category {
			return true
		}
	}
	return false
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
