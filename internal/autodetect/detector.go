// Package autodetect finds likely project and toolchain roots without executing
// package managers, SDK commands, builds, or network operations.
package autodetect

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"dev-environment-auditor/internal/domain"
)

const (
	DefaultMaxEntries        = 750_000
	DefaultMaxEntriesPerRoot = 250_000
	DefaultMaxDepth          = 10
)

var errStopSearch = errors.New("stop automatic search")
var errStopSearchRoot = errors.New("stop automatic search root")

type Family string

const (
	FamilyAndroid Family = "android"
	FamilyFlutter Family = "flutter"
	FamilyGradle  Family = "gradle"
	FamilyJava    Family = "java"
)

type Limits struct {
	MaxEntries        int
	MaxEntriesPerRoot int
	MaxDepth          int
}

type Config struct {
	NeedProjectRoots bool
	NeedAndroid      bool
	NeedFlutter      bool
	NeedFVM          bool
	NeedGradle       bool
	NeedJDK          bool
	DeepSearch       bool
	Limits           Limits
}

type Roots struct {
	ProjectRoots        []string
	AndroidSDKRoots     []string
	FlutterSDKRoots     []string
	FVMCacheRoots       []string
	GradleUserHomeRoots []string
	JDKRoots            []string
}

type Result struct {
	Roots                      Roots
	Diagnostics                []domain.Diagnostic
	HeuristicInventoryFamilies []Family
	ProjectDiscoveryHeuristic  bool
}

type FileSystem interface {
	Lstat(name string) (fs.FileInfo, error)
	ReadDir(name string) ([]fs.DirEntry, error)
	WalkDir(root string, fn fs.WalkDirFunc) error
	EvalSymlinks(path string) (string, error)
}

type Environment interface {
	UserHomeDir() (string, error)
	Getwd() (string, error)
	LookupEnv(key string) (string, bool)
	LookPath(file string) (string, error)
}

type Detector struct {
	fileSystem  FileSystem
	environment Environment
	goos        string
}

func New() *Detector {
	return NewWithAdapters(osFileSystem{}, osEnvironment{}, runtime.GOOS)
}

func NewWithAdapters(fileSystem FileSystem, environment Environment, goos string) *Detector {
	return &Detector{fileSystem: fileSystem, environment: environment, goos: goos}
}

type locatedRoots struct {
	projects map[string]string
	android  map[string]string
	flutter  map[string]string
	fvm      map[string]string
	gradle   map[string]string
	jdk      map[string]string
}

func newLocatedRoots() *locatedRoots {
	return &locatedRoots{
		projects: make(map[string]string),
		android:  make(map[string]string),
		flutter:  make(map[string]string),
		fvm:      make(map[string]string),
		gradle:   make(map[string]string),
		jdk:      make(map[string]string),
	}
}

func (detector *Detector) Detect(ctx context.Context, config Config) Result {
	result := Result{Diagnostics: []domain.Diagnostic{}, HeuristicInventoryFamilies: []Family{}}
	if detector == nil || detector.fileSystem == nil || detector.environment == nil {
		result.Diagnostics = append(result.Diagnostics, diagnostic(
			"AUTODETECT_UNAVAILABLE",
			domain.SeverityError,
			"autodetect",
			"automatic detection requires filesystem and environment adapters",
			"",
		))
		return result
	}

	limits := normalizeLimits(config.Limits)
	located := newLocatedRoots()
	home, homeErr := detector.environment.UserHomeDir()
	if homeErr != nil {
		result.Diagnostics = append(result.Diagnostics, diagnostic(
			"AUTODETECT_HOME_UNAVAILABLE",
			domain.SeverityWarning,
			"autodetect",
			"cannot determine the user home directory",
			"",
		))
	} else {
		home = filepath.Clean(home)
	}
	workingDirectory, workingDirectoryErr := detector.environment.Getwd()
	if workingDirectoryErr == nil {
		workingDirectory = filepath.Clean(workingDirectory)
	}

	detector.detectEnvironmentAndConventions(config, home, workingDirectory, located)
	searchComplete := true
	if config.DeepSearch && ctx.Err() == nil {
		var diagnostics []domain.Diagnostic
		searchComplete, diagnostics = detector.deepSearch(ctx, config, home, workingDirectory, limits, located)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
	}

	if config.NeedProjectRoots {
		located.projects = detector.filterProjectRoots(located.projects, located.flutter, located.android)
		if len(located.projects) == 0 && workingDirectoryErr == nil && detector.isDirectory(workingDirectory) {
			located.projects[workingDirectory] = "working-directory fallback"
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"AUTODETECT_PROJECT_FALLBACK",
				domain.SeverityInfo,
				"autodetect/projects",
				"no project marker was found; the working directory will be scanned",
				workingDirectory,
			))
		}
		result.ProjectDiscoveryHeuristic = true
	}

	result.Roots = Roots{
		ProjectRoots:        sortedKeys(located.projects),
		AndroidSDKRoots:     sortedKeys(located.android),
		FlutterSDKRoots:     sortedKeys(located.flutter),
		FVMCacheRoots:       sortedKeys(located.fvm),
		GradleUserHomeRoots: sortedKeys(located.gradle),
		JDKRoots:            sortedKeys(located.jdk),
	}

	result.Diagnostics = append(result.Diagnostics, foundDiagnostics("project", located.projects)...)
	result.Diagnostics = append(result.Diagnostics, foundDiagnostics("android", located.android)...)
	result.Diagnostics = append(result.Diagnostics, foundDiagnostics("flutter", located.flutter)...)
	result.Diagnostics = append(result.Diagnostics, foundDiagnostics("fvm", located.fvm)...)
	result.Diagnostics = append(result.Diagnostics, foundDiagnostics("gradle", located.gradle)...)
	result.Diagnostics = append(result.Diagnostics, foundDiagnostics("java", located.jdk)...)

	requestedFamilies := []struct {
		needed bool
		family Family
		roots  []string
	}{
		{config.NeedAndroid, FamilyAndroid, result.Roots.AndroidSDKRoots},
		{config.NeedFlutter || config.NeedFVM, FamilyFlutter, append(cloneStrings(result.Roots.FlutterSDKRoots), result.Roots.FVMCacheRoots...)},
		{config.NeedGradle, FamilyGradle, result.Roots.GradleUserHomeRoots},
		{config.NeedJDK, FamilyJava, result.Roots.JDKRoots},
	}
	for _, requested := range requestedFamilies {
		if !requested.needed {
			continue
		}
		result.HeuristicInventoryFamilies = append(result.HeuristicInventoryFamilies, requested.family)
		if len(requested.roots) == 0 {
			message := "no compatible root was found automatically"
			if !searchComplete {
				message = "no compatible root was found before the bounded search became incomplete"
			}
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"AUTODETECT_FAMILY_NOT_FOUND",
				domain.SeverityInfo,
				"autodetect/"+string(requested.family),
				message,
				"",
			))
		}
	}

	sort.SliceStable(result.Diagnostics, func(left, right int) bool {
		return diagnosticKey(result.Diagnostics[left]) < diagnosticKey(result.Diagnostics[right])
	})
	return result
}

func normalizeLimits(limits Limits) Limits {
	if limits.MaxEntries <= 0 {
		limits.MaxEntries = DefaultMaxEntries
	}
	if limits.MaxEntriesPerRoot <= 0 {
		limits.MaxEntriesPerRoot = DefaultMaxEntriesPerRoot
	}
	if limits.MaxEntriesPerRoot > limits.MaxEntries {
		limits.MaxEntriesPerRoot = limits.MaxEntries
	}
	if limits.MaxDepth <= 0 {
		limits.MaxDepth = DefaultMaxDepth
	}
	return limits
}

func (detector *Detector) detectEnvironmentAndConventions(
	config Config,
	home string,
	workingDirectory string,
	located *locatedRoots,
) {
	if config.NeedProjectRoots {
		if value, ok := detector.environment.LookupEnv("DEV_AUDIT_PROJECT_ROOTS"); ok {
			for _, candidate := range filepath.SplitList(value) {
				detector.addProjectRoot(located.projects, candidate, "DEV_AUDIT_PROJECT_ROOTS")
			}
		}
		for current := workingDirectory; current != ""; current = filepath.Dir(current) {
			if detector.isProjectRoot(current) {
				detector.addProjectRoot(located.projects, current, "working directory ancestor")
				break
			}
			next := filepath.Dir(current)
			if next == current || (home != "" && !pathWithin(home, next)) {
				break
			}
		}
	}

	if config.NeedAndroid {
		for _, variable := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
			if value, ok := detector.environment.LookupEnv(variable); ok {
				detector.addAndroidRoot(located.android, value, variable)
			}
		}
		if executable, err := detector.environment.LookPath("adb"); err == nil {
			detector.addAndroidRoot(located.android, filepath.Dir(filepath.Dir(detector.resolvePath(executable))), "adb on PATH")
		}
		for _, candidate := range []string{
			filepath.Join(home, "Library", "Android", "sdk"),
			filepath.Join(home, "Android", "Sdk"),
			"/opt/android-sdk",
			"/usr/local/share/android-sdk",
			"/opt/homebrew/share/android-sdk",
		} {
			detector.addAndroidRoot(located.android, candidate, "conventional location")
		}
	}

	if config.NeedFlutter {
		for _, variable := range []string{"FLUTTER_ROOT", "FLUTTER_HOME"} {
			if value, ok := detector.environment.LookupEnv(variable); ok {
				detector.addFlutterRoot(located.flutter, value, variable)
			}
		}
		if executable, err := detector.environment.LookPath("flutter"); err == nil {
			resolved := detector.resolvePath(executable)
			detector.addFlutterRoot(located.flutter, filepath.Dir(filepath.Dir(resolved)), "flutter on PATH")
		}
		for _, candidate := range []string{
			filepath.Join(home, "flutter"),
			filepath.Join(home, "Developer", "flutter"),
			filepath.Join(home, "Development", "flutter"),
			filepath.Join(home, "development", "flutter"),
			filepath.Join(home, "dev", "flutter"),
		} {
			detector.addFlutterRoot(located.flutter, candidate, "conventional location")
		}
	}

	if config.NeedFVM {
		for _, variable := range []string{"FVM_CACHE_PATH", "FVM_HOME"} {
			if value, ok := detector.environment.LookupEnv(variable); ok {
				detector.addFlexibleFVMRoot(located.fvm, value, variable)
			}
		}
		for _, candidate := range []string{
			filepath.Join(home, ".fvm", "versions"),
			filepath.Join(home, ".fvm", "cache"),
			filepath.Join(home, "fvm", "versions"),
			filepath.Join(home, "fvm", "cache"),
		} {
			detector.addFVMRoot(located.fvm, candidate, "conventional location")
		}
	}

	if config.NeedGradle {
		if value, ok := detector.environment.LookupEnv("GRADLE_USER_HOME"); ok {
			detector.addGradleRoot(located.gradle, value, "GRADLE_USER_HOME")
		}
		detector.addGradleRoot(located.gradle, filepath.Join(home, ".gradle"), "conventional location")
	}

	if config.NeedJDK {
		for _, variable := range []string{"STUDIO_JDK", "JDK_HOME", "JAVA_HOME"} {
			if value, ok := detector.environment.LookupEnv(variable); ok {
				detector.addJDKRoot(located.jdk, value, variable)
			}
		}
		for _, executableName := range []string{"javac", "java"} {
			if executable, err := detector.environment.LookPath(executableName); err == nil {
				resolved := detector.resolvePath(executable)
				detector.addJDKRoot(located.jdk, filepath.Dir(filepath.Dir(resolved)), executableName+" on PATH")
			}
		}
		for _, container := range []string{
			filepath.Join(home, "Library", "Java", "JavaVirtualMachines"),
			filepath.Join(home, ".sdkman", "candidates", "java"),
			filepath.Join(home, ".asdf", "installs", "java"),
			filepath.Join(home, ".local", "share", "mise", "installs", "java"),
			filepath.Join(home, ".gradle", "jdks"),
		} {
			detector.addJDKContainer(located.jdk, container, "user toolchain container")
		}
		if detector.goos == "darwin" {
			detector.addJDKContainer(located.jdk, "/Library/Java/JavaVirtualMachines", "macOS JavaVirtualMachines")
			detector.addApplicationJDKs(located.jdk, "/Applications")
			detector.addApplicationJDKs(located.jdk, filepath.Join(home, "Applications"))
			detector.addHomebrewJDKs(located.jdk, "/opt/homebrew/Cellar")
			detector.addHomebrewJDKs(located.jdk, "/usr/local/Cellar")
		}
	}
}

func (detector *Detector) deepSearch(
	ctx context.Context,
	config Config,
	home string,
	workingDirectory string,
	limits Limits,
	located *locatedRoots,
) (bool, []domain.Diagnostic) {
	searchRoots := detector.deepSearchRoots(home, workingDirectory)
	visited := 0
	diagnostics := []domain.Diagnostic{}
	complete := true

	for _, root := range searchRoots {
		if ctx.Err() != nil {
			complete = false
			break
		}
		rootVisited := 0
		err := detector.fileSystem.WalkDir(root, func(currentPath string, entry fs.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return errStopSearch
			}
			if walkErr != nil {
				diagnostics = append(diagnostics, diagnostic(
					"AUTODETECT_ENTRY_UNAVAILABLE",
					domain.SeverityInfo,
					"autodetect/search",
					"filesystem entry could not be inspected during bounded search",
					currentPath,
				))
				if entry != nil && entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}

			relative, err := filepath.Rel(root, currentPath)
			if err != nil {
				return nil
			}
			depth := pathDepth(relative)
			if depth > limits.MaxDepth {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if relative != "." && entry.Type()&fs.ModeSymlink != 0 {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}

			visited++
			rootVisited++
			if visited > limits.MaxEntries {
				complete = false
				diagnostics = append(diagnostics, diagnostic(
					"AUTODETECT_MAX_ENTRIES_REACHED",
					domain.SeverityWarning,
					"autodetect/search",
					fmt.Sprintf("automatic search stopped at the %d-entry budget", limits.MaxEntries),
					root,
				))
				return errStopSearch
			}
			if rootVisited > limits.MaxEntriesPerRoot {
				complete = false
				diagnostics = append(diagnostics, diagnostic(
					"AUTODETECT_MAX_ENTRIES_PER_ROOT_REACHED",
					domain.SeverityWarning,
					"autodetect/search",
					fmt.Sprintf("automatic search moved on after the %d-entry per-root budget", limits.MaxEntriesPerRoot),
					root,
				))
				return errStopSearchRoot
			}

			if entry.IsDir() {
				if relative != "." && shouldPruneDirectory(entry.Name()) {
					if config.NeedGradle && entry.Name() == ".gradle" {
						detector.addGradleRoot(located.gradle, currentPath, "bounded deep search")
					}
					return fs.SkipDir
				}
				if detector.detectLikelyDirectory(config, currentPath, entry.Name(), located) {
					return fs.SkipDir
				}
				return nil
			}

			detector.detectMarkerFile(config, root, currentPath, entry.Name(), located)
			return nil
		})
		if errors.Is(err, errStopSearchRoot) {
			continue
		}
		if errors.Is(err, errStopSearch) {
			if ctx.Err() != nil {
				complete = false
			}
			if !complete {
				break
			}
			continue
		}
		if err != nil {
			complete = false
			diagnostics = append(diagnostics, diagnostic(
				"AUTODETECT_ROOT_WALK_FAILED",
				domain.SeverityWarning,
				"autodetect/search",
				"bounded search could not complete this root",
				root,
			))
		}
	}

	if ctx.Err() != nil {
		diagnostics = append(diagnostics, diagnostic(
			"AUTODETECT_CANCELLED",
			domain.SeverityError,
			"autodetect/search",
			ctx.Err().Error(),
			"",
		))
	}
	return complete, diagnostics
}

func (detector *Detector) detectLikelyDirectory(
	config Config,
	path string,
	name string,
	located *locatedRoots,
) bool {
	lowerName := strings.ToLower(name)
	if config.NeedGradle && lowerName == "dists" && filepath.Base(filepath.Dir(path)) == "wrapper" {
		detector.addGradleRoot(located.gradle, filepath.Dir(filepath.Dir(path)), "bounded deep search")
	}
	if config.NeedGradle && lowerName == "modules-2" && filepath.Base(filepath.Dir(path)) == "caches" {
		detector.addGradleRoot(located.gradle, filepath.Dir(filepath.Dir(path)), "bounded deep search")
	}
	if config.NeedAndroid {
		switch lowerName {
		case "platforms", "build-tools", "platform-tools", "cmdline-tools", "ndk", "cmake":
			detector.addAndroidRoot(located.android, filepath.Dir(path), "bounded deep search")
		}
	}
	if config.NeedFlutter && (lowerName == "flutter" || lowerName == "flutter-sdk") && detector.isFlutterSDK(path) {
		detector.addLocated(located.flutter, path, "bounded deep search")
		return true
	}
	if config.NeedAndroid && (lowerName == "sdk" || lowerName == "android-sdk") && detector.isAndroidSDK(path) {
		detector.addLocated(located.android, path, "bounded deep search")
		return true
	}
	if config.NeedJDK && (lowerName == "home" || strings.HasSuffix(lowerName, ".jdk")) {
		if detector.addJDKRoot(located.jdk, path, "bounded deep search") {
			return true
		}
	}
	if config.NeedFVM && (lowerName == "versions" || lowerName == "cache") {
		if detector.addFVMRoot(located.fvm, path, "bounded deep search") {
			return true
		}
	}
	return false
}

func (detector *Detector) detectMarkerFile(
	config Config,
	searchRoot string,
	path string,
	name string,
	located *locatedRoots,
) {
	if config.NeedProjectRoots {
		switch name {
		case "pubspec.yaml", "settings.gradle", "settings.gradle.kts", ".fvmrc":
			detector.addProjectRoot(located.projects, filepath.Dir(path), "bounded deep search")
		case "AndroidManifest.xml":
			detector.addAndroidProjectAncestor(located.projects, searchRoot, filepath.Dir(path))
		}
	}
	if config.NeedFlutter && name == "flutter" && filepath.Base(filepath.Dir(path)) == "bin" {
		detector.addFlutterRoot(located.flutter, filepath.Dir(filepath.Dir(path)), "bounded deep search")
	}
	if config.NeedJDK && name == "release" {
		detector.addJDKRoot(located.jdk, filepath.Dir(path), "bounded deep search")
	}
}

func (detector *Detector) addAndroidProjectAncestor(projects map[string]string, searchRoot, start string) {
	current := start
	for range 6 {
		if detector.isProjectRoot(current) {
			detector.addLocated(projects, current, "bounded deep search")
			return
		}
		if current == searchRoot || !pathWithin(searchRoot, current) {
			return
		}
		next := filepath.Dir(current)
		if next == current {
			return
		}
		current = next
	}
}

func (detector *Detector) deepSearchRoots(home, workingDirectory string) []string {
	candidates := []string{
		filepath.Join(home, "Documents", "projects"),
		filepath.Join(home, "Documents", "Projects"),
		filepath.Join(home, "Developer"),
		filepath.Join(home, "Development"),
		filepath.Join(home, "development"),
		filepath.Join(home, "Projects"),
		filepath.Join(home, "projects"),
		filepath.Join(home, "Workspace"),
		filepath.Join(home, "Workspaces"),
		filepath.Join(home, "code"),
		filepath.Join(home, "dev"),
		filepath.Join(home, "src"),
		filepath.Join(home, ".fvm"),
		filepath.Join(home, "fvm"),
		filepath.Join(home, ".sdkman", "candidates", "java"),
		filepath.Join(home, ".asdf", "installs", "java"),
		filepath.Join(home, ".local", "share", "mise", "installs", "java"),
		filepath.Join(home, "Library", "Android"),
		filepath.Join(home, "Library", "Java"),
	}
	documents := filepath.Join(home, "Documents")
	if detector.isProjectRoot(documents) {
		candidates = append(candidates, documents)
	} else {
		candidates = append(candidates, detector.prioritizedDirectoryChildren(documents)...)
	}
	if workingDirectory != "" {
		candidates = append(candidates, workingDirectory)
	}

	existing := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = detector.canonicalCandidate(candidate)
		if candidate == "" || !detector.isDirectory(candidate) {
			continue
		}
		duplicate := false
		candidateInfo, candidateErr := detector.fileSystem.Lstat(candidate)
		for _, current := range existing {
			currentInfo, currentErr := detector.fileSystem.Lstat(current)
			if candidate == current ||
				(candidateErr == nil && currentErr == nil && os.SameFile(candidateInfo, currentInfo)) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		existing = append(existing, candidate)
	}
	roots := make([]string, 0, len(existing))
	for _, candidate := range existing {
		covered := false
		for _, root := range roots {
			if pathWithin(root, candidate) {
				covered = true
				break
			}
		}
		if !covered {
			roots = append(roots, candidate)
		}
	}
	return roots
}

func (detector *Detector) prioritizedDirectoryChildren(root string) []string {
	entries, err := detector.fileSystem.ReadDir(root)
	if err != nil {
		return nil
	}
	children := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink != 0 || !entry.IsDir() || shouldPruneDirectory(entry.Name()) {
			continue
		}
		children = append(children, filepath.Join(root, entry.Name()))
	}
	sort.SliceStable(children, func(left, right int) bool {
		leftPriority := directorySearchPriority(filepath.Base(children[left]))
		rightPriority := directorySearchPriority(filepath.Base(children[right]))
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return children[left] < children[right]
	})
	return children
}

func directorySearchPriority(name string) int {
	switch strings.ToLower(name) {
	case "projects", "project", "workspace", "workspaces", "developer", "development", "dev", "code", "src":
		return 0
	default:
		return 1
	}
}

func shouldPruneDirectory(name string) bool {
	switch name {
	case ".dart_tool", ".git", ".gradle", ".idea", ".pub-cache", ".Trash",
		".venv", "Pods", "__pycache__", "build", "DerivedData", "dist",
		"env", "node_modules", "target", "testdata", "venv":
		return true
	default:
		return false
	}
}

func (detector *Detector) addProjectRoot(roots map[string]string, candidate, source string) bool {
	candidate = detector.canonicalCandidate(candidate)
	if candidate == "" || !detector.isProjectRoot(candidate) {
		return false
	}
	detector.addLocated(roots, candidate, source)
	return true
}

func (detector *Detector) addAndroidRoot(roots map[string]string, candidate, source string) bool {
	candidate = detector.canonicalCandidate(candidate)
	if candidate == "" || !detector.isAndroidSDK(candidate) {
		return false
	}
	detector.addLocated(roots, candidate, source)
	return true
}

func (detector *Detector) addFlutterRoot(roots map[string]string, candidate, source string) bool {
	candidate = detector.canonicalCandidate(candidate)
	if candidate == "" || !detector.isFlutterSDK(candidate) {
		return false
	}
	detector.addLocated(roots, candidate, source)
	return true
}

func (detector *Detector) addFlexibleFVMRoot(roots map[string]string, candidate, source string) bool {
	if detector.addFVMRoot(roots, candidate, source) {
		return true
	}
	return detector.addFVMRoot(roots, filepath.Join(candidate, "versions"), source+"/versions")
}

func (detector *Detector) addFVMRoot(roots map[string]string, candidate, source string) bool {
	candidate = detector.canonicalCandidate(candidate)
	if candidate == "" || !detector.isFVMCache(candidate) {
		return false
	}
	detector.addLocated(roots, candidate, source)
	return true
}

func (detector *Detector) addGradleRoot(roots map[string]string, candidate, source string) bool {
	candidate = detector.canonicalCandidate(candidate)
	if candidate == "" || !detector.isGradleUserHome(candidate) {
		return false
	}
	detector.addLocated(roots, candidate, source)
	return true
}

func (detector *Detector) addJDKRoot(roots map[string]string, candidate, source string) bool {
	candidate = detector.canonicalCandidate(candidate)
	if candidate == "" {
		return false
	}
	for _, home := range []string{candidate, filepath.Join(candidate, "Contents", "Home")} {
		if detector.isJDKHome(home) {
			detector.addLocated(roots, home, source)
			return true
		}
	}
	return false
}

func (detector *Detector) addJDKContainer(roots map[string]string, container, source string) {
	container = detector.canonicalCandidate(container)
	entries, err := detector.fileSystem.ReadDir(container)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		detector.addJDKRoot(roots, filepath.Join(container, entry.Name()), source)
	}
}

func (detector *Detector) addApplicationJDKs(roots map[string]string, applicationsRoot string) {
	applicationsRoot = detector.canonicalCandidate(applicationsRoot)
	entries, err := detector.fileSystem.ReadDir(applicationsRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink != 0 || !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".app") {
			continue
		}
		application := filepath.Join(applicationsRoot, entry.Name())
		for _, relative := range []string{
			filepath.Join("Contents", "jbr", "Contents", "Home"),
			filepath.Join("Contents", "jre", "Contents", "Home"),
			filepath.Join("Contents", "Home"),
		} {
			detector.addJDKRoot(roots, filepath.Join(application, relative), "application-bundled JDK")
		}
	}
}

func (detector *Detector) addHomebrewJDKs(roots map[string]string, cellar string) {
	cellar = detector.canonicalCandidate(cellar)
	formulae, err := detector.fileSystem.ReadDir(cellar)
	if err != nil {
		return
	}
	for _, formula := range formulae {
		if !formula.IsDir() || !strings.HasPrefix(formula.Name(), "openjdk") {
			continue
		}
		formulaRoot := filepath.Join(cellar, formula.Name())
		versions, err := detector.fileSystem.ReadDir(formulaRoot)
		if err != nil {
			continue
		}
		for _, version := range versions {
			if version.Type()&fs.ModeSymlink != 0 || !version.IsDir() {
				continue
			}
			detector.addJDKRoot(
				roots,
				filepath.Join(formulaRoot, version.Name(), "libexec", "openjdk.jdk", "Contents", "Home"),
				"Homebrew Cellar",
			)
		}
	}
}

func (detector *Detector) addLocated(roots map[string]string, path, source string) {
	pathInfo, pathErr := detector.fileSystem.Lstat(path)
	if pathErr == nil {
		for existing := range roots {
			existingInfo, existingErr := detector.fileSystem.Lstat(existing)
			if existingErr == nil && os.SameFile(existingInfo, pathInfo) {
				return
			}
		}
	}
	if _, exists := roots[path]; !exists {
		roots[path] = source
	}
}

func (detector *Detector) canonicalCandidate(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return ""
	}
	if strings.HasPrefix(candidate, "~"+string(filepath.Separator)) {
		if home, err := detector.environment.UserHomeDir(); err == nil {
			candidate = filepath.Join(home, strings.TrimPrefix(candidate, "~"+string(filepath.Separator)))
		}
	}
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return ""
	}
	absolute = filepath.Clean(absolute)
	if resolved, err := detector.fileSystem.EvalSymlinks(absolute); err == nil {
		return filepath.Clean(resolved)
	}
	return absolute
}

func (detector *Detector) resolvePath(path string) string {
	if resolved, err := detector.fileSystem.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func (detector *Detector) isProjectRoot(root string) bool {
	flutter := detector.isRegularFile(filepath.Join(root, "pubspec.yaml")) &&
		(detector.isRegularFile(filepath.Join(root, ".metadata")) ||
			detector.isRegularFile(filepath.Join(root, ".fvmrc")) ||
			detector.isDirectory(filepath.Join(root, "android")))
	gradle := (detector.isRegularFile(filepath.Join(root, "settings.gradle")) ||
		detector.isRegularFile(filepath.Join(root, "settings.gradle.kts"))) &&
		detector.isRegularFile(filepath.Join(root, "gradle", "wrapper", "gradle-wrapper.properties"))
	return flutter || gradle
}

func (detector *Detector) isAndroidSDK(root string) bool {
	found := 0
	for _, directory := range []string{"platforms", "build-tools", "ndk", "cmake", "platform-tools", "cmdline-tools"} {
		if detector.isDirectory(filepath.Join(root, directory)) {
			found++
		}
	}
	return found >= 2
}

func (detector *Detector) isFlutterSDK(root string) bool {
	return detector.isRegularFile(filepath.Join(root, "bin", "flutter")) &&
		detector.isDirectory(filepath.Join(root, "packages", "flutter"))
}

func (detector *Detector) isFVMCache(root string) bool {
	entries, err := detector.fileSystem.ReadDir(root)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		if detector.isFlutterSDK(filepath.Join(root, entry.Name())) {
			return true
		}
	}
	return false
}

func (detector *Detector) isGradleUserHome(root string) bool {
	for _, directory := range []string{
		filepath.Join("wrapper", "dists"),
		filepath.Join("caches", "modules-2"),
		"jdks",
	} {
		if detector.isDirectory(filepath.Join(root, directory)) {
			return true
		}
	}
	return false
}

func (detector *Detector) isJDKHome(root string) bool {
	return detector.isRegularFile(filepath.Join(root, "release")) &&
		detector.isRegularFile(filepath.Join(root, "bin", "java")) &&
		detector.isRegularFile(filepath.Join(root, "bin", "javac"))
}

func (detector *Detector) isDirectory(path string) bool {
	info, err := detector.fileSystem.Lstat(path)
	return err == nil && info.Mode()&fs.ModeSymlink == 0 && info.IsDir()
}

func (detector *Detector) isRegularFile(path string) bool {
	info, err := detector.fileSystem.Lstat(path)
	return err == nil && info.Mode()&fs.ModeSymlink == 0 && info.Mode().IsRegular()
}

func (detector *Detector) filterProjectRoots(
	projects map[string]string,
	flutterRoots map[string]string,
	androidRoots map[string]string,
) map[string]string {
	candidates := sortedKeys(projects)
	filtered := make(map[string]string)
	for _, candidate := range candidates {
		insideSDK := false
		for sdkRoot := range flutterRoots {
			if pathWithin(sdkRoot, candidate) {
				insideSDK = true
				break
			}
		}
		if !insideSDK {
			for sdkRoot := range androidRoots {
				if pathWithin(sdkRoot, candidate) {
					insideSDK = true
					break
				}
			}
		}
		if insideSDK {
			continue
		}
		covered := false
		for root := range filtered {
			if pathWithin(root, candidate) {
				covered = true
				break
			}
		}
		if !covered {
			filtered[candidate] = projects[candidate]
		}
	}
	return filtered
}

func foundDiagnostics(family string, roots map[string]string) []domain.Diagnostic {
	paths := sortedKeys(roots)
	diagnostics := make([]domain.Diagnostic, 0, len(paths))
	for _, path := range paths {
		diagnostics = append(diagnostics, diagnostic(
			"AUTODETECT_ROOT_FOUND",
			domain.SeverityInfo,
			"autodetect/"+family,
			fmt.Sprintf("%s root found via %s", family, roots[path]),
			path,
		))
	}
	return diagnostics
}

func sortedKeys(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func pathWithin(parent, candidate string) bool {
	relative, err := filepath.Rel(parent, candidate)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func pathDepth(path string) int {
	if path == "." || path == "" {
		return 0
	}
	return strings.Count(filepath.ToSlash(filepath.Clean(path)), "/") + 1
}

func diagnostic(code string, severity domain.DiagnosticSeverity, scope, message, path string) domain.Diagnostic {
	value := domain.Diagnostic{Code: code, Severity: severity, Scope: scope, Message: message}
	if path != "" {
		pathCopy := path
		value.Path = &pathCopy
	}
	return value
}

func diagnosticKey(value domain.Diagnostic) string {
	path := ""
	if value.Path != nil {
		path = *value.Path
	}
	return strings.Join([]string{value.Scope, value.Code, string(value.Severity), path, value.Message}, "\x00")
}

func cloneStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}
