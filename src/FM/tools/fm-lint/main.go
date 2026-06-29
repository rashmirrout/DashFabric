// fm-lint is a static analysis tool that enforces FM-specific lint
// rules. Currently implements one rule:
//
//	NO_REGISTRY_BYPASS — direct calls to registry.New outside
//	pkg/registry/... are forbidden; use the typed constructors.
//
// Usage:
//
//	fm-lint [flags] <dir> [<dir>...]
//	fm-lint ./...           # lint all packages under cwd
//
// Exit codes:
//
//	0 — no violations
//	1 — one or more violations found
//	2 — tool error (bad arguments, parse failure)
//
// Integrate into CI:
//
//	go build -o bin/fm-lint ./tools/fm-lint
//	./bin/fm-lint ./...
package main

import (
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/dashfabric/fm/tools/fm-lint/noregistrybypass"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: fm-lint <dir> [<dir>...]\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		args = []string{"."}
	}

	dirs, err := expandArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fm-lint: %v\n", err)
		os.Exit(2)
	}

	fset := token.NewFileSet()
	found := 0
	for _, dir := range dirs {
		pkgs, err := parser.ParseDir(fset, dir, goFileFilter, parser.AllErrors)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fm-lint: parse %s: %v\n", dir, err)
			os.Exit(2)
		}
		for pkgPath, pkg := range pkgs {
			_ = pkgPath
			// Derive a package import path from the directory.
			importPath := dirToImportPath(dir)
			for _, f := range pkg.Files {
				for _, v := range noregistrybypass.Check(fset, f, importPath) {
					fmt.Printf("%s: %s\n", v.Pos, v.Message)
					found++
				}
			}
		}
	}
	if found > 0 {
		os.Exit(1)
	}
}

// goFileFilter excludes test files from linting.
func goFileFilter(fi os.FileInfo) bool {
	return !strings.HasSuffix(fi.Name(), "_test.go")
}

// expandArgs resolves the argument list into concrete directories.
// "./..." expands to all subdirectories containing .go files under cwd.
// Other arguments are used as-is.
func expandArgs(args []string) ([]string, error) {
	var dirs []string
	for _, arg := range args {
		if arg == "./..." || strings.HasSuffix(arg, "/...") {
			root := strings.TrimSuffix(arg, "/...")
			root = strings.TrimPrefix(root, "./")
			if root == "" || root == "." {
				wd, err := os.Getwd()
				if err != nil {
					return nil, err
				}
				root = wd
			}
			walked, err := walkGoPackageDirs(root)
			if err != nil {
				return nil, err
			}
			dirs = append(dirs, walked...)
		} else {
			dirs = append(dirs, arg)
		}
	}
	return dirs, nil
}

// walkGoPackageDirs returns all directories under root that contain at
// least one non-test .go file.
func walkGoPackageDirs(root string) ([]string, error) {
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") && !strings.HasSuffix(d.Name(), "_test.go") {
			dir := filepath.Dir(path)
			seen[dir] = true
		}
		return nil
	})
	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	return dirs, err
}

// dirToImportPath converts a file system directory (possibly relative)
// to a Go import path by finding the module root and computing the
// relative path from it.
//
// Falls back to the raw directory if no go.mod is found.
func dirToImportPath(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	modRoot, modPath := findModuleRoot(abs)
	if modRoot == "" {
		return filepath.ToSlash(dir)
	}
	rel, err := filepath.Rel(modRoot, abs)
	if err != nil {
		return modPath
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return modPath
	}
	return modPath + "/" + rel
}

// findModuleRoot walks up from dir looking for go.mod and returns the
// module root and module path declared in it.
func findModuleRoot(dir string) (root, modPath string) {
	for d := dir; ; d = filepath.Dir(d) {
		gomod := filepath.Join(d, "go.mod")
		data, err := os.ReadFile(gomod)
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module ") {
					modPath = strings.TrimSpace(strings.TrimPrefix(line, "module "))
					return d, modPath
				}
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
	}
	return "", ""
}
