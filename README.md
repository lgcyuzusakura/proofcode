# ProofCode

ProofCode is a local-first AI coding agent focused on verifiable, reversible changes. The Go agent engine powers containerized Web runners and the Windows Wails desktop shell.

## What is implemented

- OpenAI-compatible model provider with tool calling
- Optional Kev/Jev decision sidecar for fixed-set tool routing. It never generates tool arguments or bypasses approval; low-confidence and unavailable decisions fall back to the normal model loop and are audited.
- Bounded agent loop with cancellation, budgets, tool audit events, and deterministic mock provider
- Safe workspace tools for file reads, repository search, structured patches, commands, and Git diffs
- Git worktree isolation and checkpoints
- Hybrid context retrieval contracts for lexical, symbol, and vector results (not yet wired into runner tasks)
- Conditional Main, Scout, and Verifier coordination with one-writer enforcement
- Browser validation library through Chrome DevTools Protocol (not yet wired into runner tasks)
- MCP stdio extension configuration and JSON-RPC client (not yet wired into runner tasks)
- Spring Boot control plane with PostgreSQL, Redis, ActiveMQ Artemis, Outbox delivery, task APIs, and WebSocket events
- Go Runner consuming Artemis AMQP 1.0 tasks with database-backed leases, cancellation polling, isolated clone/worktree execution, event callbacks, and checkpoints
- React Web interface with project/task creation, live task events, cancellation, and task capsule timeline
- Explicit artifact review and clean-checkout apply flow (`proofcode-apply`) with base revision verification
- Docker Compose development environment

## Quick start

1. Copy `.env.example` to `.env` and set `MODEL_API_KEY`.
2. Start Docker Desktop.
3. Run `docker compose up --build`.
4. Open `http://localhost:3000`.

Compose waits for PostgreSQL, Redis, Artemis, and the control-plane health endpoint before starting dependent services. If one of the default host ports is already occupied, copy `.env.example` to `.env` and override the `PROOFCODE_*_PORT` values (for example, `PROOFCODE_WEB_PORT=3010` and `PROOFCODE_CONTROL_PORT=8090`).

For a desktop UI preview without installing Go/Wails, run `.\desktop\start-desktop.ps1`. For the native Windows Wails app, run `.\desktop\start-native.ps1`; rebuild it with `.\desktop\build-native.ps1`. Stop the static preview server with `.\desktop\stop-desktop.ps1`.

The Compose profile defaults to approval for writes and commands. Set `RUNNER_ALLOW_WRITE=true` or `RUNNER_ALLOW_EXEC=true` only when automatic execution is appropriate for the repository.

The bundled runner image includes Go 1.24, Node.js/npm/npx, Python 3/pytest, Git, and ripgrep. `RUNNER_ALLOWED_PROGRAMS` defaults to those installed commands. For Java, Rust, .NET, or project-specific binaries, build a runner image with those toolchains and explicitly extend the allowlist.

### Optional Jev routing

Run a local TypeSafe-compatible Kev/Jev service and set `JEV_BASE_URL` for the runner. `JEV_MODE=observe` records a Choice decision for every main-agent turn while exposing the complete tool set to the language model. `JEV_MODE=route` exposes only the selected tool when confidence is at least `JEV_MIN_CONFIDENCE`; otherwise the complete tool set is restored. The default Compose profile keeps this feature off unless explicitly configured.

Jev receives the user request and recent tool context plus fixed tool names and descriptions. It cannot invent tools or argument JSON. Deterministic command policy, workspace checks, write/exec approval, and cancellation remain authoritative. The live decision response is stored as `decision.tool_routed` event evidence.

### Applying a reviewed artifact

The runner never writes into the developer's checkout. After reviewing a `checkpoint` or `recovery` artifact, apply it explicitly to a clean checkout at the exact recorded base revision:

```powershell
$env:PROOFCODE_AUTH_TOKEN = "..."
go -C agent-engine run ./cmd/proofcode-apply -url http://localhost:8080 -task <task-id> -repo D:\path\to\checkout -kind checkpoint
```

The command performs a dry `git apply --check`, verifies the checkout is clean and at the artifact base SHA, and only then applies unstaged changes.

On this development machine, an ignored local Go installation is available at `.tools/go/bin/go.exe`. Use `& .\.tools\go\bin\go.exe -C agent-engine run ./cmd/proofcode-apply ...` when `go` is not on `PATH`. The runner Docker image also contains `proofcode-apply`; use `docker compose run --rm --no-deps --entrypoint proofcode-apply` with a checkout mounted at `/repo` when no host Go installation is available. On Windows, configure `core.autocrlf=true` in that clean checkout before applying through the Linux image so Git recognizes Windows line endings correctly.

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

## End-to-end smoke test

`compose.smoke.yml` adds a deterministic OpenAI-compatible streaming model for an isolated integration run. It creates two small Go files, runs their focused test, invokes the verifier, and produces a reviewable checkpoint artifact. Start it with a separate Compose project and free host ports, then create a project for `https://github.com/lgcyuzusakura/proofcode.git` (branch `main`) and a short task through the Web UI:

```powershell
$env:PROOFCODE_POSTGRES_PORT = '15432'
$env:PROOFCODE_REDIS_PORT = '16379'
$env:PROOFCODE_ARTEMIS_CORE_PORT = '16116'
$env:PROOFCODE_ARTEMIS_AMQP_PORT = '15672'
$env:PROOFCODE_ARTEMIS_CONSOLE_PORT = '18161'
$env:PROOFCODE_CONTROL_PORT = '18090'
$env:PROOFCODE_WEB_PORT = '13000'
docker compose -p proofcode-smoke -f compose.yml -f compose.smoke.yml up -d --build
```

Open `http://localhost:13000`. To include the local Kev/Jev service, also set `SMOKE_JEV_MODE=observe` and `SMOKE_JEV_BASE_URL=http://host.docker.internal:8009` before starting Compose. This fixture verifies the plumbing and does not assess model quality. Normal coding tasks require a configured OpenAI-compatible endpoint and API key.
