package inventory

import "regexp"

var (
	safeVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+*-]*$`)
	androidAPILevel    = regexp.MustCompile(`^[0-9]+$`)
	androidImageAPI    = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
)

func isSafeVersion(value string) bool {
	return len(value) > 0 && len(value) <= 80 && safeVersionPattern.MatchString(value)
}
