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
  "timeout_seconds": 900
}
```

The command is executed directly without a shell or interactive stdin from a private snapshot of the exact commit proven on the pushed remote branch and tag. Only a small baseline environment plus the manifest's explicit `env` allowlist is passed. `ship-it` supplies the immutable commit, branch, tag, and remote; the command receives `DEPLOY_IT_COMMIT`, `DEPLOY_IT_BRANCH`, `DEPLOY_IT_TAG`, and `DEPLOY_IT_ENVIRONMENT`.

Before the first deployment—or after changing the manifest or executable—review them and explicitly run `deploy-it trust`. Trust is stored outside the repository and scoped to its canonical local path and remote URL. Run `deploy-it check` to validate without deploying and `deploy-it install` to install the binary and global skill.
