package app

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/BETAER-08/quanto/internal/github"
)

var update = flag.Bool("update", false, "update golden files")

const e2eGoldenDir = "../../testdata/golden/e2e"

func compareGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join(e2eGoldenDir, name)
	if *update {
		if err := os.MkdirAll(e2eGoldenDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestEndToEndPullRequest(t *testing.T) {
	h := newHarness(t, nil)
	h.gh.files = []github.PullRequestFile{
		{Filename: ciPath, Status: "modified"},
		{Filename: "src/main.go", Status: "modified"},
	}
	h.gh.setContent(testBase, ciPath, readFixture(t, fixtureDir+"matrix-axis-added/before.yml"))
	h.gh.setContent(testHead, ciPath, readFixture(t, fixtureDir+"matrix-axis-added/after.yml"))
	web := httptest.NewServer(h.app.Handler())
	t.Cleanup(web.Close)
	body := mustJSON(t, prEvent("opened", false, testHead))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, web.URL+"/webhook", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "e2e-delivery-1")
	req.Header.Set("X-Hub-Signature-256", sign(body))
	resp, err := web.Client().Do(req)
	if err != nil {
		t.Fatalf("post webhook: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("webhook status = %d", resp.StatusCode)
	}
	h.runOne()
	processed, err := h.app.ProcessNext(context.Background())
	if err != nil || processed {
		t.Fatalf("second cycle = %v, %v", processed, err)
	}
	expectJob(t, h, "done", 1)
	checks := h.gh.find("POST", "/check-runs")
	if len(checks) != 1 {
		t.Fatalf("check runs = %d", len(checks))
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(checks[0].Body), "", "  "); err != nil {
		t.Fatalf("indent: %v", err)
	}
	pretty.WriteByte('\n')
	compareGolden(t, "check_run.json", pretty.Bytes())
	comments := h.gh.find("POST", "/issues/3/comments")
	if len(comments) != 1 {
		t.Fatalf("comments = %d", len(comments))
	}
	var comment struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(comments[0].Body), &comment); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	compareGolden(t, "comment.md", []byte(comment.Body))
}
