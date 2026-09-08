package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"dev-environment-auditor/internal/domain"
)

func TestCleanupPlanJSONRoundTripAndContentAddress(t *testing.T) {
	plan := reportPlanFixture(t)
	content, err := RenderPlanJSON(plan)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePlanJSON(content)
	if err != nil {
		t.Fatalf("decode rendered plan: %v\n%s", err, content)
	}
	if decoded.PlanID != plan.PlanID || decoded.Items[0].OfficialCommand[4] != "id=cache123456789" {
		t.Fatalf("unexpected round trip: %#v", decoded)
	}

	var altered map[string]any
	if err := json.Unmarshal(content, &altered); err != nil {
		t.Fatal(err)
	}
	altered["warnings"] = append(altered["warnings"].([]any), "altered")
	alteredContent, err := json.Marshal(altered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePlanJSON(alteredContent); err == nil || !strings.Contains(err.Error(), "plan_id does not match") {
		t.Fatalf("expected altered plan rejection, got %v", err)
	}
}

func TestCleanupPlanSchemaRejectsUnknownFields(t *testing.T) {
	content, err := RenderPlanJSON(reportPlanFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.Replace(content, []byte(`"mode": "SIMULATION_ONLY",`), []byte(`"mode": "SIMULATION_ONLY", "execute": true,`), 1)
	if err := ValidatePlanJSON(content); err == nil {
		t.Fatal("expected unknown plan field to be rejected")
	}
}

func TestCleanupPlanTerminalMakesSimulationAndRisksExplicit(t *testing.T) {
	terminal, err := RenderPlanTerminal(reportPlanFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"◆ dev-audit  plan",
		"SIMULATION ONLY · NOT EXECUTED",
		"Command    not executed",
		"[\"docker\", \"buildx\", \"prune\", \"--filter\", \"id=cache123456789\"]",
		"Confirm    required",
		"Risks      ANCIENNE,ORPHELINE_PROBABLE,RECONSTRUCTIBLE",
	} {
		if !bytes.Contains(terminal, []byte(expected)) {
			t.Errorf("terminal plan missing %q:\n%s", expected, terminal)
		}
	}
}

func reportPlanFixture(t *testing.T) domain.CleanupPlan {
	t.Helper()
	estimate := int64(2048)
	key := "installed_resources.id"
	value := "cache"
	plan := domain.CleanupPlan{
		SchemaVersion:       domain.CleanupPlanSchemaVersion,
		ToolVersion:         "test",
		Mode:                domain.PlanModeSimulationOnly,
		SourceReportSHA256:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SourceScanCompleted: "2026-09-05T12:00:01Z",
		Selection:           domain.PlanSelection{Mode: domain.SelectionDefaultSafe, RequestedResourceIDs: []string{}},
		Items: []domain.CleanupPlanItem{{
			ID: "item", ResourceID: "cache", Ecosystem: "docker", Component: "docker_build_cache", Path: "docker://build_cache/cache123456789",
			SelectedBy: domain.SelectedByDefaultSafe, EstimatedReclaimableBytes: &estimate,
			AffectedProjectIDs: []string{},
			RiskCategories:     []domain.ResourceCategory{domain.CategoryOld, domain.CategoryProbableOrphan, domain.CategoryReconstructible},
			Impact:             "future rebuild", OfficialCommand: []string{"docker", "buildx", "prune", "--filter", "id=cache123456789"},
			RequiresExplicitConfirmation: true, Rationale: "fixture",
			Evidence: []domain.Evidence{{SourceType: "scan report", Key: &key, ObservedValue: &value, RuleID: "test"}},
			Warnings: []string{},
		}},
		Excluded: []domain.CleanupPlanExclusion{},
		Summary: domain.CleanupPlanSummary{
			SelectedCount: 1, KnownPotentiallyReclaimableBytesSum: estimate,
		},
		Warnings: []string{"Simulation only: no command in this plan was executed."},
	}
	planID, err := domain.ComputeCleanupPlanID(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanID = planID
	return plan
}
