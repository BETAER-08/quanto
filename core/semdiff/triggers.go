package semdiff

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/source"
)

const (
	eventSchedule          = "schedule"
	eventPullRequestTarget = "pull_request_target"
	noneText               = "(none)"
)

func triggerIndex(w *model.Workflow) (map[string]model.Trigger, []string) {
	m := make(map[string]model.Trigger, len(w.Triggers))
	var order []string
	for _, t := range w.Triggers {
		if _, dup := m[t.Event]; dup {
			continue
		}
		m[t.Event] = t
		order = append(order, t.Event)
	}
	return m, order
}

func (c *comparer) triggerChanges() {
	bm, border := triggerIndex(c.before.wf)
	am, aorder := triggerIndex(c.after.wf)
	for _, ev := range aorder {
		if ev == eventSchedule {
			continue
		}
		at := am[ev]
		bt, ok := bm[ev]
		if !ok {
			kind := kindTriggerAdded
			if ev == eventPullRequestTarget {
				kind = kindTriggerPRTargetAdded
			}
			c.emit(Finding{Kind: kind, Subject: ev, Pos: at.Pos})
			continue
		}
		c.filterChanges(bt, at)
	}
	for _, ev := range border {
		if ev == eventSchedule {
			continue
		}
		if _, ok := am[ev]; !ok {
			c.emit(Finding{Kind: kindTriggerRemoved, Subject: ev, BasePos: bm[ev].Pos})
		}
	}
	c.scheduleChange(bm, am)
}

func sortedUnique(ps []source.Positioned[string]) []string {
	if len(ps) == 0 {
		return nil
	}
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Value)
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func difference(a, b []string) []string {
	var out []string
	for _, v := range a {
		if _, found := slices.BinarySearch(b, v); !found {
			out = append(out, v)
		}
	}
	return out
}

func (c *comparer) filterChanges(bt, at model.Trigger) {
	keys := make([]string, 0, len(bt.Filters)+len(at.Filters))
	for k := range bt.Filters {
		keys = append(keys, k)
	}
	for k := range at.Filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	keys = slices.Compact(keys)
	for _, k := range keys {
		bv, av := sortedUnique(bt.Filters[k]), sortedUnique(at.Filters[k])
		removed, added := difference(bv, av), difference(av, bv)
		if len(removed) == 0 && len(added) == 0 {
			continue
		}
		c.emit(Finding{
			Kind:    kindTriggerFilterChanged,
			Subject: at.Event,
			Before:  strings.Join(removed, ", "),
			After:   strings.Join(added, ", "),
			Detail:  k,
			Pos:     at.Pos,
		})
	}
}

func (c *comparer) scheduleChange(bm, am map[string]model.Trigger) {
	bt, bok := bm[eventSchedule]
	at, aok := am[eventSchedule]
	bc, ac := sortedUnique(bt.Crons), sortedUnique(at.Crons)
	if slices.Equal(bc, ac) {
		return
	}
	f := Finding{
		Kind:    kindTriggerScheduleChanged,
		Subject: eventSchedule,
		Before:  formatCrons(bc),
		After:   formatCrons(ac),
	}
	switch {
	case aok:
		f.Pos = at.Pos
	case bok:
		f.BasePos = bt.Pos
	}
	c.emit(f)
}

func formatCrons(crons []string) string {
	if len(crons) == 0 {
		return noneText
	}
	parts := make([]string, len(crons))
	for i, e := range crons {
		parts[i] = formatCron(e)
	}
	return strings.Join(parts, ", ")
}

func formatCron(e string) string {
	quoted := "'" + e + "'"
	runs, everyDay, ok := CronRunsPerDay(e)
	switch {
	case !ok:
		return quoted
	case everyDay:
		return fmt.Sprintf("%s (%d runs/day)", quoted, runs)
	}
	return fmt.Sprintf("%s (%d runs on matching days)", quoted, runs)
}

type cronField struct {
	min, max int
}

var cronFields = [5]cronField{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}

func CronRunsPerDay(expr string) (int, bool, bool) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return 0, false, false
	}
	counts := [5]int{}
	for i, f := range fields {
		n, ok := cronFieldCount(f, cronFields[i])
		if !ok {
			return 0, false, false
		}
		counts[i] = n
	}
	everyDay := fields[2] == "*" && fields[3] == "*" && fields[4] == "*"
	return counts[0] * counts[1], everyDay, true
}

func cronFieldCount(f string, r cronField) (int, bool) {
	seen := make(map[int]bool)
	for _, part := range strings.Split(f, ",") {
		base, stepText, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, ok := cronNumber(stepText)
			if !ok || n < 1 {
				return 0, false
			}
			step = n
		}
		lo, hi := r.min, r.max
		switch {
		case base == "*":
		case strings.Contains(base, "-"):
			a, b, _ := strings.Cut(base, "-")
			x, okA := cronNumber(a)
			y, okB := cronNumber(b)
			if !okA || !okB || x > y || x < r.min || y > r.max {
				return 0, false
			}
			lo, hi = x, y
		default:
			if hasStep {
				return 0, false
			}
			x, ok := cronNumber(base)
			if !ok || x < r.min || x > r.max {
				return 0, false
			}
			lo, hi = x, x
		}
		for v := lo; v <= hi; v += step {
			seen[v] = true
		}
	}
	return len(seen), true
}

func cronNumber(s string) (int, bool) {
	if s == "" || len(s) > 4 {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}
