# ProofCode Architecture

## Runtime profiles

The Wails desktop shell currently hosts the shared React UI; task execution still depends on the control plane and runner. The Compose profile uses Spring Boot, PostgreSQL, Redis, ActiveMQ Artemis, and a Go runner with an isolated Git worktree per task attempt. The runner workspaces live on a persistent volume so an interrupted or failed task can retain its changes.

## Ownership boundaries

- Control plane owns identity, projects, task lifecycle, authorization, reliable dispatch, runner registration, and client event delivery.
- Agent engine owns model interaction, tool execution, worktree isolation, checkpoints, and agent coordination. Browser and hybrid retrieval packages exist but are not yet connected to the runner task loop.
- PostgreSQL is the system of record. Redis contains expiring coordination state only. Artemis carries durable background commands. Token deltas use a streaming connection instead of the broker.

## Task lifecycle

```text
CREATED -> QUEUED -> RUNNING -> VERIFYING -> SUCCEEDED
                 |          |             -> FAILED
                 |          -> WAITING_APPROVAL
                 -> CANCELLED
```

Task creation and Outbox insertion occur in one PostgreSQL transaction. The dispatcher publishes the Outbox record to Artemis and marks it delivered only after broker acknowledgement. Attempt numbers prevent stale queue deliveries and events from being attributed to a retried task. A runner journals tool intent before execution and does not replay an interrupted call whose outcome is unknown.

## One writer rule

Main Agent is the only role permitted to change files. Scout and Verifier agents receive read-only tools. This rule prevents concurrent agents from corrupting the same checkout.

## Decision sidecar

The optional Kev/Jev SystemOne sidecar chooses from the registered tool names.
In `observe` mode its distribution is audit evidence and the language model
still sees every tool. In `route` mode a choice above the configured threshold
narrows the tools sent to the language model. `defer`, low confidence, invalid
responses, timeouts, and outages restore the full list. The language model
still generates arguments and cannot bypass deterministic policy or approval.
The reported confidence is not a measured accuracy rate.

## Artifact handoff

Agent work is isolated in an attempt-scoped Git worktree. Checkpoints and
recovery patches are durable artifacts. The `proofcode-apply` command requires
a clean local checkout at the artifact base SHA and applies the patch only
after `git apply --check`.

## Context retrieval

The current runner exposes paged file listing, bounded file reading, and `rg` search to the model. Hybrid retrieval structures and database tables are present but are not yet wired into task execution.
