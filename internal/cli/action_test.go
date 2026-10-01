package cli

import (
	"strings"
	"testing"
)

func TestActionRejectsArguments(t *testing.T) {
	code, stdout, stderr := run("action", "extra")
	if code != exitUsage || stdout != "" || !strings.Contains(stderr, "action takes no arguments") {
		t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestActionIgnoresOtherEvents(t *testing.T) {
	t.Setenv("GITHUB_EVENT_NAME", "push")
	code, stdout, stderr := run("action")
	if code != exitOK || stderr != "" || !strings.HasPrefix(stdout, "quanto: nothing to analyze") {
		t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestActionConfigurationError(t *testing.T) {
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_EVENT_PATH", "")
	t.Setenv("GITHUB_REPOSITORY", "o/r")
	code, _, stderr := run("action")
	if code != exitError || !strings.Contains(stderr, "quanto: GITHUB_TOKEN is not set") || !strings.Contains(stderr, "quanto: GITHUB_EVENT_PATH is not set") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestUsageListsAction(t *testing.T) {
	if !strings.Contains(usageText, "quanto action\n") {
		t.Error("usage does not list action")
	}
}
