<!-- quanto:summary -->
## quanto

Execution changes in 1 workflow file.

### `.github/workflows/ci.yml`

| Metric | Before | After |
|---|---:|---:|
| Jobs per run | 256 | 272 |
| Longest `needs` chain | 1 | 1 |
| Max concurrent jobs | 256 | 272 |

- Job `grid` matrix expands to 272 jobs (GitHub limit: 256)

---
<sub>Static analysis of workflow files only. No code from this pull request was executed. Commit `abc1234`.</sub>
