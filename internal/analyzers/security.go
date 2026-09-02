package analyzers

import "regexp"

var (
	safeVersionTokenPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+*-]*$`)
	safeDartConstraintPattern = regexp.MustCompile(`^[A-Za-z0-9<>=^][A-Za-z0-9 .<>=^|*+_-]*$`)
)

func isSafeVersionToken(value string) bool {
	return len(value) > 0 && len(value) <= 80 && safeVersionTokenPattern.MatchString(value)
}

func isSafeDartConstraint(value string) bool {
	return len(value) > 0 && len(value) <= 160 && safeDartConstraintPattern.MatchString(value)
}
