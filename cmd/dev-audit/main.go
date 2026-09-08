package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dev-environment-auditor/internal/application"
	"dev-environment-auditor/internal/autodetect"
	"dev-environment-auditor/internal/classification"
	"dev-environment-auditor/internal/domain"
	"dev-environment-auditor/internal/planning"
	"dev-environment-auditor/internal/report"
)

var version = "0.3.0-dev"

const (
	defaultScanTimeout   = 30 * time.Second
	maxInputReportBytes  = 64 << 20
	exitSuccess          = 0
	exitPartial          = 1
	exitUsage            = 2
	exitOperationalError = 3
)

type commandDependencies struct {
	scan         func(context.Context, application.Config) domain.ScanDocument
	detect       func(context.Context, autodetect.Config) autodetect.Result
	readFile     func(string) ([]byte, error)
	writeFile    func(string, []byte) error
	writeNewFile func(string, []byte) error
}

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("value cannot be empty")
	}
	*values = append(*values, value)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	scanner := application.NewScanner(application.SystemClock{}, version)
	detector := autodetect.New()
	return runWithDependencies(arguments, stdout, stderr, commandDependencies{
		scan:         scanner.Scan,
		detect:       detector.Detect,
		readFile:     readReportFile,
		writeFile:    writePrivateFile,
		writeNewFile: writeNewPrivateFile,
	})
}

func runWithDependencies(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	dependencies commandDependencies,
) int {
	if len(arguments) == 0 {
		printRootUsage(stderr)
		return exitUsage
	}
	switch arguments[0] {
	case "version":
		if len(arguments) != 1 {
			fmt.Fprintln(stderr, "version does not accept arguments")
			return exitUsage
		}
		fmt.Fprintln(stdout, version)
		return exitSuccess
	case "scan":
		return runScan(arguments[1:], stdout, stderr, dependencies)
	case "explain":
		return runExplain(arguments[1:], stdout, stderr, dependencies)
	case "plan":
		return runPlan(arguments[1:], stdout, stderr, dependencies)
	case "help", "-h", "--help":
		printRootUsage(stdout)
		return exitSuccess
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", arguments[0])
		printRootUsage(stderr)
		return exitUsage
	}
}

func runPlan(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	dependencies commandDependencies,
) int {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		printPlanUsage(stderr)
		printFlagDefaults(stderr, flags)
	}
	var reportPath string
	var outputPath string
	var format string
	var colorMode string
	var verbose bool
	var resourceIDs stringListFlag
	flags.StringVar(&reportPath, "report", "", "JSON v1 scan report to read")
	flags.Var(&resourceIDs, "resource", "resource ID to include manually; repeatable")
	flags.StringVar(&format, "format", "terminal", "output format: terminal or json")
	flags.StringVar(&outputPath, "output", "", "new output file; never overwritten; '-' means stdout")
	flags.StringVar(&colorMode, "color", "auto", "terminal color: auto, always, or never")
	flags.BoolVar(&verbose, "verbose", false, "show internal plan item identifiers")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitSuccess
		}
		return exitUsage
	}
	if strings.TrimSpace(reportPath) == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "plan requires --report FILE and accepts flags only")
		return exitUsage
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format != "terminal" && format != "json" {
		fmt.Fprintln(stderr, "--format must be terminal or json")
		return exitUsage
	}
	colorMode, err := normalizeColorMode(colorMode)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	if outputPath != "" && outputPath != "-" && sameCleanPath(reportPath, outputPath) {
		fmt.Fprintln(stderr, "--output must not overwrite the input report")
		return exitUsage
	}

	content, err := dependencies.readFile(reportPath)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read scan report: %v\n", err)
		return exitOperationalError
	}
	document, err := report.DecodeJSON(content)
	if err != nil {
		fmt.Fprintf(stderr, "invalid scan report: %v\n", err)
		return exitOperationalError
	}
	digest := sha256.Sum256(content)
	plan, err := planning.Build(document, planning.Config{
		ToolVersion:        version,
		SourceReportSHA256: hex.EncodeToString(digest[:]),
		ResourceIDs:        []string(resourceIDs),
	})
	if err != nil {
		fmt.Fprintf(stderr, "cannot build cleanup plan: %v\n", err)
		return exitOperationalError
	}
	var payload []byte
	if format == "json" {
		payload, err = report.RenderPlanJSON(plan)
	} else {
		payload, err = report.RenderPlanTerminalWithOptions(plan, report.TerminalOptions{
			Color:   terminalColorEnabled(colorMode, format, outputPath, stdout),
			Verbose: verbose,
		})
	}
	if err != nil {
		fmt.Fprintf(stderr, "cannot render cleanup plan: %v\n", err)
		return exitOperationalError
	}
	if outputPath != "" && outputPath != "-" && dependencies.writeNewFile == nil {
		fmt.Fprintln(stderr, "cannot write immutable cleanup plan: new-file writer is unavailable")
		return exitOperationalError
	}
	if err := writePayload(stdout, outputPath, payload, dependencies.writeNewFile); err != nil {
		fmt.Fprintf(stderr, "cannot write immutable cleanup plan: %v\n", err)
		return exitOperationalError
	}
	if plan.Selection.Mode == domain.SelectionExplicitResourceIDs && len(plan.Excluded) > 0 {
		return exitPartial
	}
	return exitSuccess
}

func runScan(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	dependencies commandDependencies,
) int {
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		printScanUsage(stderr)
		printFlagDefaults(stderr, flags)
	}
	var projectRoots stringListFlag
	var exclusions stringListFlag
	var androidRoots stringListFlag
	var androidAVDRoots stringListFlag
	var flutterRoots stringListFlag
	var fvmRoots stringListFlag
	var gradleRoots stringListFlag
	var jdkRoots stringListFlag
	var xcodeRoots stringListFlag
	var appleDeveloperRoots stringListFlag
	var format string
	var colorMode string
	var outputPath string
	var timeout time.Duration
	var autoDetect bool
	var deepSearch bool
	var dockerInventory bool
	var verbose bool
	var oldAfterDays int
	flags.Var(&projectRoots, "root", "project search root; repeatable; auto-detected when omitted")
	flags.Var(&exclusions, "exclude", "relative path or directory-name exclusion; repeatable")
	flags.Var(&androidRoots, "android-sdk-root", "Android SDK inventory root; repeatable")
	flags.Var(&androidAVDRoots, "android-avd-root", "Android AVD inventory root; repeatable")
	flags.Var(&flutterRoots, "flutter-sdk-root", "direct Flutter SDK root; repeatable")
	flags.Var(&fvmRoots, "fvm-cache-root", "FVM cache root whose children are SDKs; repeatable")
	flags.Var(&gradleRoots, "gradle-user-home", "Gradle user-home inventory root; repeatable")
	flags.Var(&jdkRoots, "jdk-root", "JDK home or macOS JDK container root; repeatable")
	flags.Var(&xcodeRoots, "xcode-root", "Xcode application root; repeatable")
	flags.Var(&appleDeveloperRoots, "apple-developer-root", "Apple Developer data root, such as ~/Library/Developer; repeatable")
	flags.StringVar(&format, "format", "terminal", "output format: terminal or json")
	flags.StringVar(&colorMode, "color", "auto", "terminal color: auto, always, or never")
	flags.StringVar(&outputPath, "output", "", "explicit output file; '-' means stdout")
	flags.DurationVar(&timeout, "timeout", defaultScanTimeout, "maximum total scan duration")
	flags.BoolVar(&autoDetect, "auto-detect", true, "detect omitted project and inventory roots without executing tools")
	flags.BoolVar(&deepSearch, "deep-search", true, "run a bounded search of conventional development locations")
	flags.BoolVar(&dockerInventory, "docker-inventory", true, "observe local Docker images, containers, builders and BuildKit cache when available")
	flags.BoolVar(&verbose, "verbose", false, "show roots, every requirement, resource, and diagnostic")
	flags.IntVar(&oldAfterDays, "old-after-days", classification.DefaultOldAfterDays, "classify trusted last-use observations as old after this many days")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitSuccess
		}
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "scan accepts flags only")
		return exitUsage
	}
	if timeout <= 0 {
		fmt.Fprintln(stderr, "--timeout must be greater than zero")
		return exitUsage
	}
	if oldAfterDays <= 0 {
		fmt.Fprintln(stderr, "--old-after-days must be greater than zero")
		return exitUsage
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format != "terminal" && format != "json" {
		fmt.Fprintln(stderr, "--format must be terminal or json")
		return exitUsage
	}
	colorMode, err := normalizeColorMode(colorMode)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	preScanDiagnostics := []domain.Diagnostic{}
	heuristicInventoryFamilies := []string{}
	projectDiscoveryHeuristic := false
	if autoDetect && dependencies.detect != nil {
		needFlutterFamily := len(flutterRoots)+len(fvmRoots) == 0
		needAppleFamily := len(xcodeRoots)+len(appleDeveloperRoots) == 0
		detected := dependencies.detect(ctx, autodetect.Config{
			NeedProjectRoots: len(projectRoots) == 0,
			NeedAndroid:      len(androidRoots) == 0,
			NeedAndroidAVD:   len(androidAVDRoots) == 0,
			NeedFlutter:      needFlutterFamily,
			NeedFVM:          needFlutterFamily,
			NeedGradle:       len(gradleRoots) == 0,
			NeedJDK:          len(jdkRoots) == 0,
			NeedXcode:        needAppleFamily,
			DeepSearch:       deepSearch,
		})
		preScanDiagnostics = append(preScanDiagnostics, detected.Diagnostics...)
		if len(projectRoots) == 0 {
			projectRoots = append(projectRoots, detected.Roots.ProjectRoots...)
			projectDiscoveryHeuristic = detected.ProjectDiscoveryHeuristic
		}
		if len(androidRoots) == 0 {
			androidRoots = append(androidRoots, detected.Roots.AndroidSDKRoots...)
		}
		if len(androidAVDRoots) == 0 {
			androidAVDRoots = append(androidAVDRoots, detected.Roots.AndroidAVDRoots...)
		}
		if needFlutterFamily {
			flutterRoots = append(flutterRoots, detected.Roots.FlutterSDKRoots...)
			fvmRoots = append(fvmRoots, detected.Roots.FVMCacheRoots...)
		}
		if len(gradleRoots) == 0 {
			gradleRoots = append(gradleRoots, detected.Roots.GradleUserHomeRoots...)
		}
		if len(jdkRoots) == 0 {
			jdkRoots = append(jdkRoots, detected.Roots.JDKRoots...)
		}
		if needAppleFamily {
			xcodeRoots = append(xcodeRoots, detected.Roots.XcodeRoots...)
			appleDeveloperRoots = append(appleDeveloperRoots, detected.Roots.AppleDeveloperRoots...)
		}
		for _, family := range detected.HeuristicInventoryFamilies {
			heuristicInventoryFamilies = append(heuristicInventoryFamilies, string(family))
		}
	}
	if len(projectRoots) == 0 {
		if autoDetect {
			fmt.Fprintln(stderr, "no project root was provided or detected; pass --root PATH")
		} else {
			fmt.Fprintln(stderr, "scan requires at least one --root when --auto-detect=false")
		}
		return exitUsage
	}

	document := dependencies.scan(ctx, application.Config{
		ProjectRoots:               []string(projectRoots),
		Exclusions:                 []string(exclusions),
		AndroidSDKRoots:            []string(androidRoots),
		AndroidAVDRoots:            []string(androidAVDRoots),
		FlutterSDKRoots:            []string(flutterRoots),
		FVMCacheRoots:              []string(fvmRoots),
		GradleUserHomeRoots:        []string(gradleRoots),
		JDKRoots:                   []string(jdkRoots),
		XcodeRoots:                 []string(xcodeRoots),
		AppleDeveloperRoots:        []string(appleDeveloperRoots),
		DockerInventory:            dockerInventory,
		OldAfterDays:               oldAfterDays,
		PreScanDiagnostics:         preScanDiagnostics,
		ProjectDiscoveryHeuristic:  projectDiscoveryHeuristic,
		HeuristicInventoryFamilies: heuristicInventoryFamilies,
	})

	var payload []byte
	if format == "json" {
		payload, err = report.RenderJSON(document)
	} else {
		payload, err = report.RenderTerminalWithOptions(document, report.TerminalOptions{
			Color:   terminalColorEnabled(colorMode, format, outputPath, stdout),
			Verbose: verbose,
		})
	}
	if err != nil {
		fmt.Fprintf(stderr, "cannot render scan report: %v\n", err)
		return exitOperationalError
	}
	if err := writePayload(stdout, outputPath, payload, dependencies.writeFile); err != nil {
		fmt.Fprintf(stderr, "cannot write scan report: %v\n", err)
		return exitOperationalError
	}
	if hasErrorDiagnostic(document.Diagnostics) {
		return exitPartial
	}
	return exitSuccess
}

func runExplain(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	dependencies commandDependencies,
) int {
	flags := flag.NewFlagSet("explain", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		printExplainUsage(stderr)
		printFlagDefaults(stderr, flags)
	}
	var reportPath string
	var outputPath string
	var colorMode string
	flags.StringVar(&reportPath, "report", "", "JSON v1 scan report to read")
	flags.StringVar(&outputPath, "output", "", "explicit output file; '-' means stdout")
	flags.StringVar(&colorMode, "color", "auto", "terminal color: auto, always, or never")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitSuccess
		}
		return exitUsage
	}
	if strings.TrimSpace(reportPath) == "" || flags.NArg() != 1 {
		fmt.Fprintln(stderr, "explain requires --report FILE and exactly one project, requirement, or resource ID")
		return exitUsage
	}
	if outputPath != "" && outputPath != "-" && sameCleanPath(reportPath, outputPath) {
		fmt.Fprintln(stderr, "--output must not overwrite the input report")
		return exitUsage
	}
	colorMode, err := normalizeColorMode(colorMode)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}

	content, err := dependencies.readFile(reportPath)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read scan report: %v\n", err)
		return exitOperationalError
	}
	document, err := report.DecodeJSON(content)
	if err != nil {
		fmt.Fprintf(stderr, "invalid scan report: %v\n", err)
		return exitOperationalError
	}
	payload, found, err := report.ExplainWithOptions(document, flags.Arg(0), report.TerminalOptions{
		Color: terminalColorEnabled(colorMode, "terminal", outputPath, stdout),
	})
	if err != nil {
		fmt.Fprintf(stderr, "cannot explain report item: %v\n", err)
		return exitOperationalError
	}
	if !found {
		fmt.Fprintf(stderr, "report item %q was not found\n", flags.Arg(0))
		return exitPartial
	}
	if err := writePayload(stdout, outputPath, payload, dependencies.writeFile); err != nil {
		fmt.Fprintf(stderr, "cannot write explanation: %v\n", err)
		return exitOperationalError
	}
	return exitSuccess
}

func writePayload(
	stdout io.Writer,
	outputPath string,
	payload []byte,
	writeFile func(string, []byte) error,
) error {
	if outputPath != "" && outputPath != "-" {
		return writeFile(outputPath, payload)
	}
	_, err := stdout.Write(payload)
	return err
}

func readReportFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxInputReportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxInputReportBytes {
		return nil, fmt.Errorf("report exceeds %d bytes", maxInputReportBytes)
	}
	return content, nil
}

func writePrivateFile(path string, content []byte) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("output path cannot be empty")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to write through an output symlink")
		}
		if !info.Mode().IsRegular() {
			return errors.New("output path is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func writeNewPrivateFile(path string, content []byte) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("output path cannot be empty")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("output already exists; immutable plans are never overwritten")
		}
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func sameCleanPath(left, right string) bool {
	leftInfo, leftStatErr := os.Stat(left)
	rightInfo, rightStatErr := os.Stat(right)
	if leftStatErr == nil && rightStatErr == nil && os.SameFile(leftInfo, rightInfo) {
		return true
	}
	leftPath, leftErr := filepath.Abs(left)
	rightPath, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftPath) == filepath.Clean(rightPath)
}

func hasErrorDiagnostic(diagnostics []domain.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == domain.SeverityError {
			return true
		}
	}
	return false
}

func normalizeColorMode(value string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "auto", "always", "never":
		return mode, nil
	default:
		return "", errors.New("--color must be auto, always, or never")
	}
}

func terminalColorEnabled(mode, format, outputPath string, output io.Writer) bool {
	if format != "terminal" {
		return false
	}
	if mode == "always" {
		return true
	}
	if mode == "never" || outputPath != "" && outputPath != "-" {
		return false
	}
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, isFile := output.(*os.File)
	if !isFile {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func printFlagDefaults(output io.Writer, flags *flag.FlagSet) {
	flags.VisitAll(func(item *flag.Flag) {
		label := "--" + item.Name + flagPlaceholder(item)
		fmt.Fprintf(output, "  %-34s %s", label, item.Usage)
		if item.DefValue != "" && item.DefValue != "false" {
			fmt.Fprintf(output, " (default: %s)", item.DefValue)
		}
		fmt.Fprintln(output)
	})
}

func flagPlaceholder(item *flag.Flag) string {
	if boolFlag, ok := item.Value.(interface{ IsBoolFlag() bool }); ok && boolFlag.IsBoolFlag() {
		return ""
	}
	switch item.Name {
	case "color":
		return " MODE"
	case "exclude":
		return " PATTERN"
	case "format":
		return " FORMAT"
	case "old-after-days":
		return " DAYS"
	case "output", "report":
		return " FILE"
	case "resource":
		return " ID"
	case "timeout":
		return " DURATION"
	default:
		return " PATH"
	}
}

func printRootUsage(output io.Writer) {
	fmt.Fprintln(output, "◆ dev-audit")
	fmt.Fprintln(output, "  Understand what occupies your development machine before reclaiming space.")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "USAGE")
	fmt.Fprintln(output, "  dev-audit <command> [options]")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "COMMANDS")
	fmt.Fprintln(output, "  scan      Discover projects, SDKs, caches, and their relationships")
	fmt.Fprintln(output, "  explain   Inspect the evidence behind one report item")
	fmt.Fprintln(output, "  plan      Build a review-only cleanup simulation")
	fmt.Fprintln(output, "  version   Print the installed version")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "WORKFLOW")
	fmt.Fprintln(output, "  dev-audit scan")
	fmt.Fprintln(output, "  dev-audit scan --format json --output audit.json")
	fmt.Fprintln(output, "  dev-audit explain --report audit.json <id>")
	fmt.Fprintln(output, "  dev-audit plan --report audit.json")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Run 'dev-audit <command> --help' for command options.")
}

func printScanUsage(output io.Writer) {
	fmt.Fprintln(output, "◆ dev-audit scan")
	fmt.Fprintln(output, "  Discover local development projects, runtimes, SDKs, and caches.")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "USAGE")
	fmt.Fprintln(output, "  dev-audit scan [options]")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "EXAMPLES")
	fmt.Fprintln(output, "  dev-audit scan")
	fmt.Fprintln(output, "  dev-audit scan --verbose")
	fmt.Fprintln(output, "  dev-audit scan --format json --output audit.json")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "OPTIONS")
}

func printExplainUsage(output io.Writer) {
	fmt.Fprintln(output, "◆ dev-audit explain")
	fmt.Fprintln(output, "  Inspect why one project, requirement, or resource received its status.")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "USAGE")
	fmt.Fprintln(output, "  dev-audit explain [options] --report FILE ID")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "EXAMPLE")
	fmt.Fprintln(output, "  dev-audit explain --report audit.json <resource-id>")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "OPTIONS")
}

func printPlanUsage(output io.Writer) {
	fmt.Fprintln(output, "◆ dev-audit plan")
	fmt.Fprintln(output, "  Build a reviewable cleanup simulation. No command is executed.")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "USAGE")
	fmt.Fprintln(output, "  dev-audit plan --report FILE [options]")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "EXAMPLES")
	fmt.Fprintln(output, "  dev-audit plan --report audit.json")
	fmt.Fprintln(output, "  dev-audit plan --report audit.json --resource <id>")
	fmt.Fprintln(output, "  dev-audit plan --report audit.json --format json --output plan.json")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "OPTIONS")
}
