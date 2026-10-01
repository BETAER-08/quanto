package report

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/BETAER-08/quanto/core/semdiff"
)

const maxInlineRunes = 80

func Plain(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	text := b.String()
	if utf8.RuneCountInString(text) > maxInlineRunes {
		text = string([]rune(text)[:maxInlineRunes-1]) + "…"
	}
	return text
}

func inline(s string) string {
	text := Plain(s)
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
			continue
		}
		run = 0
	}
	if text == "" {
		return "` `"
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") {
		text = " " + text + " "
	}
	return fence + text + fence
}

func number(s string) string {
	digits := strings.TrimPrefix(s, "≥")
	if s == "?" || (digits != "" && strings.Trim(digits, "0123456789") == "") {
		return s
	}
	return inline(s)
}

func scope(s string) string {
	if s == "workflow" {
		return s
	}
	if id, ok := strings.CutPrefix(s, "job `"); ok && strings.HasSuffix(id, "`") {
		return "job " + inline(strings.TrimSuffix(id, "`"))
	}
	return inline(s)
}

func Message(f semdiff.Finding) string {
	s, b, a, d := f.Subject, f.Before, f.After, f.Detail
	switch f.Kind {
	case "workflow.added":
		return "Workflow added"
	case "workflow.removed":
		return "Workflow removed"
	case "workflow.renamed":
		return "Workflow renamed from " + inline(b)
	case "workflow.unanalyzable":
		return "Could not analyze: " + inline(d)
	case "trigger.added":
		return "Trigger added: " + inline(s)
	case "trigger.removed":
		return "Trigger removed: " + inline(s)
	case "trigger.filter_changed":
		return inline(s) + " " + inline(d) + " filter: " + inline(b) + " → " + inline(a)
	case "trigger.schedule_changed":
		return "Schedule: " + inline(b) + " → " + inline(a)
	case "trigger.pull_request_target_added":
		return "Trigger added: `pull_request_target` (runs with base repository permissions and secrets)"
	case "job.added":
		return "Job added: " + inline(s)
	case "job.removed":
		return "Job removed: " + inline(s)
	case "job.renamed":
		return "Job " + inline(b) + " renamed to " + inline(a)
	case "job.runner_changed":
		return "Job " + inline(s) + " runs-on: " + inline(b) + " → " + inline(a)
	case "job.timeout_changed":
		return "Job " + inline(s) + " timeout-minutes: " + inline(b) + " → " + inline(a)
	case "job.concurrency_changed":
		return "Job " + inline(s) + " concurrency: " + inline(b) + " → " + inline(a)
	case "matrix.count_changed":
		return "Job " + inline(s) + " matrix: " + number(b) + " → " + number(a) + " jobs"
	case "matrix.dynamic":
		return "Job " + inline(s) + " matrix is computed at runtime; job count unknown"
	case "matrix.over_limit":
		return "Job " + inline(s) + " matrix expands to " + number(a) + " jobs (GitHub limit: 256)"
	case "graph.depth_changed":
		return "Longest `needs` chain: " + number(b) + " → " + number(a) + " jobs"
	case "graph.width_changed":
		return "Max concurrent jobs: " + number(b) + " → " + number(a)
	case "graph.cycle":
		return "`needs` cycle: " + inline(d)
	case "graph.unresolved":
		return "Job " + inline(s) + " needs unknown job " + inline(a)
	case "permissions.broadened", "permissions.narrowed":
		return inline(d) + " permission (" + scope(s) + "): " + inline(b) + " → " + inline(a)
	case "permissions.write_all":
		return "`permissions: write-all` set on " + scope(s)
	case "permissions.removed":
		if d != "" {
			return "`permissions` removed from " + scope(s) + "; repository default applies"
		}
		return "`permissions` removed from " + scope(s)
	case "permissions.declared":
		return "`permissions` declared on " + scope(s)
	case "secrets.added":
		return "New secret referenced: " + inline(s)
	case "secrets.inherit_added":
		return "Job " + inline(s) + " passes all secrets to " + inline(a) + " (`secrets: inherit`)"
	case "action.added":
		return "Action added: " + inline(s+"@"+a)
	case "action.removed":
		return "Action removed: " + inline(s)
	case "action.third_party_added":
		suffix := ""
		if d != "" {
			suffix = " (mutable ref)"
		}
		return "New third-party action: " + inline(s+"@"+a) + suffix
	case "action.ref_changed":
		return inline(s) + ": " + inline(b) + " → " + inline(a)
	case "action.pin_removed":
		return inline(s) + " changed from commit SHA to mutable ref " + inline(a)
	case "estimate.changed":
		return "Estimated runner minutes per run: " + number(b) + " → " + number(a) + " (" + number(d) + " historical runs per job)"
	}
	parts := []string{inline(f.Kind)}
	for _, v := range []string{s, b, a, d} {
		if v != "" {
			parts = append(parts, inline(v))
		}
	}
	return strings.Join(parts, " ")
}
