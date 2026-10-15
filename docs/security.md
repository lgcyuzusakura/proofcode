# Security Model

## Trust boundaries

Model output is untrusted. Repository contents are untrusted. MCP servers are untrusted extensions. Tool arguments pass through schema validation, workspace path validation, policy evaluation, approval, execution limits, and audit logging.

The optional Kev/Jev sidecar is also untrusted input. Its choice must match
the candidates ProofCode supplied, and its probabilities must be finite. It
cannot generate arguments, approve a write or command, or suppress the
approval boundary. Invalid responses fall back to normal LLM tool selection
and produce an audit event.

## Desktop execution

The Go runner executes direct programs inside a container and a task-specific Git worktree. It fixes the working directory, strips sensitive environment variables from child processes, limits duration and output, rejects paths outside the workspace for built-in file tools, and requires approval for writes and commands unless the operator explicitly enables automatic execution. This is not a strong sandbox against malicious build scripts.

## Cloud execution

The Compose runner is a long-lived non-root container with a persistent workspace volume. It does not currently create a disposable container, disable networking, or enforce per-task CPU/memory/process limits. Each task attempt receives a separate Git worktree; stronger per-task isolation remains future work.

## Secrets

The runner passes its model API key only to the model HTTP client and filters secret-like environment variable names from child commands. The Kev/Jev sidecar receives a bounded summary of the user request and recent tool output when enabled. Development tokens and database passwords use placeholder defaults and must be replaced before deployment. Encrypted secret storage and comprehensive log redaction are not yet implemented.
