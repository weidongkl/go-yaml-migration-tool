package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/mod/modfile"
)

var (
	scanPath = flag.String("path", ".", "Target project path")
	dryRun   = flag.Bool("dry-run", false, "Preview changes without writing files")

	// match exact module path like gopkg.in/yaml.v2 (no extra chars)
	modPathRe = regexp.MustCompile(`^gopkg\.in/yaml\.v([234])$`)

	totalFiles   uint64
	changedFiles uint64
)

func main() {
	flag.Parse()

	start := time.Now()
	fmt.Println("== yaml-ast-migrator started ==")
	fmt.Printf("path: %s\n", *scanPath)
	fmt.Printf("dry-run: %v\n\n", *dryRun)

	goVer := readGoVersion(*scanPath)
	fmt.Printf("detected go directive: %s\n", goVer)
	if compareGo(goVer, "1.22") < 0 {
		fmt.Printf("ERROR: Detected Go %s. go.yaml.in/yaml requires Go 1.22+\n", goVer)
		os.Exit(1)
	}

	if err := filepath.WalkDir(*scanPath, visit); err != nil {
		fmt.Printf("ERROR: walk failed: %v\n", err)
		os.Exit(1)
	}

	// update go.mod after walking files (we may have the version info from go.mod already)
	goModPath := filepath.Join(*scanPath, "go.mod")
	if fileExists(goModPath) {
		if err := processGoMod(goModPath); err != nil {
			fmt.Printf("ERROR updating go.mod: %v\n", err)
			os.Exit(1)
		}
	}

	if !*dryRun {
		fmt.Println("\n== running: go mod tidy ==")
		if err := runGoModTidy(*scanPath); err != nil {
			fmt.Printf("go mod tidy failed: %v\n", err)
		} else {
			fmt.Println("go mod tidy finished")
		}
	} else {
		fmt.Println("\n[DRY-RUN] skipping go mod tidy")
	}

	fmt.Printf("\nscanned files: %d\nchanged files: %d\ncompleted in %s\n", totalFiles, changedFiles, time.Since(start))
}

// visit handles files during filepath.WalkDir
func visit(path string, d fs.DirEntry, err error) error {
	if err != nil {
		return err
	}
	// skip vendor and .git
	if d.IsDir() && (d.Name() == "vendor" || d.Name() == ".git") {
		return filepath.SkipDir
	}
	if d.IsDir() {
		return nil
	}

	// Only process .go files here; go.mod handled separately
	if !strings.HasSuffix(path, ".go") {
		return nil
	}

	atomic.AddUint64(&totalFiles, 1)
	fmt.Printf("[SCAN] %s\n", path)

	changed, err := processGoFile(path)
	if err != nil {
		return fmt.Errorf("process %s: %w", path, err)
	}
	if changed {
		atomic.AddUint64(&changedFiles, 1)
		fmt.Printf("[UPDATED] %s\n", path)
	}
	return nil
}

// processGoFile parses a .go file, updates import specs if needed, and writes the file back.
// Returns (changed, error).
func processGoFile(path string) (bool, error) {
	fset := token.NewFileSet()
	parsedFile, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return false, fmt.Errorf("parse file: %w", err)
	}

	changed := false

	// iterate imports
	for _, imp := range parsedFile.Imports {
		// imp.Path.Value is a quoted string literal, e.g. "\"gopkg.in/yaml.v3\""
		raw, err := strconvUnquote(imp.Path.Value)
		if err != nil {
			// skip malformed
			continue
		}
		if m := modPathRe.FindStringSubmatch(raw); m != nil {
			major := m[1]
			newPath := "go.yaml.in/yaml/v" + major
			if raw != newPath {
				imp.Path.Value = strconvQuote(newPath) // set quoted value
				changed = true
			}
		}
	}

	if !changed {
		return false, nil
	}

	// write AST back to source (preserve format reasonably)
	var buf bytes.Buffer
	// Use printer config to print file; then run go/format to ensure canonical formatting
	cfg := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, fset, parsedFile); err != nil {
		return false, fmt.Errorf("printing AST: %w", err)
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// fallback: use unformatted AST output
		formatted = buf.Bytes()
	}

	if *dryRun {
		fmt.Printf("[DRY-RUN] would update %s\n", path)
		return true, nil
	}

	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return false, fmt.Errorf("write file: %w", err)
	}
	return true, nil
}

// processGoMod uses golang.org/x/mod/modfile to update require entries
func processGoMod(modPath string) error {
	data, err := os.ReadFile(modPath)
	if err != nil {
		return err
	}
	f, err := modfile.Parse(modPath, data, nil)
	if err != nil {
		return fmt.Errorf("parse go.mod: %w", err)
	}

	changed := false
	// iterate require entries and update module path if matches
	for _, r := range f.Require {
		if m := modPathRe.FindStringSubmatch(r.Mod.Path); m != nil {
			major := m[1]
			newPath := "go.yaml.in/yaml/v" + major
			if r.Mod.Path != newPath {
				fmt.Printf("[GO.MOD] require: %s %s -> %s %s\n", r.Mod.Path, r.Mod.Version, newPath, r.Mod.Version)
				r.Mod.Path = newPath
				changed = true
			}
		}
	}

	if !changed {
		return nil
	}

	if *dryRun {
		fmt.Println("[DRY-RUN] would write go.mod changes")
		return nil
	}

	// Correcting the modfile.Format usage to handle its single return value
	newBytes := modfile.Format(f.Syntax)
	if err := os.WriteFile(modPath, newBytes, 0o644); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}
	fmt.Println("[GO.MOD] updated")
	return nil
}

// runGoModTidy runs `go mod tidy` in the given directory
func runGoModTidy(dir string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// readGoVersion reads the 'go' directive from go.mod or returns "0.0" on error
func readGoVersion(root string) string {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "0.0"
	}
	lines := strings.Split(string(b), "\n")
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "go ") {
			return strings.TrimSpace(strings.TrimPrefix(ln, "go "))
		}
	}
	return "0.0"
}

// helper: strconvUnquote (like strconv.Unquote but accepts both " and `)
func strconvUnquote(s string) (string, error) {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') {
		return strconvUnquoteCompat(s)
	}
	return s, nil
}

// strconvQuote returns a quoted string using double quotes.
func strconvQuote(s string) string {
	return `"` + s + `"`
}

// fileExists utility
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

/***** small helper implementations to avoid importing strconv repeatedly *****/

func strconvUnquoteCompat(s string) (string, error) {
	// use strconv.Unquote which handles "..." and `...`
	return strconv.Unquote(s)
}

func compareGo(a, b string) int {
	pa := strings.SplitN(a, ".", 3)
	pb := strings.SplitN(b, ".", 3)
	ma := 0
	mb := 0
	miA := 0
	miB := 0
	if len(pa) > 0 {
		if v, err := strconv.Atoi(pa[0]); err == nil {
			ma = v
		}
	}
	if len(pb) > 0 {
		if v, err := strconv.Atoi(pb[0]); err == nil {
			mb = v
		}
	}
	if len(pa) > 1 {
		if v, err := strconv.Atoi(pa[1]); err == nil {
			miA = v
		}
	}
	if len(pb) > 1 {
		if v, err := strconv.Atoi(pb[1]); err == nil {
			miB = v
		}
	}
	if ma != mb {
		if ma < mb {
			return -1
		}
		return 1
	}
	if miA != miB {
		if miA < miB {
			return -1
		}
		return 1
	}
	return 0
}
