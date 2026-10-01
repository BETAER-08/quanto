package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

type manifestHook struct {
	URL string `json:"url"`
}

type manifestDoc struct {
	Name               string            `json:"name"`
	URL                string            `json:"url"`
	HookAttributes     manifestHook      `json:"hook_attributes"`
	Public             bool              `json:"public"`
	DefaultPermissions map[string]string `json:"default_permissions"`
	DefaultEvents      []string          `json:"default_events"`
}

func buildManifest(name, webhookURL, homepageURL string) manifestDoc {
	return manifestDoc{
		Name:           name,
		URL:            homepageURL,
		HookAttributes: manifestHook{URL: webhookURL},
		Public:         true,
		DefaultPermissions: map[string]string{
			"actions":       "read",
			"checks":        "write",
			"contents":      "read",
			"metadata":      "read",
			"pull_requests": "write",
		},
		DefaultEvents: []string{"pull_request", "workflow_run"},
	}
}

func validateURL(flagName, raw string) error {
	if raw == "" {
		return fmt.Errorf("--%s is required", flagName)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("--%s is not a valid URL: %w", flagName, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("--%s must use http or https", flagName)
	}
	if u.Host == "" || u.Hostname() == "" {
		return fmt.Errorf("--%s must include a host", flagName)
	}
	if u.User != nil {
		return fmt.Errorf("--%s must not include user information", flagName)
	}
	if u.Fragment != "" {
		return fmt.Errorf("--%s must not include a fragment", flagName)
	}
	return nil
}

func runManifest(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("manifest", stderr)
	webhookFlag := fs.String("webhook-url", "", "webhook delivery URL")
	homepageFlag := fs.String("homepage-url", "", "App homepage URL")
	nameFlag := fs.String("name", "quanto", "App name")
	positional, err := parseInterleaved(fs, args)
	if err != nil {
		return parseExit(err)
	}
	if len(positional) != 0 {
		fmt.Fprint(stderr, "quanto: manifest takes no positional arguments\n"+usageText)
		return exitUsage
	}
	var problems []string
	if err := validateURL("webhook-url", *webhookFlag); err != nil {
		problems = append(problems, err.Error())
	}
	if err := validateURL("homepage-url", *homepageFlag); err != nil {
		problems = append(problems, err.Error())
	}
	name := strings.TrimSpace(*nameFlag)
	if name == "" {
		problems = append(problems, "--name must not be empty")
	}
	if len(problems) != 0 {
		fmt.Fprintf(stderr, "quanto: %s\n%s", strings.Join(problems, "; "), usageText)
		return exitUsage
	}
	data, err := json.MarshalIndent(buildManifest(name, *webhookFlag, *homepageFlag), "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "quanto: encode manifest: %v\n", err)
		return exitError
	}
	return write(stdout, stderr, string(data)+"\n")
}
