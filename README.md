# deploy-it

`deploy-it` is the deliberately narrow handoff from a shipped Git revision to a repository-owned deployment contract. It never guesses how a project deploys.

A deployable repository tracks this root manifest:

```json
{
  "version": 1,
  "after_ship": true,
  "environment": "production",
  "command": ["./deploy.sh"],
  "env": ["CLOUDFLARE_API_TOKEN"],
  "optional_env": ["GH_CONFIG_DIR"],
  "timeout_seconds": 300
}
```

The command is executed directly without a shell or interactive stdin from a private snapshot of the exact commit proven on the pushed remote branch, plus the tag when supplied. Only a small baseline environment and explicitly declared variables are passed. Variables in `env` must be set. Variables in `optional_env` are passed only when set; the repository command owns any fallback and validates related inputs. An explicitly empty value remains empty. Both lists reject invalid names, duplicates, and reserved `DEPLOY_IT_` names.

`deploy-it` resolves the pushed revision, or accepts explicit commit, branch, remote, and optional tag arguments. The command receives `DEPLOY_IT_COMMIT`, `DEPLOY_IT_BRANCH`, `DEPLOY_IT_TAG`, and `DEPLOY_IT_ENVIRONMENT`.

For a command that uses an existing authenticated CLI configuration, declare `HOME` in `env` and any supported configuration overrides in `optional_env`. The command must still verify authentication before deployment.

The shipped, tracked contract is the deployment authority; there is no machine-local trust gate. `deploy-it` with no arguments deploys the current checkout only when its worktree is clean and `HEAD` matches its configured push destination. Release tags are optional. Run `deploy-it check` to validate a contract without deploying and `deploy-it install` to install the binary and global skill.

Copyright © 2026 ColinKnapp.com. All rights reserved.
