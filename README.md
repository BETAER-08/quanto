# quanto

quanto reads the GitHub Actions workflow files changed by a pull request, interprets the versions before and after the change as an execution model, and reports what changes in how the workflows run: how many jobs a run creates after matrix expansion, the longest `needs` chain, the maximum number of concurrent jobs, permission changes, newly referenced secrets, new third-party actions, actions that lose their commit SHA pin, added triggers, schedule frequency, and an estimate of runner minutes per run based on past runs. It reports numbers and facts, not judgments, and never blocks a merge. quanto runs as a GitHub Action in your own workflow; a self-hosted GitHub App is available as an advanced option.

## GitHub Action

```yaml
name: quanto
on:
  pull_request:
    paths: ['.github/workflows/**']
permissions:
  contents: read
  pull-requests: write
  actions: read
jobs:
  quanto:
    runs-on: ubuntu-latest
    steps:
      - uses: BETAER-08/quanto@v1
```

The action needs no checkout step. It downloads the `quanto` release binary that matches the tag the action is referenced by and the runner (Linux, macOS and Windows on x64, Linux and macOS on ARM64), verifies its SHA-256 against the `checksums.txt` of the same release, and refuses to run when the checksum does not match. Release binaries carry build provenance attestations, which can be checked with `gh attestation verify <file> --repo BETAER-08/quanto`.

Each run produces:

- a Job Summary with the metrics table and all findings, including low-significance ones;
- a `notice` annotation on the workflow file for each finding that has a location;
- a pull request comment, created when the comment threshold below is met and updated on later pushes.

### Inputs

| Input | Default | Description |
|---|---|---|
| `github-token` | `${{ github.token }}` | Token used to read the pull request and run history and to write the comment |
| `comment` | `true` | Create or update the summary comment on the pull request |
| `estimate` | `true` | Estimate runner minutes from the last 10 successful runs of each changed workflow file (at most 10 files) |
| `max-files` | `50` | Maximum number of workflow files analyzed per pull request (1 to 200) |
| `version` | tag of the action ref | Release tag to download. Set it when the action is pinned by commit SHA or referenced by a branch |

### Permissions

| Permission | Access | Used for |
|---|---|---|
| `contents` | read | Finding the merge base of the pull request and reading workflow files at the merge base and head commits |
| `pull-requests` | write | Listing changed files and creating or updating the summary comment |
| `actions` | read | Reading recent successful runs and job timings for runner-minute estimates |

Without `pull-requests: write` the analysis, Job Summary and annotations still work; the comment is skipped. Without `actions: read` the estimate is skipped.

### Fork pull requests

For `pull_request` events from forks, GitHub gives the workflow a read-only token. quanto then analyzes the pull request as usual, writes the Job Summary and annotations, and records `comment skipped: token is read-only` in the Job Summary instead of failing. The action also accepts `pull_request_target` events; it never checks out or runs code from the pull request in either case.

### Data handling

quanto runs entirely inside your job. It talks only to the GitHub API of the repository (`GITHUB_API_URL`) and, during setup, downloads its release binary from GitHub. Workflow contents and analysis results are not sent anywhere else and are not stored; the only outputs are the Job Summary, the annotations and the pull request comment.

### Exit status

The step exits `0` whatever the analysis finds, including when a GitHub API request fails (a warning is written to the log). It exits `1` only on configuration errors: no token, no event file, or an invalid input value, and when the release binary cannot be downloaded or fails checksum verification.

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

The pull request comment additionally names the analyzed head commit in the footer. Every analysis writes all findings to the Job Summary (the Check Run for the App) with annotations. A comment is posted only when the pull request has at least one finding of high significance, or at least one finding about the matrix (`matrix.*`), the `needs` graph (`graph.*`), or the runner-minute estimate (`estimate.*`). Other findings of normal significance (for example an added trigger, a changed filter, or a bumped action ref) appear only in the Job Summary or Check Run. When a comment is posted it contains all findings of normal and high significance; findings of low significance (for example a narrowed permission or an added first-party action) never appear in the comment. When a later push no longer meets the comment threshold, an existing comment is updated to say that no changes meet the comment threshold as of that commit, or that there are no workflow execution changes when no finding remains, and the Job Summary or Check Run still lists any remaining findings; no new comment is created in that case.

When the same finding text appears in two or more workflow files, the comment, the Job Summary and the Check Run summary show it once under `Across N workflow files` with the number of files; annotations and the JSON report keep one entry per file.

## CLI

The same analysis is available locally without GitHub access.

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
| `quanto action` | Entry point of the GitHub Action; reads the `GITHUB_*` environment and the action inputs |

Exit codes: `0` success, `1` execution error (for example an unreadable input file), `2` usage error. A workflow that cannot be parsed is reported as `unanalyzable` and still exits `0`.

## Advanced: self-hosted GitHub App

The same analysis is available as a GitHub App that you run yourself with PostgreSQL. The App creates a `quanto` Check Run with a summary and line annotations (always concluding `neutral`), posts the same pull request comment, and records job timings from completed workflow runs for estimates.

### App permissions

| Permission | Access | Used for |
|---|---|---|
| `contents` | read | Finding the merge base of the pull request and reading workflow files at the merge base and head commits |
| `pull_requests` | write | Listing changed files, re-reading the head commit, and creating or updating the summary comment |
| `checks` | write | Creating the `quanto` Check Run with its summary and line annotations |
| `actions` | read | Reading completed workflow runs and job timings for runner-minute estimates |
| `metadata` | read | Repository metadata required by every GitHub App |

Subscribed events: `pull_request` (opened, synchronize, reopened) and `workflow_run` (completed).

### App security

- quanto never executes code from a pull request. Workflow files are parsed as YAML and analyzed statically.
- Workflow file contents are not stored. The database holds installation and repository identifiers, the JSON analysis result, comment IDs, and job timing metadata of completed workflow runs.
- Webhook deliveries are verified with HMAC-SHA256 against the webhook secret before the payload is parsed.
- Private repositories are not analyzed unless `QUANTO_ALLOW_PRIVATE_REPOS=true` is set.
- Secrets (private key, webhook secret, database URL) are never written to logs.

### Deployment

- [docs/github-app.md](docs/github-app.md): registering the GitHub App from a manifest, downloading the private key, and installing the App.
- [docs/deploy.md](docs/deploy.md): building the container image, creating Podman secrets, running PostgreSQL, web, and worker with Quadlet, TLS termination, and local webhook forwarding.

## Known limitations

- Static analysis only sees the workflow files. Values computed at runtime are not resolved: a matrix built with `fromJSON(...)` or another expression is reported as unknown (`?`), `if:` conditions are not evaluated, and jobs that would be skipped are still counted.
- `if:` conditions on jobs are not evaluated, so a job that would not run still counts toward jobs per run and toward the maximum number of concurrent jobs.
- Matrices are compared by the number of combinations only. Replacing axis values so that fewer distinct platforms or versions are covered is not reported when the number of combinations stays the same.
- Unpinning a tool version through action inputs (for example `go-version: '1.22.3'` changed to `stable` in `with:`) is not detected; only the action reference itself is compared.
- Permissions are compared as effective permissions: a job without its own `permissions` block uses the workflow's block, and a workflow without one uses the repository default, which is not visible to static analysis. A change to the repository default is therefore not reported, and a job whose effective permissions come from the repository default is compared only when a `permissions` block is added or removed.
- Reusable workflows are not opened. A job that calls a reusable workflow counts as one job per matrix instance, and changes inside the called workflow are not reported.
- Composite actions and the contents of referenced actions are not inspected. Only the `uses:` reference and its ref are compared.
- A tag and a branch cannot be told apart statically; every ref that is not a 40-character commit SHA is treated as mutable.
- Runner-minute estimates assume that future runs take as long as the average of the last 30 successful runs of each job, multiplied by the number of matrix instances. An estimate is shown only when every job before and after the change has at least 5 recorded runs and no matrix is dynamic. Queue time, retries, and skipped jobs are not modeled, and minutes are not converted to cost.
- Job history is matched by job name. Jobs whose `name` contains an expression, and jobs whose names collide once the ` (...)` matrix suffix is removed, do not match their history, so the estimate is omitted.
- Runs of reusable workflows (job names containing ` / `) are not recorded.
- At most `QUANTO_MAX_WORKFLOW_FILES` (default 50) workflow files are analyzed per pull request; files larger than 256 KiB are reported as unanalyzable, and `quanto inspect` and `quanto diff` refuse them with exit code 1.
- If two analyses of the same head commit run at the same time, both can find no existing check run and each create one, so the commit can show two `quanto` check runs.
- A retried analysis appends only the annotations that are not yet on the check run. If the input changed between attempts (for example, newly recorded run history adds an estimate finding), the annotations already uploaded are not corrected, so the check run can show duplicated or missing annotations.
- Schema validation of workflows is out of scope; use a dedicated linter for that.
- The action does not re-read the pull request head before commenting and keeps no comment cache; it finds its comment by the marker and the `github-actions[bot]` author. A comment written by another token (for example a personal access token passed as `github-token`) is not found and a new one is created.
- When an existing comment is updated because no change meets the comment threshold, its text refers to the quanto check run; with the action the remaining findings are in the Job Summary of the run.
- The action's estimate looks at no more than 10 workflow files and the last 10 successful runs of each, so it is omitted when fewer than 5 successful runs of every job are found.
- The release to download is derived from the action ref. When the action is pinned by commit SHA or referenced by a branch, set the `version` input to a release tag.

## License

MIT. See [LICENSE](LICENSE).
