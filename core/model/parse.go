package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BETAER-08/quanto/core/expr"
	"github.com/BETAER-08/quanto/core/source"
)

var filterKeys = []string{"branches", "branches-ignore", "tags", "tags-ignore", "paths", "paths-ignore", "types", "workflows"}

type parser struct {
	diags []Diagnostic
}

func (p *parser) diag(code string, pos source.Position, format string, args ...any) {
	p.diags = append(p.diags, Diagnostic{Code: code, Message: fmt.Sprintf(format, args...), Pos: pos})
}

func Parse(doc *source.Document) (*Workflow, []Diagnostic, error) {
	if doc.Empty() {
		return nil, nil, ErrNotWorkflow
	}
	root := doc.Root()
	if root.Kind() != source.KindMapping {
		return nil, nil, ErrNotWorkflow
	}
	p := &parser{}
	w := &Workflow{
		File: doc.File,
		Name: root.Field("name").StrOr(""),
		Pos:  root.Pos(),
	}
	on := root.Field("on")
	if !on.Exists() || on.IsNull() {
		p.diag(codeNoOn, root.Pos(), "workflow has no `on` triggers")
	}
	w.Triggers = p.triggers(on)
	w.Permissions = p.permissions(root.Field("permissions"))
	w.Concurrency = concurrency(root.Field("concurrency"))
	w.EnvKeys = sortedKeys(root.Field("env"))
	w.SecretRefs = secretRefs(root)
	jobs := root.Field("jobs")
	if jobs.Kind() != source.KindMapping || jobs.Len() == 0 {
		pos := root.Pos()
		if jobs.Exists() {
			pos = jobs.Pos()
		}
		p.diag(codeNoJobs, pos, "workflow has no jobs")
	}
	for _, f := range jobs.Fields() {
		if f.Value.Kind() != source.KindMapping {
			p.diag(codeJobNotMapping, f.Value.Pos(), "job %q is not a mapping", f.Name)
			continue
		}
		w.Jobs = append(w.Jobs, p.job(f))
	}
	return w, p.diags, nil
}

func sortedKeys(n *source.Node) []string {
	if n.Kind() != source.KindMapping {
		return nil
	}
	keys := n.Keys()
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	return keys
}

func rawString(n *source.Node) string {
	return n.StrOr("")
}

func (p *parser) triggers(on *source.Node) []Trigger {
	switch on.Kind() {
	case source.KindScalar:
		if s, ok := on.Str(); ok {
			return []Trigger{{Event: s, Pos: on.Pos()}}
		}
	case source.KindSequence:
		var out []Trigger
		for _, item := range on.Items() {
			if s, ok := item.Str(); ok {
				out = append(out, Trigger{Event: s, Pos: item.Pos()})
			}
		}
		return out
	case source.KindMapping:
		var out []Trigger
		for _, f := range on.Fields() {
			out = append(out, trigger(f))
		}
		return out
	}
	return nil
}

func trigger(f source.Field) Trigger {
	t := Trigger{Event: f.Name, Pos: f.Key.Pos()}
	v := f.Value
	if f.Name == "schedule" {
		for _, item := range v.Items() {
			if c, ok := item.Field("cron").PosStr(); ok {
				t.Crons = append(t.Crons, c)
			}
		}
	}
	if v.Kind() != source.KindMapping {
		return t
	}
	for _, key := range filterKeys {
		if !v.Has(key) {
			continue
		}
		if t.Filters == nil {
			t.Filters = make(map[string][]source.Positioned[string])
		}
		t.Filters[key] = v.Field(key).StrList()
	}
	if f.Name == "workflow_dispatch" || f.Name == "workflow_call" {
		t.Inputs = sortedKeys(v.Field("inputs"))
	}
	return t
}

func (p *parser) permissions(n *source.Node) PermissionSet {
	if !n.Exists() {
		return PermissionSet{}
	}
	ps := PermissionSet{Declared: true, Pos: n.Pos()}
	switch n.Kind() {
	case source.KindMapping:
		ps.Scopes = make(map[string]source.Positioned[Level])
		for _, f := range n.Fields() {
			raw, _ := f.Value.Str()
			lvl, ok := parseLevel(raw)
			if !ok {
				p.diag(codeUnknownPermissionLevel, f.Value.Pos(), "unknown permission level %q for scope %q", raw, f.Name)
				continue
			}
			ps.Scopes[f.Name] = source.At(lvl, f.Value.Pos())
		}
	case source.KindScalar:
		if n.IsNull() {
			ps.Scopes = make(map[string]source.Positioned[Level])
			break
		}
		raw, _ := n.Str()
		if raw == "read-all" || raw == "write-all" {
			ps.All = raw
			break
		}
		p.diag(codeUnknownPermissionLevel, n.Pos(), "unknown permissions value %q", raw)
	default:
		p.diag(codeUnknownPermissionLevel, n.Pos(), "permissions must be a mapping or read-all/write-all")
	}
	return ps
}

func parseLevel(s string) (Level, bool) {
	switch s {
	case "none":
		return LevelNone, true
	case "read":
		return LevelRead, true
	case "write":
		return LevelWrite, true
	}
	return LevelNone, false
}

func concurrency(n *source.Node) *Concurrency {
	switch n.Kind() {
	case source.KindScalar:
		if n.IsNull() {
			return nil
		}
		return &Concurrency{Group: rawString(n), Pos: n.Pos()}
	case source.KindMapping:
		return &Concurrency{
			Group:            rawString(n.Field("group")),
			CancelInProgress: rawString(n.Field("cancel-in-progress")),
			Pos:              n.Pos(),
		}
	}
	return nil
}

func (p *parser) condition(n *source.Node) *Condition {
	raw, ok := n.Str()
	if !ok {
		return nil
	}
	c := &Condition{Raw: raw, Pos: n.Pos()}
	tpl, err := expr.ParseCondition(raw)
	if err != nil {
		c.ParseErr = err
		p.diag(codeExprSyntax, n.Pos(), "invalid expression in `if`: %v", err)
		return c
	}
	c.Template = tpl
	return c
}

func runsOn(n *source.Node) RunnerSpec {
	rs := RunnerSpec{Pos: n.Pos()}
	switch n.Kind() {
	case source.KindScalar, source.KindSequence:
		rs.Labels = n.StrList()
	case source.KindMapping:
		rs.Group = rawString(n.Field("group"))
		rs.Labels = n.Field("labels").StrList()
	}
	if expr.IsDynamic(rs.Group) {
		rs.Dynamic = true
	}
	for _, l := range rs.Labels {
		if expr.IsDynamic(l.Value) {
			rs.Dynamic = true
		}
	}
	return rs
}

func strategy(n *source.Node) *Strategy {
	if !n.Exists() || n.IsNull() {
		return nil
	}
	s := &Strategy{Pos: n.Pos()}
	if n.Kind() == source.KindMapping {
		s.Matrix = n.Field("matrix")
		s.FailFast = rawString(n.Field("fail-fast"))
		s.MaxParallel = rawString(n.Field("max-parallel"))
	}
	return s
}

func environment(n *source.Node) string {
	if n.Kind() == source.KindMapping {
		return rawString(n.Field("name"))
	}
	return rawString(n)
}

func containerImage(n *source.Node) string {
	if n.Kind() == source.KindMapping {
		return rawString(n.Field("image"))
	}
	return rawString(n)
}

func withMap(n *source.Node) map[string]source.Positioned[string] {
	if n.Kind() != source.KindMapping {
		return nil
	}
	out := make(map[string]source.Positioned[string])
	for _, f := range n.Fields() {
		if v, ok := f.Value.PosStr(); ok {
			out[f.Name] = v
		}
	}
	return out
}

func (p *parser) job(f source.Field) *Job {
	n := f.Value
	j := &Job{ID: f.Name, Pos: f.Key.Pos()}
	if name, ok := n.Field("name").PosStr(); ok {
		j.Name = name
	}
	j.Needs = n.Field("needs").StrList()
	if n.Has("if") {
		j.If = p.condition(n.Field("if"))
	}
	if n.Has("runs-on") {
		j.RunsOn = runsOn(n.Field("runs-on"))
	}
	j.Strategy = strategy(n.Field("strategy"))
	j.Permissions = p.permissions(n.Field("permissions"))
	j.Environment = environment(n.Field("environment"))
	j.Concurrency = concurrency(n.Field("concurrency"))
	if t, ok := n.Field("timeout-minutes").PosStr(); ok {
		j.TimeoutMinutes = &t
	}
	j.ContinueOnError = rawString(n.Field("continue-on-error"))
	j.ContainerImage = containerImage(n.Field("container"))
	j.Services = sortedKeys(n.Field("services"))
	if uses, ok := n.Field("uses").Str(); ok {
		j.Uses = parseReusableRef(uses, n.Field("uses").Pos())
	}
	j.With = withMap(n.Field("with"))
	secrets := n.Field("secrets")
	if s, ok := secrets.Str(); ok && s == "inherit" {
		j.SecretsInherit = true
	}
	j.SecretNames = sortedKeys(secrets)
	j.Outputs = sortedKeys(n.Field("outputs"))
	steps := n.Field("steps")
	for i, item := range steps.Items() {
		if item.Kind() != source.KindMapping {
			p.diag(codeStepNotMapping, item.Pos(), "step %d of job %q is not a mapping", i, f.Name)
			continue
		}
		j.Steps = append(j.Steps, p.step(f.Name, i, item))
	}
	return j
}

func (p *parser) step(jobID string, index int, n *source.Node) *Step {
	s := &Step{
		Index:            index,
		ID:               rawString(n.Field("id")),
		Name:             rawString(n.Field("name")),
		With:             withMap(n.Field("with")),
		EnvKeys:          sortedKeys(n.Field("env")),
		Shell:            rawString(n.Field("shell")),
		WorkingDirectory: rawString(n.Field("working-directory")),
		Pos:              n.Pos(),
	}
	if n.Has("if") {
		s.If = p.condition(n.Field("if"))
	}
	usesNode := n.Field("uses")
	if raw, ok := usesNode.Str(); ok {
		ref, hasRef := parseActionRef(raw, usesNode.Pos())
		if !hasRef {
			p.diag(codeActionNoRef, usesNode.Pos(), "action %q has no @ref", raw)
		}
		s.Uses = ref
	}
	if run, ok := n.Field("run").PosStr(); ok {
		s.Run = &run
	}
	if s.Uses == nil && s.Run == nil {
		p.diag(codeStepNoAction, n.Pos(), "step %d of job %q has neither `uses` nor `run`", index, jobID)
	}
	return s
}

func secretRefs(root *source.Node) []string {
	seen := make(map[string]bool)
	root.Walk(func(n *source.Node) bool {
		if n.Kind() != source.KindScalar {
			return true
		}
		s, ok := n.Str()
		if !ok || !expr.IsDynamic(s) {
			return true
		}
		tpl, err := expr.ParseTemplate(s)
		if err != nil {
			return true
		}
		for _, ref := range expr.TemplateReferences(tpl) {
			if ref.Context != "secrets" || len(ref.Path) == 0 {
				continue
			}
			name := ref.Path[0]
			if name == "*" || name == "github_token" {
				continue
			}
			seen[strings.ToUpper(name)] = true
		}
		return true
	})
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
