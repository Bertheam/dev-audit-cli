package correlation

import (
	"regexp"
	"strconv"
	"strings"

	"dev-environment-auditor/internal/domain"
)

var stableSemanticVersionPattern = regexp.MustCompile(
	`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`,
)

type semanticVersion struct {
	major int
	minor int
	patch int
}

type comparison struct {
	operator string
	version  semanticVersion
}

func compileEvaluator(
	target requirementTarget,
	constraint string,
) (func(domain.InstalledResource) candidateEvaluation, bool) {
	switch target.strategy {
	case strategyExact:
		return compileExactEvaluator(constraint)
	case strategyFlutter:
		return compileFlutterEvaluator(constraint)
	case strategyDart:
		return compileDartEvaluator(constraint)
	case strategyJDKFeature:
		return compileJDKEvaluator(constraint)
	case strategyDockerImage:
		return compileDockerImageEvaluator(constraint)
	default:
		return nil, false
	}
}

func compileDockerImageEvaluator(constraint string) (func(domain.InstalledResource) candidateEvaluation, bool) {
	wanted, valid := normalizeDockerImageReference(constraint)
	if !valid {
		return nil, false
	}
	wantedRepository, wantedDigest, digestReference := strings.Cut(wanted, "@")
	return func(resource domain.InstalledResource) candidateEvaluation {
		references := metadataValues(resource.Metadata, "reference")
		if !digestReference {
			for _, reference := range references {
				if normalized, ok := normalizeDockerImageReference(reference); ok && normalized == wanted {
					return candidateMatch
				}
			}
			if len(references) == 0 && resource.Version != nil {
				if normalized, ok := normalizeDockerImageReference(*resource.Version); ok && normalized == wanted {
					return candidateMatch
				}
			}
			return candidateNoMatch
		}

		digestMatched := false
		for _, digest := range metadataValues(resource.Metadata, "digest") {
			if strings.EqualFold(strings.TrimSpace(digest), wantedDigest) {
				digestMatched = true
				break
			}
		}
		if !digestMatched {
			return candidateNoMatch
		}
		for _, reference := range references {
			normalized, ok := normalizeDockerImageReference(reference)
			if !ok {
				continue
			}
			repository := strings.SplitN(normalized, "@", 2)[0]
			if colon := strings.LastIndex(repository, ":"); colon > strings.LastIndex(repository, "/") {
				repository = repository[:colon]
			}
			if repository == wantedRepository {
				return candidateMatch
			}
		}
		return candidateNoMatch
	}, true
}

func normalizeDockerImageReference(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t\r\n$") {
		return "", false
	}
	name := value
	digest := ""
	if at := strings.LastIndex(name, "@"); at >= 0 {
		digest = name[at+1:]
		name = name[:at]
		if name == "" || digest == "" || !strings.Contains(digest, ":") {
			return "", false
		}
	}
	slash := strings.LastIndex(name, "/")
	colon := strings.LastIndex(name, ":")
	tag := ""
	if colon > slash {
		tag = name[colon+1:]
		name = name[:colon]
		if tag == "" {
			return "", false
		}
	}
	if name == "" {
		return "", false
	}
	parts := strings.Split(name, "/")
	first := parts[0]
	if len(parts) == 1 {
		name = "docker.io/library/" + name
	} else if !strings.Contains(first, ".") && !strings.Contains(first, ":") && first != "localhost" {
		name = "docker.io/" + name
	} else if first == "index.docker.io" {
		name = "docker.io/" + strings.Join(parts[1:], "/")
	}
	if strings.HasPrefix(name, "docker.io/") && strings.Count(name, "/") == 1 {
		name = "docker.io/library/" + strings.TrimPrefix(name, "docker.io/")
	}
	name = strings.ToLower(name)
	if digest != "" {
		return name + "@" + strings.ToLower(digest), true
	}
	if tag == "" {
		tag = "latest"
	}
	return name + ":" + tag, true
}

func compileExactEvaluator(constraint string) (func(domain.InstalledResource) candidateEvaluation, bool) {
	wanted := strings.TrimSpace(constraint)
	if wanted == "" {
		return nil, false
	}
	if strings.EqualFold(wanted, "any") {
		return func(domain.InstalledResource) candidateEvaluation {
			return candidateMatch
		}, true
	}
	return func(resource domain.InstalledResource) candidateEvaluation {
		if resource.Version == nil || strings.TrimSpace(*resource.Version) == "" {
			return candidateUnknown
		}
		if strings.TrimSpace(*resource.Version) == wanted {
			return candidateMatch
		}
		return candidateNoMatch
	}, true
}

func compileFlutterEvaluator(constraint string) (func(domain.InstalledResource) candidateEvaluation, bool) {
	wanted := strings.TrimSpace(constraint)
	if strings.EqualFold(wanted, "any") {
		return func(domain.InstalledResource) candidateEvaluation {
			return candidateMatch
		}, true
	}
	if _, valid := parseStableSemanticVersion(wanted); valid {
		return compileExactEvaluator(wanted)
	}

	wantedChannel := strings.ToLower(wanted)
	switch wantedChannel {
	case "stable", "beta", "dev", "master":
		return func(resource domain.InstalledResource) candidateEvaluation {
			if resource.Version != nil && strings.EqualFold(strings.TrimSpace(*resource.Version), wantedChannel) {
				return candidateMatch
			}
			channel, exists := metadataValue(resource.Metadata, "channel")
			if !exists || strings.TrimSpace(channel) == "" {
				return candidateUnknown
			}
			if strings.EqualFold(strings.TrimSpace(channel), wantedChannel) {
				return candidateMatch
			}
			return candidateNoMatch
		}, true
	default:
		return nil, false
	}
}

func compileDartEvaluator(constraint string) (func(domain.InstalledResource) candidateEvaluation, bool) {
	matches, supported := compileDartConstraint(constraint)
	if !supported {
		return nil, false
	}
	return func(resource domain.InstalledResource) candidateEvaluation {
		rawVersion, exists := metadataValue(resource.Metadata, "dart_sdk_version")
		if !exists || strings.TrimSpace(rawVersion) == "" {
			return candidateUnknown
		}
		version, valid := parseStableSemanticVersion(strings.TrimSpace(rawVersion))
		if !valid {
			return candidateUnknown
		}
		if matches(version) {
			return candidateMatch
		}
		return candidateNoMatch
	}, true
}

func compileJDKEvaluator(constraint string) (func(domain.InstalledResource) candidateEvaluation, bool) {
	wanted, valid := parseJDKFeature(strings.TrimSpace(constraint))
	if !valid || strconv.Itoa(wanted) != strings.TrimSpace(constraint) {
		return nil, false
	}
	return func(resource domain.InstalledResource) candidateEvaluation {
		if resource.Version == nil {
			return candidateUnknown
		}
		actual, known := parseJDKFeature(strings.TrimSpace(*resource.Version))
		if !known {
			return candidateUnknown
		}
		if actual == wanted {
			return candidateMatch
		}
		return candidateNoMatch
	}, true
}

func compileDartConstraint(raw string) (func(semanticVersion) bool, bool) {
	constraint := strings.TrimSpace(raw)
	if strings.EqualFold(constraint, "any") {
		return func(semanticVersion) bool { return true }, true
	}
	if strings.HasPrefix(constraint, "^") {
		if strings.ContainsAny(strings.TrimPrefix(constraint, "^"), " \t") {
			return nil, false
		}
		minimum, valid := parseStableSemanticVersion(strings.TrimPrefix(constraint, "^"))
		if !valid {
			return nil, false
		}
		maximum := semanticVersion{major: minimum.major + 1}
		if minimum.major == 0 {
			maximum = semanticVersion{minor: minimum.minor + 1}
		}
		return func(version semanticVersion) bool {
			return compareSemantic(version, minimum) >= 0 && compareSemantic(version, maximum) < 0
		}, true
	}

	fields := strings.Fields(constraint)
	if len(fields) == 0 {
		return nil, false
	}
	comparisons := make([]comparison, 0, len(fields))
	for _, field := range fields {
		operator := "="
		versionText := field
		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(field, candidate) {
				operator = candidate
				versionText = strings.TrimPrefix(field, candidate)
				break
			}
		}
		version, valid := parseStableSemanticVersion(versionText)
		if !valid {
			return nil, false
		}
		comparisons = append(comparisons, comparison{operator: operator, version: version})
	}

	return func(version semanticVersion) bool {
		for _, condition := range comparisons {
			order := compareSemantic(version, condition.version)
			switch condition.operator {
			case "=":
				if order != 0 {
					return false
				}
			case ">=":
				if order < 0 {
					return false
				}
			case ">":
				if order <= 0 {
					return false
				}
			case "<=":
				if order > 0 {
					return false
				}
			case "<":
				if order >= 0 {
					return false
				}
			default:
				return false
			}
		}
		return true
	}, true
}

func parseStableSemanticVersion(raw string) (semanticVersion, bool) {
	match := stableSemanticVersionPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return semanticVersion{}, false
	}
	parts := [3]int{}
	for index := range parts {
		value, err := strconv.Atoi(match[index+1])
		if err != nil {
			return semanticVersion{}, false
		}
		parts[index] = value
	}
	return semanticVersion{major: parts[0], minor: parts[1], patch: parts[2]}, true
}

func compareSemantic(left, right semanticVersion) int {
	leftParts := [...]int{left.major, left.minor, left.patch}
	rightParts := [...]int{right.major, right.minor, right.patch}
	for index := range leftParts {
		if leftParts[index] < rightParts[index] {
			return -1
		}
		if leftParts[index] > rightParts[index] {
			return 1
		}
	}
	return 0
}

func parseJDKFeature(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	parts := strings.FieldsFunc(raw, func(value rune) bool {
		return value == '.' || value == '_' || value == '-' || value == '+'
	})
	if len(parts) == 0 {
		return 0, false
	}
	index := 0
	if parts[0] == "1" && len(parts) > 1 {
		index = 1
	}
	feature, err := strconv.Atoi(parts[index])
	if err != nil || feature <= 0 {
		return 0, false
	}
	return feature, true
}

func metadataValue(metadata []domain.MetadataEntry, key string) (string, bool) {
	for _, entry := range metadata {
		if entry.Key == key {
			return entry.Value, true
		}
	}
	return "", false
}

func metadataValues(metadata []domain.MetadataEntry, key string) []string {
	values := []string{}
	for _, entry := range metadata {
		if entry.Key == key {
			values = append(values, entry.Value)
		}
	}
	return values
}
