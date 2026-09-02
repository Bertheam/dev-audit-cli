package analyzers

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"dev-environment-auditor/internal/domain"
)

const (
	ruleFVMCurrent = "flutter.fvmrc.flutter.v1"
	ruleFVMFlavor  = "flutter.fvmrc.flavor.v1"
	ruleFVMLegacy  = "flutter.fvm-config.flutter-sdk-version.v1"
	rulePubDartSDK = "dart.pubspec.environment-sdk.v1"
	rulePubFlutter = "flutter.pubspec.environment-flutter.v1"
)

var yamlEnvironmentKey = regexp.MustCompile(`^([ \t]+)(sdk|flutter)[ \t]*:[ \t]*(.*)$`)

func parseFVM(document document, builder *requirementBuilder, diagnostics *[]domain.Diagnostic) {
	var raw map[string]json.RawMessage
	content := []byte(strings.Join(document.lines, "\n"))
	if err := json.Unmarshal(content, &raw); err != nil {
		*diagnostics = append(*diagnostics, newDiagnostic(
			"ANALYZER_FVM_MALFORMED",
			domain.SeverityWarning,
			"FVM configuration is not valid JSON",
			document.path,
		))
		return
	}

	key := "flutter"
	ruleID := ruleFVMCurrent
	if filepath.Base(document.path) == "fvm_config.json" {
		key = "flutterSdkVersion"
		ruleID = ruleFVMLegacy
	} else {
		parseFVMFlavors(document, raw, builder, diagnostics)
	}
	rawVersion, exists := raw[key]
	if !exists {
		return
	}
	var version string
	if err := json.Unmarshal(rawVersion, &version); err != nil || strings.TrimSpace(version) == "" {
		*diagnostics = append(*diagnostics, newDiagnostic(
			"ANALYZER_FVM_VERSION_INVALID",
			domain.SeverityWarning,
			fmt.Sprintf("FVM key %s must contain a non-empty string", key),
			document.path,
		))
		return
	}
	version = strings.TrimSpace(version)
	line := findLineContaining(document.lines, `"`+key+`"`)
	if looksDynamic(version) || !isSafeVersionToken(version) {
		evidence := newEvidence(document.path, key, line, "unsupported FVM version token", ruleID)
		builder.add(
			"flutter",
			"flutter_sdk",
			nil,
			domain.ConfidenceUnknown,
			[]domain.Evidence{evidence},
			"FVM version is dynamic or unsupported and was not evaluated",
		)
		return
	}
	evidence := newEvidence(document.path, key, line, version, ruleID)
	builder.add(
		"flutter",
		"flutter_sdk",
		&version,
		domain.RequiredExplicitly,
		[]domain.Evidence{evidence},
	)
}

func parseFVMFlavors(
	document document,
	raw map[string]json.RawMessage,
	builder *requirementBuilder,
	diagnostics *[]domain.Diagnostic,
) {
	rawFlavors, exists := raw["flavors"]
	if !exists {
		return
	}
	var flavors map[string]string
	if err := json.Unmarshal(rawFlavors, &flavors); err != nil {
		*diagnostics = append(*diagnostics, newDiagnostic(
			"ANALYZER_FVM_FLAVORS_INVALID",
			domain.SeverityWarning,
			"FVM flavors must map names to Flutter version strings",
			document.path,
		))
		return
	}
	names := make([]string, 0, len(flavors))
	for name := range flavors {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		version := strings.TrimSpace(flavors[name])
		if version == "" {
			continue
		}
		line := findLineContaining(document.lines, `"`+name+`"`)
		if looksDynamic(version) || !isSafeVersionToken(version) {
			evidence := newEvidence(document.path, "flavors."+name, line, "unsupported FVM flavor version", ruleFVMFlavor)
			builder.add(
				"flutter",
				"flutter_sdk",
				nil,
				domain.ConfidenceUnknown,
				[]domain.Evidence{evidence},
				"FVM flavor version is dynamic or unsupported and was not evaluated",
			)
			continue
		}
		evidence := newEvidence(document.path, "flavors."+name, line, version, ruleFVMFlavor)
		builder.add(
			"flutter",
			"flutter_sdk",
			&version,
			domain.RequiredExplicitly,
			[]domain.Evidence{evidence},
		)
	}
}

func parsePubspec(document document, builder *requirementBuilder, diagnostics *[]domain.Diagnostic) {
	inEnvironment := false
	environmentIndent := 0
	foundEnvironment := false

	for index, rawLine := range document.lines {
		line := stripYAMLComment(rawLine)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := leadingWhitespace(line)
		if indent == 0 {
			inEnvironment = false
			if strings.HasPrefix(trimmed, "environment:") {
				foundEnvironment = true
				remainder := strings.TrimSpace(strings.TrimPrefix(trimmed, "environment:"))
				if remainder != "" {
					*diagnostics = append(*diagnostics, newDiagnostic(
						"ANALYZER_PUBSPEC_ENVIRONMENT_UNSUPPORTED",
						domain.SeverityWarning,
						"pubspec environment must be a block mapping for static analysis",
						document.path,
					))
					continue
				}
				inEnvironment = true
				environmentIndent = indent
			}
			continue
		}
		if !inEnvironment || indent <= environmentIndent {
			continue
		}

		match := yamlEnvironmentKey.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		key := match[2]
		value, ok := yamlScalar(match[3])
		if !ok {
			*diagnostics = append(*diagnostics, newDiagnostic(
				"ANALYZER_PUBSPEC_CONSTRAINT_INVALID",
				domain.SeverityWarning,
				fmt.Sprintf("pubspec environment.%s is not a supported scalar", key),
				document.path,
			))
			continue
		}
		ruleID := rulePubDartSDK
		ecosystem := "dart"
		component := "dart_sdk"
		if key == "flutter" {
			ruleID = rulePubFlutter
			ecosystem = "flutter"
			component = "flutter_sdk"
		}
		if looksDynamic(value) || !isSafeDartConstraint(value) {
			evidence := newEvidence(document.path, "environment."+key, index+1, "unsupported pubspec SDK constraint", ruleID)
			builder.add(
				ecosystem,
				component,
				nil,
				domain.ConfidenceUnknown,
				[]domain.Evidence{evidence},
				"pubspec SDK constraint is dynamic or unsupported and was not evaluated",
			)
			continue
		}
		evidence := newEvidence(document.path, "environment."+key, index+1, value, ruleID)
		builder.add(
			ecosystem,
			component,
			&value,
			domain.RequiredExplicitly,
			[]domain.Evidence{evidence},
		)
	}

	if !foundEnvironment {
		return
	}
}

func stripYAMLComment(line string) string {
	var quote rune
	for index, character := range line {
		switch character {
		case '\'', '"':
			if quote == 0 {
				quote = character
			} else if quote == character {
				quote = 0
			}
		case '#':
			if quote == 0 {
				return line[:index]
			}
		}
	}
	return line
}

func yamlScalar(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{") {
		return "", false
	}
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	value = strings.TrimSpace(value)
	return value, value != ""
}

func leadingWhitespace(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func findLineContaining(lines []string, token string) int {
	for index, line := range lines {
		if strings.Contains(line, token) {
			return index + 1
		}
	}
	return 1
}

func looksDynamic(value string) bool {
	return strings.Contains(value, "${") ||
		strings.Contains(value, "{{") ||
		strings.HasPrefix(value, "$") ||
		strings.HasPrefix(value, "*")
}
