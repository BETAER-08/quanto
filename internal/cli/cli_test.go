package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var goldenRoot = filepath.Join("..", "..", "testdata", "golden", "semdiff")

func run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func goldenFile(name, file string) string {
	return filepath.Join(goldenRoot, name, file)
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const sampleWorkflow = `name: CI
on:
  push:
    branches: [main]
  schedule:
    - cron: '0 * * * *'
  workflow_dispatch:
    inputs:
      level: {}
permissions:
  contents: read
jobs:
  build:
    runs-on: [ubuntu-latest, self-hosted]
    strategy:
      matrix:
        go: ['1.22', '1.23']
        os: [linux, windows, mac]
    steps:
      - uses: actions/checkout@v4
      - uses: peter-evans/create-pull-request@0123456789abcdef0123456789abcdef01234567
      - uses: ./local/action
      - uses: docker://alpine:3
      - uses: broken/action
  test:
    needs: build
    runs-on: ${{ matrix.os }}
    permissions: write-all
    strategy:
      matrix:
        os: ${{ fromJSON(needs.build.outputs.list) }}
    steps:
      - run: go test ./...
  call:
    needs: [build, test]
    uses: org/repo/.github/workflows/reuse.yml@v1
    secrets: inherit
`

func TestUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", nil},
		{"unknown command", []string{"serve"}},
		{"version extra", []string{"version", "x"}},
		{"diff one arg", []string{"diff", "a"}},
		{"diff three args", []string{"diff", "a", "b", "c"}},
		{"diff bad format", []string{"diff", "/dev/null", "/dev/null", "--format", "html"}},
		{"diff unknown flag", []string{"diff", "--color", "a", "b"}},
		{"inspect no args", []string{"inspect"}},
		{"inspect bad format", []string{"inspect", "x.yml", "--format", "markdown"}},
	}
	for _, tt := range tests {
		code, stdout, stderr := run(tt.args...)
		if code != 2 {
			t.Errorf("%s: exit = %d, want 2", tt.name, code)
		}
		if stdout != "" {
			t.Errorf("%s: unexpected stdout %q", tt.name, stdout)
		}
		if !strings.Contains(stderr, "usage:") {
			t.Errorf("%s: stderr lacks usage: %q", tt.name, stderr)
		}
	}
}

func TestVersion(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "1.2.3"
	code, stdout, _ := run("version")
	if code != 0 || stdout != "1.2.3\n" {
		t.Errorf("version = %d %q", code, stdout)
	}
}

func TestDiffMatchesGoldenText(t *testing.T) {
	for _, name := range []string{"matrix-axis-added", "permissions-broadened", "action-pin-removed", "needs-cycle"} {
		want, err := os.ReadFile(goldenFile(name, "expected.txt"))
		if err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := run("diff", goldenFile(name, "before.yml"), goldenFile(name, "after.yml"), "--path", ".github/workflows/ci.yml")
		if code != 0 || stderr != "" {
			t.Fatalf("%s: exit %d stderr %q", name, code, stderr)
		}
		if stdout != string(want) {
			t.Errorf("%s: text mismatch\ngot:\n%s\nwant:\n%s", name, stdout, want)
		}
	}
}

func TestDiffMarkdown(t *testing.T) {
	name := "matrix-axis-added"
	want, err := os.ReadFile(goldenFile(name, "expected.md"))
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := run("diff", "--format", "markdown", "--path", ".github/workflows/ci.yml", goldenFile(name, "before.yml"), goldenFile(name, "after.yml"))
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	wantBody := strings.Replace(string(want), " Commit `abc1234`.", "", 1)
	if stdout != wantBody {
		t.Errorf("markdown mismatch\ngot:\n%s\nwant:\n%s", stdout, wantBody)
	}
}

type jsonReport struct {
	Schema string `json:"schema"`
	Files  []struct {
		Path     string `json:"path"`
		Status   string `json:"status"`
		Error    string `json:"error"`
		Findings []struct {
			Kind string `json:"kind"`
		} `json:"findings"`
	} `json:"files"`
}

func diffJSON(t *testing.T, args ...string) jsonReport {
	t.Helper()
	code, stdout, stderr := run(append([]string{"diff", "--format", "json"}, args...)...)
	if code != 0 {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	var rep jsonReport
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout)
	}
	if rep.Schema != "quanto.diff/v1" || len(rep.Files) != 1 {
		t.Fatalf("unexpected report %+v", rep)
	}
	return rep
}

func kinds(rep jsonReport) []string {
	var out []string
	for _, f := range rep.Files[0].Findings {
		out = append(out, f.Kind)
	}
	return out
}

func TestDiffAbsentSides(t *testing.T) {
	wf := writeTemp(t, "ci.yml", sampleWorkflow)
	empty := writeTemp(t, "empty.yml", "")
	tests := []struct {
		name   string
		args   []string
		status string
		kind   string
		path   string
	}{
		{"dev null before", []string{"/dev/null", wf}, "added", "workflow.added", wf},
		{"empty before", []string{empty, wf}, "added", "workflow.added", wf},
		{"dev null after", []string{wf, "/dev/null"}, "removed", "workflow.removed", wf},
		{"empty after", []string{wf, empty}, "removed", "workflow.removed", wf},
		{"both absent", []string{"/dev/null", "/dev/null"}, "unanalyzable", "workflow.unanalyzable", "/dev/null"},
		{"explicit path", []string{"/dev/null", wf, "--path", ".github/workflows/x.yml"}, "added", "workflow.added", ".github/workflows/x.yml"},
	}
	for _, tt := range tests {
		rep := diffJSON(t, tt.args...)
		f := rep.Files[0]
		if f.Status != tt.status || f.Path != tt.path {
			t.Errorf("%s: status %q path %q", tt.name, f.Status, f.Path)
		}
		if k := kinds(rep); len(k) != 1 || k[0] != tt.kind {
			t.Errorf("%s: kinds %v", tt.name, k)
		}
	}
}

func TestDiffIdenticalHasNoFindings(t *testing.T) {
	wf := writeTemp(t, "ci.yml", sampleWorkflow)
	rep := diffJSON(t, wf, wf)
	if rep.Files[0].Status != "modified" || len(rep.Files[0].Findings) != 0 {
		t.Errorf("identical diff = %+v", rep.Files[0])
	}
}

func TestDiffUnparseableExitsZero(t *testing.T) {
	code, stdout, _ := run("diff", goldenFile("head-unparseable", "before.yml"), goldenFile("head-unparseable", "after.yml"))
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "(unanalyzable)") || !strings.Contains(stdout, "Could not analyze:") {
		t.Errorf("unexpected output:\n%s", stdout)
	}
	notWorkflow := writeTemp(t, "list.yml", "- a\n- b\n")
	rep := diffJSON(t, notWorkflow, notWorkflow)
	if rep.Files[0].Status != "unanalyzable" || rep.Files[0].Error == "" {
		t.Errorf("non-mapping workflow = %+v", rep.Files[0])
	}
}

func TestDiffReadError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yml")
	code, stdout, stderr := run("diff", missing, "/dev/null")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "missing.yml") {
		t.Errorf("read error = %d %q %q", code, stdout, stderr)
	}
	code, _, _ = run("diff", "/dev/null", t.TempDir())
	if code != 1 {
		t.Errorf("directory input exit = %d, want 1", code)
	}
}

func TestDiffDeterministic(t *testing.T) {
	args := []string{"diff", "--format", "json", goldenFile("secrets-new-and-inherit", "before.yml"), goldenFile("secrets-new-and-inherit", "after.yml")}
	_, a, _ := run(args...)
	_, b, _ := run(args...)
	if a != b {
		t.Errorf("non-deterministic output")
	}
}

func TestInspectText(t *testing.T) {
	wf := writeTemp(t, "ci.yml", sampleWorkflow)
	code, stdout, stderr := run("inspect", wf)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	want := []string{
		"Name: CI\n",
		"  push (branches: main)\n",
		"  schedule (cron: '0 * * * *')\n",
		"  workflow_dispatch (inputs: level)\n",
		"  workflow: contents: read\n",
		"  job `test`: write-all\n",
		"Jobs per run: ?\n",
		"Longest needs chain: 3\n",
		"  build: instances: 6, runs-on: self-hosted, ubuntu-latest\n",
		"  test: instances: ?, runs-on: ${{ matrix.os }}, needs: build\n",
		"  call: instances: 1, runs-on: (none), needs: build, test, uses: org/repo/.github/workflows/reuse.yml@v1\n",
		"  actions/checkout@v4 (first-party, mutable)\n",
		"  peter-evans/create-pull-request@0123456789abcdef0123456789abcdef01234567 (third-party, sha)\n",
		"  ./local/action (local)\n",
		"  docker://alpine:3 (docker)\n",
		"  org/repo/.github/workflows/reuse.yml@v1 (third-party, mutable)\n",
		"MODEL-ACTION-NO-REF",
	}
	for _, w := range want {
		if !strings.Contains(stdout, w) {
			t.Errorf("inspect output lacks %q\n%s", w, stdout)
		}
	}
	_, again, _ := run("inspect", wf)
	if again != stdout {
		t.Errorf("non-deterministic inspect output")
	}
}

const hostileWorkflow = `name: "\e[31mred\e[0m"
on:
  "\e]0;title\apush": {}
jobs:
  "\e]0;pwned\aci":
    runs-on: "safe\rEVIL"
    needs: "\e[2Jclear"
    steps:
      - uses: "evil/act\u009b31m@v1"
      - run: echo
  "\e[2Jclear":
    runs-on: x
    steps:
      - run: echo
`

func TestInspectStripsTerminalControls(t *testing.T) {
	long := strings.Repeat("j", 100)
	wf := writeTemp(t, "ci.yml", hostileWorkflow+"  "+long+":\n    runs-on: x\n    steps:\n      - run: echo\n")
	code, stdout, stderr := run("inspect", wf)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	for _, r := range stdout {
		if r == '\n' {
			continue
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			t.Fatalf("control rune %U in inspect output %q", r, stdout)
		}
	}
	want := []string{
		"Name:  [31mred [0m\n",
		"   ]0;title push\n",
		"   ]0;pwned ci: instances: 1, runs-on: safe EVIL, needs:  [2Jclear\n",
		"  evil/act 31m@v1 (third-party, mutable)\n",
		"  " + strings.Repeat("j", 79) + "…: instances: 1",
	}
	for _, w := range want {
		if !strings.Contains(stdout, w) {
			t.Errorf("inspect output lacks %q\n%s", w, stdout)
		}
	}
}

func TestInspectJSON(t *testing.T) {
	wf := writeTemp(t, "ci.yml", sampleWorkflow)
	code, stdout, _ := run("inspect", "--format", "json", wf)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var rep inspectReport
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.JobsPerRun != "?" || rep.Depth != 3 || len(rep.Jobs) != 3 || len(rep.Triggers) != 3 {
		t.Errorf("unexpected report %+v", rep)
	}
	if rep.Jobs[0].Instances != "6" || rep.Jobs[1].Instances != "?" {
		t.Errorf("instances = %+v", rep.Jobs)
	}
	if len(rep.Diagnostics) != 1 || rep.Diagnostics[0].Code != "MODEL-ACTION-NO-REF" {
		t.Errorf("diagnostics = %+v", rep.Diagnostics)
	}
	if !strings.HasSuffix(stdout, "}\n") {
		t.Errorf("json must end with a newline")
	}
}

func TestInspectErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yml")
	if code, _, stderr := run("inspect", missing); code != 1 || stderr == "" {
		t.Errorf("missing file exit = %d", code)
	}
	bad := writeTemp(t, "bad.yml", "on: [push\njobs: {}\n")
	if code, _, stderr := run("inspect", bad); code != 1 || !strings.Contains(stderr, "bad.yml") {
		t.Errorf("syntax error exit = %d stderr %q", code, stderr)
	}
	empty := writeTemp(t, "empty.yml", "")
	if code, _, _ := run("inspect", empty); code != 1 {
		t.Errorf("empty file exit = %d", code)
	}
}
