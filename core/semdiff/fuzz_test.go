package semdiff

import (
	"encoding/json"
	"testing"
)

func FuzzCompare(f *testing.F) {
	seeds := [][2]string{
		{"on: push\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n", "on: [push, pull_request_target]\njobs:\n  b: {runs-on: y, needs: [a, ghost], steps: [{run: a}]}\n"},
		{"on: {schedule: [{cron: '0 * * * *'}]}\npermissions: read-all\njobs:\n  a:\n    runs-on: x\n    strategy: {matrix: {os: [a, b], include: [{os: c}]}}\n    steps: [{uses: actions/checkout@v4}]\n", "on: {schedule: [{cron: '*/5 * * * 1-5'}]}\npermissions: write-all\njobs:\n  a:\n    runs-on: x\n    strategy: {matrix: '${{ fromJSON(x) }}'}\n    steps: [{uses: org/x@main}]\n"},
		{"jobs:\n  a: {needs: b}\n  b: {needs: a}\n", "jobs:\n  c: {uses: ./.github/workflows/x.yml, secrets: inherit}\n"},
		{"", "[\n"},
		{"a: &x [1, 2]\nb: *x\n", "on: push\njobs: {}\n"},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, before, after string) {
		bw, berr := parseWorkflow("before.yml", []byte(before))
		aw, aerr := parseWorkflow("after.yml", []byte(after))
		d := Compare(Input{Path: "after.yml", Before: bw, After: aw, BeforeErr: berr, AfterErr: aerr}, Options{})
		if d == nil || d.Findings == nil {
			t.Fatal("nil diff or findings")
		}
		if _, err := json.Marshal(d); err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if berr == nil && bw != nil {
			self := Compare(Input{Path: "before.yml", Before: bw, After: bw}, Options{})
			if len(self.Findings) != 0 {
				t.Fatalf("Compare(a, a) findings = %v", kindList(self))
			}
		}
	})
}
