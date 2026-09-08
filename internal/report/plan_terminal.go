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
	return RenderPlanTerminalWithOptions(plan, TerminalOptions{})
}

// RenderPlanTerminalWithOptions renders a reviewable, human-first simulation.
// Presentation options never change the immutable plan or execute its commands.
func RenderPlanTerminalWithOptions(plan domain.CleanupPlan, options TerminalOptions) ([]byte, error) {
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("validate cleanup plan: %w", err)
	}
	style := newTerminalStyle(options)
	var output strings.Builder
	fmt.Fprintf(&output, "%s  %s\n", style.title("◆ dev-audit"), style.heading("plan"))
	fmt.Fprintf(&output, "  %s · %s · %s\n",
		style.warning("SIMULATION ONLY"), style.warning("NOT EXECUTED"), style.muted(terminalText(plan.ToolVersion)))

	output.WriteString(style.section("OVERVIEW"))
	estimate := plan.Summary.KnownPotentiallyReclaimableBytesSum
	fmt.Fprintf(&output, "  %s selected · %s excluded · %s estimated reclaimable · %d without estimate\n",
		style.accent(fmt.Sprintf("%d", plan.Summary.SelectedCount)),
		style.muted(fmt.Sprintf("%d", plan.Summary.ExcludedCount)),
		style.accent(formatSize(&estimate)),
		plan.Summary.SelectedItemsWithoutEstimate)
	fmt.Fprintf(&output, "  Selection  %s", plan.Selection.Mode)
	if len(plan.Selection.RequestedResourceIDs) > 0 {
		fmt.Fprintf(&output, " (%d requested)", len(plan.Selection.RequestedResourceIDs))
	}
	output.WriteByte('\n')
	fmt.Fprintf(&output, "  Plan       %s\n", style.muted(terminalText(plan.PlanID)))
	fmt.Fprintf(&output, "  Source     %s · scan completed %s\n",
		style.muted(terminalText(plan.SourceReportSHA256)), terminalText(plan.SourceScanCompleted))

	output.WriteString(style.section(fmt.Sprintf("ACTIONS  %d", len(plan.Items))))
	if len(plan.Items) == 0 {
		fmt.Fprintf(&output, "  %s No action selected\n", style.muted("—"))
	}
	for index, item := range plan.Items {
		fmt.Fprintf(&output, "  %s %d. %s/%s · %s\n",
			style.warning("!"), index+1, terminalText(item.Ecosystem), terminalText(item.Component),
			style.accent(formatSize(item.EstimatedReclaimableBytes)))
		fmt.Fprintf(&output, "    %s\n", terminalText(item.Path))
		fmt.Fprintf(&output, "    Resource   %s · selected by %s\n",
			style.muted(terminalText(item.ResourceID)), item.SelectedBy)
		fmt.Fprintf(&output, "    Size       observed %s · estimated %s\n",
			formatSize(item.ObservedSizeBytes), formatSize(item.EstimatedReclaimableBytes))
		fmt.Fprintf(&output, "    Risks      %s\n", formatPlanCategories(item.RiskCategories))
		fmt.Fprintf(&output, "    Projects   %s\n", formatPlanStrings(item.AffectedProjectIDs))
		fmt.Fprintf(&output, "    Impact     %s\n", terminalText(item.Impact))
		fmt.Fprintf(&output, "    Command    %s\n", style.warning("not executed"))
		fmt.Fprintf(&output, "      %s\n", formatCommandArguments(item.OfficialCommand))
		fmt.Fprintf(&output, "    Confirm    %s\n", confirmationLabel(style, item.RequiresExplicitConfirmation))
		fmt.Fprintf(&output, "    Why        %s\n", terminalText(item.Rationale))
		if options.Verbose {
			fmt.Fprintf(&output, "    Item       %s\n", style.muted(terminalText(item.ID)))
		}
		for _, warning := range item.Warnings {
			fmt.Fprintf(&output, "    %s %s\n", style.warning("!"), terminalText(warning))
		}
	}

	output.WriteString(style.section(fmt.Sprintf("EXCLUDED  %d", len(plan.Excluded))))
	if len(plan.Excluded) == 0 {
		fmt.Fprintf(&output, "  %s No excluded resources\n", style.muted("—"))
	}
	for _, excluded := range plan.Excluded {
		fmt.Fprintf(&output, "  %s %s · %s\n", style.muted("•"),
			terminalText(excluded.ResourceID), formatExclusionReasons(excluded.Reasons))
	}

	output.WriteString(style.section("SAFETY"))
	for _, warning := range plan.Warnings {
		fmt.Fprintf(&output, "  %s %s\n", style.warning("!"), terminalText(warning))
	}
	output.WriteString("\n  Review only. dev-audit never executes commands from this plan.\n")
	return []byte(output.String()), nil
}

func confirmationLabel(style terminalStyle, required bool) string {
	if required {
		return style.warning("required")
	}
	return style.muted("not required")
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
		quoted[index] = terminalText(value)
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
