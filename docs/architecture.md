# ProofCode Architecture

## Runtime profiles

Desktop profile embeds the Go engine in Wails and stores local metadata in an embedded database. It has no mandatory server dependencies. Cloud profile uses the Spring Boot control plane, PostgreSQL, Redis, ActiveMQ Artemis, and a horizontally scalable Go runner pool.

## Ownership boundaries

- Control plane owns identity, projects, task lifecycle, authorization, reliable dispatch, runner registration, and client event delivery.
- Agent engine owns model interaction, context construction, tool execution, worktree isolation, checkpoints, browser validation, and agent coordination.
- PostgreSQL is the system of record. Redis contains expiring coordination state only. Artemis carries durable background commands. Token deltas use a streaming connection instead of the broker.

## Task lifecycle

```text
CREATED -> QUEUED -> RUNNING -> VERIFYING -> SUCCEEDED
                 |          |             -> FAILED
                 |          -> WAITING_APPROVAL
                 -> CANCELLED
```

Task creation and Outbox insertion occur in one PostgreSQL transaction. The dispatcher publishes the Outbox record to Artemis and marks it delivered only after broker acknowledgement. Runners process commands idempotently by task identifier.

## One writer rule

Main Agent is the only role permitted to change files. Scout agents receive read-only tools. Verifier agents receive read-only, test, and browser tools. This rule prevents concurrent agents from corrupting the same checkout.

## Context retrieval

The retrieval pipeline combines exact repository search, PostgreSQL full text search, trigram similarity, vector similarity, symbol edges, recent Git changes, and active task files. Reciprocal Rank Fusion combines ranks without assuming comparable raw scores. Every selected chunk retains its path, line range, content hash, and retrieval reason.

