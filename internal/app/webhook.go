package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/BETAER-08/quanto/internal/metrics"
	"github.com/BETAER-08/quanto/internal/store"
)

const maxWebhookBody = 25 << 20

type badPayloadError struct {
	err error
}

func (e *badPayloadError) Error() string {
	return e.err.Error()
}

func (e *badPayloadError) Unwrap() error {
	return e.err
}

func validSignature(secret []byte, header string, body []byte) bool {
	hexSum, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return false
	}
	got, err := hex.DecodeString(hexSum)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

func (a *App) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			a.metrics.WebhookRejected.WithLabelValues(metrics.RejectSize).Inc()
			a.respond(w, http.StatusRequestEntityTooLarge, "payload too large")
			return
		}
		a.respond(w, http.StatusBadRequest, "unreadable body")
		return
	}
	if !validSignature(a.webhookSecret, r.Header.Get("X-Hub-Signature-256"), body) {
		a.metrics.WebhookRejected.WithLabelValues(metrics.RejectSignature).Inc()
		a.respond(w, http.StatusUnauthorized, "invalid signature")
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	delivery := r.Header.Get("X-GitHub-Delivery")
	if event == "" || delivery == "" {
		a.metrics.WebhookRejected.WithLabelValues(metrics.RejectHeaders).Inc()
		a.respond(w, http.StatusBadRequest, "missing event headers")
		return
	}
	a.metrics.WebhookReceived.WithLabelValues(event).Inc()
	ctx := r.Context()
	log := a.logger.With("event", event, "delivery", delivery)
	seen, err := a.store.DeliverySeen(ctx, delivery)
	if err != nil {
		log.Error("delivery lookup failed", "error", err.Error())
		a.respond(w, http.StatusInternalServerError, "internal error")
		return
	}
	if seen {
		a.respond(w, http.StatusOK, "duplicate")
		return
	}
	status, err := a.dispatch(ctx, event, body)
	if err != nil {
		var bad *badPayloadError
		if errors.As(err, &bad) {
			log.Warn("webhook payload rejected", "error", err.Error())
			a.respond(w, http.StatusBadRequest, "invalid payload")
			return
		}
		log.Error("webhook processing failed", "error", err.Error())
		a.respond(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := a.store.RecordDelivery(ctx, delivery, event); err != nil {
		log.Error("record delivery failed", "error", err.Error())
		a.respond(w, http.StatusInternalServerError, "internal error")
		return
	}
	log.Info("webhook processed", "status", status)
	switch status {
	case http.StatusAccepted:
		a.respond(w, status, "accepted")
	case http.StatusNoContent:
		a.respond(w, status, "")
	default:
		a.respond(w, status, "ok")
	}
}

func decodeEvent(body []byte, out any) error {
	if err := json.Unmarshal(body, out); err != nil {
		return &badPayloadError{err: fmt.Errorf("decode payload: %w", err)}
	}
	return nil
}

func (a *App) dispatch(ctx context.Context, event string, body []byte) (int, error) {
	switch event {
	case "ping":
		return http.StatusOK, nil
	case "pull_request":
		return a.onPullRequest(ctx, body)
	case "workflow_run":
		return a.onWorkflowRun(ctx, body)
	case "installation":
		return a.onInstallation(ctx, body)
	case "installation_repositories":
		return a.onInstallationRepositories(ctx, body)
	}
	return http.StatusNoContent, nil
}

func (a *App) upsertRepository(ctx context.Context, inst installationPayload, repo repositoryPayload) (store.Repository, error) {
	model := repo.model(inst.Account.Login)
	login, kind := inst.Account.Login, inst.Account.Type
	if login == "" {
		login, kind = repo.Owner.Login, repo.Owner.Type
	}
	if err := a.store.UpsertInstallation(ctx, store.Installation{ID: inst.ID, AccountLogin: login, AccountType: kind}); err != nil {
		return model, err
	}
	if err := a.store.UpsertRepositories(ctx, inst.ID, []store.Repository{model}); err != nil {
		return model, err
	}
	return model, nil
}

func (a *App) onPullRequest(ctx context.Context, body []byte) (int, error) {
	var ev pullRequestEvent
	if err := decodeEvent(body, &ev); err != nil {
		return 0, err
	}
	switch ev.Action {
	case "opened", "synchronize", "reopened":
	default:
		return http.StatusNoContent, nil
	}
	number := ev.PullRequest.Number
	if number == 0 {
		number = ev.Number
	}
	if ev.Installation.ID <= 0 || ev.Repository.ID <= 0 || number <= 0 || ev.PullRequest.Head.SHA == "" {
		return 0, &badPayloadError{err: errors.New("pull_request payload is missing installation, repository, number or head sha")}
	}
	if !a.repoAllowed(ev.Repository.Private) {
		return http.StatusNoContent, nil
	}
	repo, err := a.upsertRepository(ctx, ev.Installation, ev.Repository)
	if err != nil {
		return 0, err
	}
	payload := AnalyzePayload{
		InstallationID: ev.Installation.ID,
		RepositoryID:   repo.ID,
		Owner:          repo.Owner,
		Repo:           repo.Name,
		Number:         number,
		HeadSHA:        ev.PullRequest.Head.SHA,
		BaseSHA:        ev.PullRequest.Base.SHA,
	}
	key := fmt.Sprintf("pr:%d:%d:%s", repo.ID, number, payload.HeadSHA)
	if _, err := a.store.Enqueue(ctx, KindAnalyzePR, payload, key); err != nil {
		return 0, err
	}
	return http.StatusAccepted, nil
}

func (a *App) onWorkflowRun(ctx context.Context, body []byte) (int, error) {
	var ev workflowRunEvent
	if err := decodeEvent(body, &ev); err != nil {
		return 0, err
	}
	if ev.Action != "completed" {
		return http.StatusNoContent, nil
	}
	if ev.Installation.ID <= 0 || ev.Repository.ID <= 0 || ev.WorkflowRun.ID <= 0 {
		return 0, &badPayloadError{err: errors.New("workflow_run payload is missing installation, repository or run id")}
	}
	if !a.repoAllowed(ev.Repository.Private) {
		return http.StatusNoContent, nil
	}
	repo, err := a.upsertRepository(ctx, ev.Installation, ev.Repository)
	if err != nil {
		return 0, err
	}
	if _, err := a.enqueueIngest(ctx, ev.Installation.ID, repo, ev.WorkflowRun.ID, ev.WorkflowRun.Path); err != nil {
		return 0, err
	}
	return http.StatusAccepted, nil
}

func (a *App) enqueueIngest(ctx context.Context, installationID int64, repo store.Repository, runID int64, path string) (bool, error) {
	payload := IngestPayload{
		InstallationID: installationID,
		RepositoryID:   repo.ID,
		Owner:          repo.Owner,
		Repo:           repo.Name,
		RunID:          runID,
		WorkflowPath:   workflowPathFromRun(path),
	}
	return a.store.Enqueue(ctx, KindIngestWorkflowRun, payload, "run:"+strconv.FormatInt(runID, 10))
}

func (a *App) enqueueBackfill(ctx context.Context, installationID int64, repos []store.Repository) error {
	for _, r := range repos {
		if !a.repoAllowed(r.Private) {
			continue
		}
		payload := BackfillPayload{InstallationID: installationID, RepositoryID: r.ID, Owner: r.Owner, Repo: r.Name}
		if _, err := a.store.Enqueue(ctx, KindBackfillRepository, payload, "backfill:"+strconv.FormatInt(r.ID, 10)); err != nil {
			return err
		}
	}
	return nil
}

func repositoryModels(inst installationPayload, in []repositoryPayload) []store.Repository {
	out := make([]store.Repository, 0, len(in))
	for _, r := range in {
		out = append(out, r.model(inst.Account.Login))
	}
	return out
}

func (a *App) onInstallation(ctx context.Context, body []byte) (int, error) {
	var ev installationEvent
	if err := decodeEvent(body, &ev); err != nil {
		return 0, err
	}
	if ev.Installation.ID <= 0 {
		return 0, &badPayloadError{err: errors.New("installation payload is missing installation id")}
	}
	id := ev.Installation.ID
	switch ev.Action {
	case "created":
		if err := a.store.UpsertInstallation(ctx, store.Installation{ID: id, AccountLogin: ev.Installation.Account.Login, AccountType: ev.Installation.Account.Type}); err != nil {
			return 0, err
		}
		repos := repositoryModels(ev.Installation, ev.Repositories)
		if err := a.store.UpsertRepositories(ctx, id, repos); err != nil {
			return 0, err
		}
		if err := a.enqueueBackfill(ctx, id, repos); err != nil {
			return 0, err
		}
	case "deleted":
		if err := a.store.DeleteInstallation(ctx, id); err != nil {
			return 0, err
		}
	case "suspend", "unsuspend":
		if err := a.store.SetInstallationSuspended(ctx, id, ev.Action == "suspend"); err != nil {
			return 0, err
		}
	default:
		return http.StatusNoContent, nil
	}
	return http.StatusOK, nil
}

func (a *App) onInstallationRepositories(ctx context.Context, body []byte) (int, error) {
	var ev installationRepositoriesEvent
	if err := decodeEvent(body, &ev); err != nil {
		return 0, err
	}
	if ev.Installation.ID <= 0 {
		return 0, &badPayloadError{err: errors.New("installation_repositories payload is missing installation id")}
	}
	id := ev.Installation.ID
	switch ev.Action {
	case "added":
		if err := a.store.UpsertInstallation(ctx, store.Installation{ID: id, AccountLogin: ev.Installation.Account.Login, AccountType: ev.Installation.Account.Type}); err != nil {
			return 0, err
		}
		repos := repositoryModels(ev.Installation, ev.RepositoriesAdded)
		if err := a.store.UpsertRepositories(ctx, id, repos); err != nil {
			return 0, err
		}
		if err := a.enqueueBackfill(ctx, id, repos); err != nil {
			return 0, err
		}
	case "removed":
		ids := make([]int64, 0, len(ev.RepositoriesRemoved))
		for _, r := range ev.RepositoriesRemoved {
			ids = append(ids, r.ID)
		}
		if err := a.store.RemoveRepositories(ctx, ids); err != nil {
			return 0, err
		}
	default:
		return http.StatusNoContent, nil
	}
	return http.StatusOK, nil
}
