<!-- quanto:summary -->
## quanto

Execution changes in 1 workflow file.

### `.github/workflows/ci.yml`

| Metric | Before | After |
|---|---:|---:|
| Jobs per run | 1 | 2 |
| Longest `needs` chain | 1 | 2 |
| Max concurrent jobs | 1 | 1 |

- Job added: `publish`
- Longest `needs` chain: 1 → 2 jobs

---
<sub>Static analysis of workflow files only. No code from this pull request was executed. Commit `abc1234`.</sub>
