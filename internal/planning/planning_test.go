package planning

import (
	"reflect"
	"strings"
	"testing"

	"dev-environment-auditor/internal/domain"
)

const testReportSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestDefaultPlanSelectsOnlyEstimatedProbableOrphans(t *testing.T) {
	document := planningDocument()
	plan, err := Build(document, Config{ToolVersion: "test", SourceReportSHA256: testReportSHA256})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Selection.Mode != domain.SelectionDefaultSafe || len(plan.Items) != 1 || plan.Items[0].ResourceID != "cache-orphan" {
		t.Fatalf("unexpected default selection: %#v", plan)
	}
	item := plan.Items[0]
	wantCommand := []string{"docker", "buildx", "prune", "--filter", "id=cache123456789"}
	if !reflect.DeepEqual(item.OfficialCommand, wantCommand) {
		t.Fatalf("command = %#v, want %#v", item.OfficialCommand, wantCommand)
	}
	if item.EstimatedReclaimableBytes == nil || *item.EstimatedReclaimableBytes != 2048 ||
		!item.RequiresExplicitConfirmation {
		t.Fatalf("unsafe or missing estimate: %#v", item)
	}
	for _, argument := range item.OfficialCommand {
		if argument == "--force" || strings.Contains(argument, "system prune") {
			t.Fatalf("broad or forced command in plan: %#v", item.OfficialCommand)
		}
	}
	assertExcludedFor(t, plan, "cache-unknown", domain.ExclusionUnknownUsage)
	assertExcludedFor(t, plan, "container-sensitive", domain.ExclusionSensitive)
	assertExcludedFor(t, plan, "jdk-unsupported", domain.ExclusionUnsupportedAction)
	if plan.Summary.SelectedCount != 1 || plan.Summary.ExcludedCount != 3 ||
		plan.Summary.KnownPotentiallyReclaimableBytesSum != 2048 || plan.Summary.SelectedItemsWithoutEstimate != 0 {
		t.Fatalf("unexpected summary: %#v", plan.Summary)
	}
}

func TestManualSelectionIncludesRiskySupportedResourcesButNeverExecutes(t *testing.T) {
	plan, err := Build(planningDocument(), Config{
		ToolVersion:        "test",
		SourceReportSHA256: testReportSHA256,
		ResourceIDs:        []string{"missing", "container-sensitive", "cache-unknown", "jdk-unsupported", "cache-unknown"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Selection.Mode != domain.SelectionExplicitResourceIDs || len(plan.Selection.RequestedResourceIDs) != 4 {
		t.Fatalf("manual selection was not normalized: %#v", plan.Selection)
	}
	if len(plan.Items) != 2 || len(plan.Excluded) != 2 {
		t.Fatalf("unexpected manual plan: %#v", plan)
	}
	unknown := findItem(plan, "cache-unknown")
	if unknown == nil || unknown.SelectedBy != domain.SelectedByManual ||
		!containsWarning(unknown.Warnings, "usage is unknown") {
		t.Fatalf("unknown manual item lacks explicit risk: %#v", unknown)
	}
	sensitive := findItem(plan, "container-sensitive")
	if sensitive == nil || !containsWarning(sensitive.Warnings, "permanent data loss") ||
		!reflect.DeepEqual(sensitive.OfficialCommand, []string{"docker", "container", "rm", "abcdef123456"}) {
		t.Fatalf("sensitive manual item is not explicit: %#v", sensitive)
	}
	assertExcludedFor(t, plan, "missing", domain.ExclusionResourceNotFound)
	assertExcludedFor(t, plan, "jdk-unsupported", domain.ExclusionUnsupportedAction)
}

func TestPlanIsDeterministicAndDoesNotMutateSource(t *testing.T) {
	document := planningDocument()
	originalFirst := document.InstalledResources[0].ID
	first, err := Build(document, Config{
		ToolVersion: "test", SourceReportSHA256: testReportSHA256,
		ResourceIDs: []string{"container-sensitive", "cache-unknown"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(document, Config{
		ToolVersion: "test", SourceReportSHA256: testReportSHA256,
		ResourceIDs: []string{"cache-unknown", "container-sensitive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.PlanID != second.PlanID {
		t.Fatalf("equivalent selection produced different plans:\n%#v\n%#v", first, second)
	}
	if document.InstalledResources[0].ID != originalFirst {
		t.Fatal("source document was mutated")
	}
}

func TestOfficialActionsRemainTargetedArgumentArrays(t *testing.T) {
	document := planningDocument()
	document.InstalledResources = append(document.InstalledResources,
		domain.InstalledResource{
			ID: "android-package", Ecosystem: "android", Component: "android_sdk_platform", Path: "/sdk/platforms/android-36",
			ReferenceStatus: domain.ReferenceUnknown,
			Classifications: []domain.ResourceClassification{classification(domain.CategoryUnknown)},
			Metadata:        []domain.MetadataEntry{{Key: "management", Value: "sdkmanager"}, {Key: "package_path", Value: "platforms;android-36"}},
			Warnings:        []string{},
		},
		domain.InstalledResource{
			ID: "android-avd", Ecosystem: "android", Component: "android_avd", Path: "/avd/Pixel.avd",
			ReferenceStatus: domain.ReferenceUnknown,
			Classifications: []domain.ResourceClassification{classification(domain.CategorySensitive), classification(domain.CategoryUnknown)},
			Metadata:        []domain.MetadataEntry{{Key: "management", Value: "avdmanager"}, {Key: "avd_name", Value: "Pixel_API_36"}},
			Warnings:        []string{},
		},
		domain.InstalledResource{
			ID: "docker-image", Ecosystem: "docker", Component: "docker_image", Path: "docker://image/sha256:abcdef123456",
			ReferenceStatus: domain.ReferenceUnknown,
			Classifications: []domain.ResourceClassification{classification(domain.CategoryUnknown)},
			Metadata:        []domain.MetadataEntry{{Key: "management", Value: "docker image"}}, Warnings: []string{},
		},
	)
	plan, err := Build(document, Config{
		ToolVersion: "test", SourceReportSHA256: testReportSHA256,
		ResourceIDs: []string{"android-package", "android-avd", "docker-image"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string][]string{
		"android-package": {"sdkmanager", "--uninstall", "platforms;android-36"},
		"android-avd":     {"avdmanager", "delete", "avd", "-n", "Pixel_API_36"},
		"docker-image":    {"docker", "image", "rm", "sha256:abcdef123456"},
	}
	for resourceID, command := range wants {
		item := findItem(plan, resourceID)
		if item == nil || !reflect.DeepEqual(item.OfficialCommand, command) {
			t.Errorf("%s command = %#v, want %#v", resourceID, item, command)
		}
	}
}

func planningDocument() domain.ScanDocument {
	estimateEvidence := []domain.Evidence{{SourceType: "docker buildx du", RuleID: "test.estimate"}}
	size := int64(4096)
	estimate := func() *domain.SpaceEstimate {
		return &domain.SpaceEstimate{Bytes: 2048, Rationale: "fixture", Evidence: estimateEvidence}
	}
	return domain.ScanDocument{
		SchemaVersion: domain.SchemaVersion,
		ToolVersion:   "source",
		Scan: domain.ScanMetadata{
			StartedAt: "2026-09-05T12:00:00Z", CompletedAt: "2026-09-05T12:00:01Z",
			Roots: []string{"/work"}, Exclusions: []string{}, ReadOnly: true,
			ClassificationPolicy: &domain.ClassificationPolicy{OldAfterDays: 180},
		},
		Projects: []domain.Project{},
		InstalledResources: []domain.InstalledResource{
			{
				ID: "cache-unknown", Ecosystem: "docker", Component: "docker_build_cache", Path: "docker://build_cache/unknown123456",
				SizeBytes: &size, PotentiallyReclaimable: estimate(), ReferenceStatus: domain.ReferenceUnknown,
				Classifications: []domain.ResourceClassification{classification(domain.CategoryReconstructible), classification(domain.CategoryUnknown)},
				Metadata:        []domain.MetadataEntry{{Key: "management", Value: "docker buildx"}}, Warnings: []string{},
			},
			{
				ID: "container-sensitive", Ecosystem: "docker", Component: "docker_container", Path: "docker://container/abcdef123456",
				SizeBytes: &size, ReferenceStatus: domain.ReferenceUnknown,
				Classifications: []domain.ResourceClassification{classification(domain.CategorySensitive), classification(domain.CategoryUnknown)},
				Metadata:        []domain.MetadataEntry{{Key: "management", Value: "docker container"}}, Warnings: []string{},
			},
			{
				ID: "cache-orphan", Ecosystem: "docker", Component: "docker_build_cache", Path: "docker://build_cache/cache123456789",
				SizeBytes: &size, PotentiallyReclaimable: estimate(), ReferenceStatus: domain.NoReferenceFound,
				Classifications: []domain.ResourceClassification{
					classification(domain.CategoryReconstructible), classification(domain.CategoryOld), classification(domain.CategoryProbableOrphan),
				},
				Metadata: []domain.MetadataEntry{{Key: "management", Value: "docker buildx"}}, Warnings: []string{},
			},
			{
				ID: "jdk-unsupported", Ecosystem: "java", Component: "jdk", Path: "/jdk/21",
				ReferenceStatus: domain.ReferenceUnknown,
				Classifications: []domain.ResourceClassification{classification(domain.CategoryUnknown)},
				Metadata:        []domain.MetadataEntry{}, Warnings: []string{},
			},
		},
		Relations: []domain.Relation{}, Diagnostics: []domain.Diagnostic{},
	}
}

func classification(category domain.ResourceCategory) domain.ResourceClassification {
	return domain.ResourceClassification{
		Category: category, Rationale: "fixture",
		Evidence: []domain.Evidence{{SourceType: "test", RuleID: "test.classification." + string(category)}},
	}
}

func findItem(plan domain.CleanupPlan, resourceID string) *domain.CleanupPlanItem {
	for index := range plan.Items {
		if plan.Items[index].ResourceID == resourceID {
			return &plan.Items[index]
		}
	}
	return nil
}

func assertExcludedFor(t *testing.T, plan domain.CleanupPlan, resourceID string, reason domain.PlanExclusionReason) {
	t.Helper()
	for _, excluded := range plan.Excluded {
		if excluded.ResourceID != resourceID {
			continue
		}
		for _, candidate := range excluded.Reasons {
			if candidate == reason {
				return
			}
		}
		t.Fatalf("resource %q excluded for %#v, want %s", resourceID, excluded.Reasons, reason)
	}
	t.Fatalf("resource %q was not excluded", resourceID)
}

func containsWarning(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}
