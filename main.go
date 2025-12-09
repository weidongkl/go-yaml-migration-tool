package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

var (
	scanPath = flag.String("path", ".", "Target project path")
	dryRun   = flag.String("dry-run", "false", "Preview changes without writing files (true/false)")

	re = regexp.MustCompile(`gopkg\.in/yaml\.v([234])`)

	totalFiles   uint64
	matchedFiles uint64
	updatedFiles uint64
)

func main() {
	flag.Parse()

	start := time.Now()
	fmt.Printf("== YAML Migrator Started ==\n")
	fmt.Printf("Scan path: %s\n", *scanPath)
	fmt.Printf("Dry run: %s\n\n", *dryRun)

	if err := process(*scanPath); err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}

	fmt.Printf("\n== Scan Summary ==\n")
	fmt.Printf("Total files scanned : %d\n", totalFiles)
	fmt.Printf("Files matched       : %d\n", matchedFiles)
	if *dryRun != "true" {
		fmt.Printf("Files updated       : %d\n", updatedFiles)
	}

	if *dryRun != "true" {
		fmt.Printf("\n== Running: go mod tidy ==\n")
		_ = runGoModTidy(*scanPath)
		fmt.Printf("== go mod tidy finished ==\n")
	}

	fmt.Printf("\nCompleted in %s\n", time.Since(start))
}

func process(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Printf("[ERROR] Access failed: %s : %v\n", path, err)
			return err
		}

		// Skip vendor/.git directories
		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == ".git" {
				fmt.Printf("[SKIP DIR] %s\n", path)
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") && filepath.Base(path) != "go.mod" {
			return nil
		}

		atomic.AddUint64(&totalFiles, 1)
		fmt.Printf("[SCAN] %s\n", path)

		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("[ERROR] Read failed: %s : %v\n", path, err)
			return err
		}

		content := string(data)
		if !re.MatchString(content) {
			return nil
		}

		atomic.AddUint64(&matchedFiles, 1)
		fmt.Printf("[MATCH] %s\n", path)

		updated := re.ReplaceAllString(content, `go.yaml.in/yaml/v$1`)

		if *dryRun == "true" {
			fmt.Printf("[DRY-RUN] %s would be updated\n", path)
			return nil
		}

		err = os.WriteFile(path, []byte(updated), 0o644)
		if err != nil {
			fmt.Printf("[ERROR] Write failed: %s : %v\n", path, err)
			return err
		}

		atomic.AddUint64(&updatedFiles, 1)
		fmt.Printf("[UPDATED] %s\n", path)
		return nil
	})
}

func runGoModTidy(dir string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
