package app

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/BETAER-08/quanto/internal/github"
	"github.com/BETAER-08/quanto/internal/metrics"
	"github.com/BETAER-08/quanto/internal/store"
)

const (
	idleDelay         = time.Second
	idleJitter        = 250 * time.Millisecond
	deferJitter       = 30 * time.Second
	reapInterval      = time.Minute
	staleAfter        = 10 * time.Minute
	pruneInterval     = time.Hour
	deliveryRetention = 7 * 24 * time.Hour
	doneRetention     = 7 * 24 * time.Hour
	deadRetention     = 30 * 24 * time.Hour
	depthInterval     = 15 * time.Second
	drainTimeout      = 60 * time.Second
	recordTimeout     = 10 * time.Second
	handlerTimeout    = 5 * time.Minute
)

func jitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return rand.N(max)
}

func (a *App) RunWorker(ctx context.Context) error {
	jobCtx, cancelJobs := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelJobs()
	var wg sync.WaitGroup
	for i := 0; i < a.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.pollLoop(ctx, jobCtx)
		}()
	}
	maintained := make(chan struct{})
	go func() {
		defer close(maintained)
		a.maintain(ctx)
	}()
	a.logger.Info("worker started", "concurrency", a.concurrency)
	<-ctx.Done()
	a.logger.Info("worker stopping", "drain_timeout", drainTimeout.String())
	drained := make(chan struct{})
	go func() {
		wg.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(drainTimeout):
		a.logger.Warn("worker drain timed out; cancelling in-flight jobs")
		cancelJobs()
		<-drained
	}
	<-maintained
	a.logger.Info("worker stopped")
	return nil
}

func (a *App) pollLoop(pollCtx, jobCtx context.Context) {
	for pollCtx.Err() == nil {
		processed, err := a.processNext(pollCtx, jobCtx)
		if err != nil && pollCtx.Err() == nil {
			a.logger.Error("dequeue failed", "error", err.Error())
		}
		if processed {
			continue
		}
		timer := time.NewTimer(idleDelay + jitter(idleJitter))
		select {
		case <-pollCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (a *App) ProcessNext(ctx context.Context) (bool, error) {
	return a.processNext(ctx, ctx)
}

func (a *App) processNext(pollCtx, jobCtx context.Context) (bool, error) {
	job, err := a.store.Dequeue(pollCtx)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}
	a.process(jobCtx, job)
	return true, nil
}

func (a *App) process(ctx context.Context, job *store.QueueJob) {
	log := a.logger.With("job_id", job.ID, "kind", job.Kind, "attempt", job.Attempts)
	handlerCtx, cancelHandler := context.WithTimeout(ctx, a.handlerTimeout)
	err := a.invoke(handlerCtx, job)
	cancelHandler()
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	result, rerr := a.record(recordCtx, ctx, job, err)
	if errors.Is(rerr, store.ErrLeaseLost) {
		log.Warn("lost lease; job result discarded", "result", result)
		return
	}
	if rerr != nil {
		log.Error("recording job result failed", "error", rerr.Error())
		return
	}
	a.metrics.QueueJobs.WithLabelValues(job.Kind, result).Inc()
	if err != nil {
		log.Warn("job did not complete", "result", result, "error", err.Error())
		return
	}
	log.Info("job completed")
}

func (a *App) invoke(ctx context.Context, job *store.QueueJob) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in %s handler: %v", job.Kind, r)
		}
	}()
	switch job.Kind {
	case KindAnalyzePR:
		return a.analyzePR(ctx, job.Payload)
	case KindIngestWorkflowRun:
		return a.ingestWorkflowRun(ctx, job.Payload)
	case KindBackfillRepository:
		return a.backfillRepo(ctx, job.Payload)
	}
	return permanent(fmt.Errorf("unknown job kind %q", job.Kind))
}

func (a *App) record(ctx, jobCtx context.Context, job *store.QueueJob, cause error) (string, error) {
	var rateLimited *github.RateLimitError
	var perm *permanentError
	switch {
	case cause == nil:
		return metrics.ResultDone, a.store.Complete(ctx, job.ID, job.Attempts)
	case errors.As(cause, &rateLimited):
		return metrics.ResultDeferred, a.store.Defer(ctx, job.ID, job.Attempts, rateLimited.Reset.Add(jitter(deferJitter)))
	case errors.As(cause, &perm):
		return metrics.ResultDead, a.store.Kill(ctx, job.ID, job.Attempts, cause)
	case jobCtx.Err() != nil && errors.Is(cause, context.Canceled):
		return metrics.ResultDeferred, a.store.Defer(ctx, job.ID, job.Attempts, time.Now())
	}
	result := metrics.ResultFailed
	if job.Attempts >= store.MaxAttempts {
		result = metrics.ResultDead
	}
	return result, a.store.Fail(ctx, job.ID, job.Attempts, cause)
}

func (a *App) maintain(ctx context.Context) {
	reap := time.NewTicker(reapInterval)
	prune := time.NewTicker(pruneInterval)
	depth := time.NewTicker(depthInterval)
	defer reap.Stop()
	defer prune.Stop()
	defer depth.Stop()
	a.updateDepth(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-reap.C:
			if n, err := a.store.ReapStale(ctx, staleAfter); err != nil {
				a.logMaintenance(ctx, "reap stale jobs", err)
			} else if n > 0 {
				a.logger.Warn("reaped stale jobs", "count", n)
			}
		case <-prune.C:
			if n, err := a.store.PruneDeliveries(ctx, deliveryRetention); err != nil {
				a.logMaintenance(ctx, "prune deliveries", err)
			} else if n > 0 {
				a.logger.Info("pruned deliveries", "count", n)
			}
			if n, err := a.store.PruneQueue(ctx, doneRetention, deadRetention); err != nil {
				a.logMaintenance(ctx, "prune queue", err)
			} else if n > 0 {
				a.logger.Info("pruned queue jobs", "count", n)
			}
		case <-depth.C:
			a.updateDepth(ctx)
		}
	}
}

func (a *App) updateDepth(ctx context.Context) {
	n, err := a.store.PendingCount(ctx)
	if err != nil {
		a.logMaintenance(ctx, "count pending jobs", err)
		return
	}
	a.metrics.QueueDepth.Set(float64(n))
}

func (a *App) logMaintenance(ctx context.Context, op string, err error) {
	if ctx.Err() != nil {
		return
	}
	a.logger.Error("maintenance failed", "op", op, "error", err.Error())
}
