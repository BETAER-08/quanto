<!-- quanto:summary -->
## quanto

Execution changes in 1 workflow file.

### `.github/workflows/ci.yml`

| Metric | Before | After |
|---|---:|---:|
| Jobs per run | 4 | ? |
| Longest `needs` chain | 2 | 2 |
| Max concurrent jobs | 3 | ? |

- Job `test` matrix is computed at runtime; job count unknown

---
<sub>Static analysis of workflow files only. No code from this pull request was executed. Commit `abc1234`.</sub>
