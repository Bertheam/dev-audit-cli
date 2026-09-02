package analyzers

import (
	"regexp"
	"strings"

	"dev-environment-auditor/internal/domain"
)

const ruleVersionCatalogPlugin = "gradle.version-catalog.plugin-alias.v1"

var (
	tomlSectionPattern         = regexp.MustCompile(`^[ \t]*\[([A-Za-z0-9_.-]+)\][ \t]*$`)
	tomlEntryPattern           = regexp.MustCompile(`^[ \t]*([A-Za-z0-9_.-]+)[ \t]*=[ \t]*(.+?)[ \t]*$`)
	tomlStringPattern          = regexp.MustCompile(`^["']([^"']+)["']$`)
	tomlPluginIDPattern        = regexp.MustCompile(`\bid[ \t]*=[ \t]*["']([^"']+)["']`)
	tomlPluginVersionPattern   = regexp.MustCompile(`\bversion[ \t]*=[ \t]*["']([^"']+)["']`)
	tomlPluginReferencePattern = regexp.MustCompile(`\bversion\.ref[ \t]*=[ \t]*["']([^"']+)["']`)
	versionCatalogAliasPattern = regexp.MustCompile(`alias[ \t]*\([ \t]*libs\.plugins\.([A-Za-z0-9_.-]+)[ \t]*\)`)
)

type catalogValue struct {
	value string
	line  int
}

type catalogPlugin struct {
	alias      string
	pluginID   string
	version    string
	versionRef string
	line       int
}

type versionCatalog struct {
	path     string
	versions map[string]catalogValue
	plugins  map[string]catalogPlugin
}

func parseVersionCatalog(document document, diagnostics *[]domain.Diagnostic) *versionCatalog {
	catalog := &versionCatalog{
		path:     document.path,
		versions: make(map[string]catalogValue),
		plugins:  make(map[string]catalogPlugin),
	}
	section := ""
	for index, rawLine := range document.lines {
		line := strings.TrimSpace(stripTOMLComment(rawLine))
		if line == "" {
			continue
		}
		if match := tomlSectionPattern.FindStringSubmatch(line); match != nil {
			section = match[1]
			continue
		}
		match := tomlEntryPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		key := match[1]
		value := strings.TrimSpace(match[2])
		switch section {
		case "versions":
			stringMatch := tomlStringPattern.FindStringSubmatch(value)
			if stringMatch != nil && strings.TrimSpace(stringMatch[1]) != "" {
				catalog.versions[key] = catalogValue{value: strings.TrimSpace(stringMatch[1]), line: index + 1}
			}
		case "plugins":
			plugin, ok := parseCatalogPlugin(key, value, index+1)
			if !ok {
				*diagnostics = append(*diagnostics, newDiagnostic(
					"ANALYZER_VERSION_CATALOG_PLUGIN_UNSUPPORTED",
					domain.SeverityWarning,
					"version catalog plugin entry is not a supported static declaration",
					document.path,
				))
				continue
			}
			catalog.plugins[normalizeCatalogAlias(key)] = plugin
		}
	}
	return catalog
}

func parseCatalogPlugin(alias, value string, line int) (catalogPlugin, bool) {
	plugin := catalogPlugin{alias: alias, line: line}
	if match := tomlStringPattern.FindStringSubmatch(value); match != nil {
		parts := strings.Split(match[1], ":")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return catalogPlugin{}, false
		}
		plugin.pluginID = strings.TrimSpace(parts[0])
		plugin.version = strings.TrimSpace(parts[1])
		return plugin, true
	}
	idMatch := tomlPluginIDPattern.FindStringSubmatch(value)
	if idMatch == nil {
		return catalogPlugin{}, false
	}
	plugin.pluginID = strings.TrimSpace(idMatch[1])
	if match := tomlPluginReferencePattern.FindStringSubmatch(value); match != nil {
		plugin.versionRef = strings.TrimSpace(match[1])
		return plugin, plugin.versionRef != ""
	}
	if match := tomlPluginVersionPattern.FindStringSubmatch(value); match != nil {
		plugin.version = strings.TrimSpace(match[1])
		return plugin, plugin.version != ""
	}
	return plugin, true
}

func parseCatalogAliases(
	document document,
	catalog *versionCatalog,
	builder *requirementBuilder,
	diagnostics *[]domain.Diagnostic,
) {
	if catalog == nil {
		return
	}
	for index, rawLine := range document.lines {
		line := stripGradleComment(rawLine)
		matches := versionCatalogAliasPattern.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			aliasKey := normalizeCatalogAlias(match[1])
			plugin, exists := catalog.plugins[aliasKey]
			if !exists {
				continue
			}
			ecosystem, component, supported := componentForPlugin(plugin.pluginID)
			if !supported {
				continue
			}
			evidence := []domain.Evidence{
				newEvidence(
					document.path,
					"plugins.alias."+match[1],
					index+1,
					"version catalog alias",
					ruleVersionCatalogPlugin,
				),
				newEvidence(
					catalog.path,
					"plugins."+plugin.alias,
					plugin.line,
					plugin.pluginID,
					ruleVersionCatalogPlugin,
				),
			}
			version := plugin.version
			if plugin.versionRef != "" {
				catalogVersion, resolved := catalog.versions[plugin.versionRef]
				if resolved {
					version = catalogVersion.value
					observed := catalogVersion.value
					if !isSafeVersionToken(observed) {
						observed = "unsupported version catalog value"
					}
					evidence = append(evidence, newEvidence(
						catalog.path,
						"versions."+plugin.versionRef,
						catalogVersion.line,
						observed,
						ruleVersionCatalogPlugin,
					))
				} else {
					*diagnostics = append(*diagnostics, newDiagnostic(
						"ANALYZER_VERSION_CATALOG_REFERENCE_UNRESOLVED",
						domain.SeverityWarning,
						"used plugin alias has an unresolved version reference",
						catalog.path,
					))
				}
			}
			if version == "" || looksDynamic(version) || !isSafeVersionToken(version) {
				builder.add(
					ecosystem,
					component,
					nil,
					domain.ConfidenceUnknown,
					evidence,
					"version catalog plugin alias has no resolvable static version",
				)
				continue
			}
			builder.add(
				ecosystem,
				component,
				&version,
				domain.RequiredExplicitly,
				evidence,
			)
		}
	}
}

func normalizeCatalogAlias(alias string) string {
	alias = strings.ReplaceAll(alias, "-", ".")
	alias = strings.ReplaceAll(alias, "_", ".")
	for strings.Contains(alias, "..") {
		alias = strings.ReplaceAll(alias, "..", ".")
	}
	return strings.Trim(alias, ".")
}

func stripTOMLComment(line string) string {
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
