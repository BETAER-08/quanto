package semdiff

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/BETAER-08/quanto/core/graph"
	"github.com/BETAER-08/quanto/core/matrix"
	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/source"
)

type jobInfo struct {
	job          *model.Job
	count        int
	dynamic      bool
	invalid      bool
	materialized bool
	matrixPos    source.Position
}

func (j *jobInfo) unknown() bool {
	return j.dynamic || j.invalid
}

func (j *jobInfo) weight() int {
	if j.unknown() {
		return 1
	}
	return j.count
}

func (j *jobInfo) countText() string {
	if j.unknown() {
		return "?"
	}
	return strconv.Itoa(j.count)
}

type side struct {
	wf        *model.Workflow
	path      string
	order     []string
	jobs      map[string]*jobInfo
	graph     *graph.Graph
	metrics   Metrics
	unknown   bool
	estimable bool
	minutes   float64
	samples   int
}

func analyze(w *model.Workflow, path string, opts Options) *side {
	s := &side{wf: w, path: path, jobs: make(map[string]*jobInfo)}
	for _, j := range w.Jobs {
		if j == nil {
			continue
		}
		if _, dup := s.jobs[j.ID]; dup {
			continue
		}
		info := expandJob(j)
		s.jobs[j.ID] = info
		s.order = append(s.order, j.ID)
		if info.unknown() {
			s.unknown = true
		}
	}
	s.graph = graph.Build(w)
	weights := make(map[string]int, len(s.jobs))
	for id, info := range s.jobs {
		weights[id] = info.weight()
	}
	s.metrics.Depth = s.graph.Depth()
	s.metrics.Width = s.boundText(s.graph.Width(weights))
	s.metrics.JobsPerRun = s.jobsPerRun()
	s.estimate(opts)
	return s
}

func expandJob(j *model.Job) *jobInfo {
	info := &jobInfo{job: j}
	var node *source.Node
	if j.Strategy != nil {
		node = j.Strategy.Matrix
	}
	info.matrixPos = node.Pos()
	exp, err := matrix.Expand(node)
	if err != nil {
		info.invalid = true
		return info
	}
	info.dynamic = exp.Dynamic
	info.count = exp.Count
	info.materialized = exp.Materialized
	return info
}

func (s *side) jobsPerRun() string {
	total := 0
	for _, id := range s.order {
		total = addSat(total, s.jobs[id].count)
	}
	return s.boundText(total)
}

func (s *side) boundText(n int) string {
	if s.unknown {
		return "?"
	}
	for _, id := range s.order {
		if !s.jobs[id].materialized {
			return "≥" + strconv.Itoa(n)
		}
	}
	return strconv.Itoa(n)
}

func addSat(a, b int) int {
	if b > 0 && a > math.MaxInt-b {
		return math.MaxInt
	}
	return a + b
}

func (s *side) estimate(opts Options) {
	if opts.Durations == nil || s.unknown || len(s.order) == 0 {
		return
	}
	total := 0.0
	minSamples := -1
	for _, id := range s.order {
		info := s.jobs[id]
		if !info.materialized {
			return
		}
		avg, n, ok := opts.Durations.JobAverage(s.path, JobKey(info.job))
		if !ok || n < opts.MinSamples {
			return
		}
		total += float64(info.count) * billableMinutes(avg)
		if minSamples < 0 || n < minSamples {
			minSamples = n
		}
	}
	s.estimable = true
	s.minutes = total
	s.samples = minSamples
	s.metrics.RunnerMinutes = formatMinutes(total)
}

func billableMinutes(avg time.Duration) float64 {
	if avg <= 0 {
		return 0
	}
	return math.Ceil(avg.Seconds() / 60)
}

func formatMinutes(m float64) string {
	return fmt.Sprintf("%.0f", m)
}

func (c *comparer) estimateChange() {
	b, a := c.before, c.after
	if !b.estimable || !a.estimable {
		return
	}
	samples := b.samples
	if a.samples < samples {
		samples = a.samples
	}
	c.diff.Estimate = &Estimate{MinutesBefore: b.minutes, MinutesAfter: a.minutes, Samples: samples}
	before, after := formatMinutes(b.minutes), formatMinutes(a.minutes)
	if before == after {
		return
	}
	sig := Normal
	switch {
	case b.minutes == 0:
		sig = High
	case math.Abs(a.minutes-b.minutes)/b.minutes >= 0.5:
		sig = High
	}
	c.emitSig(Finding{
		Kind:   kindEstimateChanged,
		Before: before,
		After:  after,
		Detail: strconv.Itoa(samples),
		Pos:    a.wf.JobsPos,
	}, sig)
}

func cycleKey(cycle []string) string {
	key := ""
	for _, id := range cycle {
		key += id + "\x00"
	}
	return key
}

func (c *comparer) graphChanges() {
	b, a := c.before, c.after
	seenCycles := make(map[string]bool, len(b.graph.Cycles))
	for _, cyc := range b.graph.Cycles {
		seenCycles[cycleKey(cyc)] = true
	}
	for _, cyc := range a.graph.Cycles {
		if seenCycles[cycleKey(cyc)] || len(cyc) == 0 {
			continue
		}
		detail := ""
		for _, id := range cyc {
			detail += id + " → "
		}
		detail += cyc[0]
		c.emit(Finding{Kind: kindGraphCycle, Subject: cyc[0], Detail: detail, Pos: a.wf.JobsPos})
	}
	seenEdges := make(map[graph.Edge]bool, len(b.graph.Unresolved))
	for _, e := range b.graph.Unresolved {
		seenEdges[e] = true
	}
	for _, e := range a.graph.Unresolved {
		if seenEdges[e] {
			continue
		}
		c.emit(Finding{Kind: kindGraphUnresolved, Subject: e.From, After: e.To, Pos: a.needsPos(e)})
	}
	if len(b.graph.Cycles) > 0 || len(a.graph.Cycles) > 0 {
		return
	}
	if b.metrics.Depth != a.metrics.Depth {
		c.emit(Finding{
			Kind:   kindGraphDepthChanged,
			Before: strconv.Itoa(b.metrics.Depth),
			After:  strconv.Itoa(a.metrics.Depth),
			Pos:    a.wf.JobsPos,
		})
	}
	if b.unknown || a.unknown || c.widthFromMatrixOnly() {
		return
	}
	if b.metrics.Width != a.metrics.Width {
		c.emit(Finding{
			Kind:   kindGraphWidthChanged,
			Before: b.metrics.Width,
			After:  a.metrics.Width,
			Pos:    a.wf.JobsPos,
		})
	}
}

func (c *comparer) widthFromMatrixOnly() bool {
	matrixFinding := false
	for _, f := range c.diff.Findings {
		switch f.Kind {
		case kindMatrixCountChanged, kindMatrixOverLimit, kindMatrixDynamic:
			matrixFinding = true
		}
	}
	if !matrixFinding {
		return false
	}
	bg, ag := c.before.graph, c.after.graph
	if !slices.Equal(bg.Jobs, ag.Jobs) {
		return false
	}
	for _, id := range bg.Jobs {
		if !slices.Equal(bg.Needs[id], ag.Needs[id]) {
			return false
		}
	}
	return true
}

func (s *side) needsPos(e graph.Edge) source.Position {
	info, ok := s.jobs[e.From]
	if !ok {
		return source.Position{}
	}
	for _, n := range info.job.Needs {
		if n.Value == e.To {
			return n.Pos
		}
	}
	return info.job.Pos
}
