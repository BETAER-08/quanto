<!-- quanto:summary -->
## quanto

Execution changes in 1 workflow file.

### `.github/workflows/ci.yml`

- `pull_request` `branches` filter: +`develop`
- `pull_request` `paths-ignore` filter: +`docs/**`
- `push` `branches` filter: +`hotfix/**`
- `push` `paths` filter: −`go.mod`

---
<sub>Static analysis of workflow files only. No code from this pull request was executed. Commit `abc1234`.</sub>
