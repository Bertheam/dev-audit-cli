// Package application assembles the read-only Phase 0 scan pipeline.
package application

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dev-environment-auditor/internal/analyzers"
	"dev-environment-auditor/internal/classification"
	"dev-environment-auditor/internal/correlation"
	"dev-environment-auditor/internal/discovery"
	"dev-environment-auditor/internal/dockerinventory"
	"dev-environment-auditor/internal/domain"
	"dev-environment-auditor/internal/environments"
	"dev-environment-auditor/internal/inventory"
)

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now()
}

type Config struct {
	ProjectRoots        []string
	Exclusions          []string
	AndroidSDKRoots     []string
	AndroidAVDRoots     []string
	FlutterSDKRoots     []string
	FVMCacheRoots       []string
	GradleUserHomeRoots []string
	JDKRoots            []string
	XcodeRoots          []string
	AppleDeveloperRoots []string
	DockerInventory     bool
	OldAfterDays        int
	PreScanDiagnostics  []domain.Diagnostic
	// ProjectDiscoveryHeuristic prevents absence-of-reference conclusions when
	// project roots came from an automatic search rather than an explicit scope.
	ProjectDiscoveryHeuristic bool
	// HeuristicInventoryFamilies prevents MISSING conclusions for inventory
	// families whose roots were found heuristically and may be incomplete.
	HeuristicInventoryFamilies []string
	// Progress receives short presentation-only stage descriptions. Callers may
	// leave it nil; it never affects scan results or ordering.
	Progress func(string)
}

type Scanner struct {
	discoverer   *discovery.Discoverer
	analyzer     *analyzers.Analyzer
	environments *environments.Analyzer
	inventory    *inventory.Inventory
	docker       DockerInspector
	clock        Clock
	version      string
}

type DockerInspector interface {
	Inspect(context.Context, dockerinventory.Config) dockerinventory.Result
}

func NewScanner(clock Clock, version string) *Scanner {
	return NewScannerWithAdapters(
		discovery.New(discovery.OSFileSystem{}),
		analyzers.New(analyzers.OSFileSystem{}),
		environments.New(environments.OSFileSystem{}),
		inventory.New(inventory.OSFileSystem{}),
		dockerinventory.New(dockerinventory.OSRunner{}),
		clock,
		version,
	)
}

func NewScannerWithAdapters(
	discoverer *discovery.Discoverer,
	analyzer *analyzers.Analyzer,
	environmentAnalyzer *environments.Analyzer,
	inventoryInspector *inventory.Inventory,
	dockerInspector DockerInspector,
	clock Clock,
	version string,
) *Scanner {
	if clock == nil {
		clock = SystemClock{}
	}
	return &Scanner{
		discoverer:   discoverer,
		analyzer:     analyzer,
		environments: environmentAnalyzer,
		inventory:    inventoryInspector,
		docker:       dockerInspector,
		clock:        clock,
		version:      version,
	}
}

// Scan executes discovery, static analysis, typed inventory and correlation in
// sequence. Only adapters exposing read operations are accepted.
func (scanner *Scanner) Scan(ctx context.Context, config Config) domain.ScanDocument {
	startedAt := scanner.clock.Now().UTC()
	reportProgress(config, "Discovering projects")
	discoveryResult := scanner.discoverer.Discover(ctx, discovery.Config{
		Roots:      cloneStrings(config.ProjectRoots),
		Exclusions: cloneStrings(config.Exclusions),
	})

	reportProgress(config, progressCount("Analyzing", len(discoveryResult.Projects), "project", "projects"))
	projects, analyzerDiagnostics := scanner.analyzer.AnalyzeProjects(
		ctx,
		discoveryResult.Projects,
		analyzers.Config{},
	)
	reportProgress(config, "Reading Docker and Compose declarations")
	environmentResult := scanner.environments.AnalyzeProjects(
		ctx,
		projects,
		environments.Config{},
	)
	resources, inventoryDiagnostics, completeScopes := scanner.inspectInventory(ctx, config)
	dockerDiagnostics := []domain.Diagnostic{}
	if config.DockerInventory {
		reportProgress(config, "Inspecting the local Docker daemon")
		if scanner.docker == nil {
			dockerDiagnostics = append(dockerDiagnostics, domain.Diagnostic{
				Code:     "DOCKER_INVENTORY_UNAVAILABLE",
				Severity: domain.SeverityError,
				Scope:    "docker-inventory",
				Message:  "Docker inventory was requested but no inspector is configured",
			})
		} else {
			dockerResult := scanner.docker.Inspect(ctx, dockerinventory.Config{})
			resources = append(resources, dockerResult.Resources...)
			dockerDiagnostics = append(dockerDiagnostics, dockerResult.Diagnostics...)
			for _, component := range dockerResult.CompleteComponents {
				completeScopes = append(completeScopes, correlation.InventoryScope{
					Ecosystem: "docker",
					Component: component,
				})
			}
		}
	}

	reportProgress(config, progressCount("Correlating", len(environmentResult.Projects), "project", "projects"))
	discoveryComplete := len(discoveryResult.Roots) > 0 &&
		diagnosticsComplete(discoveryResult.Diagnostics) &&
		!config.ProjectDiscoveryHeuristic
	analysisComplete := discoveryComplete && diagnosticsComplete(analyzerDiagnostics) &&
		environmentResult.Complete
	correlationResult := correlation.Correlate(environmentResult.Projects, resources, correlation.Coverage{
		ProjectDiscoveryComplete:    discoveryComplete,
		RequirementAnalysisComplete: analysisComplete,
		CompleteInventoryScopes:     completeScopes,
	})
	completedAt := scanner.clock.Now().UTC()
	oldAfterDays := config.OldAfterDays
	if oldAfterDays <= 0 {
		oldAfterDays = classification.DefaultOldAfterDays
	}
	reportProgress(config, progressCount("Classifying", len(correlationResult.Resources), "resource", "resources"))
	classificationResult := classification.Apply(correlationResult.Resources, classification.Config{
		EvaluatedAt:  completedAt,
		OldAfterDays: oldAfterDays,
	})

	reportProgress(config, "Preparing the report")
	diagnostics := make([]domain.Diagnostic, 0,
		len(config.PreScanDiagnostics)+
			len(discoveryResult.Diagnostics)+len(analyzerDiagnostics)+
			len(environmentResult.Diagnostics)+
			len(inventoryDiagnostics)+len(dockerDiagnostics)+len(correlationResult.Diagnostics)+
			len(classificationResult.Diagnostics),
	)
	diagnostics = append(diagnostics, config.PreScanDiagnostics...)
	diagnostics = append(diagnostics, discoveryResult.Diagnostics...)
	diagnostics = append(diagnostics, analyzerDiagnostics...)
	diagnostics = append(diagnostics, environmentResult.Diagnostics...)
	diagnostics = append(diagnostics, inventoryDiagnostics...)
	diagnostics = append(diagnostics, dockerDiagnostics...)
	diagnostics = append(diagnostics, correlationResult.Diagnostics...)
	diagnostics = append(diagnostics, classificationResult.Diagnostics...)
	sortDiagnostics(diagnostics)

	roots := cloneStrings(discoveryResult.Roots)
	if len(roots) == 0 {
		roots = explicitRoots(config.ProjectRoots)
	}
	exclusions := normalizedStrings(config.Exclusions)
	relations := append([]domain.Relation{}, correlationResult.Relations...)
	relations = append(relations, environmentResult.Relations...)
	return domain.ScanDocument{
		SchemaVersion: domain.SchemaVersion,
		ToolVersion:   scanner.version,
		Scan: domain.ScanMetadata{
			StartedAt:   startedAt.Format(time.RFC3339Nano),
			CompletedAt: completedAt.Format(time.RFC3339Nano),
			Roots:       roots,
			Exclusions:  exclusions,
			ReadOnly:    true,
			ClassificationPolicy: &domain.ClassificationPolicy{
				OldAfterDays: oldAfterDays,
			},
		},
		Projects:           correlationResult.Projects,
		InstalledResources: classificationResult.Resources,
		Relations:          relations,
		Diagnostics:        diagnostics,
	}
}

type inventoryGroup struct {
	family     string
	configured bool
	config     inventory.Config
	scopes     []correlation.InventoryScope
}

func (scanner *Scanner) inspectInventory(
	ctx context.Context,
	config Config,
) ([]domain.InstalledResource, []domain.Diagnostic, []correlation.InventoryScope) {
	groups := []inventoryGroup{
		{
			family:     "android",
			configured: len(config.AndroidSDKRoots) > 0,
			config: inventory.Config{
				AndroidSDKRoots: cloneStrings(config.AndroidSDKRoots),
			},
			scopes: []correlation.InventoryScope{
				{Ecosystem: "android", Component: "android_sdk_platform"},
				{Ecosystem: "android", Component: "ndk"},
				{Ecosystem: "android", Component: "cmake"},
			},
		},
		{
			family:     "android_avd",
			configured: len(config.AndroidAVDRoots) > 0,
			config: inventory.Config{
				AndroidAVDRoots: cloneStrings(config.AndroidAVDRoots),
			},
			scopes: nil,
		},
		{
			family:     "flutter",
			configured: len(config.FlutterSDKRoots)+len(config.FVMCacheRoots) > 0,
			config: inventory.Config{
				FlutterSDKRoots: cloneStrings(config.FlutterSDKRoots),
				FVMCacheRoots:   cloneStrings(config.FVMCacheRoots),
			},
			scopes: []correlation.InventoryScope{
				{Ecosystem: "flutter", Component: "flutter_sdk"},
			},
		},
		{
			family:     "gradle",
			configured: len(config.GradleUserHomeRoots) > 0,
			config: inventory.Config{
				GradleUserHomeRoots: cloneStrings(config.GradleUserHomeRoots),
			},
			scopes: []correlation.InventoryScope{
				{Ecosystem: "gradle", Component: "gradle"},
				{Ecosystem: "android", Component: "android_gradle_plugin"},
				{Ecosystem: "kotlin", Component: "kotlin_gradle_plugin"},
			},
		},
		{
			family:     "java",
			configured: len(config.JDKRoots) > 0,
			config: inventory.Config{
				JDKRoots: cloneStrings(config.JDKRoots),
			},
			scopes: []correlation.InventoryScope{
				{Ecosystem: "java", Component: "jdk"},
			},
		},
		{
			family:     "apple",
			configured: len(config.XcodeRoots)+len(config.AppleDeveloperRoots) > 0,
			config: inventory.Config{
				XcodeRoots:          cloneStrings(config.XcodeRoots),
				AppleDeveloperRoots: cloneStrings(config.AppleDeveloperRoots),
			},
			scopes: nil,
		},
	}

	resources := []domain.InstalledResource{}
	diagnostics := []domain.Diagnostic{}
	completeScopes := []correlation.InventoryScope{}
	configuredGroups := 0
	for _, group := range groups {
		if !group.configured {
			continue
		}
		configuredGroups++
		reportProgress(config, inventoryProgressMessage(group.family))
		result := scanner.inventory.Inspect(ctx, group.config)
		resources = append(resources, result.Resources...)
		diagnostics = append(diagnostics, result.Diagnostics...)
		if diagnosticsComplete(result.Diagnostics) &&
			!containsString(config.HeuristicInventoryFamilies, group.family) {
			completeScopes = append(completeScopes, group.scopes...)
		}
	}
	if configuredGroups == 0 {
		diagnostics = append(diagnostics, domain.Diagnostic{
			Code:     "INVENTORY_NOT_CONFIGURED",
			Severity: domain.SeverityInfo,
			Scope:    "inventory",
			Message:  "no typed inventory roots were provided; missing-resource conclusions are disabled",
		})
	}
	sortDiagnostics(diagnostics)
	return resources, diagnostics, completeScopes
}

func reportProgress(config Config, message string) {
	if config.Progress != nil {
		config.Progress(message)
	}
}

func progressCount(verb string, count int, singular, plural string) string {
	label := plural
	if count == 1 {
		label = singular
	}
	return fmt.Sprintf("%s %d %s", verb, count, label)
}

func inventoryProgressMessage(family string) string {
	switch family {
	case "android":
		return "Inventorying the Android SDK"
	case "android_avd":
		return "Inventorying Android virtual devices"
	case "flutter":
		return "Inventorying Flutter and FVM SDKs"
	case "gradle":
		return "Inventorying Gradle caches"
	case "java":
		return "Inventorying Java toolchains"
	case "apple":
		return "Inventorying Xcode and simulator data"
	default:
		return "Inventorying local development resources"
	}
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func diagnosticsComplete(diagnostics []domain.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == domain.SeverityWarning || diagnostic.Severity == domain.SeverityError {
			return false
		}
	}
	return true
}

func explicitRoots(roots []string) []string {
	result := make([]string, 0, len(roots))
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			result = append(result, filepath.Clean(root))
			continue
		}
		result = append(result, filepath.Clean(absolute))
	}
	return normalizedStrings(result)
}

func normalizedStrings(values []string) []string {
	result := cloneStrings(values)
	sort.Strings(result)
	unique := result[:0]
	for _, value := range result {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique
}

func cloneStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func sortDiagnostics(diagnostics []domain.Diagnostic) {
	sort.SliceStable(diagnostics, func(left, right int) bool {
		leftPath := ""
		if diagnostics[left].Path != nil {
			leftPath = *diagnostics[left].Path
		}
		rightPath := ""
		if diagnostics[right].Path != nil {
			rightPath = *diagnostics[right].Path
		}
		leftKey := strings.Join([]string{
			diagnostics[left].Scope,
			diagnostics[left].Code,
			leftPath,
			diagnostics[left].Message,
		}, "\x00")
		rightKey := strings.Join([]string{
			diagnostics[right].Scope,
			diagnostics[right].Code,
			rightPath,
			diagnostics[right].Message,
		}, "\x00")
		return leftKey < rightKey
	})
}
