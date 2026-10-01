CREATE TABLE installations (
    id BIGINT PRIMARY KEY,
    account_login TEXT NOT NULL,
    account_type TEXT NOT NULL,
    suspended BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE repositories (
    id BIGINT PRIMARY KEY,
    installation_id BIGINT NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    owner TEXT NOT NULL,
    name TEXT NOT NULL,
    private BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX repositories_installation ON repositories (installation_id);

CREATE TABLE webhook_deliveries (
    delivery_id TEXT PRIMARY KEY,
    event TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE queue_jobs (
    id BIGSERIAL PRIMARY KEY,
    kind TEXT NOT NULL,
    payload JSONB NOT NULL,
    dedupe_key TEXT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'done', 'dead')),
    attempts INT NOT NULL DEFAULT 0,
    run_after TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX queue_jobs_ready ON queue_jobs (run_after, id) WHERE status = 'pending';
CREATE UNIQUE INDEX queue_jobs_dedupe ON queue_jobs (dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('pending', 'running');

CREATE TABLE analyses (
    id BIGSERIAL PRIMARY KEY,
    repository_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    pr_number INT NOT NULL,
    head_sha TEXT NOT NULL,
    base_sha TEXT NOT NULL,
    result JSONB NOT NULL,
    finding_count INT NOT NULL,
    check_run_id BIGINT,
    duration_ms INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX analyses_unique ON analyses (repository_id, pr_number, head_sha);

CREATE TABLE pr_comments (
    repository_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    pr_number INT NOT NULL,
    comment_id BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, pr_number)
);

CREATE TABLE job_runs (
    job_id BIGINT PRIMARY KEY,
    repository_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    run_id BIGINT NOT NULL,
    workflow_path TEXT NOT NULL,
    job_key TEXT NOT NULL,
    runner_labels TEXT[] NOT NULL,
    conclusion TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL,
    duration_seconds INT NOT NULL
);
CREATE INDEX job_runs_lookup ON job_runs (repository_id, workflow_path, job_key, completed_at DESC);

CREATE TABLE job_stats (
    repository_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    workflow_path TEXT NOT NULL,
    job_key TEXT NOT NULL,
    avg_seconds INT NOT NULL,
    p50_seconds INT NOT NULL,
    sample_count INT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, workflow_path, job_key)
);
