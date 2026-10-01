package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func prEvent(action string, private bool, head string) map[string]any {
	return map[string]any{
		"action": action,
		"number": 3,
		"pull_request": map[string]any{
			"number": 3,
			"head":   map[string]any{"sha": head},
			"base":   map[string]any{"sha": testBase},
		},
		"repository": map[string]any{
			"id": 42, "name": testRepo, "full_name": testOwner + "/" + testRepo, "private": private,
			"owner": map[string]any{"login": testOwner, "type": "Organization"},
		},
		"installation": map[string]any{"id": 7},
	}
}

func repoJSON(id int64, name string, private bool) map[string]any {
	return map[string]any{"id": id, "name": name, "full_name": testOwner + "/" + name, "private": private}
}

func TestWebhookPingAndDuplicate(t *testing.T) {
	h := newHarness(t, nil)
	handler := h.app.Handler()
	body := []byte(`{"zen":"Keep it logically awesome."}`)
	expectStatus(t, serve(handler, webhookRequest("ping", "d-1", body)), http.StatusOK, "ok")
	expectStatus(t, serve(handler, webhookRequest("ping", "d-1", body)), http.StatusOK, "duplicate")
	if n := h.count("SELECT count(*) FROM webhook_deliveries WHERE delivery_id = 'd-1' AND event = 'ping'"); n != 1 {
		t.Fatalf("deliveries = %d", n)
	}
	if got := testutil.ToFloat64(h.metrics.WebhookReceived.WithLabelValues("ping")); got != 2 {
		t.Fatalf("received = %v", got)
	}
	expectStatus(t, serve(handler, httptest.NewRequest(http.MethodGet, "/readyz", nil)), http.StatusOK, "ready")
}

func TestWebhookPullRequest(t *testing.T) {
	h := newHarness(t, nil)
	handler := h.app.Handler()
	expectStatus(t, serve(handler, webhookRequest("pull_request", "d-1", mustJSON(t, prEvent("opened", false, testHead)))), http.StatusAccepted, "accepted")
	expectStatus(t, serve(handler, webhookRequest("pull_request", "d-2", mustJSON(t, prEvent("synchronize", false, testHead)))), http.StatusAccepted, "accepted")
	expectStatus(t, serve(handler, webhookRequest("pull_request", "d-3", mustJSON(t, prEvent("closed", false, testHead)))), http.StatusNoContent, "")
	expectStatus(t, serve(handler, webhookRequest("pull_request", "d-4", mustJSON(t, prEvent("reopened", false, "3333333333333333333333333333333333333333")))), http.StatusAccepted, "accepted")
	q := h.queue()
	if len(q) != 2 {
		t.Fatalf("queue = %+v", q)
	}
	if q[0].Kind != KindAnalyzePR || q[0].DedupeKey != "pr:42:3:"+testHead {
		t.Fatalf("job = %+v", q[0])
	}
	var p AnalyzePayload
	if err := json.Unmarshal([]byte(q[0].Payload), &p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if p != analyzePayload() {
		t.Fatalf("payload = %+v", p)
	}
	if n := h.count("SELECT count(*) FROM repositories WHERE id = 42 AND owner = $1 AND name = $2", testOwner, testRepo); n != 1 {
		t.Fatalf("repository rows = %d", n)
	}
	if n := h.count("SELECT count(*) FROM installations WHERE id = 7 AND account_login = $1 AND account_type = 'Organization'", testOwner); n != 1 {
		t.Fatalf("installation rows = %d", n)
	}
	if n := h.count("SELECT count(*) FROM webhook_deliveries"); n != 4 {
		t.Fatalf("deliveries = %d", n)
	}
}

func TestWebhookPrivateRepository(t *testing.T) {
	h := newHarness(t, nil)
	expectStatus(t, serve(h.app.Handler(), webhookRequest("pull_request", "d-1", mustJSON(t, prEvent("opened", true, testHead)))), http.StatusNoContent, "")
	if q := h.queue(); len(q) != 0 {
		t.Fatalf("queue = %+v", q)
	}
	if n := h.count("SELECT count(*) FROM repositories"); n != 0 {
		t.Fatalf("repositories = %d", n)
	}
	allowed := newHarness(t, func(o *Options) { o.AllowPrivateRepos = true })
	expectStatus(t, serve(allowed.app.Handler(), webhookRequest("pull_request", "d-1", mustJSON(t, prEvent("opened", true, testHead)))), http.StatusAccepted, "accepted")
	if q := allowed.queue(); len(q) != 1 {
		t.Fatalf("queue = %+v", q)
	}
}

func TestWebhookWorkflowRun(t *testing.T) {
	h := newHarness(t, nil)
	handler := h.app.Handler()
	ev := map[string]any{
		"action":       "completed",
		"workflow_run": map[string]any{"id": 555, "path": ".github/workflows/ci.yml@refs/heads/main"},
		"repository":   prEvent("opened", false, testHead)["repository"],
		"installation": map[string]any{"id": 7},
	}
	expectStatus(t, serve(handler, webhookRequest("workflow_run", "d-1", mustJSON(t, ev))), http.StatusAccepted, "accepted")
	expectStatus(t, serve(handler, webhookRequest("workflow_run", "d-2", mustJSON(t, ev))), http.StatusAccepted, "accepted")
	ev["action"] = "requested"
	expectStatus(t, serve(handler, webhookRequest("workflow_run", "d-3", mustJSON(t, ev))), http.StatusNoContent, "")
	q := h.queue()
	if len(q) != 1 || q[0].Kind != KindIngestWorkflowRun || q[0].DedupeKey != "run:555" {
		t.Fatalf("queue = %+v", q)
	}
	var p IngestPayload
	if err := json.Unmarshal([]byte(q[0].Payload), &p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	want := IngestPayload{InstallationID: 7, RepositoryID: 42, Owner: testOwner, Repo: testRepo, RunID: 555, WorkflowPath: ".github/workflows/ci.yml"}
	if p != want {
		t.Fatalf("payload = %+v", p)
	}
}

func TestWebhookInstallationLifecycle(t *testing.T) {
	h := newHarness(t, nil)
	handler := h.app.Handler()
	inst := map[string]any{"id": 7, "account": map[string]any{"login": testOwner, "type": "Organization"}}
	created := map[string]any{"action": "created", "installation": inst, "repositories": []any{repoJSON(1, "pub", false), repoJSON(2, "priv", true)}}
	expectStatus(t, serve(handler, webhookRequest("installation", "d-1", mustJSON(t, created))), http.StatusOK, "ok")
	if n := h.count("SELECT count(*) FROM repositories WHERE installation_id = 7"); n != 2 {
		t.Fatalf("repositories = %d", n)
	}
	q := h.queue()
	if len(q) != 1 || q[0].Kind != KindBackfillRepository || q[0].DedupeKey != "backfill:1" {
		t.Fatalf("queue = %+v", q)
	}
	expectStatus(t, serve(handler, webhookRequest("installation", "d-2", mustJSON(t, map[string]any{"action": "suspend", "installation": inst}))), http.StatusOK, "ok")
	suspended, err := h.store.InstallationSuspended(context.Background(), 7)
	if err != nil || !suspended {
		t.Fatalf("suspended = %v, %v", suspended, err)
	}
	expectStatus(t, serve(handler, webhookRequest("installation", "d-3", mustJSON(t, map[string]any{"action": "unsuspend", "installation": inst}))), http.StatusOK, "ok")
	suspended, err = h.store.InstallationSuspended(context.Background(), 7)
	if err != nil || suspended {
		t.Fatalf("suspended = %v, %v", suspended, err)
	}
	expectStatus(t, serve(handler, webhookRequest("installation", "d-4", mustJSON(t, map[string]any{"action": "new_permissions_accepted", "installation": inst}))), http.StatusNoContent, "")
	added := map[string]any{"action": "added", "installation": inst, "repositories_added": []any{repoJSON(3, "three", false)}}
	expectStatus(t, serve(handler, webhookRequest("installation_repositories", "d-5", mustJSON(t, added))), http.StatusOK, "ok")
	removed := map[string]any{"action": "removed", "installation": inst, "repositories_removed": []any{repoJSON(1, "pub", false)}}
	expectStatus(t, serve(handler, webhookRequest("installation_repositories", "d-6", mustJSON(t, removed))), http.StatusOK, "ok")
	if n := h.count("SELECT count(*) FROM repositories WHERE id IN (2, 3)"); n != 2 {
		t.Fatalf("repositories after add/remove = %d", n)
	}
	if n := h.count("SELECT count(*) FROM repositories WHERE id = 1"); n != 0 {
		t.Fatal("removed repository still present")
	}
	q = h.queue()
	if len(q) != 2 || q[1].DedupeKey != "backfill:3" {
		t.Fatalf("queue = %+v", q)
	}
	var bp BackfillPayload
	if err := json.Unmarshal([]byte(q[1].Payload), &bp); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if bp != (BackfillPayload{InstallationID: 7, RepositoryID: 3, Owner: testOwner, Repo: "three"}) {
		t.Fatalf("payload = %+v", bp)
	}
	expectStatus(t, serve(handler, webhookRequest("installation", "d-7", mustJSON(t, map[string]any{"action": "deleted", "installation": inst}))), http.StatusOK, "ok")
	if n := h.count("SELECT count(*) FROM installations") + h.count("SELECT count(*) FROM repositories"); n != 0 {
		t.Fatalf("rows after delete = %d", n)
	}
}

func TestWebhookOtherEventsAndErrors(t *testing.T) {
	h := newHarness(t, nil)
	handler := h.app.Handler()
	expectStatus(t, serve(handler, webhookRequest("issues", "d-1", []byte(`{"action":"opened"}`))), http.StatusNoContent, "")
	expectStatus(t, serve(handler, webhookRequest("pull_request", "d-2", []byte(`{not json`))), http.StatusBadRequest, "invalid payload")
	expectStatus(t, serve(handler, webhookRequest("pull_request", "d-3", []byte(`{"action":"opened"}`))), http.StatusBadRequest, "invalid payload")
	if n := h.count("SELECT count(*) FROM webhook_deliveries WHERE delivery_id IN ('d-2', 'd-3')"); n != 0 {
		t.Fatalf("invalid deliveries recorded: %d", n)
	}
	if n := h.count("SELECT count(*) FROM webhook_deliveries WHERE delivery_id = 'd-1'"); n != 1 {
		t.Fatal("ignored event delivery not recorded")
	}
	h.store.Close()
	expectStatus(t, serve(handler, webhookRequest("ping", "d-4", []byte(`{}`))), http.StatusInternalServerError, "internal error")
	expectStatus(t, serve(handler, httptest.NewRequest(http.MethodGet, "/readyz", nil)), http.StatusServiceUnavailable, "unavailable")
	if n := h.count("SELECT count(*) FROM webhook_deliveries WHERE delivery_id = 'd-4'"); n != 0 {
		t.Fatal("failed delivery recorded")
	}
}

func TestWebhookDatabaseErrorDuringProcessing(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.db.Exec(context.Background(), "DROP TABLE queue_jobs"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	expectStatus(t, serve(h.app.Handler(), webhookRequest("pull_request", "d-1", mustJSON(t, prEvent("opened", false, testHead)))), http.StatusInternalServerError, "internal error")
	if n := h.count("SELECT count(*) FROM webhook_deliveries"); n != 0 {
		t.Fatalf("deliveries = %d", n)
	}
}
