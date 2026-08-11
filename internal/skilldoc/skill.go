package skilldoc

const SkillMD = `---
name: deploy-it
description: Deploy an already-shipped Git revision through the compiled deploy-it contract. Use for deployment, rollout, release activation, production or staging delivery, post-ship deployment, deploy.sh replacement, or when ship-it invokes deploy-it after a successful push.
---

# Deploy It

Treat Git shipping and deployment as separate proofs. Let ` + "`ship-it`" + ` finish synchronization, commit, merge, tag, and push; then let its automatic ` + "`deploy-it`" + ` handoff execute the tracked deployment contract.

Require a tracked root ` + "`.deploy-it.json`" + ` with version 1, ` + "`after_ship: true`" + `, an explicit environment, a 1-3600 second timeout, a literal repo-local command argv, and an explicit environment-variable allowlist. Deploy-it proves the commit on the pushed remote branch and tag, then runs a private snapshot of that commit without a shell or interactive stdin. Never guess deployment from filenames, Make targets, Compose files, package scripts, SSH hosts, or infrastructure state.

Local trust is an authorization boundary outside the repository. Only run ` + "`deploy-it trust`" + ` after the user explicitly authorizes that exact repository contract; never infer permission from a tracked file or invoke trust automatically. Re-trust after the manifest or executable changes. Use ` + "`deploy-it check`" + ` to validate without deployment. A manifest absent from the shipped commit means the repository is intentionally non-deployable and is skipped. An invalid or untrusted contract, unsafe executable, missing remote revision, timeout, or failed command is a hard failure.

Do not rerun failed deployments automatically. Report that Git shipping already succeeded, preserve the exact failure, and require diagnosis before another deploy. The repository command owns its bounded health check and application-level acceptance assertion.
`

const OpenAIYAML = `interface:
  display_name: "Deploy It"
  short_description: "Deploy shipped revisions through one contract"
  default_prompt: "Use $deploy-it to deploy the revision that ship-it just shipped."
`
