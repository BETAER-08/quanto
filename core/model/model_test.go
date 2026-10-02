package model

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/BETAER-08/quanto/core/expr"
	"github.com/BETAER-08/quanto/core/source"
)

func parse(t *testing.T, content string) (*Workflow, []Diagnostic) {
	t.Helper()
	doc, err := source.Load("wf.yml", []byte(content))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	w, diags, err := Parse(doc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return w, diags
}

func values(ps []source.Positioned[string]) []string {
	if ps == nil {
		return nil
	}
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Value
	}
	return out
}

func codes(diags []Diagnostic) []string {
	var out []string
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}

func jobByID(t *testing.T, w *Workflow, id string) *Job {
	t.Helper()
	for _, j := range w.Jobs {
		if j.ID == id {
			return j
		}
	}
	t.Fatalf("job %q not found", id)
	return nil
}

func at(line, col int) [2]int {
	return [2]int{line, col}
}

func start(p source.Position) [2]int {
	return [2]int{p.Line, p.Column}
}

const minimalJobs = "jobs:\n  a:\n    runs-on: x\n    steps:\n      - run: echo\n"

func TestParseNotWorkflow(t *testing.T) {
	for _, in := range []string{"", "# only a comment\n", "- a\n- b\n", "just a scalar\n"} {
		doc, err := source.Load("wf.yml", []byte(in))
		if err != nil {
			t.Fatalf("load %q: %v", in, err)
		}
		if _, _, err := Parse(doc); !errors.Is(err, ErrNotWorkflow) {
			t.Errorf("Parse(%q) error = %v, want ErrNotWorkflow", in, err)
		}
	}
	if _, _, err := Parse(nil); !errors.Is(err, ErrNotWorkflow) {
		t.Errorf("Parse(nil) error = %v", err)
	}
}

func TestTriggerForms(t *testing.T) {
	tests := []struct {
		name   string
		on     string
		events []string
		pos    [][2]int
	}{
		{"scalar", "on: push\n", []string{"push"}, [][2]int{at(1, 5)}},
		{"sequence", "on: [push, pull_request]\n", []string{"push", "pull_request"}, [][2]int{at(1, 6), at(1, 12)}},
		{"block sequence", "on:\n  - push\n  - workflow_dispatch\n", []string{"push", "workflow_dispatch"}, [][2]int{at(2, 5), at(3, 5)}},
		{"mapping", "on:\n  push:\n  pull_request:\n    branches: [main]\n", []string{"push", "pull_request"}, [][2]int{at(2, 3), at(3, 3)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, diags := parse(t, tt.on+minimalJobs)
			if len(diags) != 0 {
				t.Fatalf("diagnostics: %v", diags)
			}
			var events [][2]int
			var names []string
			for _, tr := range w.Triggers {
				names = append(names, tr.Event)
				events = append(events, start(tr.Pos))
			}
			if !reflect.DeepEqual(names, tt.events) || !reflect.DeepEqual(events, tt.pos) {
				t.Errorf("triggers = %v %v, want %v %v", names, events, tt.events, tt.pos)
			}
		})
	}
}

func TestOnKeyIsString(t *testing.T) {
	w, _ := parse(t, "on:\n  push:\n"+minimalJobs)
	if len(w.Triggers) != 1 || w.Triggers[0].Event != "push" {
		t.Fatalf("triggers = %+v", w.Triggers)
	}
}

func TestTriggerFilters(t *testing.T) {
	w, _ := parse(t, `on:
  push:
    branches: main
    tags: ['v*', "release-*"]
    paths-ignore:
      - docs/**
    unknown-key: x
  pull_request:
    types: opened
  pull_request_target:
    types: [opened, synchronize]
    branches-ignore: [wip]
  workflow_run:
    workflows: [ci]
    types: [completed]
`+minimalJobs)
	want := map[string]map[string][]string{
		"push":                {"branches": {"main"}, "tags": {"v*", "release-*"}, "paths-ignore": {"docs/**"}},
		"pull_request":        {"types": {"opened"}},
		"pull_request_target": {"types": {"opened", "synchronize"}, "branches-ignore": {"wip"}},
		"workflow_run":        {"workflows": {"ci"}, "types": {"completed"}},
	}
	for _, tr := range w.Triggers {
		got := make(map[string][]string)
		for k, v := range tr.Filters {
			got[k] = values(v)
		}
		if !reflect.DeepEqual(got, want[tr.Event]) {
			t.Errorf("%s filters = %v, want %v", tr.Event, got, want[tr.Event])
		}
	}
	tags := w.Triggers[0].Filters["tags"]
	if start(tags[0].Pos) != at(4, 12) || start(tags[1].Pos) != at(4, 18) {
		t.Errorf("tag positions = %v %v", tags[0].Pos, tags[1].Pos)
	}
}

func TestSchedule(t *testing.T) {
	w, _ := parse(t, "on:\n  schedule:\n    - cron: '0 * * * *'\n    - cron: \"30 2 * * 1-5\"\n"+minimalJobs)
	tr := w.Triggers[0]
	if tr.Event != "schedule" || !reflect.DeepEqual(values(tr.Crons), []string{"0 * * * *", "30 2 * * 1-5"}) {
		t.Fatalf("schedule = %+v", tr)
	}
	if start(tr.Crons[0].Pos) != at(3, 13) || tr.Filters != nil {
		t.Errorf("cron pos = %v, filters = %v", tr.Crons[0].Pos, tr.Filters)
	}
}

func TestDispatchInputs(t *testing.T) {
	w, _ := parse(t, `on:
  workflow_dispatch:
    inputs:
      zeta:
        type: string
      alpha:
        type: boolean
  workflow_call:
    inputs:
      m: {type: string}
      b: {type: number}
    secrets:
      token: {required: true}
  push:
    inputs:
      ignored: {}
`+minimalJobs)
	got := map[string][]string{}
	for _, tr := range w.Triggers {
		got[tr.Event] = tr.Inputs
	}
	want := map[string][]string{"workflow_dispatch": {"alpha", "zeta"}, "workflow_call": {"b", "m"}, "push": nil}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("inputs = %v, want %v", got, want)
	}
}

func TestMissingOnAndJobs(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"name: x\n" + minimalJobs, []string{"MODEL-NO-ON"}},
		{"on:\n" + minimalJobs, []string{"MODEL-NO-ON"}},
		{"on: push\n", []string{"MODEL-NO-JOBS"}},
		{"on: push\njobs: {}\n", []string{"MODEL-NO-JOBS"}},
		{"on: push\njobs: nope\n", []string{"MODEL-NO-JOBS"}},
		{"on: push\njobs:\n  a: scalar\n  b:\n    runs-on: x\n    steps: [{run: y}]\n", []string{"MODEL-JOB-NOT-MAPPING"}},
	}
	for _, tt := range tests {
		w, diags := parse(t, tt.in)
		if got := codes(diags); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q diagnostics = %v, want %v", tt.in, got, tt.want)
		}
		for _, d := range diags {
			if !d.Pos.Valid() || d.Message == "" {
				t.Errorf("diagnostic %+v lacks position or message", d)
			}
		}
		if w == nil {
			t.Errorf("%q returned nil workflow", tt.in)
		}
	}
}

func TestJobsOrderPreserved(t *testing.T) {
	w, _ := parse(t, "on: push\njobs:\n  zeta: {runs-on: x, steps: [{run: a}]}\n  alpha: {runs-on: x, steps: [{run: a}]}\n  mid: {runs-on: x, steps: [{run: a}]}\n")
	var ids []string
	for _, j := range w.Jobs {
		ids = append(ids, j.ID)
	}
	if !reflect.DeepEqual(ids, []string{"zeta", "alpha", "mid"}) {
		t.Errorf("job order = %v", ids)
	}
	if start(w.Jobs[0].Pos) != at(3, 3) {
		t.Errorf("job pos = %v", w.Jobs[0].Pos)
	}
	if start(w.JobsPos) != at(2, 1) || w.JobsPos.EndColumn != 4 {
		t.Errorf("jobs key pos = %v", w.JobsPos)
	}
}

func TestJobsPosAbsent(t *testing.T) {
	w, _ := parse(t, "on: push\nname: x\n")
	if w.JobsPos.Valid() {
		t.Errorf("jobs key pos = %v, want zero", w.JobsPos)
	}
}

func TestNeeds(t *testing.T) {
	w, _ := parse(t, `on: push
jobs:
  a: {runs-on: x, steps: [{run: a}]}
  b:
    needs: a
    runs-on: x
    steps: [{run: a}]
  c:
    needs: [a, b]
    runs-on: x
    steps: [{run: a}]
`)
	if got := values(jobByID(t, w, "a").Needs); got != nil {
		t.Errorf("a needs = %v", got)
	}
	if got := values(jobByID(t, w, "b").Needs); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("b needs = %v", got)
	}
	c := jobByID(t, w, "c")
	if got := values(c.Needs); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("c needs = %v", got)
	}
	if start(c.Needs[1].Pos) != at(9, 16) {
		t.Errorf("needs pos = %v", c.Needs[1].Pos)
	}
}

func TestRunsOn(t *testing.T) {
	w, _ := parse(t, `on: push
jobs:
  scalar:
    runs-on: ubuntu-latest
    steps: [{run: a}]
  list:
    runs-on: [self-hosted, linux, x64]
    steps: [{run: a}]
  group:
    runs-on:
      group: big-runners
      labels: [linux]
  groupscalar:
    runs-on:
      group: g
      labels: gpu
  dynamic:
    runs-on: ${{ matrix.os }}
  dynlist:
    runs-on: [self-hosted, "${{ inputs.label }}"]
  dyngroup:
    runs-on:
      group: ${{ vars.GROUP }}
  absent:
    steps: [{run: a}]
`)
	tests := []struct {
		id      string
		labels  []string
		group   string
		dynamic bool
	}{
		{"scalar", []string{"ubuntu-latest"}, "", false},
		{"list", []string{"self-hosted", "linux", "x64"}, "", false},
		{"group", []string{"linux"}, "big-runners", false},
		{"groupscalar", []string{"gpu"}, "g", false},
		{"dynamic", []string{"${{ matrix.os }}"}, "", true},
		{"dynlist", []string{"self-hosted", "${{ inputs.label }}"}, "", true},
		{"dyngroup", nil, "${{ vars.GROUP }}", true},
		{"absent", nil, "", false},
	}
	for _, tt := range tests {
		rs := jobByID(t, w, tt.id).RunsOn
		if got := values(rs.Labels); !reflect.DeepEqual(got, tt.labels) || rs.Group != tt.group || rs.Dynamic != tt.dynamic {
			t.Errorf("%s runs-on = %v %q %v, want %v %q %v", tt.id, got, rs.Group, rs.Dynamic, tt.labels, tt.group, tt.dynamic)
		}
	}
	if p := jobByID(t, w, "scalar").RunsOn.Pos; start(p) != at(4, 14) {
		t.Errorf("runs-on pos = %v", p)
	}
	if p := jobByID(t, w, "absent").RunsOn.Pos; p.Valid() {
		t.Errorf("absent runs-on has position %v", p)
	}
}

func TestPermissions(t *testing.T) {
	w, diags := parse(t, `on: push
permissions:
  contents: read
  pull-requests: write
  id-token: none
  custom-scope: read
jobs:
  readall:
    permissions: read-all
    runs-on: x
    steps: [{run: a}]
  writeall:
    permissions: write-all
    runs-on: x
    steps: [{run: a}]
  empty:
    permissions: {}
    runs-on: x
    steps: [{run: a}]
  undeclared:
    runs-on: x
    steps: [{run: a}]
  bad:
    permissions:
      contents: admin
      issues: write
    runs-on: x
    steps: [{run: a}]
  badall:
    permissions: read
    runs-on: x
    steps: [{run: a}]
`)
	wp := w.Permissions
	if !wp.Declared || wp.All != "" || len(wp.Scopes) != 4 {
		t.Fatalf("workflow permissions = %+v", wp)
	}
	wantLevels := map[string]Level{"contents": LevelRead, "pull-requests": LevelWrite, "id-token": LevelNone, "custom-scope": LevelRead}
	for k, v := range wantLevels {
		if wp.Scopes[k].Value != v {
			t.Errorf("scope %s = %v, want %v", k, wp.Scopes[k].Value, v)
		}
	}
	if start(wp.Scopes["contents"].Pos) != at(3, 13) || start(wp.Pos) != at(3, 3) {
		t.Errorf("permission positions = %v %v", wp.Scopes["contents"].Pos, wp.Pos)
	}
	if p := jobByID(t, w, "readall").Permissions; !p.Declared || p.All != "read-all" || p.Scopes != nil {
		t.Errorf("read-all = %+v", p)
	}
	if p := jobByID(t, w, "writeall").Permissions; !p.Declared || p.All != "write-all" {
		t.Errorf("write-all = %+v", p)
	}
	if p := jobByID(t, w, "empty").Permissions; !p.Declared || p.All != "" || p.Scopes == nil || len(p.Scopes) != 0 {
		t.Errorf("empty = %+v", p)
	}
	if p := jobByID(t, w, "undeclared").Permissions; p.Declared || p.Pos.Valid() {
		t.Errorf("undeclared = %+v", p)
	}
	bad := jobByID(t, w, "bad").Permissions
	if _, ok := bad.Scopes["contents"]; ok || bad.Scopes["issues"].Value != LevelWrite {
		t.Errorf("bad = %+v", bad)
	}
	if got := codes(diags); !reflect.DeepEqual(got, []string{"MODEL-UNKNOWN-PERMISSION-LEVEL", "MODEL-UNKNOWN-PERMISSION-LEVEL"}) {
		t.Errorf("diagnostics = %v", got)
	}
	if start(diags[0].Pos) != at(25, 17) {
		t.Errorf("diagnostic pos = %v", diags[0].Pos)
	}
}

func TestActionRefs(t *testing.T) {
	sha := "8e5e7e5ab8b370d6c329ec480221332ada57f0ab"
	tests := []struct {
		raw        string
		owner      string
		repo       string
		path       string
		ref        string
		kind       RefKind
		local      bool
		docker     bool
		image      string
		firstParty bool
		identity   string
		hasRef     bool
	}{
		{raw: "actions/checkout@v4", owner: "actions", repo: "checkout", ref: "v4", kind: RefMutable, firstParty: true, identity: "actions/checkout", hasRef: true},
		{raw: "Actions/Setup-Go@main", owner: "Actions", repo: "Setup-Go", ref: "main", kind: RefMutable, firstParty: true, identity: "actions/setup-go", hasRef: true},
		{raw: "github/codeql-action/init@v3", owner: "github", repo: "codeql-action", path: "init", ref: "v3", kind: RefMutable, firstParty: true, identity: "github/codeql-action/init", hasRef: true},
		{raw: "aws-actions/configure-aws-credentials/sub/Dir@v4.0.1", owner: "aws-actions", repo: "configure-aws-credentials", path: "sub/Dir", ref: "v4.0.1", kind: RefMutable, identity: "aws-actions/configure-aws-credentials/sub/dir", hasRef: true},
		{raw: "peter-evans/create-pull-request@" + sha, owner: "peter-evans", repo: "create-pull-request", ref: sha, kind: RefSHA, identity: "peter-evans/create-pull-request", hasRef: true},
		{raw: "owner/repo@" + strings.ToUpper(sha), owner: "owner", repo: "repo", ref: strings.ToUpper(sha), kind: RefSHA, identity: "owner/repo", hasRef: true},
		{raw: "owner/repo@8e5e7e5", owner: "owner", repo: "repo", ref: "8e5e7e5", kind: RefMutable, identity: "owner/repo", hasRef: true},
		{raw: "owner/repo@" + sha + "0", owner: "owner", repo: "repo", ref: sha + "0", kind: RefMutable, identity: "owner/repo", hasRef: true},
		{raw: "owner/repo@" + sha[:39] + "g", owner: "owner", repo: "repo", ref: sha[:39] + "g", kind: RefMutable, identity: "owner/repo", hasRef: true},
		{raw: "./.github/actions/setup", path: "./.github/actions/setup", kind: RefUnknown, local: true, identity: "./.github/actions/setup", hasRef: true},
		{raw: `.\actions\win`, path: `.\actions\win`, kind: RefUnknown, local: true, identity: `.\actions\win`, hasRef: true},
		{raw: "docker://alpine:3.19", kind: RefUnknown, docker: true, image: "alpine:3.19", identity: "docker://alpine:3.19", hasRef: true},
		{raw: "owner/repo", owner: "owner", repo: "repo", kind: RefUnknown, identity: "owner/repo"},
		{raw: "github/foo", owner: "github", repo: "foo", kind: RefUnknown, firstParty: true, identity: "github/foo"},
		{raw: "actionsx/foo@v1", owner: "actionsx", repo: "foo", ref: "v1", kind: RefMutable, identity: "actionsx/foo", hasRef: true},
	}
	for _, tt := range tests {
		a, hasRef := parseActionRef(tt.raw, source.Position{})
		got := []any{a.Owner, a.Repo, a.Path, a.Ref, a.Kind, a.Local, a.Docker, a.DockerImage, a.FirstParty, a.Identity(), hasRef, a.Raw}
		want := []any{tt.owner, tt.repo, tt.path, tt.ref, tt.kind, tt.local, tt.docker, tt.image, tt.firstParty, tt.identity, tt.hasRef, tt.raw}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("parseActionRef(%q) = %v, want %v", tt.raw, got, want)
		}
	}
	var nilRef *ActionRef
	if nilRef.Identity() != "" {
		t.Error("nil identity not empty")
	}
}

func TestStepUsesInWorkflow(t *testing.T) {
	w, diags := parse(t, `on: push
jobs:
  build:
    runs-on: x
    steps:
      - uses: actions/checkout@v4
      - name: no ref
        uses: owner/repo
      - id: sh
        name: Shell
        if: success()
        run: |
          make
          make test
        shell: bash
        working-directory: src
        env:
          B: 1
          A: 2
        with:
          ignored: x
      - name: nothing
      - just a string
      - uses: docker://alpine
        with:
          args: echo hi
          entrypoint: /bin/sh
`)
	steps := jobByID(t, w, "build").Steps
	if len(steps) != 5 {
		t.Fatalf("steps = %d", len(steps))
	}
	if steps[0].Uses.Identity() != "actions/checkout" || start(steps[0].Uses.Pos) != at(6, 15) || start(steps[0].Pos) != at(6, 9) {
		t.Errorf("step 0 = %+v", steps[0].Uses)
	}
	if steps[1].Uses.Kind != RefUnknown {
		t.Errorf("step 1 kind = %v", steps[1].Uses.Kind)
	}
	s := steps[2]
	if s.Index != 2 || s.ID != "sh" || s.Name != "Shell" || s.Shell != "bash" || s.WorkingDirectory != "src" || !reflect.DeepEqual(s.EnvKeys, []string{"A", "B"}) {
		t.Errorf("step 2 = %+v", s)
	}
	if s.Run == nil || s.Run.Value != "make\nmake test\n" || start(s.Run.Pos) != at(12, 14) || s.Run.Pos.EndLine != 14 {
		t.Errorf("run = %+v", s.Run)
	}
	if s.If == nil || s.If.Template == nil || s.If.ParseErr != nil || s.If.Raw != "success()" {
		t.Errorf("if = %+v", s.If)
	}
	if s.Uses != nil || s.With["ignored"].Value != "x" {
		t.Errorf("step 2 uses/with = %+v %+v", s.Uses, s.With)
	}
	if steps[3].Index != 3 || steps[3].Uses != nil || steps[3].Run != nil {
		t.Errorf("step 3 = %+v", steps[3])
	}
	if steps[4].Index != 5 || !steps[4].Uses.Docker || steps[4].With["args"].Value != "echo hi" || len(steps[4].With) != 2 {
		t.Errorf("step 5 = %+v", steps[4])
	}
	want := []string{"MODEL-ACTION-NO-REF", "MODEL-STEP-NO-ACTION", "MODEL-STEP-NOT-MAPPING"}
	if got := codes(diags); !reflect.DeepEqual(got, want) {
		t.Errorf("diagnostics = %v, want %v", got, want)
	}
}

func TestJobScalars(t *testing.T) {
	w, diags := parse(t, `on: push
concurrency: ci-${{ github.ref }}
env:
  Z: 1
  A: 2
jobs:
  deploy:
    name: Deploy prod
    environment: production
    concurrency:
      group: deploy
      cancel-in-progress: true
    timeout-minutes: 30
    continue-on-error: ${{ matrix.experimental }}
    container: node:20
    services:
      redis: {image: redis}
      postgres: {image: postgres}
    strategy:
      fail-fast: false
      max-parallel: 2
      matrix:
        os: [a, b]
    outputs:
      z: x
      a: y
    runs-on: x
    steps: [{run: a}]
  other:
    environment:
      name: staging
      url: https://x
    container:
      image: golang:1.24
    timeout-minutes: ${{ inputs.t }}
    runs-on: x
    steps: [{run: a}]
`)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %v", diags)
	}
	if w.Concurrency == nil || w.Concurrency.Group != "ci-${{ github.ref }}" || w.Concurrency.CancelInProgress != "" {
		t.Errorf("workflow concurrency = %+v", w.Concurrency)
	}
	if !reflect.DeepEqual(w.EnvKeys, []string{"A", "Z"}) {
		t.Errorf("env keys = %v", w.EnvKeys)
	}
	d := jobByID(t, w, "deploy")
	if d.Name.Value != "Deploy prod" || start(d.Name.Pos) != at(8, 11) {
		t.Errorf("name = %+v", d.Name)
	}
	if d.Environment != "production" || d.Concurrency.Group != "deploy" || d.Concurrency.CancelInProgress != "true" {
		t.Errorf("deploy = %+v %+v", d.Environment, d.Concurrency)
	}
	if d.TimeoutMinutes == nil || d.TimeoutMinutes.Value != "30" || d.ContinueOnError != "${{ matrix.experimental }}" {
		t.Errorf("timeout/continue = %+v %q", d.TimeoutMinutes, d.ContinueOnError)
	}
	if d.ContainerImage != "node:20" || !reflect.DeepEqual(d.Services, []string{"postgres", "redis"}) || !reflect.DeepEqual(d.Outputs, []string{"a", "z"}) {
		t.Errorf("container/services/outputs = %q %v %v", d.ContainerImage, d.Services, d.Outputs)
	}
	if d.Strategy == nil || d.Strategy.FailFast != "false" || d.Strategy.MaxParallel != "2" || d.Strategy.Matrix.Path() != "jobs.deploy.strategy.matrix" {
		t.Errorf("strategy = %+v", d.Strategy)
	}
	o := jobByID(t, w, "other")
	if o.Environment != "staging" || o.ContainerImage != "golang:1.24" || o.TimeoutMinutes.Value != "${{ inputs.t }}" || o.Strategy != nil || o.Concurrency != nil {
		t.Errorf("other = %+v", o)
	}
}

func TestReusableWorkflowJobs(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	w, diags := parse(t, `on: push
jobs:
  local:
    uses: ./.github/workflows/build.yml
    with:
      target: linux
      debug: true
    secrets: inherit
  remote:
    uses: octo-org/shared/.github/workflows/deploy.yml@`+sha+`
    secrets:
      TOKEN: ${{ secrets.DEPLOY_TOKEN }}
      AWS: ${{ secrets.AWS_KEY }}
  tagged:
    uses: octo-org/shared/.github/workflows/test.yml@v2
`)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %v", diags)
	}
	l := jobByID(t, w, "local")
	if l.Uses == nil || !l.Uses.Local || l.Uses.Path != "./.github/workflows/build.yml" || !l.SecretsInherit || l.SecretNames != nil {
		t.Errorf("local = %+v %+v", l.Uses, l)
	}
	if l.With["target"].Value != "linux" || l.With["debug"].Value != "true" || start(l.Uses.Pos) != at(4, 11) {
		t.Errorf("local with = %+v", l.With)
	}
	r := jobByID(t, w, "remote")
	gotRemote := []any{r.Uses.Owner, r.Uses.Repo, r.Uses.Path, r.Uses.Ref, r.Uses.Kind, r.Uses.Local, r.SecretsInherit, r.SecretNames}
	wantRemote := []any{"octo-org", "shared", ".github/workflows/deploy.yml", sha, RefSHA, false, false, []string{"AWS", "TOKEN"}}
	if !reflect.DeepEqual(gotRemote, wantRemote) {
		t.Errorf("remote = %v, want %v", gotRemote, wantRemote)
	}
	if tg := jobByID(t, w, "tagged"); tg.Uses.Kind != RefMutable || tg.Uses.Ref != "v2" || tg.Steps != nil {
		t.Errorf("tagged = %+v", tg.Uses)
	}
	if !reflect.DeepEqual(w.SecretRefs, []string{"AWS_KEY", "DEPLOY_TOKEN"}) {
		t.Errorf("secret refs = %v", w.SecretRefs)
	}
}

func TestReusableRefForms(t *testing.T) {
	tests := []struct {
		raw  string
		want ReusableRef
	}{
		{"./.github/workflows/a.yml", ReusableRef{Raw: "./.github/workflows/a.yml", Local: true, Path: "./.github/workflows/a.yml"}},
		{"o/r/.github/workflows/a.yml@main", ReusableRef{Raw: "o/r/.github/workflows/a.yml@main", Owner: "o", Repo: "r", Path: ".github/workflows/a.yml", Ref: "main", Kind: RefMutable}},
		{"o/r/.github/workflows/a.yml", ReusableRef{Raw: "o/r/.github/workflows/a.yml", Owner: "o", Repo: "r", Path: ".github/workflows/a.yml"}},
	}
	for _, tt := range tests {
		if got := parseReusableRef(tt.raw, source.Position{}); !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("parseReusableRef(%q) = %+v, want %+v", tt.raw, *got, tt.want)
		}
	}
}

func TestAnchorSharedRunsOn(t *testing.T) {
	w, diags := parse(t, `on: push
jobs:
  a:
    runs-on: &runner ubuntu-24.04
    steps: [{run: a}]
  b:
    runs-on: *runner
    steps: [{run: b}]
`)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %v", diags)
	}
	a, b := jobByID(t, w, "a").RunsOn, jobByID(t, w, "b").RunsOn
	if !reflect.DeepEqual(values(a.Labels), []string{"ubuntu-24.04"}) || !reflect.DeepEqual(values(b.Labels), []string{"ubuntu-24.04"}) {
		t.Errorf("labels = %v %v", values(a.Labels), values(b.Labels))
	}
	if start(a.Labels[0].Pos) != at(4, 14) || start(b.Labels[0].Pos) != at(7, 14) {
		t.Errorf("label positions = %v %v", a.Labels[0].Pos, b.Labels[0].Pos)
	}
}

func TestConditions(t *testing.T) {
	w, diags := parse(t, `on: push
jobs:
  ok:
    if: github.event_name == 'push'
    runs-on: x
    steps:
      - if: ${{ failure() }}
        run: a
      - if: always() && (
        run: b
  bad:
    if: ${{ github.ref == }}
    runs-on: x
    steps: [{run: a}]
  boolean:
    if: false
    runs-on: x
    steps: [{run: a}]
`)
	ok := jobByID(t, w, "ok")
	if ok.If == nil || ok.If.ParseErr != nil || ok.If.Template == nil || len(ok.If.Template.Segments) != 1 {
		t.Fatalf("ok if = %+v", ok.If)
	}
	refs := expr.TemplateReferences(ok.If.Template)
	if len(refs) != 1 || refs[0].Context != "github" {
		t.Errorf("refs = %v", refs)
	}
	if ok.Steps[0].If.ParseErr != nil || ok.Steps[1].If.ParseErr == nil || ok.Steps[1].If.Template != nil {
		t.Errorf("step conditions = %+v %+v", ok.Steps[0].If, ok.Steps[1].If)
	}
	bad := jobByID(t, w, "bad")
	var se *expr.SyntaxError
	if bad.If == nil || !errors.As(bad.If.ParseErr, &se) || bad.If.Raw != "${{ github.ref == }}" {
		t.Errorf("bad if = %+v", bad.If)
	}
	if b := jobByID(t, w, "boolean"); b.If == nil || b.If.Raw != "false" || b.If.ParseErr != nil {
		t.Errorf("boolean if = %+v", b.If)
	}
	if got := codes(diags); !reflect.DeepEqual(got, []string{"MODEL-EXPR-SYNTAX", "MODEL-EXPR-SYNTAX"}) {
		t.Errorf("diagnostics = %v", got)
	}
	if start(diags[1].Pos) != at(12, 9) {
		t.Errorf("diagnostic pos = %v", diags[1].Pos)
	}
	if len(w.Jobs) != 3 {
		t.Errorf("jobs = %d", len(w.Jobs))
	}
}

func TestSecretRefs(t *testing.T) {
	w, _ := parse(t, `on: push
env:
  A: ${{ secrets.npm_token }}
  B: ${{ secrets['Other-Secret'] }} and ${{ secrets.NPM_TOKEN }}
  C: ${{ secrets.GITHUB_TOKEN }}
  D: ${{ secrets[matrix.name] }}
  E: ${{ toJSON(secrets) }}
  F: ${{ broken
  G: secrets.NOT_DYNAMIC
jobs:
  a:
    if: secrets.IN_IF != ''
    runs-on: x
    steps:
      - run: echo "${{ secrets.STEP_SECRET }}"
        env:
          ${{ secrets.KEY_ONLY }}: x
`)
	want := []string{"NPM_TOKEN", "OTHER-SECRET", "STEP_SECRET"}
	if !reflect.DeepEqual(w.SecretRefs, want) {
		t.Errorf("secret refs = %v, want %v", w.SecretRefs, want)
	}
	wantPos := map[string]string{
		"NPM_TOKEN":    "env.A",
		"OTHER-SECRET": "env.B",
		"STEP_SECRET":  "",
	}
	for name, path := range wantPos {
		pos, ok := w.SecretRefPos[name]
		if !ok || !pos.Valid() {
			t.Errorf("secret %s has no position", name)
			continue
		}
		if path != "" && pos.Path != path {
			t.Errorf("secret %s path = %q, want %q", name, pos.Path, path)
		}
	}
	if len(w.SecretRefPos) != len(want) {
		t.Errorf("secret positions = %v", w.SecretRefPos)
	}
	if w.FirstKeyPos.Path != "on" || w.FirstKeyPos.Line != 1 || w.FirstKeyPos.Column != 1 || w.FirstKeyPos.EndColumn != 2 {
		t.Errorf("first key pos = %+v", w.FirstKeyPos)
	}
}

func TestDeterministicParse(t *testing.T) {
	content := `on:
  push: {branches: [main], tags: [v*], paths: [a, b]}
permissions: {contents: read, issues: write, checks: none}
jobs:
  a:
    runs-on: x
    with: {z: 1, a: 2}
    steps: [{run: "${{ secrets.Z }} ${{ secrets.A }}"}]
`
	first, firstDiags := parse(t, content)
	for i := 0; i < 20; i++ {
		w, diags := parse(t, content)
		if !reflect.DeepEqual(w, first) || !reflect.DeepEqual(diags, firstDiags) {
			t.Fatalf("iteration %d differs", i)
		}
	}
}

func corpusFiles(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "corpus")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read corpus: %v", err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	return files
}

func TestCorpusModel(t *testing.T) {
	files := corpusFiles(t)
	if len(files) == 0 {
		t.Skip("testdata/corpus is empty; run scripts/fetch-corpus.sh")
	}
	counts := make(map[string]int)
	jobs := 0
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		doc, err := source.Load(filepath.Base(file), content)
		if err != nil {
			t.Errorf("%s: load: %v", filepath.Base(file), err)
			continue
		}
		w, diags, err := Parse(doc)
		if err != nil {
			t.Errorf("%s: parse: %v", filepath.Base(file), err)
			continue
		}
		jobs += len(w.Jobs)
		for _, d := range diags {
			counts[d.Code]++
			t.Logf("%s: %s %s at %s", filepath.Base(file), d.Code, d.Message, d.Pos)
		}
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("diagnostic %s: %d", k, counts[k])
	}
	t.Logf("corpus files: %d, jobs: %d, diagnostic codes: %d", len(files), jobs, len(keys))
}

func TestVersionHint(t *testing.T) {
	tests := []struct {
		comment string
		want    string
	}{
		{"v4.1.1", "v4.1.1"},
		{"4.2", "4.2"},
		{"v4 pinned by bot", "v4"},
		{"v4\tpinned", "v4"},
		{"v4.1.1-beta", ""},
		{"tag=v4", ""},
		{"pinned", ""},
		{"", ""},
		{"v", ""},
	}
	for _, tt := range tests {
		if got := versionHint(tt.comment); got != tt.want {
			t.Errorf("versionHint(%q) = %q, want %q", tt.comment, got, tt.want)
		}
	}
	w, _ := parse(t, "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: actions/checkout@8e5e7e5ab8b370d6c329ec480221332ada57f0ab # v4.1.1\n      - uses: actions/cache@v4 # 4.0\n      - uses: actions/setup-go@v5\n")
	steps := w.Jobs[0].Steps
	if steps[0].Uses.VersionHint != "v4.1.1" || steps[1].Uses.VersionHint != "4.0" || steps[2].Uses.VersionHint != "" {
		t.Errorf("hints = %q %q %q", steps[0].Uses.VersionHint, steps[1].Uses.VersionHint, steps[2].Uses.VersionHint)
	}
}
