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
  "timeout_seconds": 1800
}
```

The command is executed directly without a shell or interactive stdin from a private snapshot of the exact commit proven on the pushed remote branch, plus the tag when supplied. Only a small baseline environment and explicitly declared variables are passed. Variables in `env` must be set. Variables in `optional_env` are passed only when set; the repository command owns any fallback and validates related inputs. An explicitly empty value remains empty. Both lists reject invalid names, duplicates, and reserved `DEPLOY_IT_` names.

`revision_policy` defaults to `exact_tip`. Configuration installers that can safely apply a frozen patch after another push can set it to `branch_ancestor`. This fetches the configured remote branch into a private proof ref and requires the selected commit to be its ancestor. The command still runs from that commit's snapshot, and an optional tag must still match that exact commit.

`deploy-it` resolves the pushed revision, or accepts explicit commit, branch, remote, and optional tag arguments. The command receives `DEPLOY_IT_COMMIT`, `DEPLOY_IT_BRANCH`, `DEPLOY_IT_TAG`, and `DEPLOY_IT_ENVIRONMENT`.

Normal no-argument and explicit deploy calls enqueue a durable operation and return its handle; they do not wait for Jenkins or live acceptance. Operation records and logs are kept under `~/.local/state/deploy-it/operations`, or under `DEPLOY_IT_STATE_DIR` when set. The operation ID is stable for a repository, environment, and commit. Workers survive the invoking hook, and operations targeting the same environment are serialized.

Use `deploy-it status HANDLE` for the JSON receipt, `deploy-it logs HANDLE` for retained command output, or `deploy-it observe HANDLE` to refresh status without starting activation. Status states are `queued`, `running`, `passed`, or `failed`. Live acceptance remains `unknown` until the repository command writes a matching receipt after its live checks pass. Commands receive `DEPLOY_IT_OPERATION_ID` and `DEPLOY_IT_RECEIPT_FILE`; the receipt must identify the shipped revision, target, and `"passed": true`. The operation log retains Jenkins queue and build handles printed by the command.

An interrupted or failed operation is not submitted again automatically. Check its Jenkins queue/build and destination state before taking recovery action; this avoids starting duplicate activation when the outcome is uncertain. A successful Git push or Jenkins job alone does not prove that the intended revision is live.

For a command that uses an existing authenticated CLI configuration, declare `HOME` in `env` and any supported configuration overrides in `optional_env`. The command must still verify authentication before deployment.

The shipped, tracked contract is the deployment authority; there is no machine-local trust gate. `deploy-it` with no arguments enqueues `HEAD` using the committed revision policy and the branch's configured push destination. Local worktree and index changes do not block deployment or enter the deployment snapshot. The working agent commits and normal-pushes completed work, then invokes `deploy-it` during the active turn. Release tags are optional. Run `deploy-it check` to validate a contract without deploying and `deploy-it install` to install the binary and global skill.

Use `deploy-it --wait` to enqueue and observe the final repository command result, or `deploy-it wait HANDLE` to observe an existing operation. Stopping a waiter leaves the detached command running. `deploy-it retry HANDLE` retries only failures whose durable record proves the repository command never started. Each retry keeps the previous attempt receipt. Once a command may have run, reconcile the existing Jenkins operation and target through the repository’s recovery path.

Jenkins job `deploy-deploy-it` tests and installs the exact pushed main revision at `/Users/aedev/.local/bin/deploy-it` through the shared fixed-target CLI controller. The tracked handoff only submits or observes that job. The existing tag-release workflow remains until its cross-platform release publication is replaced and verified in Jenkins.

Copyright © 2026 ColinKnapp.com. All rights reserved.
