package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestManifestOutput(t *testing.T) {
	code, stdout, stderr := run("manifest", "--webhook-url", "https://quanto.example.com/webhook", "--homepage-url", "https://example.com/quanto")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := `{
  "name": "quanto",
  "url": "https://example.com/quanto",
  "hook_attributes": {
    "url": "https://quanto.example.com/webhook"
  },
  "public": true,
  "default_permissions": {
    "actions": "read",
    "checks": "write",
    "contents": "read",
    "metadata": "read",
    "pull_requests": "write"
  },
  "default_events": [
    "pull_request",
    "workflow_run"
  ]
}
`
	if stdout != want {
		t.Errorf("manifest =\n%s\nwant\n%s", stdout, want)
	}
}

func TestManifestFields(t *testing.T) {
	code, stdout, stderr := run("manifest", "--name", "quanto-staging", "--homepage-url", "http://localhost:8080", "--webhook-url", "http://localhost:8080/webhook")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got struct {
		Name           string `json:"name"`
		URL            string `json:"url"`
		HookAttributes struct {
			URL string `json:"url"`
		} `json:"hook_attributes"`
		Public             bool              `json:"public"`
		DefaultPermissions map[string]string `json:"default_permissions"`
		DefaultEvents      []string          `json:"default_events"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "quanto-staging" {
		t.Errorf("name = %q", got.Name)
	}
	if got.URL != "http://localhost:8080" || got.HookAttributes.URL != "http://localhost:8080/webhook" {
		t.Errorf("urls = %q %q", got.URL, got.HookAttributes.URL)
	}
	if !got.Public {
		t.Error("public = false")
	}
	wantPerms := map[string]string{"actions": "read", "checks": "write", "contents": "read", "metadata": "read", "pull_requests": "write"}
	if !reflect.DeepEqual(got.DefaultPermissions, wantPerms) {
		t.Errorf("permissions = %v", got.DefaultPermissions)
	}
	if !reflect.DeepEqual(got.DefaultEvents, []string{"pull_request", "workflow_run"}) {
		t.Errorf("events = %v", got.DefaultEvents)
	}
}

func TestManifestDeterministic(t *testing.T) {
	args := []string{"manifest", "--webhook-url", "https://a.example/webhook", "--homepage-url", "https://a.example"}
	_, first, _ := run(args...)
	for i := 0; i < 5; i++ {
		_, again, _ := run(args...)
		if again != first {
			t.Fatal("manifest output differs between runs")
		}
	}
}

func TestManifestUsageErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		message string
	}{
		{"missing both", []string{"manifest"}, "--webhook-url is required"},
		{"missing homepage", []string{"manifest", "--webhook-url", "https://a.example/webhook"}, "--homepage-url is required"},
		{"missing webhook", []string{"manifest", "--homepage-url", "https://a.example"}, "--webhook-url is required"},
		{"relative webhook", []string{"manifest", "--webhook-url", "/webhook", "--homepage-url", "https://a.example"}, "--webhook-url must use http or https"},
		{"ftp homepage", []string{"manifest", "--webhook-url", "https://a.example/webhook", "--homepage-url", "ftp://a.example"}, "--homepage-url must use http or https"},
		{"no host", []string{"manifest", "--webhook-url", "https:///webhook", "--homepage-url", "https://a.example"}, "--webhook-url must include a host"},
		{"bad escape", []string{"manifest", "--webhook-url", "https://a.example/%zz", "--homepage-url", "https://a.example"}, "--webhook-url is not a valid URL"},
		{"user info", []string{"manifest", "--webhook-url", "https://u:p@a.example/webhook", "--homepage-url", "https://a.example"}, "--webhook-url must not include user information"},
		{"fragment", []string{"manifest", "--webhook-url", "https://a.example/webhook#x", "--homepage-url", "https://a.example"}, "--webhook-url must not include a fragment"},
		{"empty name", []string{"manifest", "--webhook-url", "https://a.example/webhook", "--homepage-url", "https://a.example", "--name", " "}, "--name must not be empty"},
		{"positional", []string{"manifest", "extra", "--webhook-url", "https://a.example/webhook", "--homepage-url", "https://a.example"}, "takes no positional arguments"},
		{"unknown flag", []string{"manifest", "--secret", "x"}, "flag provided but not defined"},
	}
	for _, tt := range tests {
		code, stdout, stderr := run(tt.args...)
		if code != 2 {
			t.Errorf("%s: exit = %d, want 2", tt.name, code)
		}
		if stdout != "" {
			t.Errorf("%s: unexpected stdout %q", tt.name, stdout)
		}
		if !strings.Contains(stderr, tt.message) {
			t.Errorf("%s: stderr %q lacks %q", tt.name, stderr, tt.message)
		}
		if !strings.Contains(stderr, "usage:") {
			t.Errorf("%s: stderr lacks usage", tt.name)
		}
	}
}
