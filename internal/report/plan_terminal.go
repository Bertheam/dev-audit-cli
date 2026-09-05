package report

import (
	"fmt"
	"strconv"
	"strings"

	"dev-environment-auditor/internal/domain"
)

// RenderPlanTerminal renders a reviewable simulation. It intentionally labels
// every command as unexecuted and preserves argument boundaries.
func RenderPlanTerminal(plan domain.CleanupPlan) ([]byte, error) {
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("validate cleanup plan: %w", err)
	}
	var output strings.Builder
	fmt.Fprintf(&output, "Dev Environment Auditor cleanup plan %s\n", terminalValue(plan.ToolVersion))
	fmt.Fprintf(&output, "Mode: %s (nothing was executed)\n", plan.Mode)
	fmt.Fprintf(&output, "Plan ID: %s\n", terminalValue(plan.PlanID))
	fmt.Fprintf(&output, "Source report SHA-256: %s\n", terminalValue(plan.SourceReportSHA256))
	fmt.Fprintf(&output, "Source scan completed: %s\n", terminalValue(plan.SourceScanCompleted))
	fmt.Fprintf(&output, "Selection: %s", plan.Selection.Mode)
	if len(plan.Selection.RequestedResourceIDs) > 0 {
		fmt.Fprintf(&output, " (%d requested)", len(plan.Selection.RequestedResourceIDs))
	}
	output.WriteByte('\n')
	estimate := plan.Summary.KnownPotentiallyReclaimableBytesSum
	fmt.Fprintf(&output,
		"Summary: selected=%d excluded=%d known_estimate_sum=%s selected_without_estimate=%d\n",
		plan.Summary.SelectedCount,
		plan.Summary.ExcludedCount,
		formatSize(&estimate),
		plan.Summary.SelectedItemsWithoutEstimate,
	)

	fmt.Fprintf(&output, "\nSimulated items (%d):\n", len(plan.Items))
	if len(plan.Items) == 0 {
		output.WriteString("  (none)\n")
	}
	for _, item := range plan.Items {
		fmt.Fprintf(&output, "- %s resource=%s selected_by=%s\n",
			terminalValue(item.ID), terminalValue(item.ResourceID), item.SelectedBy)
		fmt.Fprintf(&output, "    type=%s/%s path=%s\n",
			terminalValue(item.Ecosystem), terminalValue(item.Component), terminalValue(item.Path))
		fmt.Fprintf(&output, "    observed_size=%s estimated_reclaimable=%s risks=%s\n",
			formatSize(item.ObservedSizeBytes), formatSize(item.EstimatedReclaimableBytes), formatPlanCategories(item.RiskCategories))
		fmt.Fprintf(&output, "    affected_projects=%s\n", formatPlanStrings(item.AffectedProjectIDs))
		fmt.Fprintf(&output, "    impact=%s\n", terminalValue(item.Impact))
		fmt.Fprintf(&output, "    command_not_executed=%s\n", formatCommandArguments(item.OfficialCommand))
		fmt.Fprintf(&output, "    explicit_confirmation_required=%t\n", item.RequiresExplicitConfirmation)
		fmt.Fprintf(&output, "    rationale=%s\n", terminalValue(item.Rationale))
		for _, warning := range item.Warnings {
			fmt.Fprintf(&output, "    warning=%s\n", terminalValue(warning))
		}
	}

	fmt.Fprintf(&output, "\nExcluded resources (%d):\n", len(plan.Excluded))
	if len(plan.Excluded) == 0 {
		output.WriteString("  (none)\n")
	}
	for _, excluded := range plan.Excluded {
		fmt.Fprintf(&output, "- resource=%s reasons=%s\n",
			terminalValue(excluded.ResourceID), formatExclusionReasons(excluded.Reasons))
	}

	output.WriteString("\nSafety:\n")
	for _, warning := range plan.Warnings {
		fmt.Fprintf(&output, "- %s\n", terminalValue(warning))
	}
	return []byte(output.String()), nil
}

func formatCommandArguments(arguments []string) string {
	quoted := make([]string, len(arguments))
	for index, argument := range arguments {
		quoted[index] = strconv.QuoteToASCII(argument)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func formatPlanCategories(categories []domain.ResourceCategory) string {
	if len(categories) == 0 {
		return "none"
	}
	values := make([]string, len(categories))
	for index, category := range categories {
		values[index] = string(category)
	}
	return strings.Join(values, ",")
}

func formatPlanStrings(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = terminalValue(value)
	}
	return strings.Join(quoted, ",")
}

func formatExclusionReasons(reasons []domain.PlanExclusionReason) string {
	values := make([]string, len(reasons))
	for index, reason := range reasons {
		values[index] = string(reason)
	}
	return strings.Join(values, ",")
}
