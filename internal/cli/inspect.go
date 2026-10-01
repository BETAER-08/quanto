package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/BETAER-08/quanto/core/matrix"
	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/report"
	"github.com/BETAER-08/quanto/core/semdiff"
	"github.com/BETAER-08/quanto/core/source"
)

type sourceString = source.Positioned[string]

type inspectTrigger struct {
	Event   string              `json:"event"`
	Filters map[string][]string `json:"filters"`
	Crons   []string            `json:"crons"`
	Inputs  []string            `json:"inputs"`
}

type inspectPermission struct {
	Subject  string            `json:"subject"`
	Declared bool              `json:"declared"`
	All      string            `json:"all"`
	Scopes   map[string]string `json:"scopes"`
}

type inspectJob struct {
	ID        string   `json:"id"`
	Instances string   `json:"instances"`
	RunsOn    string   `json:"runs_on"`
	Needs     []string `json:"needs"`
	Uses      string   `json:"uses"`
}

type inspectAction struct {
	Identity string `json:"identity"`
	Ref      string `json:"ref"`
	Category string `json:"category"`
	Pin      string `json:"pin"`
}

type inspectDiagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

type inspectReport struct {
	File        string              `json:"file"`
	Name        string              `json:"name"`
	Triggers    []inspectTrigger    `json:"triggers"`
	Permissions []inspectPermission `json:"permissions"`
	JobsPerRun  string              `json:"jobs_per_run"`
	Depth       int                 `json:"depth"`
	Width       int                 `json:"width"`
	Jobs        []inspectJob        `json:"jobs"`
	Actions     []inspectAction     `json:"actions"`
	Diagnostics []inspectDiagnostic `json:"diagnostics"`
}

func runInspect(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("inspect", stderr)
	format := fs.String("format", "text", "output format: text or json")
	positional, err := parseInterleaved(fs, args)
	if err != nil {
		return parseExit(err)
	}
	if len(positional) != 1 {
		fmt.Fprint(stderr, "quanto: inspect requires exactly one <file>\n"+usageText)
		return exitUsage
	}
	if err := checkFormat(*format, "text", "json"); err != nil {
		fmt.Fprintf(stderr, "quanto: %v\n%s", err, usageText)
		return exitUsage
	}
	path := positional[0]
	content, err := readLimited(path)
	if err != nil {
		fmt.Fprintf(stderr, "quanto: read %s: %v\n", path, err)
		return exitError
	}
	w, diags, err := parseWorkflow(path, content)
	if err != nil {
		fmt.Fprintf(stderr, "quanto: %v\n", err)
		return exitError
	}
	rep := buildInspect(path, w, diags)
	if *format == "json" {
		out, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "quanto: marshal inspect report: %v\n", err)
			return exitError
		}
		return write(stdout, stderr, string(out)+"\n")
	}
	return write(stdout, stderr, renderInspect(rep))
}

func positionedValues[T any](ps []T, value func(T) string) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, value(p))
	}
	return out
}

func buildInspect(path string, w *model.Workflow, diags []model.Diagnostic) inspectReport {
	metrics := semdiff.Compare(semdiff.Input{Path: path, After: w}, semdiff.Options{}).After
	rep := inspectReport{
		File:        path,
		Name:        w.Name,
		Triggers:    []inspectTrigger{},
		Permissions: []inspectPermission{permissionView("workflow", w.Permissions)},
		JobsPerRun:  metrics.JobsPerRun,
		Depth:       metrics.Depth,
		Width:       metrics.Width,
		Jobs:        []inspectJob{},
		Actions:     []inspectAction{},
		Diagnostics: []inspectDiagnostic{},
	}
	for _, t := range w.Triggers {
		it := inspectTrigger{
			Event:   t.Event,
			Filters: map[string][]string{},
			Crons:   []string{},
			Inputs:  []string{},
		}
		for k, vs := range t.Filters {
			it.Filters[k] = positionedValues(vs, func(p sourceString) string { return p.Value })
		}
		it.Crons = append(it.Crons, positionedValues(t.Crons, func(p sourceString) string { return p.Value })...)
		it.Inputs = append(it.Inputs, t.Inputs...)
		rep.Triggers = append(rep.Triggers, it)
	}
	actions := make(map[inspectAction]bool)
	for _, j := range w.Jobs {
		if j == nil {
			continue
		}
		ij := inspectJob{
			ID:        j.ID,
			Instances: instances(j),
			RunsOn:    runnerText(j.RunsOn),
			Needs:     positionedValues(j.Needs, func(p sourceString) string { return p.Value }),
		}
		if j.Uses != nil {
			ij.Uses = j.Uses.Raw
			actions[reusableAction(j.Uses)] = true
		}
		rep.Jobs = append(rep.Jobs, ij)
		if j.Permissions.Declared {
			rep.Permissions = append(rep.Permissions, permissionView("job `"+j.ID+"`", j.Permissions))
		}
		for _, s := range j.Steps {
			if s != nil && s.Uses != nil {
				actions[stepAction(s.Uses)] = true
			}
		}
	}
	for a := range actions {
		rep.Actions = append(rep.Actions, a)
	}
	sort.Slice(rep.Actions, func(i, k int) bool {
		a, b := rep.Actions[i], rep.Actions[k]
		if a.Identity != b.Identity {
			return a.Identity < b.Identity
		}
		if a.Ref != b.Ref {
			return a.Ref < b.Ref
		}
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		return a.Pin < b.Pin
	})
	for _, d := range diags {
		rep.Diagnostics = append(rep.Diagnostics, inspectDiagnostic{Code: d.Code, Message: d.Message, Line: d.Pos.Line, Column: d.Pos.Column})
	}
	return rep
}

func instances(j *model.Job) string {
	var exp *matrix.Expansion
	var err error
	if j.Strategy != nil {
		exp, err = matrix.Expand(j.Strategy.Matrix)
	} else {
		exp, err = matrix.Expand(nil)
	}
	switch {
	case err != nil, exp.Dynamic:
		return "?"
	case !exp.Materialized:
		return "≥" + strconv.Itoa(exp.Count)
	}
	return strconv.Itoa(exp.Count)
}

func runnerText(r model.RunnerSpec) string {
	labels := positionedValues(r.Labels, func(p sourceString) string { return p.Value })
	sort.Strings(labels)
	text := strings.Join(labels, ", ")
	if r.Group != "" {
		text = "group " + r.Group + ": " + text
	}
	if text == "" {
		return "(none)"
	}
	return text
}

func levelText(l model.Level) string {
	switch l {
	case model.LevelRead:
		return "read"
	case model.LevelWrite:
		return "write"
	}
	return "none"
}

func permissionView(subject string, ps model.PermissionSet) inspectPermission {
	p := inspectPermission{Subject: subject, Declared: ps.Declared, All: ps.All, Scopes: map[string]string{}}
	for k, v := range ps.Scopes {
		p.Scopes[k] = levelText(v.Value)
	}
	return p
}

func pinText(k model.RefKind) string {
	switch k {
	case model.RefSHA:
		return "sha"
	case model.RefMutable:
		return "mutable"
	}
	return "unknown"
}

func firstParty(owner string) bool {
	o := strings.ToLower(owner)
	return o == "actions" || o == "github"
}

func stepAction(a *model.ActionRef) inspectAction {
	out := inspectAction{Identity: a.Identity(), Ref: a.Ref, Pin: pinText(a.Kind)}
	switch {
	case a.Local:
		out.Category, out.Pin = "local", ""
	case a.Docker:
		out.Category, out.Pin = "docker", ""
	case a.FirstParty:
		out.Category = "first-party"
	default:
		out.Category = "third-party"
	}
	return out
}

func reusableAction(r *model.ReusableRef) inspectAction {
	if r.Local {
		return inspectAction{Identity: r.Path, Category: "local"}
	}
	id := strings.ToLower(r.Owner + "/" + r.Repo)
	if r.Path != "" {
		id += "/" + strings.ToLower(r.Path)
	}
	out := inspectAction{Identity: id, Ref: r.Ref, Pin: pinText(r.Kind), Category: "third-party"}
	if firstParty(r.Owner) {
		out.Category = "first-party"
	}
	return out
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func permissionText(p inspectPermission) string {
	switch {
	case !p.Declared:
		return "not declared"
	case p.All != "":
		return report.Plain(p.All)
	case len(p.Scopes) == 0:
		return "{}"
	}
	keys := make([]string, 0, len(p.Scopes))
	for k := range p.Scopes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = report.Plain(k) + ": " + p.Scopes[k]
	}
	return strings.Join(parts, ", ")
}

func triggerText(t inspectTrigger) string {
	var parts []string
	keys := make([]string, 0, len(t.Filters))
	for k := range t.Filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, k+": "+plainList(t.Filters[k]))
	}
	for _, c := range t.Crons {
		parts = append(parts, "cron: '"+report.Plain(c)+"'")
	}
	if len(t.Inputs) > 0 {
		parts = append(parts, "inputs: "+plainList(t.Inputs))
	}
	event := report.Plain(t.Event)
	if len(parts) == 0 {
		return event
	}
	return event + " (" + strings.Join(parts, "; ") + ")"
}

func plainList(values []string) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = report.Plain(v)
	}
	return strings.Join(out, ", ")
}

func actionText(a inspectAction) string {
	text := report.Plain(a.Identity)
	if a.Ref != "" {
		text += "@" + report.Plain(a.Ref)
	}
	text += " (" + a.Category
	if a.Pin != "" {
		text += ", " + a.Pin
	}
	return text + ")"
}

func renderInspect(rep inspectReport) string {
	var b strings.Builder
	b.WriteString("File: " + report.Plain(rep.File) + "\n")
	b.WriteString("Name: " + orNone(report.Plain(rep.Name)) + "\n")
	b.WriteString("Triggers:\n")
	if len(rep.Triggers) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, t := range rep.Triggers {
		b.WriteString("  " + triggerText(t) + "\n")
	}
	b.WriteString("Permissions:\n")
	for _, p := range rep.Permissions {
		b.WriteString("  " + report.Plain(p.Subject) + ": " + permissionText(p) + "\n")
	}
	b.WriteString("Jobs per run: " + rep.JobsPerRun + "\n")
	b.WriteString("Longest needs chain: " + strconv.Itoa(rep.Depth) + "\n")
	b.WriteString("Max concurrent jobs: " + strconv.Itoa(rep.Width) + "\n")
	b.WriteString("Jobs:\n")
	if len(rep.Jobs) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, j := range rep.Jobs {
		line := "  " + report.Plain(j.ID) + ": instances: " + j.Instances + ", runs-on: " + report.Plain(j.RunsOn)
		if len(j.Needs) > 0 {
			line += ", needs: " + plainList(j.Needs)
		}
		if j.Uses != "" {
			line += ", uses: " + report.Plain(j.Uses)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("Actions:\n")
	if len(rep.Actions) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, a := range rep.Actions {
		b.WriteString("  " + actionText(a) + "\n")
	}
	b.WriteString("Diagnostics:\n")
	if len(rep.Diagnostics) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, d := range rep.Diagnostics {
		b.WriteString(fmt.Sprintf("  %d:%d %s %s\n", d.Line, d.Column, d.Code, report.Plain(d.Message)))
	}
	return b.String()
}
