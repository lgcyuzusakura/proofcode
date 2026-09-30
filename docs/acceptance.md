# Version 0.4 Acceptance Criteria

## Daily use

- Open a Git repository and complete a multi-file coding task without modifying the base worktree.
- Resume a task after process restart from persisted messages and checkpoints.
- Inspect every selected context chunk, tool call, patch, command, test result, browser artifact, and model usage record.
- Accept or reject the final change set.

## Agent behavior

- Simple tasks remain single-agent.
- Cross-module tasks may start at most two read-only Scouts.
- All completed changes receive an independent Verifier pass.
- Only Main Agent can write files.
- Cancellation stops model streams and child processes.

## Reliability

- Cloud task creation and dispatch use a transactional Outbox.
- Duplicate delivery does not create a second task execution.
- Failed jobs retry with a bounded count and then enter a dead letter queue.
- Redis loss does not remove durable tasks or events.

## Performance targets

- Desktop cold start under two seconds on the reference development machine.
- Incremental indexing of one changed source file under one second excluding remote embedding latency.
- Exact search results begin within 500 milliseconds for a repository with 100,000 files on SSD storage.
- Command cancellation is observed within one second.

