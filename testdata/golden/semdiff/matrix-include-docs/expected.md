<!-- quanto:summary -->
## quanto

Execution changes in 1 workflow file.

### `.github/workflows/ci.yml`

| Metric | Before | After |
|---|---:|---:|
| Jobs per run | 4 | 6 |
| Longest `needs` chain | 1 | 1 |
| Max concurrent jobs | 4 | 6 |

- Job `example` matrix: 4 → 6 jobs
- Max concurrent jobs: 4 → 6

---
<sub>Static analysis of workflow files only. No code from this pull request was executed. Commit `abc1234`.</sub>
