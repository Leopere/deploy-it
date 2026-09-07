package skilldoc

const SkillMD = `---
name: deploy-it
description: Deploy an already-shipped Git revision through the compiled deploy-it contract. Use for deployment, rollout, release activation, production or staging delivery, post-ship deployment, deploy.sh replacement, or when ship-it invokes deploy-it after a successful push.
---

# Deploy It

For an authorized destination, complete the requested change and its focused checks. Prepare the repository command to deploy that change and check the live result. Native ` + "`ship-it`" + ` hooks push the revision, then invoke ` + "`deploy-it`" + ` for repositories with a tracked deployment contract.

Use the shortest existing local build and application rollout. Use GitHub Actions by default only for static GitHub Pages sites. Preserve a working legacy pipeline until its local replacement passes the required signing, revision, and live acceptance checks.

A root ` + "`.deploy-it.json`" + ` declares version 1, ` + "`after_ship: true`" + `, environment, literal repository command argv, environment-variable allowlist, and a 1–300 second timeout. The command runs without interactive stdin from a private snapshot of the exact pushed commit. It receives ` + "`DEPLOY_IT_COMMIT`" + `, ` + "`DEPLOY_IT_BRANCH`" + `, ` + "`DEPLOY_IT_TAG`" + `, and ` + "`DEPLOY_IT_ENVIRONMENT`" + `. An explicit tag is optional. The no-argument invocation resolves the branch's configured push destination and needs no release tag. Local worktree and index changes do not block deployment or enter the snapshot. Native ship-it hooks stage all changes, commit when needed, and push before invoking deploy-it.

The tracked contract is the deployment authority; there is no machine-local trust gate. Never guess a production target. Read existing project configuration and the user's destination to create or repair a missing command. A missing manifest is setup work when the target is known. Ask only when the destination or required access cannot be established from available evidence. Use ` + "`deploy-it check`" + ` for contract validation without deployment.

Variables in ` + "`env`" + ` must be set. Use ` + "`optional_env`" + ` for explicitly allowed variables that the command can handle when absent. The command owns fallback authentication and validation of related optional inputs. Neither list permits invalid names, duplicates, or reserved ` + "`DEPLOY_IT_`" + ` names. Undeclared variables remain excluded.

If deployment fails, inspect the command output and destination state. Fix recoverable causes within the authorized scope. After diagnosis, rerun the affected command when its effects are understood. Do not blindly repeat a failed command or roll back an unknown result. A failed attempt alone does not establish a blocker. Report an unresolved blocker with the attempted command, observed failure, and specific external input needed.

The repository command owns health and application acceptance checks. Report production success only after those checks pass. Git push success alone is not deployment proof. Commands share deploy-it's five-minute budget, or the earlier inherited deadline. An expired deadline records a failure; it does not mean the destination is healthy.

Copyright © 2026 ColinKnapp.com. All rights reserved.
`

const OpenAIYAML = `interface:
  display_name: "Deploy It"
  short_description: "Deploy shipped revisions through one contract"
  default_prompt: "Use $deploy-it to deploy the revision that ship-it just shipped."
`
