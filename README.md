# deploy-it (retired)

The deployment dispatcher is retired. Use normal Git commands to commit and push the intended main revision, then run that repository’s tracked Jenkins submission command for the exact pushed revision. Observe its existing operation until deployment and live acceptance finish.

This repository retains a minimal retirement CLI. `deploy-it help` explains the replacement workflow, and `deploy-it version` reports the binary version. Every other invocation fails without reading stdin, looking for configuration, running external commands, making network requests, installing files, or creating or resuming deployment operations.

The former dispatcher, installer, generated agent skill, release workflow and deployment entry points have been removed. Existing operation records, logs, backups and installed files are not deleted or migrated by this CLI. Retiring the installed executable and its Jenkins job is separate administrative cleanup; do not use the former dispatcher to deliver this retirement.

Run `go test ./...` for the retirement safeguards. This project no longer publishes or installs releases.

Copyright © 2026 ColinKnapp.com. All rights reserved.
