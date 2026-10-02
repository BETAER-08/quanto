<!-- quanto:summary -->
## quanto

Execution changes in 1 workflow file.

### `.github/workflows/ci.yml`

| Metric | Before | After |
|---|---:|---:|
| Jobs per run | 7 | 25 |
| Longest `needs` chain | 1 | 1 |
| Max concurrent jobs | 7 | 25 |
| Est. billable runner minutes per run | 13 | 49 |

- Job `test` matrix: 6 → 24 jobs
- Est. billable runner minutes per run: 13 → 49 (5 historical runs per job)

---
<sub>Static analysis of workflow files only. No code from this pull request was executed. Commit `1111111`.</sub>
