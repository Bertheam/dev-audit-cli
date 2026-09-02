package analyzers

import (
	"path/filepath"

	"dev-environment-auditor/internal/domain"
)

func parseDocuments(
	documents []document,
	builder *requirementBuilder,
	diagnostics *[]domain.Diagnostic,
) {
	var catalog *versionCatalog
	for _, document := range documents {
		if filepath.Base(document.path) == "libs.versions.toml" {
			catalog = parseVersionCatalog(document, diagnostics)
			break
		}
	}

	for _, document := range documents {
		switch filepath.Base(document.path) {
		case ".fvmrc", "fvm_config.json":
			parseFVM(document, builder, diagnostics)
		case "pubspec.yaml":
			parsePubspec(document, builder, diagnostics)
		case "gradle-wrapper.properties":
			parseGradleWrapper(document, builder, diagnostics)
		default:
			if isGradleScript(document) {
				parseGradleScript(document, builder)
				parseCatalogAliases(document, catalog, builder, diagnostics)
			}
		}
	}
}
