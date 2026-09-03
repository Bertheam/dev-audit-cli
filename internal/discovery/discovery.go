// Package discovery finds Flutter and Android project boundaries without
// interpreting project configuration or following symbolic links.
package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"dev-environment-auditor/internal/domain"
)

const (
	DefaultMaxEntries = 250_000
	DefaultMaxDepth   = 64
)

var errStopRoot = errors.New("stop discovery root")

var defaultExcludedDirectories = map[string]struct{}{
	".dart_tool":   {},
	".fvm":         {},
	".git":         {},
	".gradle":      {},
	".idea":        {},
	".pub-cache":   {},
	".Trash":       {},
	"Pods":         {},
	"build":        {},
	"node_modules": {},
}

type Limits struct {
	MaxEntries int
	MaxDepth   int
}

type Config struct {
	Roots      []string
	Exclusions []string
	Limits     Limits
}

type Stats struct {
	RootsScanned   int
	EntriesVisited int
	ProjectsFound  int
}

type Result struct {
	Roots       []string
	Projects    []domain.Project
	Diagnostics []domain.Diagnostic
	Stats       Stats
}

type FileSystem interface {
	Lstat(name string) (fs.FileInfo, error)
	WalkDir(root string, fn fs.WalkDirFunc) error
}

type Discoverer struct {
	fileSystem FileSystem
}

func New(fileSystem FileSystem) *Discoverer {
	return &Discoverer{fileSystem: fileSystem}
}

func (discoverer *Discoverer) Discover(ctx context.Context, config Config) Result {
	result := Result{}
	if discoverer == nil || discoverer.fileSystem == nil {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"DISCOVERY_FILESYSTEM_UNAVAILABLE",
			domain.SeverityError,
			"filesystem adapter is required",
			"",
		))
		return result
	}

	limits := normalizeLimits(config.Limits)
	rules, diagnostics := parseExclusionRules(config.Exclusions)
	result.Diagnostics = append(result.Diagnostics, diagnostics...)
	if len(config.Roots) == 0 {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"DISCOVERY_NO_ROOTS",
			domain.SeverityError,
			"at least one explicit scan root is required",
			"",
		))
		sortDiagnostics(result.Diagnostics)
		return result
	}

	roots, diagnostics := discoverer.canonicalRoots(config.Roots)
	result.Roots = roots
	result.Diagnostics = append(result.Diagnostics, diagnostics...)

	markers := make(map[string]*projectMarkers)
	manifestPaths := make([]string, 0)

	for _, root := range roots {
		if ctx.Err() != nil {
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"DISCOVERY_CANCELLED",
				domain.SeverityError,
				ctx.Err().Error(),
				root,
			))
			break
		}

		visited, rootDiagnostics := discoverer.walkRoot(
			ctx,
			root,
			limits,
			rules,
			markers,
			&manifestPaths,
		)
		result.Stats.RootsScanned++
		result.Stats.EntriesVisited += visited
		result.Diagnostics = append(result.Diagnostics, rootDiagnostics...)
	}

	projects, diagnostics := discoverer.buildProjects(markers, manifestPaths)
	result.Projects = projects
	result.Stats.ProjectsFound = len(projects)
	result.Diagnostics = append(result.Diagnostics, diagnostics...)
	sortDiagnostics(result.Diagnostics)
	return result
}

func normalizeLimits(limits Limits) Limits {
	if limits.MaxEntries <= 0 {
		limits.MaxEntries = DefaultMaxEntries
	}
	if limits.MaxDepth <= 0 {
		limits.MaxDepth = DefaultMaxDepth
	}
	return limits
}

func (discoverer *Discoverer) canonicalRoots(input []string) ([]string, []domain.Diagnostic) {
	candidates := make([]string, 0, len(input))
	diagnostics := make([]domain.Diagnostic, 0)

	for _, rawRoot := range input {
		if strings.TrimSpace(rawRoot) == "" {
			diagnostics = append(diagnostics, newDiagnostic(
				"DISCOVERY_INVALID_ROOT",
				domain.SeverityError,
				"scan root cannot be empty",
				"",
			))
			continue
		}

		absoluteRoot, err := filepath.Abs(rawRoot)
		if err != nil {
			diagnostics = append(diagnostics, newDiagnostic(
				"DISCOVERY_INVALID_ROOT",
				domain.SeverityError,
				fmt.Sprintf("cannot canonicalize scan root: %v", err),
				rawRoot,
			))
			continue
		}
		absoluteRoot = filepath.Clean(absoluteRoot)

		info, err := discoverer.fileSystem.Lstat(absoluteRoot)
		if err != nil {
			diagnostic := fileErrorDiagnostic(
				"DISCOVERY_ROOT_UNAVAILABLE",
				"cannot access scan root",
				absoluteRoot,
				err,
			)
			diagnostic.Severity = domain.SeverityError
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			diagnostics = append(diagnostics, newDiagnostic(
				"DISCOVERY_SYMLINK_ROOT_REJECTED",
				domain.SeverityError,
				"scan root is a symbolic link; provide its explicit real directory instead",
				absoluteRoot,
			))
			continue
		}
		if !info.IsDir() {
			diagnostics = append(diagnostics, newDiagnostic(
				"DISCOVERY_ROOT_NOT_DIRECTORY",
				domain.SeverityError,
				"scan root must be a directory",
				absoluteRoot,
			))
			continue
		}
		candidates = append(candidates, absoluteRoot)
	}

	sort.Strings(candidates)
	roots := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		coveredBy := ""
		for _, root := range roots {
			if isWithin(root, candidate) {
				coveredBy = root
				break
			}
		}
		if coveredBy != "" {
			diagnostics = append(diagnostics, newDiagnostic(
				"DISCOVERY_ROOT_ALREADY_COVERED",
				domain.SeverityInfo,
				fmt.Sprintf("root is already covered by %s", coveredBy),
				candidate,
			))
			continue
		}
		roots = append(roots, candidate)
	}

	return roots, diagnostics
}

func (discoverer *Discoverer) walkRoot(
	ctx context.Context,
	root string,
	limits Limits,
	rules []exclusionRule,
	markers map[string]*projectMarkers,
	manifestPaths *[]string,
) (int, []domain.Diagnostic) {
	visited := 0
	diagnostics := make([]domain.Diagnostic, 0)
	depthReported := make(map[string]struct{})
	symlinksSkipped := 0

	err := discoverer.fileSystem.WalkDir(root, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return errStopRoot
		}

		if walkErr != nil {
			diagnostics = append(diagnostics, fileErrorDiagnostic(
				"DISCOVERY_ENTRY_UNAVAILABLE",
				"cannot inspect filesystem entry",
				currentPath,
				walkErr,
			))
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		relativePath, err := filepath.Rel(root, currentPath)
		if err != nil {
			diagnostics = append(diagnostics, newDiagnostic(
				"DISCOVERY_RELATIVE_PATH_FAILED",
				domain.SeverityWarning,
				err.Error(),
				currentPath,
			))
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		depth := pathDepth(relativePath)
		if depth > limits.MaxDepth {
			if _, reported := depthReported[currentPath]; !reported {
				diagnostics = append(diagnostics, newDiagnostic(
					"DISCOVERY_MAX_DEPTH_REACHED",
					domain.SeverityWarning,
					fmt.Sprintf("maximum discovery depth %d reached", limits.MaxDepth),
					currentPath,
				))
				depthReported[currentPath] = struct{}{}
			}
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if relativePath != "." && shouldExclude(relativePath, entry, rules) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		visited++
		if visited > limits.MaxEntries {
			diagnostics = append(diagnostics, newDiagnostic(
				"DISCOVERY_MAX_ENTRIES_REACHED",
				domain.SeverityWarning,
				fmt.Sprintf("maximum entry budget %d reached", limits.MaxEntries),
				root,
			))
			return errStopRoot
		}

		if entry.Type()&fs.ModeSymlink != 0 {
			symlinksSkipped++
			return nil
		}

		recordMarkers(currentPath, entry, markers, manifestPaths)
		return nil
	})

	if ctx.Err() != nil {
		diagnostics = append(diagnostics, newDiagnostic(
			"DISCOVERY_CANCELLED",
			domain.SeverityError,
			ctx.Err().Error(),
			root,
		))
	} else if err != nil && !errors.Is(err, errStopRoot) {
		diagnostics = append(diagnostics, fileErrorDiagnostic(
			"DISCOVERY_ROOT_WALK_FAILED",
			"cannot complete scan root",
			root,
			err,
		))
	}
	if symlinksSkipped > 0 {
		diagnostics = append(diagnostics, newDiagnostic(
			"DISCOVERY_SYMLINKS_SKIPPED",
			domain.SeverityInfo,
			fmt.Sprintf("%d symbolic links were not followed", symlinksSkipped),
			root,
		))
	}

	return visited, diagnostics
}

type projectMarkers struct {
	pubspec          bool
	flutterMetadata  bool
	fvmConfig        bool
	androidDirectory bool
	gradleSettings   bool
	gradleWrapper    bool
}

func recordMarkers(
	currentPath string,
	entry fs.DirEntry,
	markers map[string]*projectMarkers,
	manifestPaths *[]string,
) {
	baseName := entry.Name()
	if entry.IsDir() {
		if baseName == "android" {
			markersFor(markers, filepath.Dir(currentPath)).androidDirectory = true
		}
		return
	}

	directory := filepath.Dir(currentPath)
	switch baseName {
	case "pubspec.yaml":
		markersFor(markers, directory).pubspec = true
	case ".metadata":
		markersFor(markers, directory).flutterMetadata = true
	case ".fvmrc":
		markersFor(markers, directory).fvmConfig = true
	case "settings.gradle", "settings.gradle.kts":
		markersFor(markers, directory).gradleSettings = true
	case "gradle-wrapper.properties":
		if filepath.Base(directory) == "wrapper" && filepath.Base(filepath.Dir(directory)) == "gradle" {
			projectRoot := filepath.Dir(filepath.Dir(directory))
			markersFor(markers, projectRoot).gradleWrapper = true
		}
	case "AndroidManifest.xml":
		if filepath.Base(directory) == "main" && filepath.Base(filepath.Dir(directory)) == "src" {
			*manifestPaths = append(*manifestPaths, currentPath)
		}
	}
}

func markersFor(markers map[string]*projectMarkers, directory string) *projectMarkers {
	marker, exists := markers[directory]
	if !exists {
		marker = &projectMarkers{}
		markers[directory] = marker
	}
	return marker
}

func (discoverer *Discoverer) buildProjects(
	markers map[string]*projectMarkers,
	manifestPaths []string,
) ([]domain.Project, []domain.Diagnostic) {
	flutterRoots := make(map[string]domain.ProjectKind)
	androidRoots := make(map[string]struct{})
	diagnostics := make([]domain.Diagnostic, 0)

	for directory, marker := range markers {
		if marker.pubspec && (marker.flutterMetadata || marker.fvmConfig || marker.androidDirectory) {
			flutterRoots[directory] = domain.ProjectFlutter
		}
	}

	for directory, marker := range markers {
		if !marker.gradleSettings {
			continue
		}
		if marker.gradleWrapper || containsManifest(directory, manifestPaths) {
			androidRoots[directory] = struct{}{}
			continue
		}
		diagnostics = append(diagnostics, newDiagnostic(
			"DISCOVERY_ANDROID_CANDIDATE_INCOMPLETE",
			domain.SeverityInfo,
			"Gradle settings found without an Android manifest or Gradle wrapper",
			directory,
		))
	}

	for flutterRoot := range flutterRoots {
		androidRoot := filepath.Join(flutterRoot, "android")
		if marker, exists := markers[androidRoot]; exists && marker.gradleSettings {
			flutterRoots[flutterRoot] = domain.ProjectHybrid
			delete(androidRoots, androidRoot)
		}
	}

	projects := make([]domain.Project, 0, len(flutterRoots)+len(androidRoots))
	for directory, kind := range flutterRoots {
		project, diagnostic := discoverer.newProject(directory, kind)
		projects = append(projects, project)
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	for directory := range androidRoots {
		if _, exists := flutterRoots[directory]; exists {
			continue
		}
		project, diagnostic := discoverer.newProject(directory, domain.ProjectAndroid)
		projects = append(projects, project)
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}

	sort.Slice(projects, func(left, right int) bool {
		return projects[left].Path < projects[right].Path
	})
	return projects, diagnostics
}

func (discoverer *Discoverer) newProject(directory string, kind domain.ProjectKind) (domain.Project, *domain.Diagnostic) {
	project := domain.Project{
		ID:           stableProjectID(directory),
		Path:         directory,
		Kind:         kind,
		Requirements: []domain.Requirement{},
		Warnings:     []string{},
	}

	info, err := discoverer.fileSystem.Lstat(directory)
	if err != nil {
		diagnostic := fileErrorDiagnostic(
			"DISCOVERY_PROJECT_METADATA_UNAVAILABLE",
			"project disappeared before metadata could be read",
			directory,
			err,
		)
		return project, &diagnostic
	}
	modifiedAt := info.ModTime().UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	project.LastModifiedAt = &modifiedAt
	return project, nil
}

func stableProjectID(projectPath string) string {
	digest := sha256.Sum256([]byte(filepath.Clean(projectPath)))
	return "project-" + hex.EncodeToString(digest[:8])
}

func containsManifest(projectRoot string, manifestPaths []string) bool {
	for _, manifestPath := range manifestPaths {
		if isWithin(projectRoot, manifestPath) {
			return true
		}
	}
	return false
}

func isWithin(parent, candidate string) bool {
	relative, err := filepath.Rel(parent, candidate)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func pathDepth(relativePath string) int {
	if relativePath == "." {
		return 0
	}
	return strings.Count(filepath.ToSlash(relativePath), "/") + 1
}

type exclusionRule struct {
	pattern      string
	basenameOnly bool
	prefix       bool
}

func parseExclusionRules(input []string) ([]exclusionRule, []domain.Diagnostic) {
	rules := make([]exclusionRule, 0, len(input))
	diagnostics := make([]domain.Diagnostic, 0)
	for _, rawPattern := range input {
		patternValue := strings.Trim(strings.TrimSpace(filepath.ToSlash(rawPattern)), "/")
		patternValue = strings.TrimPrefix(patternValue, "./")
		if patternValue == "" {
			diagnostics = append(diagnostics, newDiagnostic(
				"DISCOVERY_INVALID_EXCLUSION",
				domain.SeverityWarning,
				"empty exclusion pattern was ignored",
				"",
			))
			continue
		}

		rule := exclusionRule{
			pattern:      patternValue,
			basenameOnly: !strings.Contains(patternValue, "/"),
			prefix:       strings.HasSuffix(patternValue, "/**"),
		}
		if rule.prefix {
			rule.pattern = strings.TrimSuffix(patternValue, "/**")
		}
		if _, err := path.Match(rule.pattern, rule.pattern); err != nil {
			diagnostics = append(diagnostics, newDiagnostic(
				"DISCOVERY_INVALID_EXCLUSION",
				domain.SeverityWarning,
				fmt.Sprintf("invalid exclusion pattern %q: %v", rawPattern, err),
				"",
			))
			continue
		}
		rules = append(rules, rule)
	}
	return rules, diagnostics
}

func shouldExclude(relativePath string, entry fs.DirEntry, rules []exclusionRule) bool {
	if entry.IsDir() {
		if _, excluded := defaultExcludedDirectories[entry.Name()]; excluded {
			return true
		}
	}

	normalizedPath := filepath.ToSlash(relativePath)
	for _, rule := range rules {
		candidate := normalizedPath
		if rule.basenameOnly {
			candidate = entry.Name()
		}
		if rule.prefix {
			if candidate == rule.pattern || strings.HasPrefix(candidate, rule.pattern+"/") {
				return true
			}
			continue
		}
		if matched, err := path.Match(rule.pattern, candidate); err == nil && matched {
			return true
		}
		if !rule.basenameOnly && (candidate == rule.pattern || strings.HasPrefix(candidate, rule.pattern+"/")) {
			return true
		}
	}
	return false
}

func newDiagnostic(
	code string,
	severity domain.DiagnosticSeverity,
	message string,
	diagnosticPath string,
) domain.Diagnostic {
	diagnostic := domain.Diagnostic{
		Code:     code,
		Severity: severity,
		Scope:    "discovery",
		Message:  message,
	}
	if diagnosticPath != "" {
		pathCopy := diagnosticPath
		diagnostic.Path = &pathCopy
	}
	return diagnostic
}

func fileErrorDiagnostic(code, message, diagnosticPath string, err error) domain.Diagnostic {
	severity := domain.SeverityWarning
	if errors.Is(err, fs.ErrNotExist) {
		message += ": path does not exist"
	} else if errors.Is(err, fs.ErrPermission) {
		message += ": permission denied"
	} else {
		message += ": " + err.Error()
	}
	return newDiagnostic(code, severity, message, diagnosticPath)
}

func sortDiagnostics(diagnostics []domain.Diagnostic) {
	sort.SliceStable(diagnostics, func(left, right int) bool {
		leftPath := ""
		rightPath := ""
		if diagnostics[left].Path != nil {
			leftPath = *diagnostics[left].Path
		}
		if diagnostics[right].Path != nil {
			rightPath = *diagnostics[right].Path
		}
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		if diagnostics[left].Code != diagnostics[right].Code {
			return diagnostics[left].Code < diagnostics[right].Code
		}
		return diagnostics[left].Message < diagnostics[right].Message
	})
}
