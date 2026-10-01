package report

import (
	"strings"

	"github.com/BETAER-08/quanto/core/semdiff"
)

func code(s string) string {
	return "`" + s + "`"
}

func Message(f semdiff.Finding) string {
	s, b, a, d := f.Subject, f.Before, f.After, f.Detail
	switch f.Kind {
	case "workflow.added":
		return "Workflow added"
	case "workflow.removed":
		return "Workflow removed"
	case "workflow.renamed":
		return "Workflow renamed from " + code(b)
	case "workflow.unanalyzable":
		return "Could not analyze: " + d
	case "trigger.added":
		return "Trigger added: " + code(s)
	case "trigger.removed":
		return "Trigger removed: " + code(s)
	case "trigger.filter_changed":
		return code(s) + " " + code(d) + " filter: " + b + " → " + a
	case "trigger.schedule_changed":
		return "Schedule: " + b + " → " + a
	case "trigger.pull_request_target_added":
		return "Trigger added: `pull_request_target` (runs with base repository permissions and secrets)"
	case "job.added":
		return "Job added: " + code(s)
	case "job.removed":
		return "Job removed: " + code(s)
	case "job.renamed":
		return "Job " + code(b) + " renamed to " + code(a)
	case "job.runner_changed":
		return "Job " + code(s) + " runs-on: " + b + " → " + a
	case "job.timeout_changed":
		return "Job " + code(s) + " timeout-minutes: " + b + " → " + a
	case "job.concurrency_changed":
		return "Job " + code(s) + " concurrency: " + b + " → " + a
	case "matrix.count_changed":
		return "Job " + code(s) + " matrix: " + b + " → " + a + " jobs"
	case "matrix.dynamic":
		return "Job " + code(s) + " matrix is computed at runtime; job count unknown"
	case "matrix.over_limit":
		return "Job " + code(s) + " matrix expands to " + a + " jobs (GitHub limit: 256)"
	case "graph.depth_changed":
		return "Longest `needs` chain: " + b + " → " + a + " jobs"
	case "graph.width_changed":
		return "Max concurrent jobs: " + b + " → " + a
	case "graph.cycle":
		return "`needs` cycle: " + d
	case "graph.unresolved":
		return "Job " + code(s) + " needs unknown job " + code(a)
	case "permissions.broadened", "permissions.narrowed":
		return code(d) + " permission (" + s + "): " + code(b) + " → " + code(a)
	case "permissions.write_all":
		return "`permissions: write-all` set on " + s
	case "permissions.removed":
		return "`permissions` removed from " + s + "; repository default applies"
	case "permissions.declared":
		return "`permissions` declared on " + s
	case "secrets.added":
		return "New secret referenced: " + code(s)
	case "secrets.inherit_added":
		return "Job " + code(s) + " passes all secrets to " + code(a) + " (`secrets: inherit`)"
	case "action.added":
		return "Action added: " + code(s+"@"+a)
	case "action.removed":
		return "Action removed: " + code(s)
	case "action.third_party_added":
		return "New third-party action: " + code(s+"@"+a) + d
	case "action.ref_changed":
		return code(s) + ": " + code(b) + " → " + code(a)
	case "action.pin_removed":
		return code(s) + " changed from commit SHA to mutable ref " + code(a)
	case "estimate.changed":
		return "Estimated runner minutes per run: " + b + " → " + a + " (" + d + " historical runs per job)"
	}
	parts := []string{f.Kind}
	for _, v := range []string{s, b, a, d} {
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " ")
}
