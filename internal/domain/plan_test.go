package domain

import "testing"

func TestCleanupPlanRejectsContentChangeAndUnsafeDefaultSelection(t *testing.T) {
	estimate := int64(10)
	plan := validCleanupPlanForDomainTest()
	plan.Items[0].EstimatedReclaimableBytes = &estimate
	plan.Summary.KnownPotentiallyReclaimableBytesSum = estimate
	planID, err := ComputeCleanupPlanID(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanID = planID
	if err := plan.Validate(); err != nil {
		t.Fatalf("expected valid plan: %v", err)
	}

	changed := plan
	changed.Items = append([]CleanupPlanItem(nil), plan.Items...)
	changed.Items[0].Impact = "changed after creation"
	if err := changed.Validate(); err == nil {
		t.Fatal("expected content-address mismatch after plan alteration")
	}

	unsafe := plan
	unsafe.Items = append([]CleanupPlanItem(nil), plan.Items...)
	unsafe.Items[0].RiskCategories = append([]ResourceCategory(nil), plan.Items[0].RiskCategories...)
	unsafe.Items[0].RiskCategories = append(unsafe.Items[0].RiskCategories, CategoryUnknown)
	unsafe.PlanID, err = ComputeCleanupPlanID(unsafe)
	if err != nil {
		t.Fatal(err)
	}
	if err := unsafe.Validate(); err == nil {
		t.Fatal("expected unknown resource in default-safe selection to be rejected")
	}
}

func validCleanupPlanForDomainTest() CleanupPlan {
	key := "installed_resources.id"
	value := "resource"
	return CleanupPlan{
		SchemaVersion:       CleanupPlanSchemaVersion,
		ToolVersion:         "test",
		Mode:                PlanModeSimulationOnly,
		SourceReportSHA256:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SourceScanCompleted: "2026-09-05T12:00:01Z",
		Selection:           PlanSelection{Mode: SelectionDefaultSafe, RequestedResourceIDs: []string{}},
		Items: []CleanupPlanItem{{
			ID: "item", ResourceID: "resource", Ecosystem: "docker", Component: "docker_build_cache", Path: "docker://build_cache/cache",
			SelectedBy: SelectedByDefaultSafe, AffectedProjectIDs: []string{},
			RiskCategories: []ResourceCategory{CategoryReconstructible, CategoryOld, CategoryProbableOrphan},
			Impact:         "rebuild", OfficialCommand: []string{"docker", "buildx", "prune", "--filter", "id=cache"},
			RequiresExplicitConfirmation: true, Rationale: "fixture",
			Evidence: []Evidence{{SourceType: "scan report", Key: &key, ObservedValue: &value, RuleID: "test"}}, Warnings: []string{},
		}},
		Excluded: []CleanupPlanExclusion{},
		Summary:  CleanupPlanSummary{SelectedCount: 1},
		Warnings: []string{},
	}
}
