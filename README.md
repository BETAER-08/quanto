# quanto

quanto is a GitHub App that reads the GitHub Actions workflow files changed by a pull request, interprets the versions before and after the change as an execution model, and reports what changes in how the workflows run: how many jobs a run creates after matrix expansion, the longest `needs` chain, the maximum number of concurrent jobs, permission changes, newly referenced secrets, new third-party actions, actions that lose their commit SHA pin, added triggers, schedule frequency, and an estimate of runner minutes per run based on past runs. It reports numbers and facts, not judgments: the Check Run it creates always concludes `neutral` and never blocks a merge.

## Example comment

Output of `quanto diff --format markdown` for the `matrix-axis-added` test case, in which a `shard` axis is added to a matrix:

```markdown
<!-- quanto:summary -->
## quanto

Execution changes in 1 workflow file.

### `.github/workflows/ci.yml`

| Metric | Before | After |
|---|---:|---:|
| Jobs per run | 7 | 25 |
| Longest `needs` chain | 1 | 1 |
| Max concurrent jobs | 7 | 25 |

- Job `test` matrix: 6 → 24 jobs
- Max concurrent jobs: 7 → 25

---
<sub>Static analysis of workflow files only. No code from this pull request was executed.</sub>
```

The pull request comment additionally names the analyzed head commit in the footer. Findings of low significance (for example a narrowed permission or an added first-party action) appear in the Check Run and its annotations but not in the comment. When a later push leaves no findings of normal or high significance, an existing comment is updated to say that there are no workflow execution changes as of that commit; no new comment is created in that case.

## Permissions

| Permission | Access | Used for |
|---|---|---|
| `contents` | read | Finding the merge base of the pull request and reading workflow files at the merge base and head commits |
| `pull_requests` | write | Listing changed files, re-reading the head commit, and creating or updating the summary comment |
| `checks` | write | Creating the `quanto` Check Run with its summary and line annotations |
| `actions` | read | Reading completed workflow runs and job timings for runner-minute estimates |
| `metadata` | read | Repository metadata required by every GitHub App |

Subscribed events: `pull_request` (opened, synchronize, reopened) and `workflow_run` (completed).

## Security

- quanto never executes code from a pull request. Workflow files are parsed as YAML and analyzed statically.
- Workflow file contents are not stored. The database holds installation and repository identifiers, the JSON analysis result, comment IDs, and job timing metadata of completed workflow runs.
- Webhook deliveries are verified with HMAC-SHA256 against the webhook secret before the payload is parsed.
- Private repositories are not analyzed unless `QUANTO_ALLOW_PRIVATE_REPOS=true` is set.
- Secrets (private key, webhook secret, database URL) are never written to logs.

## CLI

The same analysis is available locally without a GitHub App or database.

```sh
make build

./bin/quanto version
./bin/quanto inspect .github/workflows/ci.yml
./bin/quanto inspect .github/workflows/ci.yml --format json
./bin/quanto diff old.yml .github/workflows/ci.yml --path .github/workflows/ci.yml
./bin/quanto diff old.yml new.yml --format markdown
./bin/quanto diff /dev/null new.yml --format json
```

| Command | Description |
|---|---|
| `quanto version` | Print the version |
| `quanto inspect <file> [--format text\|json]` | Triggers, permissions, matrix instances per job, `runs-on`, actions, graph depth and width, model diagnostics |
| `quanto diff <before> <after> [--format text\|markdown\|json] [--path <name>]` | Compare two versions of a workflow. `/dev/null` or an empty file means the file does not exist on that side. `--path` defaults to the after path, or the before path when after is absent |
| `quanto serve --role web\|worker\|all` | Run the GitHub App web role, worker role, or both |
| `quanto migrate` | Apply database migrations |
| `quanto manifest --webhook-url <url> --homepage-url <url> [--name quanto]` | Print a GitHub App manifest |

Exit codes: `0` success, `1` execution error (for example an unreadable input file), `2` usage error. A workflow that cannot be parsed is reported as `unanalyzable` and still exits `0`.

## Self-hosting

- [docs/github-app.md](docs/github-app.md): registering the GitHub App from a manifest, downloading the private key, and installing the App.
- [docs/deploy.md](docs/deploy.md): building the container image, creating Podman secrets, running PostgreSQL, web, and worker with Quadlet, TLS termination, and local webhook forwarding.

## Known limitations

- Static analysis only sees the workflow files. Values computed at runtime are not resolved: a matrix built with `fromJSON(...)` or another expression is reported as unknown (`?`), `if:` conditions are not evaluated, and jobs that would be skipped are still counted.
- Reusable workflows are not opened. A job that calls a reusable workflow counts as one job per matrix instance, and changes inside the called workflow are not reported.
- Composite actions and the contents of referenced actions are not inspected. Only the `uses:` reference and its ref are compared.
- A tag and a branch cannot be told apart statically; every ref that is not a 40-character commit SHA is treated as mutable.
- Runner-minute estimates assume that future runs take as long as the average of the last 30 successful runs of each job, multiplied by the number of matrix instances. An estimate is shown only when every job before and after the change has at least 5 recorded runs and no matrix is dynamic. Queue time, retries, and skipped jobs are not modeled, and minutes are not converted to cost.
- Job history is matched by job name. Jobs whose `name` contains an expression, and jobs whose names collide once the ` (...)` matrix suffix is removed, do not match their history, so the estimate is omitted.
- Runs of reusable workflows (job names containing ` / `) are not recorded.
- At most `QUANTO_MAX_WORKFLOW_FILES` (default 50) workflow files are analyzed per pull request; files larger than 256 KiB are reported as unanalyzable, and `quanto inspect` and `quanto diff` refuse them with exit code 1.
- Schema validation of workflows is out of scope; use a dedicated linter for that.

## License

MIT. See [LICENSE](LICENSE).
