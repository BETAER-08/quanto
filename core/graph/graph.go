package graph

import (
	"math"
	"sort"
	"time"

	"github.com/BETAER-08/quanto/core/model"
)

type Edge struct{ From, To string }

type Graph struct {
	Jobs       []string
	Needs      map[string][]string
	Dependents map[string][]string
	Levels     map[string]int
	Cycles     [][]string
	Unresolved []Edge
}

func Build(w *model.Workflow) *Graph {
	g := &Graph{
		Needs:      make(map[string][]string),
		Dependents: make(map[string][]string),
		Levels:     make(map[string]int),
	}
	if w == nil {
		return g
	}
	known := make(map[string]bool, len(w.Jobs))
	for _, j := range w.Jobs {
		if j == nil || known[j.ID] {
			continue
		}
		known[j.ID] = true
		g.Jobs = append(g.Jobs, j.ID)
	}
	sort.Strings(g.Jobs)
	for _, id := range g.Jobs {
		g.Needs[id] = []string{}
		g.Dependents[id] = []string{}
	}
	seen := make(map[Edge]bool)
	for _, j := range w.Jobs {
		if j == nil {
			continue
		}
		for _, n := range j.Needs {
			e := Edge{From: j.ID, To: n.Value}
			if seen[e] {
				continue
			}
			seen[e] = true
			if !known[n.Value] {
				g.Unresolved = append(g.Unresolved, e)
				continue
			}
			g.Needs[j.ID] = append(g.Needs[j.ID], n.Value)
			g.Dependents[n.Value] = append(g.Dependents[n.Value], j.ID)
		}
	}
	for _, id := range g.Jobs {
		sort.Strings(g.Needs[id])
		sort.Strings(g.Dependents[id])
	}
	sort.Slice(g.Unresolved, func(a, b int) bool {
		if g.Unresolved[a].From != g.Unresolved[b].From {
			return g.Unresolved[a].From < g.Unresolved[b].From
		}
		return g.Unresolved[a].To < g.Unresolved[b].To
	})
	g.Cycles = g.findCycles()
	g.computeLevels()
	return g
}

const (
	white = iota
	gray
	black
)

func (g *Graph) findCycles() [][]string {
	color := make(map[string]int, len(g.Jobs))
	var stack []string
	found := make(map[string]bool)
	var cycles [][]string
	var visit func(id string)
	visit = func(id string) {
		color[id] = gray
		stack = append(stack, id)
		for _, dep := range g.Needs[id] {
			switch color[dep] {
			case white:
				visit(dep)
			case gray:
				start := len(stack) - 1
				for stack[start] != dep {
					start--
				}
				cycle := rotate(stack[start:])
				key := joinKey(cycle)
				if !found[key] {
					found[key] = true
					cycles = append(cycles, cycle)
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
	}
	for _, id := range g.Jobs {
		if color[id] == white {
			visit(id)
		}
	}
	sort.Slice(cycles, func(a, b int) bool { return lessPath(cycles[a], cycles[b]) })
	return cycles
}

func rotate(cycle []string) []string {
	min := 0
	for i, id := range cycle {
		if id < cycle[min] {
			min = i
		}
	}
	out := make([]string, 0, len(cycle))
	out = append(out, cycle[min:]...)
	out = append(out, cycle[:min]...)
	return out
}

func joinKey(path []string) string {
	key := ""
	for _, p := range path {
		key += p + "\x00"
	}
	return key
}

func lessPath(a, b []string) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func (g *Graph) computeLevels() {
	inCycle := make(map[string]bool)
	for _, c := range g.Cycles {
		for _, id := range c {
			inCycle[id] = true
		}
	}
	remaining := make(map[string]int, len(g.Jobs))
	var ready []string
	for _, id := range g.Jobs {
		if inCycle[id] {
			continue
		}
		remaining[id] = len(g.Needs[id])
		if remaining[id] == 0 {
			ready = append(ready, id)
			g.Levels[id] = 0
		}
	}
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		for _, dep := range g.Dependents[id] {
			if inCycle[dep] {
				continue
			}
			if lv := g.Levels[id] + 1; lv > g.Levels[dep] {
				g.Levels[dep] = lv
			}
			remaining[dep]--
			if remaining[dep] == 0 {
				ready = append(ready, dep)
			}
		}
	}
	for _, id := range g.Jobs {
		if !inCycle[id] && remaining[id] > 0 {
			delete(g.Levels, id)
		}
	}
}

func (g *Graph) Depth() int {
	depth := 0
	for _, lv := range g.Levels {
		if lv+1 > depth {
			depth = lv + 1
		}
	}
	return depth
}

func (g *Graph) Width(weights map[string]int) int {
	sums := make(map[int]int)
	width := 0
	for _, id := range g.Jobs {
		lv, ok := g.Levels[id]
		if !ok {
			continue
		}
		w, ok := weights[id]
		if !ok {
			w = 1
		}
		sums[lv] = addSat(sums[lv], w)
		if sums[lv] > width {
			width = sums[lv]
		}
	}
	return width
}

func addSat(a, b int) int {
	if b > 0 && a > math.MaxInt-b {
		return math.MaxInt
	}
	if b < 0 && a < math.MinInt-b {
		return math.MinInt
	}
	return a + b
}

func (g *Graph) ordered() []string {
	out := make([]string, 0, len(g.Levels))
	for _, id := range g.Jobs {
		if _, ok := g.Levels[id]; ok {
			out = append(out, id)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return g.Levels[out[a]] < g.Levels[out[b]] })
	return out
}

func (g *Graph) sink(id string) bool {
	for _, dep := range g.Dependents[id] {
		if _, ok := g.Levels[dep]; ok {
			return false
		}
	}
	return true
}

func (g *Graph) CriticalPath(durations map[string]time.Duration) ([]string, time.Duration, bool) {
	type best struct {
		path  []string
		total time.Duration
	}
	bests := make(map[string]best, len(g.Levels))
	var winner best
	have := false
	for _, id := range g.ordered() {
		var prev best
		chosen := false
		for _, dep := range g.Needs[id] {
			b, ok := bests[dep]
			if !ok {
				continue
			}
			if !chosen || b.total > prev.total || (b.total == prev.total && lessPath(b.path, prev.path)) {
				prev = b
				chosen = true
			}
		}
		path := make([]string, 0, len(prev.path)+1)
		path = append(path, prev.path...)
		path = append(path, id)
		cur := best{path: path, total: prev.total + durations[id]}
		bests[id] = cur
		if !g.sink(id) {
			continue
		}
		if !have || cur.total > winner.total || (cur.total == winner.total && lessPath(cur.path, winner.path)) {
			winner = cur
			have = true
		}
	}
	ok := true
	for _, id := range winner.path {
		if _, found := durations[id]; !found {
			ok = false
		}
	}
	return winner.path, winner.total, ok
}
