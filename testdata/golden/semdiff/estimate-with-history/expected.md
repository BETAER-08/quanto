<!-- quanto:summary -->
## quanto

Execution changes in 1 workflow file.

### `.github/workflows/ci.yml`

| Metric | Before | After |
|---|---:|---:|
| Jobs per run | 3 | 5 |
| Longest `needs` chain | 2 | 2 |
| Max concurrent jobs | 2 | 4 |
| Est. billable runner minutes per run | 25 | 45 |

- Job `test` matrix: 2 → 4 jobs
- Est. billable runner minutes per run: 25 → 45 (10 historical runs per job)

---
<sub>Static analysis of workflow files only. No code from this pull request was executed. Commit `abc1234`.</sub>
