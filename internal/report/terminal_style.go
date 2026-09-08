package report

import "fmt"

// TerminalOptions controls presentation only. It never changes the underlying
// scan or cleanup-plan data.
type TerminalOptions struct {
	Color   bool
	Verbose bool
}

type terminalStyle struct {
	color bool
}

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiCyan   = "\x1b[36m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiPurple = "\x1b[35m"
)

func newTerminalStyle(options TerminalOptions) terminalStyle {
	return terminalStyle{color: options.Color}
}

func (style terminalStyle) paint(code, value string) string {
	if !style.color || value == "" {
		return value
	}
	return code + value + ansiReset
}

func (style terminalStyle) title(value string) string   { return style.paint(ansiPurple+ansiBold, value) }
func (style terminalStyle) heading(value string) string { return style.paint(ansiBold, value) }
func (style terminalStyle) accent(value string) string  { return style.paint(ansiCyan, value) }
func (style terminalStyle) success(value string) string { return style.paint(ansiGreen, value) }
func (style terminalStyle) warning(value string) string { return style.paint(ansiYellow, value) }
func (style terminalStyle) failure(value string) string { return style.paint(ansiRed, value) }
func (style terminalStyle) muted(value string) string   { return style.paint(ansiDim, value) }

func (style terminalStyle) section(title string) string {
	return "\n" + style.heading(title) + "\n"
}

func plural(count int, singular, pluralValue string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}
	return fmt.Sprintf("%d %s", count, pluralValue)
}

func statusSymbol(style terminalStyle, status string) string {
	switch status {
	case "MATCHED", "REFERENCED", "UTILISEE", "OK":
		return style.success("✓")
	case "MISSING", "ERROR":
		return style.failure("✗")
	case "AMBIGUOUS", "WARNING", "SENSIBLE":
		return style.warning("!")
	case "UNKNOWN", "INCONNUE":
		return style.warning("?")
	case "ORPHELINE_PROBABLE", "NO_REFERENCE_FOUND":
		return style.accent("○")
	default:
		return style.muted("•")
	}
}
