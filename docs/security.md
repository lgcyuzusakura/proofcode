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

## Native development pages

The Windows IDE and command console are explicit user actions on registered
local directories. The console runs with the desktop user's permissions and
environment; it is not an agent sandbox. Its methods are not offered as model
tools. npm/npx batch wrappers are resolved to Node entry points without shell
argument interpolation. A project has one active command and bounded output;
Stop and app shutdown terminate its Windows process tree.

Editor writes compare the opened content hash and preserve local history.
Git actions require a refreshed status fingerprint, operate only at the
registered repository root, and stage explicit source paths. Remote URL
userinfo and query strings are redacted before displaying origin. Git's own hooks
and credential configuration still apply. These checks cannot make a Git
command atomic against other programs changing the same checkout.

The authenticated source-files API validates the project/workspace/snapshot
chain. User authentication is required; runner credentials cannot use this
public endpoint. Authentication remains shared bearer-token based rather than
per-user project authorization.

Desktop preview pages run in a temporary Chromium profile. They have no Wails
bridge and do not inherit personal browser logins. An optional Runner browser
is separately scoped to one code task and retains normal EXEC approval for
navigation, clicks and typing. Its network access is not restricted to the
project. Standard Docker blocks Chromium user namespace sandboxes; the
explicit Compose setting RUNNER_BROWSER_NO_SANDBOX=true uses the non-root
Runner container as the deployment isolation boundary without granting extra
container capabilities. Supported hosts can set it to false.

Conversation Markdown disables raw HTML and automatic remote image loads.
Editor drafts are scoped to the project in sessionStorage; review notes use
device-local localStorage. Neither is encrypted or a server-synchronized
backup. Persisted source snapshots and model requests contain the selected
source contents according to the configured deployment and provider.

## Secrets

The runner passes its model API key only to the model HTTP client and filters secret-like environment variable names from child commands. The Kev/Jev sidecar receives a bounded summary of the user request and recent tool output when enabled. Development tokens and database passwords use placeholder defaults and must be replaced before deployment. Encrypted secret storage and comprehensive log redaction are not yet implemented.
