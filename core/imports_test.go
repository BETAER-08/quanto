package core

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var allowedCore = map[string][]string{
	"source":  {},
	"expr":    {},
	"model":   {"source", "expr"},
	"matrix":  {"source", "expr"},
	"graph":   {"model"},
	"semdiff": {"source", "expr", "model", "matrix", "graph"},
	"report":  {"semdiff", "source"},
}

var allowedExternal = map[string][]string{
	"source": {"gopkg.in/yaml.v3"},
}

var forbiddenStd = []string{"net/http", "database/sql"}

func modulePath(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	t.Fatal("module directive not found")
	return ""
}

func isStd(path string) bool {
	first := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		first = path[:i]
	}
	return !strings.Contains(first, ".")
}

func checkImport(module, pkg, imp string) error {
	allowed, known := allowedCore[pkg]
	if !known {
		return fmt.Errorf("core/%s is not listed in the dependency table", pkg)
	}
	if isStd(imp) {
		for _, f := range forbiddenStd {
			if imp == f || strings.HasPrefix(imp, f+"/") {
				return fmt.Errorf("core/%s imports forbidden %s", pkg, imp)
			}
		}
		return nil
	}
	if strings.HasPrefix(imp, module+"/core/") {
		target := strings.TrimPrefix(imp, module+"/core/")
		for _, a := range allowed {
			if target == a {
				return nil
			}
		}
		return fmt.Errorf("core/%s must not import core/%s", pkg, target)
	}
	for _, ext := range allowedExternal[pkg] {
		if imp == ext {
			return nil
		}
	}
	return fmt.Errorf("core/%s must not import %s", pkg, imp)
}

func TestCheckImport(t *testing.T) {
	const mod = "example.com/quanto"
	cases := []struct {
		pkg, imp string
		ok       bool
	}{
		{"source", "strings", true},
		{"source", "gopkg.in/yaml.v3", true},
		{"expr", "gopkg.in/yaml.v3", false},
		{"model", mod + "/core/source", true},
		{"model", mod + "/core/matrix", false},
		{"graph", mod + "/core/model", true},
		{"graph", mod + "/core/source", false},
		{"semdiff", mod + "/core/graph", true},
		{"report", mod + "/core/semdiff", true},
		{"report", mod + "/core/model", false},
		{"source", "net/http", false},
		{"source", "net/http/httptest", false},
		{"model", "database/sql", false},
		{"report", mod + "/internal/github", false},
		{"semdiff", "github.com/jackc/pgx/v5", false},
		{"report", "github.com/prometheus/client_golang/prometheus", false},
		{"unknown", "strings", false},
	}
	for _, tc := range cases {
		err := checkImport(mod, tc.pkg, tc.imp)
		if (err == nil) != tc.ok {
			t.Errorf("checkImport(%s, %s) = %v, want ok=%v", tc.pkg, tc.imp, err, tc.ok)
		}
	}
}

func TestCoreImports(t *testing.T) {
	module := modulePath(t)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read core: %v", err)
	}
	var pkgs []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "testdata" {
			pkgs = append(pkgs, e.Name())
		}
	}
	sort.Strings(pkgs)
	fset := token.NewFileSet()
	checked := 0
	for _, pkg := range pkgs {
		files, err := filepath.Glob(filepath.Join(pkg, "*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", pkg, err)
		}
		sort.Strings(files)
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			checked++
			for _, spec := range f.Imports {
				imp, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatalf("%s: bad import %s", file, spec.Path.Value)
				}
				if err := checkImport(module, pkg, imp); err != nil {
					t.Errorf("%s: %v", file, err)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no core source files found")
	}
}
