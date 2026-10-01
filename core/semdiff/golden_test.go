package semdiff

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "update golden files")

const goldenPath = ".github/workflows/ci.yml"

type sample struct {
	avg time.Duration
	n   int
}

type fakeDurations map[string]sample

func (f fakeDurations) JobAverage(workflowPath, jobKey string) (time.Duration, int, bool) {
	if workflowPath != goldenPath {
		return 0, 0, false
	}
	s, ok := f[jobKey]
	if !ok {
		return 0, 0, false
	}
	return s.avg, s.n, true
}

var goldenDurations = map[string]DurationSource{
	"estimate-with-history": fakeDurations{
		"build": {avg: 5 * time.Minute, n: 10},
		"Test":  {avg: 10 * time.Minute, n: 12},
	},
	"estimate-insufficient": fakeDurations{
		"build": {avg: 5 * time.Minute, n: 10},
		"Test":  {avg: 10 * time.Minute, n: 4},
	},
}

var goldenKinds = map[string][]string{
	"identical":                     {},
	"reformatted":                   {},
	"matrix-axis-added":             {kindMatrixCountChanged, kindGraphWidthChanged},
	"matrix-include-docs":           {kindMatrixCountChanged, kindGraphWidthChanged},
	"matrix-exclude":                {kindMatrixCountChanged, kindGraphWidthChanged},
	"matrix-dynamic":                {kindMatrixDynamic},
	"matrix-over-limit":             {kindMatrixOverLimit, kindGraphWidthChanged},
	"permissions-broadened":         {kindPermissionsBroadened},
	"permissions-removed":           {kindPermissionsRemoved},
	"permissions-write-all":         {kindPermissionsWriteAll},
	"third-party-action-mutable":    {kindActionThirdPartyAdded},
	"action-pin-removed":            {kindActionPinRemoved},
	"action-major-bump":             {kindActionRefChanged},
	"schedule-added":                {kindTriggerScheduleChanged},
	"pull-request-target-added":     {kindTriggerPRTargetAdded},
	"job-renamed":                   {kindJobRenamed},
	"job-added-depth":               {kindJobAdded, kindGraphDepthChanged},
	"needs-cycle":                   {kindGraphCycle},
	"secrets-new-and-inherit":       {kindSecretsInheritAdded, kindSecretsAdded},
	"runner-macos-added":            {kindJobRunnerChanged},
	"workflow-added":                {kindWorkflowAdded},
	"workflow-removed":              {kindWorkflowRemoved},
	"head-unparseable":              {kindWorkflowUnanalyzable},
	"anchor-shared-change":          {kindJobRunnerChanged, kindJobRunnerChanged},
	"estimate-with-history":         {kindMatrixCountChanged, kindEstimateChanged, kindGraphWidthChanged},
	"estimate-insufficient":         {kindMatrixCountChanged, kindGraphWidthChanged},
	"permissions-job-write-added":   {kindPermissionsBroadened},
	"permissions-job-write-removed": {kindPermissionsNarrowed},
	"permissions-new-job-write-all": {kindPermissionsWriteAll, kindJobAdded, kindGraphWidthChanged},
	"permissions-release-split":     {kindPermissionsNarrowed, kindPermissionsNarrowed},
}

func loadGoldenSide(t *testing.T, dir, name string) (*Input, bool) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	w, perr := parseWorkflow(goldenPath, content)
	return &Input{After: w, AfterErr: perr}, true
}

func goldenInput(t *testing.T, dir string) Input {
	t.Helper()
	in := Input{Path: goldenPath}
	if b, ok := loadGoldenSide(t, dir, "before.yml"); ok {
		in.Before, in.BeforeErr = b.After, b.AfterErr
	}
	if a, ok := loadGoldenSide(t, dir, "after.yml"); ok {
		in.After, in.AfterErr = a.After, a.AfterErr
	}
	return in
}

func marshalDiff(t *testing.T, d *FileDiff) []byte {
	t.Helper()
	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return append(out, '\n')
}

func goldenCases(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "testdata", "golden", "semdiff")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read golden dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func TestGoldenCasesComplete(t *testing.T) {
	got := goldenCases(t)
	var want []string
	for name := range goldenKinds {
		want = append(want, name)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("golden cases = %v, want %v", got, want)
	}
}

func TestGolden(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "golden", "semdiff")
	for _, name := range goldenCases(t) {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(root, name)
			opts := Options{Durations: goldenDurations[name]}
			d := Compare(goldenInput(t, dir), opts)
			got := marshalDiff(t, d)
			again := marshalDiff(t, Compare(goldenInput(t, dir), opts))
			if !bytes.Equal(got, again) {
				t.Fatalf("non-deterministic output")
			}
			in := goldenInput(t, dir)
			if in.Before != nil && in.After != nil {
				checkPermissionCoverage(t, in.Before, in.After, d)
			}
			if want, ok := goldenKinds[name]; ok {
				kinds := kindList(d)
				if len(want) != len(kinds) || (len(want) > 0 && !reflect.DeepEqual(kinds, want)) {
					t.Errorf("kinds = %v, want %v", kinds, want)
				}
			}
			path := filepath.Join(dir, "expected.json")
			if *update {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("mismatch with %s\ngot:\n%s", path, got)
			}
		})
	}
}
