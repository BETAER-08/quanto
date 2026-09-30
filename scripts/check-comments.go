//go:build ignore

package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var allowed = []string{"//go:build", "//go:embed", "//go:generate"}

func permitted(text string) bool {
	for _, prefix := range allowed {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func main() {
	var files []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != "." && (name == "testdata" || name == ".git" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk: %v\n", err)
		os.Exit(1)
	}
	sort.Strings(files)
	found := false
	fset := token.NewFileSet()
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: parse: %v\n", path, err)
			found = true
			continue
		}
		for _, group := range f.Comments {
			for _, c := range group.List {
				if permitted(c.Text) {
					continue
				}
				fmt.Printf("%s:%d: comment\n", path, fset.Position(c.Pos()).Line)
				found = true
			}
		}
	}
	if found {
		os.Exit(1)
	}
}
