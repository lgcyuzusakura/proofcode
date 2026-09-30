# ProofCode

ProofCode is a local-first AI coding agent focused on verifiable, reversible changes. The Go agent engine powers containerized Web runners and the Windows Wails desktop shell.

## What is implemented

- OpenAI-compatible model provider with tool calling
- Bounded agent loop with cancellation, budgets, tool audit events, and deterministic mock provider
- Safe workspace tools for file reads, repository search, structured patches, commands, and Git diffs
- Git worktree isolation and checkpoints
- Hybrid context retrieval contracts for lexical, symbol, and vector results
- Conditional Main, Scout, and Verifier coordination with one-writer enforcement
- Browser validation through Chrome DevTools Protocol
- MCP stdio extension configuration and JSON-RPC client
- Spring Boot control plane with PostgreSQL, Redis, ActiveMQ Artemis, Outbox delivery, task APIs, and WebSocket events
- Go Runner consuming Artemis AMQP 1.0 tasks with database-backed leases, cancellation polling, isolated clone/worktree execution, event callbacks, and checkpoints
- React Web interface with project/task creation, live task events, cancellation, and task capsule timeline
- Docker Compose development environment

## Quick start

1. Copy `.env.example` to `.env` and set `MODEL_API_KEY`.
2. Start Docker Desktop.
3. Run `docker compose up --build`.
4. Open `http://localhost:3000`.

For a desktop UI preview without installing Go/Wails, run `.\desktop\start-desktop.ps1`. For the native Windows Wails app, run `.\desktop\start-native.ps1`; rebuild it with `.\desktop\build-native.ps1`. Stop the static preview server with `.\desktop\stop-desktop.ps1`.

The development Compose profile enables Runner writes and command execution so a task can complete without a remote approval UI. Set `RUNNER_ALLOW_WRITE=false` or `RUNNER_ALLOW_EXEC=false` when testing approval pauses.

The default model endpoint is OpenAI-compatible and can be changed with `MODEL_BASE_URL` and `MODEL_NAME`. Use `DEV_AUTH_TOKEN` as the bearer token in API requests.

## Repository layout

```text
agent-engine/   Go agent runtime, tools, retrieval, browser, MCP and runner
control-plane/  Java task control plane and reliable dispatch
frontend/       Shared React UI and Web application
desktop/        Windows desktop preview launcher
protocol/       Versioned JSON and event contracts
deploy/         Container configuration
docs/           Architecture, security and operations documentation
thesis/         Thesis sources and generation scripts
```

## Development checks

```powershell
docker compose -f compose.test.yml up --build --abort-on-container-exit
```

No model key is required for unit and contract tests.
