// Package analyzers extracts declared Flutter and Android toolchain
// requirements from a bounded allowlist of project files.
package analyzers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"dev-environment-auditor/internal/domain"
)

const (
	DefaultMaxEntries   = 50_000
	DefaultMaxFiles     = 256
	DefaultMaxDepth     = 12
	DefaultMaxFileBytes = 1 << 20
)

var errStopAnalysis = errors.New("stop project analysis")

var excludedDirectories = map[string]struct{}{
	".dart_tool":   {},
	".fvm":         {},
	".git":         {},
	".gradle":      {},
	".idea":        {},
	".pub-cache":   {},
	"Pods":         {},
	"build":        {},
	"node_modules": {},
}

type Limits struct {
	MaxEntries   int
	MaxFiles     int
	MaxDepth     int
	MaxFileBytes int64
}

type Config struct {
	Limits Limits
}

type Stats struct {
	EntriesVisited int
	FilesRead      int
	BytesRead      int64
}

type Result struct {
	Requirements []domain.Requirement
	Diagnostics  []domain.Diagnostic
	Stats        Stats
}

type FileSystem interface {
	Lstat(name string) (fs.FileInfo, error)
	Open(name string) (fs.File, error)
	WalkDir(root string, fn fs.WalkDirFunc) error
}

type Analyzer struct {
	fileSystem FileSystem
}

func New(fileSystem FileSystem) *Analyzer {
	return &Analyzer{fileSystem: fileSystem}
}

func (analyzer *Analyzer) Analyze(ctx context.Context, project domain.Project, config Config) Result {
	result := Result{Requirements: []domain.Requirement{}, Diagnostics: []domain.Diagnostic{}}
	if analyzer == nil || analyzer.fileSystem == nil {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"ANALYZER_FILESYSTEM_UNAVAILABLE",
			domain.SeverityError,
			"filesystem adapter is required",
			"",
		))
		return result
	}

	root, diagnostic := analyzer.validateProject(project)
	if diagnostic != nil {
		result.Diagnostics = append(result.Diagnostics, *diagnostic)
		return result
	}
	if ctx.Err() != nil {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"ANALYZER_CANCELLED",
			domain.SeverityError,
			ctx.Err().Error(),
			root,
		))
		return result
	}

	limits := normalizeLimits(config.Limits)
	documents := make([]document, 0)
	appendDocument := func(filePath string, optional bool) bool {
		if result.Stats.FilesRead >= limits.MaxFiles {
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"ANALYZER_MAX_FILES_REACHED",
				domain.SeverityWarning,
				fmt.Sprintf("maximum analyzer file budget %d reached", limits.MaxFiles),
				root,
			))
			return false
		}

		content, diagnostic := analyzer.readBoundedFile(filePath, limits.MaxFileBytes, optional)
		if diagnostic != nil {
			result.Diagnostics = append(result.Diagnostics, *diagnostic)
			return true
		}
		if content == nil {
			return true
		}
		documents = append(documents, newDocument(filePath, content))
		result.Stats.FilesRead++
		result.Stats.BytesRead += int64(len(content))
		return true
	}

	if project.Kind == domain.ProjectFlutter || project.Kind == domain.ProjectHybrid {
		appendDocument(filepath.Join(root, ".fvmrc"), true)
		appendDocument(filepath.Join(root, "pubspec.yaml"), true)
		analyzer.appendLegacyFVM(root, limits, &result, &documents)
	}

	androidRoot := root
	if project.Kind == domain.ProjectHybrid {
		androidRoot = filepath.Join(root, "android")
	}
	if project.Kind == domain.ProjectAndroid || project.Kind == domain.ProjectHybrid {
		analyzer.walkAndroid(ctx, androidRoot, limits, &result, &documents)
	}

	builder := newRequirementBuilder(project.ID)
	parseDocuments(documents, builder, &result.Diagnostics)
	result.Requirements = builder.requirements()
	appendRequirementConflictDiagnostics(root, result.Requirements, &result.Diagnostics)
	sortDiagnostics(result.Diagnostics)
	return result
}

func appendRequirementConflictDiagnostics(
	projectPath string,
	requirements []domain.Requirement,
	diagnostics *[]domain.Diagnostic,
) {
	versions := make(map[string]map[string]struct{})
	for _, requirement := range requirements {
		if requirement.Confidence != domain.RequiredExplicitly || requirement.VersionConstraint == nil {
			continue
		}
		key := requirement.Ecosystem + "\x00" + requirement.Component
		if versions[key] == nil {
			versions[key] = make(map[string]struct{})
		}
		versions[key][*requirement.VersionConstraint] = struct{}{}
	}
	for _, componentVersions := range versions {
		if len(componentVersions) > 1 {
			*diagnostics = append(*diagnostics, newDiagnostic(
				"ANALYZER_MULTIPLE_EXPLICIT_REQUIREMENTS",
				domain.SeverityInfo,
				"multiple distinct explicit constraints were found for the same component; compatibility was not evaluated",
				projectPath,
			))
		}
	}
}

// AnalyzeProjects returns copies of the supplied projects with their requirements
// attached. A failure in one project does not discard results from another.
func (analyzer *Analyzer) AnalyzeProjects(
	ctx context.Context,
	projects []domain.Project,
	config Config,
) ([]domain.Project, []domain.Diagnostic) {
	output := make([]domain.Project, len(projects))
	copy(output, projects)
	diagnostics := make([]domain.Diagnostic, 0)
	for index, project := range projects {
		if ctx.Err() != nil {
			diagnostics = append(diagnostics, newDiagnostic(
				"ANALYZER_CANCELLED",
				domain.SeverityError,
				ctx.Err().Error(),
				project.Path,
			))
			break
		}
		result := analyzer.Analyze(ctx, project, config)
		output[index].Requirements = result.Requirements
		diagnostics = append(diagnostics, result.Diagnostics...)
	}
	sortDiagnostics(diagnostics)
	return output, diagnostics
}

func normalizeLimits(limits Limits) Limits {
	if limits.MaxEntries <= 0 {
		limits.MaxEntries = DefaultMaxEntries
	}
	if limits.MaxFiles <= 0 {
		limits.MaxFiles = DefaultMaxFiles
	}
	if limits.MaxDepth <= 0 {
		limits.MaxDepth = DefaultMaxDepth
	}
	if limits.MaxFileBytes <= 0 {
		limits.MaxFileBytes = DefaultMaxFileBytes
	}
	return limits
}

func (analyzer *Analyzer) validateProject(project domain.Project) (string, *domain.Diagnostic) {
	if strings.TrimSpace(project.Path) == "" {
		diagnostic := newDiagnostic(
			"ANALYZER_INVALID_PROJECT_PATH",
			domain.SeverityError,
			"project path cannot be empty",
			"",
		)
		return "", &diagnostic
	}
	root, err := filepath.Abs(project.Path)
	if err != nil {
		diagnostic := newDiagnostic(
			"ANALYZER_INVALID_PROJECT_PATH",
			domain.SeverityError,
			"cannot canonicalize project path",
			project.Path,
		)
		return "", &diagnostic
	}
	root = filepath.Clean(root)
	info, err := analyzer.fileSystem.Lstat(root)
	if err != nil {
		diagnostic := fileErrorDiagnostic(
			"ANALYZER_PROJECT_UNAVAILABLE",
			"cannot access project root",
			root,
			err,
		)
		diagnostic.Severity = domain.SeverityError
		return "", &diagnostic
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		diagnostic := newDiagnostic(
			"ANALYZER_SYMLINK_ROOT_REJECTED",
			domain.SeverityError,
			"project root is a symbolic link",
			root,
		)
		return "", &diagnostic
	}
	if !info.IsDir() {
		diagnostic := newDiagnostic(
			"ANALYZER_PROJECT_NOT_DIRECTORY",
			domain.SeverityError,
			"project root must be a directory",
			root,
		)
		return "", &diagnostic
	}
	return root, nil
}

func (analyzer *Analyzer) appendLegacyFVM(
	root string,
	limits Limits,
	result *Result,
	documents *[]document,
) {
	parent := filepath.Join(root, ".fvm")
	info, err := analyzer.fileSystem.Lstat(parent)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
			"ANALYZER_FILE_UNAVAILABLE",
			"cannot inspect legacy FVM directory",
			parent,
			err,
		))
		return
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"ANALYZER_SYMLINK_SKIPPED",
			domain.SeverityInfo,
			"symbolic link was not followed",
			parent,
		))
		return
	}
	if !info.IsDir() || result.Stats.FilesRead >= limits.MaxFiles {
		return
	}
	filePath := filepath.Join(parent, "fvm_config.json")
	content, diagnostic := analyzer.readBoundedFile(filePath, limits.MaxFileBytes, true)
	if diagnostic != nil {
		result.Diagnostics = append(result.Diagnostics, *diagnostic)
		return
	}
	if content == nil {
		return
	}
	*documents = append(*documents, newDocument(filePath, content))
	result.Stats.FilesRead++
	result.Stats.BytesRead += int64(len(content))
}

func (analyzer *Analyzer) walkAndroid(
	ctx context.Context,
	root string,
	limits Limits,
	result *Result,
	documents *[]document,
) {
	info, err := analyzer.fileSystem.Lstat(root)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
			"ANALYZER_ANDROID_ROOT_UNAVAILABLE",
			"cannot access Android project root",
			root,
			err,
		))
		return
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"ANALYZER_SYMLINK_ROOT_REJECTED",
			domain.SeverityError,
			"Android project root is a symbolic link",
			root,
		))
		return
	}
	if !info.IsDir() {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"ANALYZER_ANDROID_ROOT_NOT_DIRECTORY",
			domain.SeverityError,
			"Android project root must be a directory",
			root,
		))
		return
	}

	err = analyzer.fileSystem.WalkDir(root, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return errStopAnalysis
		}
		if walkErr != nil {
			result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
				"ANALYZER_ENTRY_UNAVAILABLE",
				"cannot inspect filesystem entry",
				currentPath,
				walkErr,
			))
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		relative, relativeErr := filepath.Rel(root, currentPath)
		if relativeErr != nil {
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"ANALYZER_RELATIVE_PATH_FAILED",
				domain.SeverityWarning,
				"cannot determine path relative to project",
				currentPath,
			))
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		depth := pathDepth(relative)
		if depth > limits.MaxDepth {
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"ANALYZER_MAX_DEPTH_REACHED",
				domain.SeverityWarning,
				fmt.Sprintf("maximum analyzer depth %d reached", limits.MaxDepth),
				currentPath,
			))
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if relative != "." && entry.IsDir() {
			if _, excluded := excludedDirectories[entry.Name()]; excluded {
				return fs.SkipDir
			}
		}

		result.Stats.EntriesVisited++
		if result.Stats.EntriesVisited > limits.MaxEntries {
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"ANALYZER_MAX_ENTRIES_REACHED",
				domain.SeverityWarning,
				fmt.Sprintf("maximum analyzer entry budget %d reached", limits.MaxEntries),
				root,
			))
			return errStopAnalysis
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"ANALYZER_SYMLINK_SKIPPED",
				domain.SeverityInfo,
				"symbolic link was not followed",
				currentPath,
			))
			return nil
		}
		if entry.IsDir() || !isAllowedAndroidFile(relative) {
			return nil
		}
		if result.Stats.FilesRead >= limits.MaxFiles {
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"ANALYZER_MAX_FILES_REACHED",
				domain.SeverityWarning,
				fmt.Sprintf("maximum analyzer file budget %d reached", limits.MaxFiles),
				root,
			))
			return errStopAnalysis
		}
		content, diagnostic := analyzer.readBoundedFile(currentPath, limits.MaxFileBytes, false)
		if diagnostic != nil {
			result.Diagnostics = append(result.Diagnostics, *diagnostic)
			return nil
		}
		*documents = append(*documents, newDocument(currentPath, content))
		result.Stats.FilesRead++
		result.Stats.BytesRead += int64(len(content))
		return nil
	})

	if ctx.Err() != nil {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"ANALYZER_CANCELLED",
			domain.SeverityError,
			ctx.Err().Error(),
			root,
		))
	} else if err != nil && !errors.Is(err, errStopAnalysis) {
		result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
			"ANALYZER_ROOT_WALK_FAILED",
			"cannot complete Android project analysis",
			root,
			err,
		))
	}
}

func (analyzer *Analyzer) readBoundedFile(
	filePath string,
	maxBytes int64,
	optional bool,
) ([]byte, *domain.Diagnostic) {
	info, err := analyzer.fileSystem.Lstat(filePath)
	if optional && errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		diagnostic := fileErrorDiagnostic(
			"ANALYZER_FILE_UNAVAILABLE",
			"cannot inspect analyzer input",
			filePath,
			err,
		)
		return nil, &diagnostic
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		diagnostic := newDiagnostic(
			"ANALYZER_SYMLINK_SKIPPED",
			domain.SeverityInfo,
			"symbolic link was not followed",
			filePath,
		)
		return nil, &diagnostic
	}
	if !info.Mode().IsRegular() {
		diagnostic := newDiagnostic(
			"ANALYZER_NON_REGULAR_FILE_SKIPPED",
			domain.SeverityWarning,
			"analyzer input is not a regular file",
			filePath,
		)
		return nil, &diagnostic
	}
	if info.Size() > maxBytes {
		diagnostic := newDiagnostic(
			"ANALYZER_MAX_FILE_SIZE_REACHED",
			domain.SeverityWarning,
			fmt.Sprintf("analyzer input exceeds maximum size %d bytes", maxBytes),
			filePath,
		)
		return nil, &diagnostic
	}

	file, err := analyzer.fileSystem.Open(filePath)
	if err != nil {
		diagnostic := fileErrorDiagnostic(
			"ANALYZER_FILE_UNAVAILABLE",
			"cannot open analyzer input",
			filePath,
			err,
		)
		return nil, &diagnostic
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		diagnostic := fileErrorDiagnostic(
			"ANALYZER_FILE_READ_FAILED",
			"cannot read analyzer input",
			filePath,
			err,
		)
		return nil, &diagnostic
	}
	if int64(len(content)) > maxBytes {
		diagnostic := newDiagnostic(
			"ANALYZER_MAX_FILE_SIZE_REACHED",
			domain.SeverityWarning,
			fmt.Sprintf("analyzer input exceeds maximum size %d bytes", maxBytes),
			filePath,
		)
		return nil, &diagnostic
	}
	return content, nil
}

func isAllowedAndroidFile(relative string) bool {
	normalized := filepath.ToSlash(relative)
	switch normalized {
	case "settings.gradle", "settings.gradle.kts",
		"build.gradle", "build.gradle.kts",
		"gradle/wrapper/gradle-wrapper.properties",
		"gradle/libs.versions.toml":
		return true
	}
	base := filepath.Base(relative)
	return base == "build.gradle" || base == "build.gradle.kts"
}

func pathDepth(relative string) int {
	if relative == "." {
		return 0
	}
	return strings.Count(filepath.ToSlash(relative), "/") + 1
}

type document struct {
	path  string
	lines []string
}

func newDocument(filePath string, content []byte) document {
	return document{path: filePath, lines: strings.Split(string(content), "\n")}
}

type requirementBuilder struct {
	projectID string
	items     map[string]*domain.Requirement
}

func newRequirementBuilder(projectID string) *requirementBuilder {
	return &requirementBuilder{projectID: projectID, items: make(map[string]*domain.Requirement)}
}

func (builder *requirementBuilder) add(
	ecosystem string,
	component string,
	version *string,
	confidence domain.Confidence,
	evidence []domain.Evidence,
	warnings ...string,
) {
	versionValue := ""
	if version != nil {
		versionValue = *version
	}
	key := strings.Join([]string{ecosystem, component, versionValue, string(confidence)}, "\x00")
	requirement, exists := builder.items[key]
	if !exists {
		requirement = &domain.Requirement{
			ID:                stableRequirementID(builder.projectID, key),
			Ecosystem:         ecosystem,
			Component:         component,
			VersionConstraint: copyStringPointer(version),
			Confidence:        confidence,
			Evidence:          []domain.Evidence{},
			Warnings:          []string{},
		}
		builder.items[key] = requirement
	}
	requirement.Evidence = append(requirement.Evidence, evidence...)
	for _, warning := range warnings {
		if warning != "" && !containsString(requirement.Warnings, warning) {
			requirement.Warnings = append(requirement.Warnings, warning)
		}
	}
}

func (builder *requirementBuilder) requirements() []domain.Requirement {
	result := make([]domain.Requirement, 0, len(builder.items))
	for _, requirement := range builder.items {
		sortEvidence(requirement.Evidence)
		sort.Strings(requirement.Warnings)
		result = append(result, *requirement)
	}
	sort.Slice(result, func(left, right int) bool {
		leftVersion := pointerValue(result[left].VersionConstraint)
		rightVersion := pointerValue(result[right].VersionConstraint)
		leftKey := strings.Join([]string{
			result[left].Ecosystem,
			result[left].Component,
			leftVersion,
			string(result[left].Confidence),
			result[left].ID,
		}, "\x00")
		rightKey := strings.Join([]string{
			result[right].Ecosystem,
			result[right].Component,
			rightVersion,
			string(result[right].Confidence),
			result[right].ID,
		}, "\x00")
		return leftKey < rightKey
	})
	return result
}

func stableRequirementID(projectID, key string) string {
	digest := sha256.Sum256([]byte(projectID + "\x00" + key))
	return "requirement-" + hex.EncodeToString(digest[:8])
}

func copyStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func sortEvidence(evidence []domain.Evidence) {
	sort.Slice(evidence, func(left, right int) bool {
		leftKey := fmt.Sprintf("%s\x00%09d\x00%s\x00%s",
			pointerValue(evidence[left].FilePath),
			intPointerValue(evidence[left].LineHint),
			evidence[left].RuleID,
			pointerValue(evidence[left].Key),
		)
		rightKey := fmt.Sprintf("%s\x00%09d\x00%s\x00%s",
			pointerValue(evidence[right].FilePath),
			intPointerValue(evidence[right].LineHint),
			evidence[right].RuleID,
			pointerValue(evidence[right].Key),
		)
		return leftKey < rightKey
	})
}

func intPointerValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func newEvidence(filePath, key string, line int, observed, ruleID string) domain.Evidence {
	pathCopy := filePath
	keyCopy := key
	lineCopy := line
	observedCopy := sanitizeObserved(observed)
	return domain.Evidence{
		SourceType:    "file",
		FilePath:      &pathCopy,
		Key:           &keyCopy,
		LineHint:      &lineCopy,
		ObservedValue: &observedCopy,
		RuleID:        ruleID,
	}
}

func sanitizeObserved(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	const maxObservedBytes = 160
	if len(value) > maxObservedBytes {
		value = value[:maxObservedBytes] + "…"
	}
	return value
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
		Scope:    "analyzers",
		Message:  message,
	}
	if diagnosticPath != "" {
		pathCopy := diagnosticPath
		diagnostic.Path = &pathCopy
	}
	return diagnostic
}

func fileErrorDiagnostic(code, message, diagnosticPath string, err error) domain.Diagnostic {
	if errors.Is(err, fs.ErrNotExist) {
		message += ": path does not exist"
	} else if errors.Is(err, fs.ErrPermission) {
		message += ": permission denied"
	} else {
		message += ": " + err.Error()
	}
	return newDiagnostic(code, domain.SeverityWarning, message, diagnosticPath)
}

func sortDiagnostics(diagnostics []domain.Diagnostic) {
	sort.Slice(diagnostics, func(left, right int) bool {
		leftKey := strings.Join([]string{
			diagnostics[left].Scope,
			diagnostics[left].Code,
			pointerValue(diagnostics[left].Path),
			diagnostics[left].Message,
		}, "\x00")
		rightKey := strings.Join([]string{
			diagnostics[right].Scope,
			diagnostics[right].Code,
			pointerValue(diagnostics[right].Path),
			diagnostics[right].Message,
		}, "\x00")
		return leftKey < rightKey
	})
}
