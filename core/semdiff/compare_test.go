package semdiff

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/source"
)

func mustParse(t *testing.T, content string) *model.Workflow {
	t.Helper()
	w, err := parseWorkflow("wf.yml", []byte(content))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return w
}

func diff(t *testing.T, before, after string) *FileDiff {
	t.Helper()
	in := Input{Path: "wf.yml"}
	if before != "" {
		in.Before = mustParse(t, before)
	}
	if after != "" {
		in.After = mustParse(t, after)
	}
	return Compare(in, Options{})
}

func findingsOf(d *FileDiff, kind string) []Finding {
	var out []Finding
	for _, f := range d.Findings {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func TestKindsTable(t *testing.T) {
	kinds := Kinds()
	if len(kinds) != 35 {
		t.Fatalf("len(Kinds()) = %d", len(kinds))
	}
	if kinds[0] != "workflow.added" || kinds[len(kinds)-1] != "estimate.changed" {
		t.Errorf("kind order = %v", kinds)
	}
	seen := make(map[string]bool)
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("duplicate kind %s", k)
		}
		seen[k] = true
	}
	kinds[0] = "mutated"
	if Kinds()[0] != "workflow.added" {
		t.Error("Kinds returned shared slice")
	}
}

func TestSignificanceText(t *testing.T) {
	for s, want := range map[Significance]string{Low: "low", Normal: "normal", High: "high"} {
		b, err := s.MarshalText()
		if err != nil || string(b) != want {
			t.Errorf("MarshalText(%d) = %q, %v", s, b, err)
		}
	}
	if _, err := Significance(9).MarshalText(); err == nil {
		t.Error("expected error for unknown significance")
	}
	b, err := json.Marshal(Finding{Kind: "x", Significance: High})
	if err != nil || !strings.Contains(string(b), `"significance":"high"`) || !strings.Contains(string(b), `"base_pos":{`) {
		t.Errorf("json = %s, %v", b, err)
	}
}

func TestJobKey(t *testing.T) {
	cases := []struct {
		id, name, want string
	}{
		{"build", "", "build"},
		{"build", "Build app", "Build app"},
		{"build", "Build ${{ matrix.os }}", "build"},
		{"test", "Test (unit)", "Test"},
		{"test", "Test (a (b))", "Test"},
		{"test", "(only)", "(only)"},
	}
	for _, c := range cases {
		j := &model.Job{ID: c.id, Name: source.At(c.name, source.Position{})}
		if got := JobKey(j); got != c.want {
			t.Errorf("JobKey(%q, %q) = %q, want %q", c.id, c.name, got, c.want)
		}
	}
	if JobKey(nil) != "" {
		t.Error("JobKey(nil) not empty")
	}
}

func TestNormalizeRunJobName(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"build", "build", true},
		{"test (ubuntu-latest, 18)", "test", true},
		{"test (ubuntu-latest, (x))", "test", true},
		{"call / build", "", false},
		{"call / build (a)", "", false},
		{"weird(a)", "weird(a)", true},
		{"unbalanced a)", "unbalanced a)", true},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := NormalizeRunJobName(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("NormalizeRunJobName(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestRenameThreshold(t *testing.T) {
	before := `on: push
jobs:
  old:
    runs-on: x
    steps:
      - run: a
      - run: b
      - run: c
      - run: d
`
	similar := strings.Replace(before, "  old:", "  fresh:", 1)
	similar = strings.Replace(similar, "- run: d", "- run: d\n      - run: e", 1)
	d := diff(t, before, similar)
	if got := kindList(d); !reflect.DeepEqual(got, []string{kindJobRenamed}) {
		t.Errorf("4/5 overlap kinds = %v", got)
	}
	different := strings.Replace(before, "  old:", "  fresh:", 1)
	different = strings.Replace(different, "- run: c\n      - run: d", "- run: y\n      - run: z", 1)
	d = diff(t, before, different)
	if got := kindList(d); !reflect.DeepEqual(got, []string{kindJobAdded, kindJobRemoved}) {
		t.Errorf("2/6 overlap kinds = %v", got)
	}
}

func TestRenameGreedyTieBreak(t *testing.T) {
	before := "on: push\njobs:\n  b1: {runs-on: x, steps: [{run: s}]}\n  b2: {runs-on: x, steps: [{run: s}]}\n"
	after := "on: push\njobs:\n  a2: {runs-on: x, steps: [{run: s}]}\n  a1: {runs-on: x, steps: [{run: s}]}\n"
	d := diff(t, before, after)
	got := findingsOf(d, kindJobRenamed)
	if len(got) != 2 || got[0].Before != "b1" || got[0].After != "a1" || got[1].Before != "b2" || got[1].After != "a2" {
		t.Errorf("renames = %+v", got)
	}
}

func TestRenameReusable(t *testing.T) {
	before := "on: push\njobs:\n  call: {uses: org/repo/.github/workflows/x.yml@v1}\n"
	same := "on: push\njobs:\n  invoke: {uses: org/repo/.github/workflows/x.yml@v1}\n"
	other := "on: push\njobs:\n  invoke: {uses: org/repo/.github/workflows/y.yml@v1}\n"
	if got := kindList(diff(t, before, same)); !reflect.DeepEqual(got, []string{kindJobRenamed}) {
		t.Errorf("same uses kinds = %v", got)
	}
	got := kindList(diff(t, before, other))
	if !slices.Contains(got, kindJobAdded) || !slices.Contains(got, kindJobRemoved) || slices.Contains(got, kindJobRenamed) {
		t.Errorf("different uses kinds = %v", got)
	}
}

func TestRenamedPairCompared(t *testing.T) {
	before := "on: push\njobs:\n  build: {runs-on: ubuntu-latest, steps: [{run: a}, {run: b}]}\n"
	after := "on: push\njobs:\n  compile: {runs-on: macos-latest, steps: [{run: a}, {run: b}]}\n"
	d := diff(t, before, after)
	if got := kindList(d); !reflect.DeepEqual(got, []string{kindJobRunnerChanged, kindJobRenamed}) {
		t.Errorf("kinds = %v", got)
	}
}

func TestTriggerChanges(t *testing.T) {
	before := `on:
  push:
    branches: [main]
    paths: [src/**]
  pull_request:
  schedule:
    - cron: '0 0 * * *'
jobs:
  a: {runs-on: x, steps: [{run: a}]}
`
	after := `on:
  push:
    branches: [main, develop]
  workflow_dispatch:
  schedule:
    - cron: '0 */6 * * 1-5'
jobs:
  a: {runs-on: x, steps: [{run: a}]}
`
	d := diff(t, before, after)
	filters := findingsOf(d, kindTriggerFilterChanged)
	if len(filters) != 2 || filters[0].Detail != "branches" || filters[0].Before != "main" || filters[0].After != "develop, main" || filters[1].Detail != "paths" || filters[1].After != "(none)" {
		t.Errorf("filters = %+v", filters)
	}
	if got := findingsOf(d, kindTriggerAdded); len(got) != 1 || got[0].Subject != "workflow_dispatch" {
		t.Errorf("added = %+v", got)
	}
	removed := findingsOf(d, kindTriggerRemoved)
	if len(removed) != 1 || removed[0].Subject != "pull_request" || removed[0].Pos.Valid() || !removed[0].BasePos.Valid() {
		t.Errorf("removed = %+v", removed)
	}
	sched := findingsOf(d, kindTriggerScheduleChanged)
	if len(sched) != 1 || sched[0].Before != "'0 0 * * *' (1 runs/day)" || sched[0].After != "'0 */6 * * 1-5' (4 runs on matching days)" {
		t.Errorf("schedule = %+v", sched)
	}
	removedSched := diff(t, before, strings.Replace(before, "  schedule:\n    - cron: '0 0 * * *'\n", "", 1))
	got := findingsOf(removedSched, kindTriggerScheduleChanged)
	if len(got) != 1 || got[0].After != "(none)" || got[0].Pos.Valid() || !got[0].BasePos.Valid() || len(removedSched.Findings) != 1 {
		t.Errorf("schedule removed = %+v", removedSched.Findings)
	}
}

func TestPermissionScopes(t *testing.T) {
	before := "on: push\npermissions: read-all\njobs:\n  a:\n    runs-on: x\n    permissions: {contents: write, issues: read}\n    steps: [{run: a}]\n"
	after := "on: push\npermissions: {contents: write, custom-scope: read}\njobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n"
	d := diff(t, before, after)
	broad := findingsOf(d, kindPermissionsBroadened)
	if len(broad) != 2 || broad[0].Subject != "job `a`" || broad[0].Detail != "custom-scope" || broad[0].Before != "none" || broad[0].After != "read" || broad[1].Subject != "workflow" || broad[1].Detail != "contents" || broad[1].Before != "read" || broad[1].After != "write" {
		t.Errorf("broadened = %+v", broad)
	}
	if n := len(findingsOf(d, kindPermissionsNarrowed)); n != 14 {
		t.Errorf("narrowed = %d", n)
	}
	if removed := findingsOf(d, kindPermissionsRemoved); len(removed) != 0 {
		t.Errorf("removed = %+v", removed)
	}
	custom := diff(t, "on: push\npermissions: {contents: read}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n", "on: push\npermissions: {contents: read, custom-scope: write}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n")
	if got := findingsOf(custom, kindPermissionsBroadened); len(got) != 1 || got[0].Detail != "custom-scope" || got[0].Before != "none" || got[0].Pos.Line != 2 {
		t.Errorf("custom scope = %+v", custom.Findings)
	}
	declared := diff(t, "on: push\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n", "on: push\npermissions: {}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n")
	if got := kindList(declared); !reflect.DeepEqual(got, []string{kindPermissionsDeclared}) {
		t.Errorf("declared kinds = %v", got)
	}
	same := diff(t, "on: push\npermissions: write-all\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n", "on: push\npermissions: write-all\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n")
	if len(same.Findings) != 0 {
		t.Errorf("write-all unchanged = %+v", same.Findings)
	}
}

func TestActionRefSets(t *testing.T) {
	sha := "b5ca514318bd6ebac0fb2aedd5d36ec1b5c232a2"
	before := "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: actions/checkout@v4\n      - uses: actions/checkout@v3\n      - uses: org/tool@" + sha + "\n      - uses: org/gone@v1\n      - uses: ./local\n"
	after := "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: actions/checkout@v4\n      - uses: org/tool@" + sha + "\n      - uses: org/new/sub@" + sha + "\n      - uses: docker://alpine:3.20\n      - uses: ./local\n"
	d := diff(t, before, after)
	ref := findingsOf(d, kindActionRefChanged)
	if len(ref) != 1 || ref[0].Subject != "actions/checkout" || ref[0].Before != "v3, v4" || ref[0].After != "v4" {
		t.Errorf("ref changed = %+v", ref)
	}
	third := findingsOf(d, kindActionThirdPartyAdded)
	if len(third) != 1 || third[0].Subject != "org/new/sub" || third[0].Detail != "" {
		t.Errorf("third party = %+v", third)
	}
	added := findingsOf(d, kindActionAdded)
	if len(added) != 1 || added[0].Subject != "docker://alpine:3.20" {
		t.Errorf("added = %+v", added)
	}
	removed := findingsOf(d, kindActionRemoved)
	if len(removed) != 1 || removed[0].Subject != "org/gone" || !removed[0].BasePos.Valid() {
		t.Errorf("removed = %+v", removed)
	}
}

func TestReusableUsesCompared(t *testing.T) {
	sha := "b5ca514318bd6ebac0fb2aedd5d36ec1b5c232a2"
	before := "on: push\njobs:\n  call: {uses: Org/Repo/.github/workflows/ci.yml@" + sha + "}\n  local: {uses: ./.github/workflows/a.yml}\n"
	after := "on: push\njobs:\n  call: {uses: Org/Repo/.github/workflows/ci.yml@main}\n  local: {uses: ./.github/workflows/a.yml}\n  first: {uses: actions/reusable/.github/workflows/x.yml@v1}\n"
	d := diff(t, before, after)
	pin := findingsOf(d, kindActionPinRemoved)
	if len(pin) != 1 || pin[0].Subject != "org/repo/.github/workflows/ci.yml" || pin[0].After != "main" {
		t.Errorf("pin removed = %+v", pin)
	}
	added := findingsOf(d, kindActionAdded)
	if len(added) != 1 || added[0].Subject != "actions/reusable/.github/workflows/x.yml" {
		t.Errorf("added = %+v", added)
	}
	if n := len(findingsOf(d, kindActionThirdPartyAdded)); n != 0 {
		t.Errorf("third party = %d", n)
	}
	third := diff(t, before, before+"  ext: {uses: other/repo/.github/workflows/z.yml@v2}\n")
	if got := findingsOf(third, kindActionThirdPartyAdded); len(got) != 1 || got[0].Detail != " (mutable ref)" {
		t.Errorf("reusable third party = %+v", got)
	}
}

func TestMatrixTransitions(t *testing.T) {
	wf := func(matrix string) string {
		return "on: push\njobs:\n  t:\n    runs-on: x\n    strategy:\n      matrix:\n" + matrix + "    steps: [{run: a}]\n"
	}
	values := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = "v" + strings.Repeat("x", i+1)
		}
		return "        a: [" + strings.Join(parts, ", ") + "]\n"
	}
	dyn := wf("        a: ${{ fromJSON(vars.A) }}\n")
	d := diff(t, dyn, wf(values(3)))
	cc := findingsOf(d, kindMatrixCountChanged)
	if len(cc) != 1 || cc[0].Before != "?" || cc[0].After != "3" || cc[0].Significance != Normal {
		t.Errorf("dynamic to static = %+v", d.Findings)
	}
	if n := len(findingsOf(d, kindGraphWidthChanged)); n != 0 {
		t.Errorf("width reported with dynamic side")
	}
	if got := kindList(diff(t, dyn, dyn)); len(got) != 0 {
		t.Errorf("dynamic unchanged = %v", got)
	}
	over := diff(t, wf(values(300)), wf(values(310)))
	if got := findingsOf(over, kindMatrixCountChanged); len(got) != 1 || got[0].Significance != Normal {
		t.Errorf("already over = %+v", over.Findings)
	}
	if n := len(findingsOf(over, kindMatrixOverLimit)); n != 0 {
		t.Errorf("over limit repeated")
	}
	halved := diff(t, wf(values(4)), wf(values(2)))
	if got := findingsOf(halved, kindMatrixCountChanged); len(got) != 1 || got[0].Significance != High {
		t.Errorf("halved = %+v", halved.Findings)
	}
	base := "on: push\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n"
	added := diff(t, base, base+"  big:\n    runs-on: x\n    strategy:\n      matrix:\n"+values(300)+"    steps: [{run: a}]\n  dyn:\n    runs-on: x\n    strategy: {matrix: {a: '${{ fromJSON(vars.A) }}'}}\n    steps: [{run: a}]\n")
	if len(findingsOf(added, kindMatrixOverLimit)) != 1 || len(findingsOf(added, kindMatrixDynamic)) != 1 || len(findingsOf(added, kindJobAdded)) != 2 {
		t.Errorf("added jobs = %v", kindList(added))
	}
	if added.After.JobsPerRun != "?" {
		t.Errorf("jobs per run = %q", added.After.JobsPerRun)
	}
	huge := diff(t, base, "on: push\njobs:\n  h:\n    runs-on: x\n    strategy:\n      matrix:\n"+values(400)+strings.Replace(values(400), "a:", "b:", 1)+"    steps: [{run: a}]\n")
	if huge.After.JobsPerRun != "≥160000" {
		t.Errorf("unmaterialized jobs per run = %q", huge.After.JobsPerRun)
	}
}

func TestJobLevelChanges(t *testing.T) {
	before := "on: push\njobs:\n  a:\n    runs-on: [self-hosted, linux]\n    timeout-minutes: 10\n    concurrency: deploy\n    steps: [{run: a}]\n"
	after := "on: push\njobs:\n  a:\n    runs-on: {group: big, labels: [linux, self-hosted]}\n    concurrency: {group: deploy, cancel-in-progress: true}\n    steps: [{run: a}]\n"
	d := diff(t, before, after)
	r := findingsOf(d, kindJobRunnerChanged)
	if len(r) != 1 || r[0].Before != "linux, self-hosted" || r[0].After != "group big: linux, self-hosted" {
		t.Errorf("runner = %+v", r)
	}
	to := findingsOf(d, kindJobTimeoutChanged)
	if len(to) != 1 || to[0].Before != "10" || to[0].After != "(none)" {
		t.Errorf("timeout = %+v", to)
	}
	cc := findingsOf(d, kindJobConcurrencyChanged)
	if len(cc) != 1 || cc[0].Before != "deploy" || cc[0].After != "deploy (cancel-in-progress: true)" {
		t.Errorf("concurrency = %+v", cc)
	}
	reordered := diff(t, before, strings.Replace(before, "[self-hosted, linux]", "[linux, self-hosted]", 1))
	if len(reordered.Findings) != 0 {
		t.Errorf("label order change = %+v", reordered.Findings)
	}
}

func TestGraphUnresolvedAndDepth(t *testing.T) {
	before := "on: push\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n"
	after := "on: push\njobs:\n  a: {runs-on: x, needs: [ghost], steps: [{run: a}]}\n"
	d := diff(t, before, after)
	u := findingsOf(d, kindGraphUnresolved)
	if len(u) != 1 || u[0].Subject != "a" || u[0].After != "ghost" || u[0].Pos.Line != 3 {
		t.Errorf("unresolved = %+v", u)
	}
}

func TestStatuses(t *testing.T) {
	w := mustParse(t, "on: push\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n")
	renamed := Compare(Input{Path: "new.yml", OldPath: "old.yml", Before: w, After: w}, Options{})
	if renamed.Status != StatusRenamed || len(renamed.Findings) != 1 || renamed.Findings[0].Kind != kindWorkflowRenamed || renamed.Findings[0].Before != "old.yml" {
		t.Errorf("renamed = %+v", renamed)
	}
	both := Compare(Input{Path: "x.yml", BeforeErr: errors.New("b"), AfterErr: errors.New("a")}, Options{})
	if both.Status != StatusUnanalyzable || both.Error != "base: b; head: a" {
		t.Errorf("both errors = %+v", both)
	}
	baseErr := Compare(Input{Path: "x.yml", BeforeErr: errors.New("bad"), After: w}, Options{})
	if baseErr.Status != StatusUnanalyzable || baseErr.After.JobsPerRun != "1" || baseErr.Before.JobsPerRun != "" {
		t.Errorf("base error = %+v", baseErr)
	}
	empty := Compare(Input{Path: "x.yml"}, Options{})
	if empty.Status != StatusUnanalyzable || len(empty.Findings) != 1 {
		t.Errorf("empty = %+v", empty)
	}
}

type mapDurations map[string]sample

func (m mapDurations) JobAverage(path, key string) (time.Duration, int, bool) {
	s, ok := m[path+"|"+key]
	return s.avg, s.n, ok
}

func TestEstimateRules(t *testing.T) {
	w := mustParse(t, "on: push\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n  b: {name: 'B (fast)', runs-on: x, steps: [{run: b}]}\n")
	src := mapDurations{
		"old.yml|a": {avg: 2 * time.Minute, n: 5},
		"old.yml|B": {avg: 3 * time.Minute, n: 7},
		"new.yml|a": {avg: 2 * time.Minute, n: 5},
		"new.yml|B": {avg: 9 * time.Minute, n: 6},
	}
	d := Compare(Input{Path: "new.yml", OldPath: "old.yml", Before: w, After: w}, Options{Durations: src})
	if d.Estimate == nil || d.Estimate.MinutesBefore != 5 || d.Estimate.MinutesAfter != 11 || d.Estimate.Samples != 5 {
		t.Fatalf("estimate = %+v", d.Estimate)
	}
	e := findingsOf(d, kindEstimateChanged)
	if len(e) != 1 || e[0].Significance != High || e[0].Before != "5" || e[0].After != "11" || e[0].Detail != "5" {
		t.Errorf("estimate finding = %+v", e)
	}
	d = Compare(Input{Path: "new.yml", OldPath: "old.yml", Before: w, After: w}, Options{Durations: src, MinSamples: 6})
	if d.Estimate != nil || d.Before.RunnerMinutes != "" || d.After.RunnerMinutes != "" {
		t.Errorf("min samples 6: estimate = %+v, metrics = %+v %+v", d.Estimate, d.Before, d.After)
	}
	same := Compare(Input{Path: "old.yml", Before: w, After: w}, Options{Durations: src})
	if same.Estimate == nil || len(same.Findings) != 0 {
		t.Errorf("unchanged estimate = %+v %+v", same.Estimate, same.Findings)
	}
}

func TestSortOrder(t *testing.T) {
	fs := []Finding{
		{Kind: kindActionAdded, Significance: Low, Subject: "b"},
		{Kind: kindJobAdded, Significance: Normal, Subject: "z"},
		{Kind: kindActionAdded, Significance: Low, Subject: "a"},
		{Kind: kindTriggerAdded, Significance: Normal, Subject: "z"},
		{Kind: kindPermissionsBroadened, Significance: High, Subject: "workflow", Detail: "issues"},
		{Kind: kindPermissionsBroadened, Significance: High, Subject: "workflow", Detail: "contents"},
	}
	sortFindings(fs)
	var got []string
	for _, f := range fs {
		got = append(got, f.Kind+":"+f.Subject+":"+f.Detail)
	}
	want := []string{
		"permissions.broadened:workflow:contents",
		"permissions.broadened:workflow:issues",
		"trigger.added:z:",
		"job.added:z:",
		"action.added:a:",
		"action.added:b:",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v", got)
	}
}

func TestWidthText(t *testing.T) {
	tests := []struct {
		name string
		wf   string
		want string
	}{
		{"static", "on: push\njobs:\n  a: {runs-on: x, strategy: {matrix: {v: [1, 2, 3]}}, steps: [{run: a}]}\n  b: {runs-on: x, steps: [{run: b}]}\n", "4"},
		{"dynamic", "on: push\njobs:\n  a: {runs-on: x, strategy: {matrix: {v: '${{ fromJSON(x) }}'}}, steps: [{run: a}]}\n  b: {runs-on: x, needs: a, steps: [{run: b}]}\n", "?"},
		{"lower bound", "on: push\njobs:\n  a:\n    runs-on: x\n    strategy: {matrix: {a: [1,2,3,4,5,6,7,8,9,10,11], b: [1,2,3,4,5,6,7,8,9,10], c: [1,2,3,4,5,6,7,8,9,10]}}\n    steps: [{run: a}]\n", "≥1100"},
	}
	for _, tt := range tests {
		d := diff(t, "", tt.wf)
		if d.After.Width != tt.want {
			t.Errorf("%s: Width = %q, want %q", tt.name, d.After.Width, tt.want)
		}
		if d.Before.Width != "" {
			t.Errorf("%s: absent Width = %q", tt.name, d.Before.Width)
		}
	}
}
