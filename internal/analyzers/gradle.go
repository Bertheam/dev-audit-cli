package analyzers

import (
	"path/filepath"
	"regexp"
	"strings"

	"dev-environment-auditor/internal/domain"
)

const (
	ruleGradleWrapper       = "gradle.wrapper.distribution-url.v1"
	ruleGradlePlugin        = "gradle.plugins-dsl.plugin-version.v1"
	ruleGradlePluginDynamic = "gradle.plugins-dsl.dynamic-version.v1"
	ruleGradleBuildscript   = "gradle.buildscript.classpath.v1"
	ruleAndroidSDK          = "android.gradle.sdk-level.v1"
	ruleAndroidNDK          = "android.gradle.ndk-version.v1"
	ruleAndroidCMake        = "android.gradle.cmake-version.v1"
	ruleJavaToolchain       = "java.gradle.toolchain.v1"
	ruleKotlinToolchain     = "kotlin.gradle.jvm-toolchain.v1"
	ruleJavaCompatibility   = "java.gradle.source-compatibility.v1"
)

var (
	gradleDistributionPattern = regexp.MustCompile(`gradle-([0-9][A-Za-z0-9.+-]*)-(bin|all)\.zip([?#].*)?$`)
	pluginKotlinPattern       = regexp.MustCompile(`id[ \t]*\([ \t]*["']([^"']+)["'][ \t]*\)[ \t]*version[ \t]*["']([^"']+)["']`)
	pluginGroovyPattern       = regexp.MustCompile(`id[ \t]+["']([^"']+)["'][ \t]+version[ \t]+["']([^"']+)["']`)
	pluginKotlinShortPattern  = regexp.MustCompile(`kotlin[ \t]*\([ \t]*["']([^"']+)["'][ \t]*\)[ \t]*version[ \t]*["']([^"']+)["']`)
	pluginIDKotlinPattern     = regexp.MustCompile(`id[ \t]*\([ \t]*["']([^"']+)["'][ \t]*\)`)
	pluginIDGroovyPattern     = regexp.MustCompile(`id[ \t]+["']([^"']+)["']`)
	pluginKotlinShortID       = regexp.MustCompile(`kotlin[ \t]*\([ \t]*["']([^"']+)["'][ \t]*\)`)
	legacyClasspathPattern    = regexp.MustCompile(`classpath[ \t(]*["']([^"']+)["']`)
	androidValuePattern       = regexp.MustCompile(`^[ \t]*(compileSdk|compileSdkVersion|minSdk|minSdkVersion|targetSdk|targetSdkVersion|ndkVersion)[ \t]*(=[ \t]*|[ \t]+)(.+?)[ \t]*$`)
	cmakeVersionPattern       = regexp.MustCompile(`^[ \t]*version[ \t]*(=[ \t]*|[ \t]+)(.+?)[ \t]*$`)
	cmakeBlockPattern         = regexp.MustCompile(`\bcmake[ \t]*\{`)
	javaToolchainPattern      = regexp.MustCompile(`JavaLanguageVersion\.of[ \t]*\([ \t]*([0-9]+)[ \t]*\)`)
	kotlinToolchainPattern    = regexp.MustCompile(`\bjvmToolchain[ \t]*\([ \t]*([0-9]+)[ \t]*\)`)
	javaToolchainAnyPattern   = regexp.MustCompile(`JavaLanguageVersion\.of[ \t]*\((.+)\)`)
	kotlinToolchainAnyPattern = regexp.MustCompile(`\bjvmToolchain[ \t]*\((.+)\)`)
	javaCompatibilityPattern  = regexp.MustCompile(`^[ \t]*(sourceCompatibility|targetCompatibility)[ \t]*(=[ \t]*|[ \t]+)JavaVersion\.VERSION_([0-9]+)[ \t]*$`)
)

func parseGradleWrapper(document document, builder *requirementBuilder, diagnostics *[]domain.Diagnostic) {
	for index, rawLine := range document.lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		separator := strings.IndexAny(line, "=:")
		if separator < 0 || strings.TrimSpace(line[:separator]) != "distributionUrl" {
			continue
		}
		value := strings.TrimSpace(line[separator+1:])
		value = strings.ReplaceAll(value, `\:`, ":")
		match := gradleDistributionPattern.FindStringSubmatch(value)
		if match == nil {
			*diagnostics = append(*diagnostics, newDiagnostic(
				"ANALYZER_GRADLE_DISTRIBUTION_UNSUPPORTED",
				domain.SeverityWarning,
				"Gradle distributionUrl does not contain a supported static distribution filename",
				document.path,
			))
			return
		}
		version := match[1]
		builder.add(
			"gradle",
			"gradle",
			&version,
			domain.RequiredExplicitly,
			[]domain.Evidence{newEvidence(
				document.path,
				"distributionUrl",
				index+1,
				version,
				ruleGradleWrapper,
			)},
		)
		return
	}
}

func parseGradleScript(document document, builder *requirementBuilder) {
	braceDepth := 0
	cmakeDepth := 0
	for index, rawLine := range document.lines {
		line := strings.TrimSpace(stripGradleComment(rawLine))
		if line == "" {
			continue
		}
		lineNumber := index + 1

		parseGradlePluginLine(document.path, lineNumber, line, builder)
		parseLegacyClasspath(document.path, lineNumber, line, builder)
		parseAndroidValue(document.path, lineNumber, line, builder)
		parseJVMToolchains(document.path, lineNumber, line, builder)

		if cmakeBlockPattern.MatchString(line) {
			cmakeDepth = braceDepth + 1
		}
		if cmakeDepth > 0 {
			parseCMakeValue(document.path, lineNumber, line, builder)
		}
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
		if cmakeDepth > 0 && braceDepth < cmakeDepth {
			cmakeDepth = 0
		}
	}
}

func parseGradlePluginLine(filePath string, lineNumber int, line string, builder *requirementBuilder) {
	if match := pluginKotlinPattern.FindStringSubmatch(line); match != nil {
		addPluginRequirement(filePath, lineNumber, match[1], match[2], ruleGradlePlugin, builder)
		return
	}
	if match := pluginGroovyPattern.FindStringSubmatch(line); match != nil {
		addPluginRequirement(filePath, lineNumber, match[1], match[2], ruleGradlePlugin, builder)
		return
	}
	if match := pluginKotlinShortPattern.FindStringSubmatch(line); match != nil {
		pluginID := kotlinShortPluginID(match[1])
		if pluginID != "" {
			addPluginRequirement(filePath, lineNumber, pluginID, match[2], ruleGradlePlugin, builder)
		}
		return
	}

	pluginID := ""
	if match := pluginIDKotlinPattern.FindStringSubmatch(line); match != nil {
		pluginID = match[1]
	} else if match := pluginIDGroovyPattern.FindStringSubmatch(line); match != nil {
		pluginID = match[1]
	} else if match := pluginKotlinShortID.FindStringSubmatch(line); match != nil {
		pluginID = kotlinShortPluginID(match[1])
	}
	if pluginID == "" || !strings.Contains(line, "version") {
		return
	}
	ecosystem, component, supported := componentForPlugin(pluginID)
	if !supported {
		return
	}
	confidence, warning, observed := dynamicConfidence(line)
	builder.add(
		ecosystem,
		component,
		nil,
		confidence,
		[]domain.Evidence{newEvidence(
			filePath,
			"plugins."+pluginID+".version",
			lineNumber,
			observed,
			ruleGradlePluginDynamic,
		)},
		warning,
	)
}

func addPluginRequirement(
	filePath string,
	lineNumber int,
	pluginID string,
	version string,
	ruleID string,
	builder *requirementBuilder,
) {
	ecosystem, component, supported := componentForPlugin(pluginID)
	if !supported {
		return
	}
	version = strings.TrimSpace(version)
	if looksDynamic(version) || !isSafeVersionToken(version) {
		confidence, warning, observed := dynamicConfidence(version)
		builder.add(
			ecosystem,
			component,
			nil,
			confidence,
			[]domain.Evidence{newEvidence(filePath, "plugins."+pluginID+".version", lineNumber, observed, ruleID)},
			warning,
		)
		return
	}
	builder.add(
		ecosystem,
		component,
		&version,
		domain.RequiredExplicitly,
		[]domain.Evidence{newEvidence(filePath, "plugins."+pluginID+".version", lineNumber, version, ruleID)},
	)
}

func componentForPlugin(pluginID string) (string, string, bool) {
	if strings.HasPrefix(pluginID, "com.android.") {
		switch pluginID {
		case "com.android.application", "com.android.library", "com.android.test", "com.android.dynamic-feature":
			return "android", "android_gradle_plugin", true
		}
	}
	if pluginID == "org.jetbrains.kotlin.android" || pluginID == "org.jetbrains.kotlin.jvm" {
		return "kotlin", "kotlin_gradle_plugin", true
	}
	return "", "", false
}

func kotlinShortPluginID(value string) string {
	switch value {
	case "android":
		return "org.jetbrains.kotlin.android"
	case "jvm":
		return "org.jetbrains.kotlin.jvm"
	default:
		return ""
	}
}

func parseLegacyClasspath(filePath string, lineNumber int, line string, builder *requirementBuilder) {
	match := legacyClasspathPattern.FindStringSubmatch(line)
	if match == nil {
		return
	}
	coordinates := strings.Split(match[1], ":")
	if len(coordinates) < 3 {
		return
	}
	ecosystem := ""
	component := ""
	switch strings.Join(coordinates[:2], ":") {
	case "com.android.tools.build:gradle":
		ecosystem = "android"
		component = "android_gradle_plugin"
	case "org.jetbrains.kotlin:kotlin-gradle-plugin":
		ecosystem = "kotlin"
		component = "kotlin_gradle_plugin"
	default:
		return
	}
	version := strings.TrimSpace(coordinates[2])
	if looksDynamic(version) || !isSafeVersionToken(version) {
		confidence, warning, observed := dynamicConfidence(version)
		builder.add(
			ecosystem,
			component,
			nil,
			confidence,
			[]domain.Evidence{newEvidence(filePath, "buildscript.classpath", lineNumber, observed, ruleGradleBuildscript)},
			warning,
		)
		return
	}
	builder.add(
		ecosystem,
		component,
		&version,
		domain.RequiredExplicitly,
		[]domain.Evidence{newEvidence(filePath, "buildscript.classpath", lineNumber, version, ruleGradleBuildscript)},
	)
}

func parseAndroidValue(filePath string, lineNumber int, line string, builder *requirementBuilder) {
	match := androidValuePattern.FindStringSubmatch(line)
	if match == nil {
		return
	}
	key := match[1]
	expression := cleanGradleExpression(match[3])
	ecosystem := "android"
	component := ""
	ruleID := ruleAndroidSDK
	switch key {
	case "compileSdk", "compileSdkVersion":
		component = "compile_sdk"
	case "minSdk", "minSdkVersion":
		component = "min_sdk"
	case "targetSdk", "targetSdkVersion":
		component = "target_sdk"
	case "ndkVersion":
		component = "ndk"
		ruleID = ruleAndroidNDK
	}
	addExpressionRequirement(filePath, lineNumber, key, expression, ecosystem, component, ruleID, builder)
}

func parseCMakeValue(filePath string, lineNumber int, line string, builder *requirementBuilder) {
	match := cmakeVersionPattern.FindStringSubmatch(line)
	if match == nil {
		return
	}
	addExpressionRequirement(
		filePath,
		lineNumber,
		"externalNativeBuild.cmake.version",
		cleanGradleExpression(match[2]),
		"android",
		"cmake",
		ruleAndroidCMake,
		builder,
	)
}

func parseJVMToolchains(filePath string, lineNumber int, line string, builder *requirementBuilder) {
	javaMatched := false
	if match := javaToolchainPattern.FindStringSubmatch(line); match != nil {
		javaMatched = true
		version := match[1]
		builder.add(
			"java",
			"jdk",
			&version,
			domain.RequiredExplicitly,
			[]domain.Evidence{newEvidence(filePath, "java.toolchain.languageVersion", lineNumber, version, ruleJavaToolchain)},
		)
	}
	if !javaMatched {
		if match := javaToolchainAnyPattern.FindStringSubmatch(line); match != nil {
			expression := cleanGradleExpression(match[1])
			confidence, warning, observed := dynamicConfidence(expression)
			builder.add(
				"java",
				"jdk",
				nil,
				confidence,
				[]domain.Evidence{newEvidence(filePath, "java.toolchain.languageVersion", lineNumber, observed, ruleJavaToolchain)},
				warning,
			)
		}
	}
	kotlinMatched := false
	if match := kotlinToolchainPattern.FindStringSubmatch(line); match != nil {
		kotlinMatched = true
		version := match[1]
		builder.add(
			"java",
			"jdk",
			&version,
			domain.RequiredExplicitly,
			[]domain.Evidence{newEvidence(filePath, "kotlin.jvmToolchain", lineNumber, version, ruleKotlinToolchain)},
		)
	}
	if !kotlinMatched {
		if match := kotlinToolchainAnyPattern.FindStringSubmatch(line); match != nil {
			expression := cleanGradleExpression(match[1])
			confidence, warning, observed := dynamicConfidence(expression)
			builder.add(
				"java",
				"jdk",
				nil,
				confidence,
				[]domain.Evidence{newEvidence(filePath, "kotlin.jvmToolchain", lineNumber, observed, ruleKotlinToolchain)},
				warning,
			)
		}
	}
	if match := javaCompatibilityPattern.FindStringSubmatch(line); match != nil {
		version := match[3]
		builder.add(
			"java",
			"jdk",
			&version,
			domain.ProbablyRequired,
			[]domain.Evidence{newEvidence(filePath, match[1], lineNumber, version, ruleJavaCompatibility)},
			"Java compatibility level is not proof of the JDK used to run Gradle",
		)
	}
}

func addExpressionRequirement(
	filePath string,
	lineNumber int,
	key string,
	expression string,
	ecosystem string,
	component string,
	ruleID string,
	builder *requirementBuilder,
) {
	if literal, ok := staticGradleLiteral(expression); ok {
		builder.add(
			ecosystem,
			component,
			&literal,
			domain.RequiredExplicitly,
			[]domain.Evidence{newEvidence(filePath, key, lineNumber, literal, ruleID)},
		)
		return
	}
	confidence, warning, observed := dynamicConfidence(expression)
	builder.add(
		ecosystem,
		component,
		nil,
		confidence,
		[]domain.Evidence{newEvidence(filePath, key, lineNumber, observed, ruleID)},
		warning,
	)
}

func staticGradleLiteral(expression string) (string, bool) {
	expression = strings.TrimSpace(expression)
	if len(expression) >= 2 &&
		((expression[0] == '"' && expression[len(expression)-1] == '"') ||
			(expression[0] == '\'' && expression[len(expression)-1] == '\'')) {
		value := strings.TrimSpace(expression[1 : len(expression)-1])
		if value != "" && !strings.Contains(value, "$") && isSafeVersionToken(value) {
			return value, true
		}
		return "", false
	}
	for _, character := range expression {
		if character < '0' || character > '9' {
			return "", false
		}
	}
	return expression, expression != ""
}

func dynamicConfidence(expression string) (domain.Confidence, string, string) {
	if strings.Contains(expression, "flutter.") || strings.Contains(expression, "libs.versions.") {
		return domain.ProbablyRequired,
			"Gradle value is a recognized static indirection but was not resolved",
			"recognized Gradle indirection"
	}
	return domain.ConfidenceUnknown,
		"Gradle value is dynamic and was not evaluated",
		"dynamic Gradle expression"
}

func cleanGradleExpression(expression string) string {
	expression = strings.TrimSpace(expression)
	expression = strings.TrimSuffix(expression, ",")
	return strings.TrimSpace(expression)
}

func stripGradleComment(line string) string {
	var quote rune
	for index, character := range line {
		switch character {
		case '\'', '"':
			if quote == 0 {
				quote = character
			} else if quote == character {
				quote = 0
			}
		case '/':
			if quote == 0 && index+1 < len(line) && line[index+1] == '/' {
				return line[:index]
			}
		}
	}
	return line
}

func isGradleScript(document document) bool {
	base := filepath.Base(document.path)
	return base == "settings.gradle" || base == "settings.gradle.kts" ||
		base == "build.gradle" || base == "build.gradle.kts"
}
