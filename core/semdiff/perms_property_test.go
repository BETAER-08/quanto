package semdiff

import (
	"testing"

	"github.com/BETAER-08/quanto/core/model"
)

func propLevel(ps model.PermissionSet, scope string) model.Level {
	switch ps.All {
	case "read-all":
		return model.LevelRead
	case "write-all":
		return model.LevelWrite
	}
	return ps.Scopes[scope].Value
}

func propEffective(j *model.Job, w *model.Workflow) model.PermissionSet {
	if j.Permissions.Declared {
		return j.Permissions
	}
	return w.Permissions
}

func propScopes(sets ...model.PermissionSet) map[string]bool {
	out := make(map[string]bool)
	for _, s := range officialScopes {
		out[s] = true
	}
	for _, ps := range sets {
		for s := range ps.Scopes {
			out[s] = true
		}
	}
	return out
}

func covered(d *FileDiff, subject, scope string) bool {
	for _, f := range d.Findings {
		if f.Subject != subject && f.Subject != "workflow" {
			continue
		}
		if f.Kind == kindPermissionsWriteAll || (f.Kind == kindPermissionsBroadened && f.Detail == scope) {
			return true
		}
	}
	return false
}

func checkPermissionCoverage(t *testing.T, before, after *model.Workflow, d *FileDiff) {
	t.Helper()
	renamedFrom := make(map[string]string)
	for _, f := range d.Findings {
		if f.Kind == kindJobRenamed {
			renamedFrom[f.After] = f.Before
		}
	}
	beforeJobs := make(map[string]*model.Job)
	for _, j := range before.Jobs {
		if j != nil {
			if _, dup := beforeJobs[j.ID]; !dup {
				beforeJobs[j.ID] = j
			}
		}
	}
	seen := make(map[string]bool)
	for _, aj := range after.Jobs {
		if aj == nil || seen[aj.ID] {
			continue
		}
		seen[aj.ID] = true
		ea := propEffective(aj, after)
		if !ea.Declared {
			continue
		}
		subject := "job `" + aj.ID + "`"
		bid := aj.ID
		if from, ok := renamedFrom[aj.ID]; ok {
			bid = from
		}
		bj, paired := beforeJobs[bid]
		if !paired {
			if !aj.Permissions.Declared {
				continue
			}
			for s := range propScopes(ea) {
				if propLevel(ea, s) == model.LevelWrite && !covered(d, subject, s) {
					t.Errorf("added job %s scope %s is write without a covering finding: %v", aj.ID, s, kindList(d))
				}
			}
			continue
		}
		eb := propEffective(bj, before)
		if !eb.Declared {
			continue
		}
		for s := range propScopes(eb, ea) {
			if propLevel(ea, s) > propLevel(eb, s) && !covered(d, subject, s) {
				t.Errorf("job %s scope %s rose without a covering finding: %v", aj.ID, s, kindList(d))
			}
		}
	}
}

func TestPermissionCoverageCases(t *testing.T) {
	cases := [][2]string{
		{"on: push\npermissions: {}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n", "on: push\npermissions: {}\njobs:\n  a: {runs-on: x, permissions: {contents: write}, steps: [{run: a}]}\n"},
		{"on: push\npermissions: {contents: read}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n", "on: push\npermissions: {contents: write}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n  b: {runs-on: x, steps: [{run: b}]}\n"},
		{"on: push\npermissions: read-all\njobs:\n  a: {runs-on: x, permissions: {contents: read}, steps: [{run: a}]}\n", "on: push\npermissions: write-all\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n"},
		{"on: push\npermissions: {}\njobs:\n  a: {runs-on: x, permissions: {issues: write}, steps: [{run: a}]}\n", "on: push\npermissions: {issues: write}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n"},
		{"on: push\npermissions: {}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n", "on: push\npermissions: {}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n  b: {runs-on: x, permissions: write-all, steps: [{run: b}]}\n"},
	}
	for i, c := range cases {
		bw, aw := mustParse(t, c[0]), mustParse(t, c[1])
		d := Compare(Input{Path: "wf.yml", Before: bw, After: aw}, Options{})
		checkPermissionCoverage(t, bw, aw, d)
		if len(d.Findings) == 0 {
			t.Errorf("case %d: no findings", i)
		}
	}
}

func TestPermissionInheritanceNotDuplicated(t *testing.T) {
	before := "on: push\npermissions: {contents: read}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n  b: {runs-on: x, steps: [{run: b}]}\n"
	after := "on: push\npermissions: {contents: write}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n  b: {runs-on: x, steps: [{run: b}]}\n"
	d := diff(t, before, after)
	if got := findingsOf(d, kindPermissionsBroadened); len(got) != 1 || got[0].Subject != "workflow" {
		t.Errorf("broadened = %+v", d.Findings)
	}
	removedAll := diff(t, "on: push\npermissions: {contents: read}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n", "on: push\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n")
	if got := findingsOf(removedAll, kindPermissionsRemoved); len(got) != 1 || got[0].Detail != repositoryDefault {
		t.Errorf("removed all = %+v", removedAll.Findings)
	}
	removedPartial := diff(t, "on: push\npermissions: {contents: read}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n  b: {runs-on: x, permissions: {issues: write}, steps: [{run: b}]}\n", "on: push\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n  b: {runs-on: x, permissions: {issues: write}, steps: [{run: b}]}\n")
	if got := findingsOf(removedPartial, kindPermissionsRemoved); len(got) != 1 || got[0].Subject != "workflow" || got[0].Detail != "" {
		t.Errorf("removed partial = %+v", removedPartial.Findings)
	}
	newInherit := diff(t, "on: push\npermissions: {contents: write}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n", "on: push\npermissions: {contents: write}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n  c: {runs-on: x, steps: [{run: c}]}\n")
	if n := len(findingsOf(newInherit, kindPermissionsBroadened)) + len(findingsOf(newInherit, kindPermissionsWriteAll)); n != 0 {
		t.Errorf("new inheriting job = %+v", newInherit.Findings)
	}
	newInheritAll := diff(t, "on: push\npermissions: write-all\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n", "on: push\npermissions: write-all\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n  c: {runs-on: x, steps: [{run: c}]}\n")
	if n := len(findingsOf(newInheritAll, kindPermissionsBroadened)) + len(findingsOf(newInheritAll, kindPermissionsWriteAll)); n != 0 {
		t.Errorf("new job inheriting write-all = %+v", newInheritAll.Findings)
	}
	newDeclared := diff(t, "on: push\npermissions: {contents: write}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n", "on: push\npermissions: {contents: write}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n  c: {runs-on: x, permissions: {contents: write, issues: read}, steps: [{run: c}]}\n")
	got := findingsOf(newDeclared, kindPermissionsBroadened)
	if len(got) != 1 || got[0].Subject != "job `c`" || got[0].Before != newJobLevel || got[0].Detail != "contents" {
		t.Errorf("new declaring job = %+v", newDeclared.Findings)
	}
	newDeclaredRead := diff(t, "on: push\npermissions: {}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n", "on: push\npermissions: {}\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n  c: {runs-on: x, permissions: read-all, steps: [{run: c}]}\n")
	if n := len(findingsOf(newDeclaredRead, kindPermissionsBroadened)) + len(findingsOf(newDeclaredRead, kindPermissionsWriteAll)); n != 0 {
		t.Errorf("new read-all job = %+v", newDeclaredRead.Findings)
	}
	unknown := diff(t, "on: push\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n", "on: push\njobs:\n  a: {runs-on: x, steps: [{run: a}]}\n  c: {runs-on: x, steps: [{run: c}]}\n")
	if n := len(findingsOf(unknown, kindPermissionsBroadened)); n != 0 {
		t.Errorf("unknown new job = %+v", unknown.Findings)
	}
}
