<!-- quanto:summary -->
## quanto

Execution changes in 1 workflow file.

### `.github/workflows/ci.yml`

| Metric | Before | After |
|---|---:|---:|
| Jobs per run | 3 | 5 |
| Longest `needs` chain | 2 | 2 |
| Max concurrent jobs | 2 | 4 |

- Job `test` matrix: 2 → 4 jobs
- Max concurrent jobs: 2 → 4

---
<sub>Static analysis of workflow files only. No code from this pull request was executed. Commit `abc1234`.</sub>
