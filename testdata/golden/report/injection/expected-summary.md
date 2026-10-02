47 execution changes

## quanto

Execution changes in 2 workflow files.

### `.github/workflows/ci.yml`

| Metric | Before | After |
|---|---:|---:|
| Jobs per run | 1 | 2 |
| Longest `needs` chain | 1 | 1 |
| Max concurrent jobs | 1 | 2 |

- New third-party action: ``` ``evil/two@main ``` (mutable ref)
- New third-party action: ``evil/act@v1`x` @someone`` (mutable ref)
- `push` `branches` filter: −`main` +`<details>`, `@org/admins`, `x  ## Approved`
- Schedule: `(none)` → `'<!-- quanto:summary -->'`
- Job added: ``a`b``
- Job `build` runs-on: `ubuntu-latest` → `x  ## Approved by security team ![](https://evil.example/p.png) @org/admins`
- Max concurrent jobs: 1 → 2
- Job ``a`b`` needs unknown job `<!-- quanto:summary -->`
- Job `build` timeout-minutes: `10` → ```` ``` ````
- Job `build` concurrency: `(none)` → `<details><summary>@org/admins</summary>`

### ``.github/workflows/x`y<details>@org/admins.yml``

| Metric | Before | After |
|---|---:|---:|
| Jobs per run | 1 | 2 |
| Longest `needs` chain | 1 | 1 |
| Max concurrent jobs | 1 | 2 |

- Workflow added
- Workflow removed
- Workflow renamed from `@org/admins`
- Could not analyze: `<!-- quanto:summary -->`
- Trigger added: ```` ``` ````
- Trigger removed: `<details>`
- `<!-- quanto:summary -->` ````aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa   ```<de…```` filter: −`` `lead and trail` `` +`[link](https://evil.example)`
- Schedule: `[link](https://evil.example)` → ````aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa   ```<de…````
- Trigger added: `pull_request_target` (runs with base repository permissions and secrets)
- Job added: ````aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa   ```<de…````
- Job removed: `x  ## Approved`
- Job `![](https://evil.example/p.png)` renamed to `@org/admins`
- Job `![](https://evil.example/p.png)` runs-on: `@org/admins` → ```` ``` ````
- Job `@org/admins` timeout-minutes: ```` ``` ```` → `<details>`
- Job ```` ``` ```` concurrency: `<details>` → `<!-- quanto:summary -->`
- Job `<details>` matrix: `<!-- quanto:summary -->` → `` `lead and trail` `` jobs
- Job `<!-- quanto:summary -->` matrix is computed at runtime; job count unknown
- Job `` `lead and trail` `` matrix expands to ````aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa   ```<de…```` jobs (GitHub limit: 256)
- Longest `needs` chain: ````aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa   ```<de…```` → `x  ## Approved` jobs
- Max concurrent jobs: `x  ## Approved` → ` `
- `needs` cycle: `@org/admins`
- Job ` ` needs unknown job `@org/admins`
- `<details>` permission (`![](https://evil.example/p.png)`): `@org/admins` → ```` ``` ````
- `<!-- quanto:summary -->` permission (`@org/admins`): ```` ``` ```` → `<details>`
- `permissions: write-all` set on ```` ``` ````
- `permissions` removed from `<details>`; repository default applies
- `permissions` declared on `<!-- quanto:summary -->`
- New secret referenced: `` `lead and trail` ``
- Job `[link](https://evil.example)` passes all secrets to `x  ## Approved` (`secrets: inherit`)
- Action added: ````aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa   ```<de…````
- Action removed: `x  ## Approved`
- New third-party action: `@@org/admins` (mutable ref)
- `![](https://evil.example/p.png)`: `@org/admins` → ```` ``` ````
- `@org/admins` changed from commit SHA to mutable ref `<details>`
- Est. billable runner minutes per run: `<details>` → `<!-- quanto:summary -->` (`` `lead and trail` `` historical runs per job)
- `<details>` permission (job `x  ## Approved`): `read` → `write`
- `unknown.kind ## Approved` `@org/admins`

---
<sub>Static analysis of workflow files only. No code from this pull request was executed. Commit `abc1234`.</sub>
