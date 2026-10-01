<!-- quanto:summary -->
## quanto

Execution changes in 1 workflow file.

### `.github/workflows/ci.yml`

| Metric | Before | After |
|---|---:|---:|
| Jobs per run | 2 | 2 |
| Longest `needs` chain | 2 | 0 |
| Max concurrent jobs | 1 | 0 |

- `needs` cycle: a → b → a

---
<sub>Static analysis of workflow files only. No code from this pull request was executed. Commit `abc1234`.</sub>
