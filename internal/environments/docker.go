// Package environments identifies declarative project toolchains that are
// separate from resources installed on the host.
package environments

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
	"strconv"
	"strings"
	"unicode"

	"dev-environment-auditor/internal/domain"
)

const (
	DefaultMaxEntries   = 50_000
	DefaultMaxFiles     = 32
	DefaultMaxDepth     = 6
	DefaultMaxFileBytes = 1 << 20
)

var errStopDockerAnalysis = errors.New("stop Docker environment analysis")

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
	"vendor":       {},
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
	Projects     []domain.Project
	Requirements []domain.Requirement
	Relations    []domain.Relation
	Diagnostics  []domain.Diagnostic
	Stats        Stats
	Complete     bool
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

func (analyzer *Analyzer) AnalyzeProjects(
	ctx context.Context,
	projects []domain.Project,
	config Config,
) Result {
	result := Result{
		Projects:    cloneProjects(projects),
		Relations:   []domain.Relation{},
		Diagnostics: []domain.Diagnostic{},
		Complete:    true,
	}
	if analyzer == nil || analyzer.fileSystem == nil {
		result.Diagnostics = append(result.Diagnostics, diagnostic(
			"DOCKER_ANALYZER_FILESYSTEM_UNAVAILABLE",
			domain.SeverityError,
			"docker-environment",
			"filesystem adapter is required",
			"",
		))
		result.Complete = false
		return result
	}

	limits := normalizeLimits(config.Limits)
	for index, project := range projects {
		if ctx.Err() != nil {
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"DOCKER_ANALYZER_CANCELLED",
				domain.SeverityError,
				project.ID,
				ctx.Err().Error(),
				project.Path,
			))
			break
		}
		projectResult := analyzer.analyzeProject(ctx, project, limits)
		result.Projects[index].Requirements = append(
			result.Projects[index].Requirements,
			projectResult.Requirements...,
		)
		result.Relations = append(result.Relations, projectResult.Relations...)
		result.Diagnostics = append(result.Diagnostics, projectResult.Diagnostics...)
		result.Stats.EntriesVisited += projectResult.Stats.EntriesVisited
		result.Stats.FilesRead += projectResult.Stats.FilesRead
		result.Stats.BytesRead += projectResult.Stats.BytesRead
	}
	sortRelations(result.Relations)
	sortDiagnostics(result.Diagnostics)
	result.Complete = dockerAnalysisComplete(result.Diagnostics)
	return result
}

type jdkProvider struct {
	feature  int
	evidence domain.Evidence
}

type imageDeclaration struct {
	image    string
	evidence domain.Evidence
}

func (analyzer *Analyzer) analyzeProject(
	ctx context.Context,
	project domain.Project,
	limits Limits,
) Result {
	result := Result{
		Requirements: []domain.Requirement{},
		Relations:    []domain.Relation{},
		Diagnostics:  []domain.Diagnostic{},
	}
	root, err := filepath.Abs(project.Path)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, diagnostic(
			"DOCKER_ANALYZER_INVALID_PROJECT",
			domain.SeverityWarning,
			project.ID,
			"cannot canonicalize project path",
			project.Path,
		))
		return result
	}
	root = filepath.Clean(root)
	info, err := analyzer.fileSystem.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		result.Diagnostics = append(result.Diagnostics, diagnostic(
			"DOCKER_ANALYZER_PROJECT_UNAVAILABLE",
			domain.SeverityWarning,
			project.ID,
			"project root is not an accessible real directory",
			root,
		))
		return result
	}

	providers := []jdkProvider{}
	declarations := []imageDeclaration{}
	err = analyzer.fileSystem.WalkDir(root, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return errStopDockerAnalysis
		}
		if walkErr != nil {
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"DOCKER_ANALYZER_ENTRY_UNAVAILABLE",
				domain.SeverityWarning,
				project.ID,
				"cannot inspect a project entry while looking for Dockerfiles",
				currentPath,
			))
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		relative, relativeErr := filepath.Rel(root, currentPath)
		if relativeErr != nil {
			return nil
		}
		if relative != "." && entry.IsDir() {
			if _, excluded := excludedDirectories[entry.Name()]; excluded {
				return fs.SkipDir
			}
		}
		if pathDepth(relative) > limits.MaxDepth {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		result.Stats.EntriesVisited++
		if result.Stats.EntriesVisited > limits.MaxEntries {
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"DOCKER_ANALYZER_MAX_ENTRIES_REACHED",
				domain.SeverityWarning,
				project.ID,
				fmt.Sprintf("maximum Dockerfile search budget %d reached", limits.MaxEntries),
				root,
			))
			return errStopDockerAnalysis
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() || (!isDockerfile(entry.Name()) && !isComposeFile(entry.Name())) {
			return nil
		}
		if result.Stats.FilesRead >= limits.MaxFiles {
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"DOCKER_ANALYZER_MAX_FILES_REACHED",
				domain.SeverityWarning,
				project.ID,
				fmt.Sprintf("maximum Docker declaration file budget %d reached", limits.MaxFiles),
				root,
			))
			return errStopDockerAnalysis
		}

		content, readDiagnostic := analyzer.readBounded(currentPath, limits.MaxFileBytes, project.ID)
		if readDiagnostic != nil {
			result.Diagnostics = append(result.Diagnostics, *readDiagnostic)
			return nil
		}
		result.Stats.FilesRead++
		result.Stats.BytesRead += int64(len(content))
		fileProviders := []jdkProvider{}
		fileDeclarations := []imageDeclaration{}
		fileDiagnostics := []domain.Diagnostic{}
		if isDockerfile(entry.Name()) {
			fileProviders, fileDeclarations, fileDiagnostics = parseDockerfile(project, currentPath, content)
		} else {
			fileDeclarations, fileDiagnostics = parseComposeFile(project, currentPath, content)
		}
		providers = append(providers, fileProviders...)
		declarations = append(declarations, fileDeclarations...)
		result.Diagnostics = append(result.Diagnostics, fileDiagnostics...)
		return nil
	})

	if ctx.Err() != nil {
		result.Diagnostics = append(result.Diagnostics, diagnostic(
			"DOCKER_ANALYZER_CANCELLED",
			domain.SeverityError,
			project.ID,
			ctx.Err().Error(),
			root,
		))
	} else if err != nil && !errors.Is(err, errStopDockerAnalysis) {
		result.Diagnostics = append(result.Diagnostics, diagnostic(
			"DOCKER_ANALYZER_WALK_FAILED",
			domain.SeverityWarning,
			project.ID,
			"cannot complete the bounded Dockerfile search",
			root,
		))
	}
	result.Relations = dockerJDKRelations(project, providers)
	result.Requirements = dockerImageRequirements(project, declarations)
	return result
}

func (analyzer *Analyzer) readBounded(path string, maxBytes int64, scope string) ([]byte, *domain.Diagnostic) {
	file, err := analyzer.fileSystem.Open(path)
	if err != nil {
		item := diagnostic(
			"DOCKERFILE_READ_FAILED",
			domain.SeverityWarning,
			scope,
			"cannot read Docker declaration file",
			path,
		)
		return nil, &item
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		item := diagnostic(
			"DOCKERFILE_READ_FAILED",
			domain.SeverityWarning,
			scope,
			"cannot read Docker declaration file",
			path,
		)
		return nil, &item
	}
	if int64(len(content)) > maxBytes {
		item := diagnostic(
			"DOCKERFILE_TOO_LARGE",
			domain.SeverityWarning,
			scope,
			fmt.Sprintf("Docker declaration file exceeds the %d byte analysis limit", maxBytes),
			path,
		)
		return nil, &item
	}
	return content, nil
}

func parseDockerfile(
	project domain.Project,
	filePath string,
	content []byte,
) ([]jdkProvider, []imageDeclaration, []domain.Diagnostic) {
	providers := []jdkProvider{}
	declarations := []imageDeclaration{}
	diagnostics := []domain.Diagnostic{}
	stageAliases := map[string]struct{}{}
	for index, rawLine := range strings.Split(string(content), "\n") {
		image, alias, found := dockerFromDirective(rawLine)
		if !found {
			continue
		}
		if strings.Contains(image, "$") {
			diagnostics = append(diagnostics, diagnostic(
				"DOCKER_IMAGE_DYNAMIC",
				domain.SeverityInfo,
				project.ID,
				"dynamic Docker base image was not interpreted",
				filePath,
			))
			if alias != "" {
				stageAliases[strings.ToLower(alias)] = struct{}{}
			}
			continue
		}
		if strings.EqualFold(image, "scratch") {
			if alias != "" {
				stageAliases[strings.ToLower(alias)] = struct{}{}
			}
			continue
		}
		if _, internalStage := stageAliases[strings.ToLower(image)]; internalStage {
			if alias != "" {
				stageAliases[strings.ToLower(alias)] = struct{}{}
			}
			continue
		}

		line := index + 1
		key := "FROM"
		observed := boundedObservedValue(image)
		pathCopy := filePath
		evidence := domain.Evidence{
			SourceType:    "file",
			FilePath:      &pathCopy,
			Key:           &key,
			LineHint:      &line,
			ObservedValue: &observed,
			RuleID:        "docker.from.image.v1",
		}
		declarations = append(declarations, imageDeclaration{image: image, evidence: evidence})
		feature, isJDK, versionKnown := dockerImageJDKFeature(image)
		if !isJDK {
			if alias != "" {
				stageAliases[strings.ToLower(alias)] = struct{}{}
			}
			continue
		}
		if !versionKnown {
			diagnostics = append(diagnostics, diagnostic(
				"DOCKER_JDK_VERSION_UNKNOWN",
				domain.SeverityInfo,
				project.ID,
				"recognized a Docker JDK image but could not extract a static feature version",
				filePath,
			))
			if alias != "" {
				stageAliases[strings.ToLower(alias)] = struct{}{}
			}
			continue
		}
		evidence.RuleID = "docker.from.jdk.v1"
		providers = append(providers, jdkProvider{
			feature:  feature,
			evidence: evidence,
		})
		if alias != "" {
			stageAliases[strings.ToLower(alias)] = struct{}{}
		}
	}
	return providers, declarations, diagnostics
}

func parseComposeFile(
	project domain.Project,
	filePath string,
	content []byte,
) ([]imageDeclaration, []domain.Diagnostic) {
	declarations := []imageDeclaration{}
	diagnostics := []domain.Diagnostic{}
	servicesIndent := -1
	serviceIndent := -1
	attributeIndent := -1
	unsupportedStructureReported := false

	for index, rawLine := range strings.Split(string(content), "\n") {
		lineWithoutComment := stripYAMLComment(rawLine)
		if strings.TrimSpace(lineWithoutComment) == "" {
			continue
		}
		indent, tabs := yamlIndent(lineWithoutComment)
		if tabs {
			if !unsupportedStructureReported {
				diagnostics = append(diagnostics, diagnostic(
					"DOCKER_COMPOSE_STRUCTURE_UNSUPPORTED",
					domain.SeverityInfo,
					project.ID,
					"Compose indentation with tabs was not interpreted",
					filePath,
				))
				unsupportedStructureReported = true
			}
			continue
		}
		key, value, mapping := yamlMappingLine(strings.TrimSpace(lineWithoutComment))
		if !mapping {
			continue
		}

		if servicesIndent < 0 {
			if indent == 0 && key == "services" {
				if strings.TrimSpace(value) != "" {
					diagnostics = append(diagnostics, diagnostic(
						"DOCKER_COMPOSE_STRUCTURE_UNSUPPORTED",
						domain.SeverityInfo,
						project.ID,
						"inline Compose services were not interpreted",
						filePath,
					))
					return declarations, diagnostics
				}
				servicesIndent = indent
			}
			continue
		}

		if indent <= servicesIndent {
			servicesIndent = -1
			serviceIndent = -1
			attributeIndent = -1
			continue
		}
		if serviceIndent < 0 || indent == serviceIndent {
			if serviceIndent < 0 {
				serviceIndent = indent
			}
			if indent == serviceIndent {
				attributeIndent = -1
				if strings.TrimSpace(value) != "" && !unsupportedStructureReported {
					diagnostics = append(diagnostics, diagnostic(
						"DOCKER_COMPOSE_STRUCTURE_UNSUPPORTED",
						domain.SeverityInfo,
						project.ID,
						"inline Compose service mappings were not interpreted",
						filePath,
					))
					unsupportedStructureReported = true
				}
				continue
			}
		}
		if indent < serviceIndent {
			continue
		}
		if attributeIndent < 0 {
			attributeIndent = indent
		}
		if indent != attributeIndent || key != "image" {
			continue
		}

		image, scalar := yamlScalar(value)
		if !scalar || image == "" || strings.Contains(image, "$") || strings.HasPrefix(image, "*") {
			diagnostics = append(diagnostics, diagnostic(
				"DOCKER_IMAGE_DYNAMIC",
				domain.SeverityInfo,
				project.ID,
				"dynamic or non-scalar Compose image was not interpreted",
				filePath,
			))
			continue
		}
		line := index + 1
		keyCopy := "services.*.image"
		pathCopy := filePath
		observed := boundedObservedValue(image)
		declarations = append(declarations, imageDeclaration{
			image: image,
			evidence: domain.Evidence{
				SourceType:    "file",
				FilePath:      &pathCopy,
				Key:           &keyCopy,
				LineHint:      &line,
				ObservedValue: &observed,
				RuleID:        "docker.compose.image.v1",
			},
		})
	}
	return declarations, diagnostics
}

func dockerImageRequirements(project domain.Project, declarations []imageDeclaration) []domain.Requirement {
	byImage := map[string]*domain.Requirement{}
	for _, declaration := range declarations {
		image := strings.TrimSpace(declaration.image)
		if image == "" {
			continue
		}
		requirement := byImage[image]
		if requirement == nil {
			digest := sha256.Sum256([]byte(project.ID + "\x00docker\x00image\x00" + image))
			constraint := image
			requirement = &domain.Requirement{
				ID:                "requirement-" + hex.EncodeToString(digest[:8]),
				Ecosystem:         "docker",
				Component:         "image",
				VersionConstraint: &constraint,
				Confidence:        domain.RequiredExplicitly,
				Evidence:          []domain.Evidence{},
				Warnings:          []string{},
			}
			byImage[image] = requirement
		}
		requirement.Evidence = append(requirement.Evidence, declaration.evidence)
	}
	images := make([]string, 0, len(byImage))
	for image := range byImage {
		images = append(images, image)
	}
	sort.Strings(images)
	result := make([]domain.Requirement, 0, len(images))
	for _, image := range images {
		result = append(result, *byImage[image])
	}
	return result
}

func yamlIndent(line string) (int, bool) {
	indent := 0
	for _, character := range line {
		switch character {
		case ' ':
			indent++
		case '\t':
			return indent, true
		default:
			return indent, false
		}
	}
	return indent, false
}

func yamlMappingLine(line string) (string, string, bool) {
	quoted := rune(0)
	for index, character := range line {
		if quoted != 0 {
			if character == quoted {
				quoted = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quoted = character
			continue
		}
		if character == ':' {
			key, ok := yamlScalar(strings.TrimSpace(line[:index]))
			return key, strings.TrimSpace(line[index+1:]), ok && key != ""
		}
	}
	return "", "", false
}

func stripYAMLComment(line string) string {
	quoted := rune(0)
	for index, character := range line {
		if quoted != 0 {
			if character == quoted {
				quoted = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quoted = character
			continue
		}
		if character == '#' && (index == 0 || line[index-1] == ' ' || line[index-1] == '\t') {
			return strings.TrimRight(line[:index], " \t")
		}
	}
	return line
}

func yamlScalar(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", true
	}
	if strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{") ||
		strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") || strings.HasPrefix(value, "!") ||
		strings.ContainsAny(value, "\r\n") {
		return "", false
	}
	if value[0] == '"' {
		unquoted, err := strconv.Unquote(value)
		return unquoted, err == nil
	}
	if value[0] == '\'' {
		if len(value) < 2 || value[len(value)-1] != '\'' {
			return "", false
		}
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'"), true
	}
	return value, true
}

func dockerJDKRelations(project domain.Project, providers []jdkProvider) []domain.Relation {
	if len(providers) == 0 {
		return []domain.Relation{}
	}
	relations := []domain.Relation{}
	for _, requirement := range project.Requirements {
		if !strings.EqualFold(requirement.Ecosystem, "java") ||
			!strings.EqualFold(requirement.Component, "jdk") {
			continue
		}
		relation := domain.Relation{
			ProjectID:     project.ID,
			RequirementID: requirement.ID,
			Environment:   domain.EnvironmentDocker,
			MatchStatus:   domain.MatchUnknown,
			Rationale:     "Docker JDK declarations were observed but could not be matched safely",
			Evidence:      []domain.Evidence{},
			Warnings: []string{
				"Dockerfile declarations do not prove that an image is present or a container is running",
			},
		}
		for _, provider := range providers {
			relation.Evidence = append(relation.Evidence, provider.evidence)
		}
		if requirement.Confidence != domain.RequiredExplicitly || requirement.VersionConstraint == nil {
			relations = append(relations, relation)
			continue
		}
		feature, ok := jdkConstraintFeature(*requirement.VersionConstraint)
		if !ok {
			relation.Rationale = "the JDK constraint could not be reduced to a static feature version for Docker matching"
			relations = append(relations, relation)
			continue
		}

		matchingEvidence := []domain.Evidence{}
		for _, provider := range providers {
			if provider.feature == feature {
				matchingEvidence = append(matchingEvidence, provider.evidence)
			}
		}
		if len(matchingEvidence) > 0 {
			relation.MatchStatus = domain.Matched
			relation.Evidence = matchingEvidence
			relation.Rationale = fmt.Sprintf(
				"%d Dockerfile FROM declaration(s) provide JDK feature %d required by the project",
				len(matchingEvidence),
				feature,
			)
		} else {
			relation.Rationale = fmt.Sprintf(
				"Dockerfiles declare JDK images, but none was proven to provide required feature %d; no Docker MISSING conclusion was made",
				feature,
			)
		}
		relations = append(relations, relation)
	}
	return relations
}

func dockerFromImage(line string) (string, bool) {
	image, _, found := dockerFromDirective(line)
	return image, found
}

func dockerFromDirective(line string) (string, string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "FROM") {
		return "", "", false
	}
	index := 1
	for index < len(fields) && strings.HasPrefix(fields[index], "--") {
		index++
	}
	if index >= len(fields) || strings.HasPrefix(fields[index], "#") {
		return "", "", false
	}
	image := fields[index]
	alias := ""
	if index+2 < len(fields) && strings.EqualFold(fields[index+1], "AS") {
		alias = fields[index+2]
	}
	return image, alias, true
}

func dockerImageJDKFeature(image string) (feature int, isJDK bool, versionKnown bool) {
	name, tag := splitImageNameAndTag(strings.ToLower(strings.TrimSpace(image)))
	base := name
	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}
	if strings.Contains(tag, "jre") {
		return 0, false, false
	}

	switch base {
	case "eclipse-temurin", "openjdk", "amazoncorretto", "corretto", "sapmachine", "liberica-openjdk", "zulu-openjdk":
		feature, ok := leadingFeature(tag)
		return feature, true, ok
	case "gradle":
		feature, ok := featureAfterMarker(tag, "jdk")
		return feature, true, ok
	case "maven":
		for _, marker := range []string{"temurin-", "openjdk-", "corretto-"} {
			if feature, ok := featureAfterMarker(tag, marker); ok {
				return feature, true, true
			}
		}
		return 0, true, false
	default:
		return 0, false, false
	}
}

func splitImageNameAndTag(image string) (string, string) {
	if digest := strings.IndexByte(image, '@'); digest >= 0 {
		image = image[:digest]
	}
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon <= slash {
		return image, ""
	}
	return image[:colon], image[colon+1:]
}

func leadingFeature(value string) (int, bool) {
	digits := leadingDigits(value)
	if digits == "" {
		return 0, false
	}
	feature, err := strconv.Atoi(digits)
	return feature, err == nil && feature > 0
}

func featureAfterMarker(value, marker string) (int, bool) {
	start := strings.LastIndex(value, marker)
	if start < 0 {
		return 0, false
	}
	start += len(marker)
	digits := leadingDigits(value[start:])
	if digits == "" {
		return 0, false
	}
	feature, err := strconv.Atoi(digits)
	return feature, err == nil && feature > 0
}

func jdkConstraintFeature(value string) (int, bool) {
	value = strings.Trim(strings.TrimSpace(value), "\"'")
	if strings.HasPrefix(value, "1.") {
		value = strings.TrimPrefix(value, "1.")
	}
	return leadingFeature(value)
}

func leadingDigits(value string) string {
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	return value[:end]
}

func boundedObservedValue(value string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
	const maxRunes = 512
	runes := []rune(value)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return value
}

func isDockerfile(name string) bool {
	name = strings.ToLower(name)
	return name == "dockerfile" || strings.HasPrefix(name, "dockerfile.")
}

func isComposeFile(name string) bool {
	switch strings.ToLower(name) {
	case "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml":
		return true
	default:
		return false
	}
}

func pathDepth(relative string) int {
	if relative == "." {
		return 0
	}
	return strings.Count(filepath.ToSlash(relative), "/") + 1
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

func dockerAnalysisComplete(diagnostics []domain.Diagnostic) bool {
	for _, item := range diagnostics {
		if item.Severity == domain.SeverityWarning || item.Severity == domain.SeverityError {
			return false
		}
		switch item.Code {
		case "DOCKER_IMAGE_DYNAMIC", "DOCKER_COMPOSE_STRUCTURE_UNSUPPORTED":
			return false
		}
	}
	return true
}

func cloneProjects(projects []domain.Project) []domain.Project {
	result := make([]domain.Project, len(projects))
	for index, project := range projects {
		result[index] = project
		result[index].Requirements = append([]domain.Requirement(nil), project.Requirements...)
		result[index].Warnings = append([]string(nil), project.Warnings...)
	}
	return result
}

func diagnostic(
	code string,
	severity domain.DiagnosticSeverity,
	scope string,
	message string,
	path string,
) domain.Diagnostic {
	result := domain.Diagnostic{Code: code, Severity: severity, Scope: scope, Message: message}
	if path != "" {
		pathCopy := path
		result.Path = &pathCopy
	}
	return result
}

func sortRelations(relations []domain.Relation) {
	sort.SliceStable(relations, func(left, right int) bool {
		return relations[left].ProjectID+"\x00"+relations[left].RequirementID <
			relations[right].ProjectID+"\x00"+relations[right].RequirementID
	})
}

func sortDiagnostics(diagnostics []domain.Diagnostic) {
	sort.SliceStable(diagnostics, func(left, right int) bool {
		leftPath, rightPath := "", ""
		if diagnostics[left].Path != nil {
			leftPath = *diagnostics[left].Path
		}
		if diagnostics[right].Path != nil {
			rightPath = *diagnostics[right].Path
		}
		return diagnostics[left].Scope+"\x00"+diagnostics[left].Code+"\x00"+leftPath <
			diagnostics[right].Scope+"\x00"+diagnostics[right].Code+"\x00"+rightPath
	})
}
