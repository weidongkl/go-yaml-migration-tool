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
)

var (
	scanPath = flag.String("path", ".", "Target project path")
	dryRun   = flag.Bool("dry-run", false, "Preview changes without writing files")

	re = regexp.MustCompile(`gopkg\.in/yaml\.v([234])`)
)

func main() {
	flag.Parse()

	if err := process(*scanPath); err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}

	if !*dryRun {
		_ = runGoModTidy(*scanPath)
	}
}

func process(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip vendor/.git directories
		if d.IsDir() && (d.Name() == "vendor" || d.Name() == ".git") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}

		if !strings.HasSuffix(path, ".go") && filepath.Base(path) != "go.mod" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		content := string(data)
		if !re.MatchString(content) {
			return nil
		}

		updated := re.ReplaceAllString(content, `go.yaml.in/yaml/v$1`)

		if *dryRun {
			fmt.Printf("[DRY-RUN] %s would be updated\n", path)
			return nil
		}

		return os.WriteFile(path, []byte(updated), 0o644)
	})
}

func runGoModTidy(dir string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
