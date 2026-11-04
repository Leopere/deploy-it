package skilldoc

const SkillMD = `---
name: deploy-it
description: Deploy an already-shipped Git revision through the compiled deploy-it contract. Use for deployment, rollout, release activation, production or staging delivery, post-ship deployment, deploy.sh replacement, or when ship-it invokes deploy-it after a successful push.
---

# Deploy It

For an authorized destination, complete the requested change and its focused checks. Prepare the repository command to deploy that change and check the live result. The working agent commits and normal-pushes completed work during the active turn, then invokes ` + "`deploy-it`" + ` for repositories with a tracked deployment contract.

Jenkins owns build, test, publication, deployment, and live acceptance. ` + "`deploy-it`" + ` submits or observes the exact pushed revision; it does not build, publish, or activate a release locally. Migrate existing GitHub Actions responsibilities to Jenkins and verify the replacement before removing the old workflow.

A root ` + "`.deploy-it.json`" + ` declares version 1, ` + "`after_ship: true`" + `, environment, literal repository command argv, environment-variable allowlist, and a 1–1800 second timeout. The command runs without interactive stdin from a private snapshot of the exact pushed commit. It receives ` + "`DEPLOY_IT_COMMIT`" + `, ` + "`DEPLOY_IT_BRANCH`" + `, ` + "`DEPLOY_IT_TAG`" + `, and ` + "`DEPLOY_IT_ENVIRONMENT`" + `. An explicit tag is optional. The no-argument invocation resolves the branch's configured push destination and needs no release tag. Local worktree and index changes do not block deployment or enter the snapshot. Commit and normal-push before invoking deploy-it; do not wait for a Stop hook.

The optional ` + "`revision_policy`" + ` field defaults to ` + "`exact_tip`" + `. Configuration installers can explicitly select ` + "`branch_ancestor`" + ` to prove that the frozen commit remains in the freshly fetched configured branch after another push. The policy comes from that exact commit, and optional tags still bind the exact commit. Repository commands retain their deployment and acceptance checks.

The tracked contract is the deployment authority; there is no machine-local trust gate. Never guess a production target. Read existing project configuration and the user's destination to create or repair a missing command. A missing manifest is setup work when the target is known. Ask only when the destination or required access cannot be established from available evidence. Use ` + "`deploy-it check`" + ` for contract validation without deployment.

Variables in ` + "`env`" + ` must be set. Use ` + "`optional_env`" + ` for explicitly allowed variables that the command can handle when absent. The command owns fallback authentication and validation of related optional inputs. Neither list permits invalid names, duplicates, or reserved ` + "`DEPLOY_IT_`" + ` names. Undeclared variables remain excluded.

If deployment fails, inspect the command output and destination state. Fix recoverable causes within the authorized scope. After diagnosis, rerun the affected command when its effects are understood. Do not blindly repeat a failed command or roll back an unknown result. A failed attempt alone does not establish a blocker. Report an unresolved blocker with the attempted command, observed failure, and specific external input needed.

The repository command owns health and application acceptance checks. Report production success only after those checks pass. Git push success alone is not deployment proof. Detached operations have a persisted 30-minute budget, independent of the hook's foreground deadline. Target queue time counts against that budget. An expired deadline records a failure; it does not mean the destination is healthy.

No-argument and explicit deploy calls return a durable operation handle without waiting for delivery. Use ` + "`deploy-it status HANDLE`" + ` for its JSON receipt, ` + "`deploy-it logs HANDLE`" + ` for retained output, and ` + "`deploy-it observe HANDLE`" + ` to refresh status without starting activation. Status distinguishes ` + "`queued`" + `, ` + "`running`" + `, ` + "`passed`" + `, and ` + "`failed`" + `. Live acceptance stays unknown until the repository command writes a receipt matching the operation's revision and target after its live checks pass. The command receives ` + "`DEPLOY_IT_OPERATION_ID`" + ` and ` + "`DEPLOY_IT_RECEIPT_FILE`" + `. Logs retain Jenkins queue and build handles.

Operation records default to ` + "`~/.local/state/deploy-it/operations`" + ` and can be relocated with ` + "`DEPLOY_IT_STATE_DIR`" + `. IDs are stable for a repository, environment, and commit, and deployments to one environment are serialized. Failed or interrupted operations are not submitted again automatically. Reconcile the existing Jenkins queue/build and destination before recovery so an uncertain activation is not duplicated.

Copyright © 2026 ColinKnapp.com. All rights reserved.
`

const OpenAIYAML = `interface:
  display_name: "Deploy It"
  short_description: "Deploy shipped revisions through one contract"
  default_prompt: "Use $deploy-it to deploy the exact revision that was committed and pushed."
`
