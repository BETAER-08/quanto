package report

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/BETAER-08/quanto/core/semdiff"
	"github.com/BETAER-08/quanto/core/source"
)

const specExample = "<!-- quanto:summary -->\n" +
	"## quanto\n" +
	"\n" +
	"Execution changes in 1 workflow file.\n" +
	"\n" +
	"### `.github/workflows/ci.yml`\n" +
	"\n" +
	"| Metric | Before | After |\n" +
	"|---|---:|---:|\n" +
	"| Jobs per run | 7 | 25 |\n" +
	"| Longest `needs` chain | 3 | 3 |\n" +
	"| Max concurrent jobs | 2 | 4 |\n" +
	"| Est. runner minutes per run | 43 | 172 |\n" +
	"\n" +
	"- Job `test` matrix: 6 → 24 jobs\n" +
	"- `contents` permission (workflow): `read` → `write`\n" +
	"- New third-party action: `peter-evans/create-pull-request@v6` (mutable ref)\n" +
	"\n" +
	"---\n" +
	"<sub>Static analysis of workflow files only. No code from this pull request was executed. Commit `abc1234`.</sub>\n"

func pos(line, col, endLine, endCol int) source.Position {
	return source.Position{File: ".github/workflows/ci.yml", Line: line, Column: col, EndLine: endLine, EndColumn: endCol}
}

func specDiff() *semdiff.FileDiff {
	return &semdiff.FileDiff{
		Path:   ".github/workflows/ci.yml",
		Status: semdiff.StatusModified,
		Before: semdiff.Metrics{JobsPerRun: "7", Depth: 3, Width: 2, RunnerMinutes: "43"},
		After:  semdiff.Metrics{JobsPerRun: "25", Depth: 3, Width: 4, RunnerMinutes: "172"},
		Findings: []semdiff.Finding{
			{Kind: "matrix.count_changed", Significance: semdiff.High, Subject: "test", Before: "6", After: "24", Pos: pos(13, 9, 15, 27)},
			{Kind: "permissions.broadened", Significance: semdiff.High, Subject: "workflow", Before: "read", After: "write", Detail: "contents", Pos: pos(5, 13, 5, 17)},
			{Kind: "action.third_party_added", Significance: semdiff.High, Subject: "peter-evans/create-pull-request", After: "v6", Detail: " (mutable ref)", Pos: pos(30, 15, 30, 48)},
			{Kind: "action.added", Significance: semdiff.Low, Subject: "actions/cache", After: "v4", Pos: pos(28, 15, 28, 30)},
		},
	}
}

func TestMarkdownSpecExample(t *testing.T) {
	got := Markdown([]*semdiff.FileDiff{specDiff()}, Meta{HeadSHA: "abc1234567890"})
	if got != specExample {
		t.Errorf("markdown mismatch\ngot:\n%s\nwant:\n%s", got, specExample)
	}
}

func TestMessageAllKinds(t *testing.T) {
	tests := []struct {
		f    semdiff.Finding
		want string
	}{
		{semdiff.Finding{Kind: "workflow.added"}, "Workflow added"},
		{semdiff.Finding{Kind: "workflow.removed"}, "Workflow removed"},
		{semdiff.Finding{Kind: "workflow.renamed", Before: "a.yml", After: "b.yml"}, "Workflow renamed from `a.yml`"},
		{semdiff.Finding{Kind: "workflow.unanalyzable", Detail: "bad yaml"}, "Could not analyze: `bad yaml`"},
		{semdiff.Finding{Kind: "trigger.added", Subject: "push"}, "Trigger added: `push`"},
		{semdiff.Finding{Kind: "trigger.removed", Subject: "push"}, "Trigger removed: `push`"},
		{semdiff.Finding{Kind: "trigger.filter_changed", Subject: "push", Detail: "branches", Before: "main", After: "dev, main"}, "`push` `branches` filter: `main` → `dev, main`"},
		{semdiff.Finding{Kind: "trigger.schedule_changed", Before: "(none)", After: "'0 * * * *' (24 runs/day)"}, "Schedule: `(none)` → `'0 * * * *' (24 runs/day)`"},
		{semdiff.Finding{Kind: "trigger.pull_request_target_added", Subject: "pull_request_target"}, "Trigger added: `pull_request_target` (runs with base repository permissions and secrets)"},
		{semdiff.Finding{Kind: "job.added", Subject: "lint"}, "Job added: `lint`"},
		{semdiff.Finding{Kind: "job.removed", Subject: "lint"}, "Job removed: `lint`"},
		{semdiff.Finding{Kind: "job.renamed", Subject: "check", Before: "lint", After: "check"}, "Job `lint` renamed to `check`"},
		{semdiff.Finding{Kind: "job.runner_changed", Subject: "build", Before: "ubuntu-latest", After: "macos-latest, ubuntu-latest"}, "Job `build` runs-on: `ubuntu-latest` → `macos-latest, ubuntu-latest`"},
		{semdiff.Finding{Kind: "job.timeout_changed", Subject: "build", Before: "10", After: "30"}, "Job `build` timeout-minutes: `10` → `30`"},
		{semdiff.Finding{Kind: "job.concurrency_changed", Subject: "build", Before: "(none)", After: "ci"}, "Job `build` concurrency: `(none)` → `ci`"},
		{semdiff.Finding{Kind: "matrix.count_changed", Subject: "test", Before: "6", After: "24"}, "Job `test` matrix: 6 → 24 jobs"},
		{semdiff.Finding{Kind: "matrix.dynamic", Subject: "test"}, "Job `test` matrix is computed at runtime; job count unknown"},
		{semdiff.Finding{Kind: "matrix.over_limit", Subject: "test", After: "300"}, "Job `test` matrix expands to 300 jobs (GitHub limit: 256)"},
		{semdiff.Finding{Kind: "graph.depth_changed", Before: "2", After: "3"}, "Longest `needs` chain: 2 → 3 jobs"},
		{semdiff.Finding{Kind: "graph.width_changed", Before: "2", After: "4"}, "Max concurrent jobs: 2 → 4"},
		{semdiff.Finding{Kind: "graph.cycle", Subject: "a", Detail: "a → b → a"}, "`needs` cycle: `a → b → a`"},
		{semdiff.Finding{Kind: "graph.unresolved", Subject: "deploy", After: "bild"}, "Job `deploy` needs unknown job `bild`"},
		{semdiff.Finding{Kind: "permissions.broadened", Subject: "job `build`", Before: "read", After: "write", Detail: "contents"}, "`contents` permission (job `build`): `read` → `write`"},
		{semdiff.Finding{Kind: "permissions.narrowed", Subject: "workflow", Before: "write", After: "none", Detail: "issues"}, "`issues` permission (workflow): `write` → `none`"},
		{semdiff.Finding{Kind: "permissions.write_all", Subject: "workflow"}, "`permissions: write-all` set on workflow"},
		{semdiff.Finding{Kind: "permissions.removed", Subject: "job `build`", Detail: "repository-default"}, "`permissions` removed from job `build`; repository default applies"},
		{semdiff.Finding{Kind: "permissions.removed", Subject: "workflow"}, "`permissions` removed from workflow"},
		{semdiff.Finding{Kind: "permissions.declared", Subject: "workflow"}, "`permissions` declared on workflow"},
		{semdiff.Finding{Kind: "secrets.added", Subject: "NPM_TOKEN"}, "New secret referenced: `NPM_TOKEN`"},
		{semdiff.Finding{Kind: "secrets.inherit_added", Subject: "deploy", After: "org/repo/.github/workflows/d.yml@v1"}, "Job `deploy` passes all secrets to `org/repo/.github/workflows/d.yml@v1` (`secrets: inherit`)"},
		{semdiff.Finding{Kind: "action.added", Subject: "actions/cache", After: "v4"}, "Action added: `actions/cache@v4`"},
		{semdiff.Finding{Kind: "action.removed", Subject: "actions/cache", Before: "v4"}, "Action removed: `actions/cache`"},
		{semdiff.Finding{Kind: "action.third_party_added", Subject: "a/b", After: "v1", Detail: " (mutable ref)"}, "New third-party action: `a/b@v1` (mutable ref)"},
		{semdiff.Finding{Kind: "action.third_party_added", Subject: "a/b", After: "0123456789012345678901234567890123456789"}, "New third-party action: `a/b@0123456789012345678901234567890123456789`"},
		{semdiff.Finding{Kind: "action.ref_changed", Subject: "actions/checkout", Before: "v4", After: "v5"}, "`actions/checkout`: `v4` → `v5`"},
		{semdiff.Finding{Kind: "action.pin_removed", Subject: "a/b", Before: "0123456789012345678901234567890123456789", After: "v1"}, "`a/b` changed from commit SHA to mutable ref `v1`"},
		{semdiff.Finding{Kind: "estimate.changed", Before: "43", After: "172", Detail: "12"}, "Estimated runner minutes per run: 43 → 172 (12 historical runs per job)"},
	}
	covered := make(map[string]bool)
	for _, tt := range tests {
		covered[tt.f.Kind] = true
		if got := Message(tt.f); got != tt.want {
			t.Errorf("Message(%s) = %q, want %q", tt.f.Kind, got, tt.want)
		}
	}
	for _, k := range semdiff.Kinds() {
		if !covered[k] {
			t.Errorf("kind %s has no message test", k)
		}
	}
}

func TestPublishable(t *testing.T) {
	low := &semdiff.FileDiff{Path: "a.yml", Findings: []semdiff.Finding{{Kind: "action.added", Significance: semdiff.Low}}}
	normal := &semdiff.FileDiff{Path: "b.yml", Findings: []semdiff.Finding{{Kind: "job.added", Significance: semdiff.Normal}}}
	tests := []struct {
		name  string
		diffs []*semdiff.FileDiff
		want  bool
	}{
		{"empty", nil, false},
		{"nil entry", []*semdiff.FileDiff{nil}, false},
		{"low only", []*semdiff.FileDiff{low}, false},
		{"normal", []*semdiff.FileDiff{low, normal}, true},
		{"high", []*semdiff.FileDiff{specDiff()}, true},
	}
	for _, tt := range tests {
		if got := Publishable(tt.diffs); got != tt.want {
			t.Errorf("%s: Publishable = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestMarkdownExcludesLow(t *testing.T) {
	got := Markdown([]*semdiff.FileDiff{specDiff()}, Meta{})
	if strings.Contains(got, "actions/cache") {
		t.Errorf("low finding in markdown:\n%s", got)
	}
	if strings.Contains(got, "Commit `") {
		t.Errorf("commit shown without head sha:\n%s", got)
	}
}

func TestMarkdownMultipleFilesSortedAndPlural(t *testing.T) {
	b := specDiff()
	b.Path = ".github/workflows/b.yml"
	a := &semdiff.FileDiff{
		Path:     ".github/workflows/a.yml",
		Status:   semdiff.StatusAdded,
		After:    semdiff.Metrics{JobsPerRun: "1", Depth: 1, Width: 1},
		Findings: []semdiff.Finding{{Kind: "workflow.added", Significance: semdiff.Normal}},
	}
	quiet := &semdiff.FileDiff{
		Path:     ".github/workflows/c.yml",
		Status:   semdiff.StatusModified,
		Before:   semdiff.Metrics{JobsPerRun: "1", Depth: 1, Width: 1},
		After:    semdiff.Metrics{JobsPerRun: "1", Depth: 1, Width: 1},
		Findings: []semdiff.Finding{{Kind: "action.added", Significance: semdiff.Low, Subject: "actions/cache", After: "v4"}},
	}
	got := Markdown([]*semdiff.FileDiff{b, quiet, a}, Meta{HeadSHA: "1234567", SkippedFiles: 3})
	if !strings.Contains(got, "Execution changes in 2 workflow files.\n") {
		t.Errorf("missing plural header:\n%s", got)
	}
	ia, ib := strings.Index(got, "a.yml"), strings.Index(got, "b.yml")
	if ia < 0 || ib < 0 || ia > ib {
		t.Errorf("files not sorted:\n%s", got)
	}
	if strings.Contains(got, "c.yml") {
		t.Errorf("file without visible changes shown:\n%s", got)
	}
	if !strings.Contains(got, "| Jobs per run | — | 1 |\n") {
		t.Errorf("absent side cell missing:\n%s", got)
	}
	if !strings.Contains(got, "\n3 additional workflow files were not analyzed.\n\n---\n") {
		t.Errorf("skipped line missing:\n%s", got)
	}
	one := Markdown([]*semdiff.FileDiff{a}, Meta{SkippedFiles: 1})
	if !strings.Contains(one, "1 additional workflow file was not analyzed.") {
		t.Errorf("singular skipped line missing:\n%s", one)
	}
}

func TestMarkdownNoTableWhenMetricsEqual(t *testing.T) {
	d := &semdiff.FileDiff{
		Path:     ".github/workflows/ci.yml",
		Status:   semdiff.StatusModified,
		Before:   semdiff.Metrics{JobsPerRun: "2", Depth: 1, Width: 2, RunnerMinutes: "10"},
		After:    semdiff.Metrics{JobsPerRun: "2", Depth: 1, Width: 2, RunnerMinutes: "10"},
		Findings: []semdiff.Finding{{Kind: "trigger.added", Significance: semdiff.Normal, Subject: "push"}},
	}
	got := Markdown([]*semdiff.FileDiff{d}, Meta{})
	if strings.Contains(got, "| Metric |") {
		t.Errorf("table shown for unchanged metrics:\n%s", got)
	}
	d.After.RunnerMinutes = ""
	d.After.JobsPerRun = "3"
	got = Markdown([]*semdiff.FileDiff{d}, Meta{})
	if !strings.Contains(got, "| Metric |") || strings.Contains(got, "Est. runner") {
		t.Errorf("estimate row shown with one side missing:\n%s", got)
	}
}

func paddedDiff(path string, pad int) *semdiff.FileDiff {
	d := &semdiff.FileDiff{Path: path, Status: semdiff.StatusUnanalyzable}
	for pad > 0 {
		n := min(pad, 50)
		d.Findings = append(d.Findings, semdiff.Finding{Kind: "workflow.unanalyzable", Significance: semdiff.Normal, Detail: strings.Repeat("가", n)})
		pad -= n
	}
	if len(d.Findings) == 0 {
		d.Findings = []semdiff.Finding{{Kind: "workflow.unanalyzable", Significance: semdiff.Normal}}
	}
	return d
}

func padLen(meta Meta, a, b int) int {
	return utf8.RuneCountInString(Markdown([]*semdiff.FileDiff{paddedDiff("a.yml", a), paddedDiff("b.yml", b)}, meta))
}

func TestMarkdownTruncationBoundary(t *testing.T) {
	meta := Meta{HeadSHA: "abcdef0123"}
	aPad, bPad, found := 0, 0, false
	for rem := 1; rem <= 50 && !found; rem++ {
		aPad = 50*400 + rem
		base := padLen(meta, aPad, 0)
		k := (MaxBodyRunes - base) / 74
		for ; k >= 0 && !found; k-- {
			extra := MaxBodyRunes - padLen(meta, aPad, 50*k)
			if extra > 74 {
				break
			}
			if extra >= 25 {
				bPad = 50*k + extra - 24
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no padding hits the limit exactly")
	}
	exact := Markdown([]*semdiff.FileDiff{paddedDiff("a.yml", aPad), paddedDiff("b.yml", bPad)}, meta)
	if n := utf8.RuneCountInString(exact); n != MaxBodyRunes {
		t.Fatalf("exact body has %d runes, want %d", n, MaxBodyRunes)
	}
	if len(exact) <= MaxBodyRunes {
		t.Fatalf("padding must be multi-byte")
	}
	if strings.Contains(exact, "omitted") || !strings.Contains(exact, "b.yml") {
		t.Errorf("body at limit was truncated")
	}
	over := Markdown([]*semdiff.FileDiff{paddedDiff("a.yml", aPad), paddedDiff("b.yml", bPad+1)}, meta)
	if utf8.RuneCountInString(over) > MaxBodyRunes {
		t.Errorf("truncated body exceeds limit")
	}
	if strings.Contains(over, "b.yml") || !strings.Contains(over, "a.yml") {
		t.Errorf("expected only last file removed:\n%.300s", over)
	}
	if !strings.Contains(over, "\n1 file omitted due to size.\n\n---\n") {
		t.Errorf("omitted line missing")
	}
	if !strings.Contains(over, "Execution changes in 2 workflow files.") {
		t.Errorf("header must count all changed files")
	}
	single := Markdown([]*semdiff.FileDiff{paddedDiff("a.yml", MaxBodyRunes)}, meta)
	if utf8.RuneCountInString(single) > MaxBodyRunes {
		t.Errorf("single oversized file not removed")
	}
	if strings.Contains(single, "a.yml") || !strings.Contains(single, "1 file omitted due to size.") {
		t.Errorf("single oversized file handling wrong:\n%.300s", single)
	}
	if !strings.HasSuffix(single, footer(meta)) || !strings.HasPrefix(single, CommentMarker+"\n") {
		t.Errorf("truncated body lost marker or footer:\n%.300s", single)
	}
}

func TestCheckSummary(t *testing.T) {
	quiet := &semdiff.FileDiff{Path: ".github/workflows/ci.yml", Status: semdiff.StatusModified, Findings: []semdiff.Finding{}}
	title, summary := CheckSummary([]*semdiff.FileDiff{quiet}, Meta{HeadSHA: "abcdef0123"})
	if title != "No execution changes" {
		t.Errorf("title = %q", title)
	}
	if !strings.Contains(summary, "- `.github/workflows/ci.yml`\n") {
		t.Errorf("summary lacks file list:\n%s", summary)
	}
	title, summary = CheckSummary([]*semdiff.FileDiff{specDiff()}, Meta{HeadSHA: "abc1234ffff"})
	if title != "4 execution changes" {
		t.Errorf("title = %q", title)
	}
	if strings.Contains(summary, CommentMarker) {
		t.Errorf("summary contains marker")
	}
	if !strings.Contains(summary, "Action added: `actions/cache@v4`") {
		t.Errorf("summary lacks low finding:\n%s", summary)
	}
	want := strings.TrimPrefix(specExample, CommentMarker+"\n")
	want = strings.Replace(want, "(mutable ref)\n", "(mutable ref)\n- Action added: `actions/cache@v4`\n", 1)
	if summary != want {
		t.Errorf("summary mismatch\ngot:\n%s\nwant:\n%s", summary, want)
	}
	one := &semdiff.FileDiff{Path: "x.yml", Findings: []semdiff.Finding{{Kind: "action.added", Significance: semdiff.Low, Subject: "a/b", After: "v1"}}}
	if title, _ := CheckSummary([]*semdiff.FileDiff{one}, Meta{}); title != "1 execution change" {
		t.Errorf("singular title = %q", title)
	}
}

func TestAnnotations(t *testing.T) {
	d := specDiff()
	d.Findings = append(d.Findings, semdiff.Finding{Kind: "job.removed", Significance: semdiff.Normal, Subject: "old", BasePos: pos(3, 3, 3, 5)})
	got := Annotations([]*semdiff.FileDiff{d})
	if len(got) != 4 {
		t.Fatalf("annotations = %d, want 4", len(got))
	}
	multi := got[0]
	if multi.StartLine != 13 || multi.EndLine != 15 || multi.StartColumn != 0 || multi.EndColumn != 0 {
		t.Errorf("multi-line annotation = %+v", multi)
	}
	single := got[1]
	if single.StartLine != 5 || single.EndLine != 5 || single.StartColumn != 13 || single.EndColumn != 17 {
		t.Errorf("single-line annotation = %+v", single)
	}
	for _, a := range got {
		if a.Level != "notice" || a.Path != d.Path || a.Title == "" || a.Message == "" {
			t.Errorf("bad annotation %+v", a)
		}
	}
	if got[0].Title != "matrix.count_changed" || got[0].Message != "Job `test` matrix: 6 → 24 jobs" {
		t.Errorf("annotation content = %+v", got[0])
	}
	if empty := Annotations(nil); empty == nil || len(empty) != 0 {
		t.Errorf("Annotations(nil) = %v", empty)
	}
}

func TestJSON(t *testing.T) {
	out, err := JSON([]*semdiff.FileDiff{specDiff()}, Meta{HeadSHA: "abc", SkippedFiles: 2})
	if err != nil {
		t.Fatal(err)
	}
	if out[len(out)-1] != '\n' || out[len(out)-2] == '\n' {
		t.Errorf("json must end with exactly one newline")
	}
	if !strings.HasPrefix(string(out), "{\n  \"schema\": \"quanto.diff/v1\",\n  \"head_sha\": \"abc\",\n  \"skipped_files\": 2,\n  \"files\": [") {
		t.Errorf("unexpected json head:\n%.200s", out)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	files := doc["files"].([]any)
	first := files[0].(map[string]any)
	f0 := first["findings"].([]any)[0].(map[string]any)
	if f0["significance"] != "high" {
		t.Errorf("significance = %v", f0["significance"])
	}
	empty, err := JSON(nil, Meta{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(empty), "\"files\": []") {
		t.Errorf("empty files must be []:\n%s", empty)
	}
}

func TestText(t *testing.T) {
	got := Text([]*semdiff.FileDiff{specDiff()})
	want := "`.github/workflows/ci.yml` (modified)\n" +
		"  Jobs per run: 7 → 25\n" +
		"  Max concurrent jobs: 2 → 4\n" +
		"  Est. runner minutes per run: 43 → 172\n" +
		"  - [high] Job `test` matrix: 6 → 24 jobs\n" +
		"  - [high] `contents` permission (workflow): `read` → `write`\n" +
		"  - [high] New third-party action: `peter-evans/create-pull-request@v6` (mutable ref)\n" +
		"  - [low] Action added: `actions/cache@v4`\n"
	if got != want {
		t.Errorf("text mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("text contains escape codes")
	}
	renamed := &semdiff.FileDiff{Path: "b.yml", OldPath: "a.yml", Status: semdiff.StatusRenamed, Findings: []semdiff.Finding{}}
	if got := Text([]*semdiff.FileDiff{renamed}); got != "`b.yml` (renamed, from `a.yml`)\n  No execution changes\n" {
		t.Errorf("renamed text = %q", got)
	}
	if got := Text(nil); got != "No workflow files\n" {
		t.Errorf("empty text = %q", got)
	}
}

func TestDeterministicOrder(t *testing.T) {
	a := specDiff()
	a.Path = "a.yml"
	b := specDiff()
	b.Path = "b.yml"
	meta := Meta{HeadSHA: "abcdef0"}
	x, y := []*semdiff.FileDiff{a, b}, []*semdiff.FileDiff{b, a}
	if Markdown(x, meta) != Markdown(y, meta) || Text(x) != Text(y) {
		t.Errorf("output depends on input order")
	}
	jx, _ := JSON(x, meta)
	jy, _ := JSON(y, meta)
	if string(jx) != string(jy) {
		t.Errorf("json depends on input order")
	}
	_, sx := CheckSummary(x, meta)
	_, sy := CheckSummary(y, meta)
	if sx != sy {
		t.Errorf("check summary depends on input order")
	}
}

func TestPlain(t *testing.T) {
	long := strings.Repeat("a", 81)
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "ubuntu-latest", "ubuntu-latest"},
		{"ansi color", "\x1b[31mred\x1b[0m", " [31mred [0m"},
		{"osc title", "\x1b]0;title\x07rest", " ]0;title rest"},
		{"carriage return overwrite", "safe\rEVIL", "safe EVIL"},
		{"c1 csi", "a\u009b31mb", "a 31mb"},
		{"del", "a\x7fb", "a b"},
		{"newline and tab", "a\nb\tc", "a b c"},
		{"backticks kept", "`x`", "`x`"},
		{"korean", "한글", "한글"},
		{"exactly 80", strings.Repeat("a", 80), strings.Repeat("a", 80)},
		{"81 runes", long, strings.Repeat("a", 79) + "…"},
		{"81 korean runes", strings.Repeat("가", 81), strings.Repeat("가", 79) + "…"},
	}
	for _, tt := range tests {
		got := Plain(tt.in)
		if got != tt.want {
			t.Errorf("%s: Plain(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
		if utf8.RuneCountInString(got) > 80 {
			t.Errorf("%s: %d runes", tt.name, utf8.RuneCountInString(got))
		}
		for _, r := range got {
			if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
				t.Errorf("%s: control rune %U in %q", tt.name, r, got)
			}
		}
	}
}

func TestTextStripsTerminalControls(t *testing.T) {
	d := &semdiff.FileDiff{
		Path:   ".github/workflows/\x1b[31mci\x1b]0;title\x07.yml",
		Status: semdiff.StatusModified,
		Before: semdiff.Metrics{JobsPerRun: "1", Depth: 1, Width: 1},
		After:  semdiff.Metrics{JobsPerRun: "1", Depth: 1, Width: 1},
		Findings: []semdiff.Finding{
			{Kind: "job.added", Significance: semdiff.Normal, Subject: "safe\rEVIL"},
		},
	}
	out := Text([]*semdiff.FileDiff{d})
	for _, bad := range []string{"\x1b", "\x07", "\r"} {
		if strings.Contains(out, bad) {
			t.Errorf("text output contains %q: %q", bad, out)
		}
	}
	if !strings.Contains(out, "Job added: `safe EVIL`") {
		t.Errorf("text output = %q", out)
	}
}
