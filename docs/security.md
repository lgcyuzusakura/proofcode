# Security Model

## Trust boundaries

Model output is untrusted. Repository contents are untrusted. MCP servers are untrusted extensions. Tool arguments pass through schema validation, workspace path validation, policy evaluation, approval, execution limits, and audit logging.

## Desktop execution

Desktop execution is a controlled local executor, not a strong sandbox. It fixes the working directory, strips sensitive environment variables, limits duration and output, rejects paths outside the workspace, and requires approval for commands or destructive patches. Optional Docker execution provides stronger isolation.

## Cloud execution

Cloud tasks run in disposable containers with a non-root user, read-only root filesystem, bounded CPU, memory and process count, no Docker socket, and networking disabled unless the task policy grants it. Each task receives a separate Git worktree and browser profile.

## Secrets

API keys are never included in model context or tool output. Server secrets are encrypted at rest. Runner and internal API credentials are separate from user tokens. Logs redact authorization headers and environment values matching configured secret names.

