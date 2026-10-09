"""Isolated deterministic model/Git/database fixture; never a model benchmark."""

import json
import os
import subprocess
import tempfile
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit


ROOT = Path(tempfile.mkdtemp(prefix="proofcode-fixture-"))
DIFF = (
    "diff --git a/add.go b/add.go\n--- a/add.go\n+++ b/add.go\n"
    "@@ -1,3 +1,3 @@\n package example\n \n"
    "-func Add(a, b int) int { return a - b }\n"
    "+func Add(a, b int) int { return a + b }\n"
)


def command(args, cwd=None):
    return subprocess.run(args, cwd=cwd, check=True, capture_output=True, text=True).stdout.strip()


def database(sql):
    return command([
        "psql", "-X", "-A", "-t", "-h", "data-postgres", "-U", "fixture",
        "-d", "fixture", "-v", "ON_ERROR_STOP=1", "-c", sql,
    ])


def initialize():
    source, bare = ROOT / "source", ROOT / "repo.git"
    source.mkdir()
    command(["git", "init", "-q", "-b", "main", str(source)])
    files = {
        "go.mod": "module example\n\ngo 1.22\n",
        "add.go": "package example\n\nfunc Add(a, b int) int { return a - b }\n",
        "add_test.go": (
            'package example\n\nimport "testing"\n\n'
            'func TestAdd(t *testing.T) { if Add(2,3)!=5 { t.Fatal("wrong Add") } }\n'
        ),
        ".env": "DUMMY_NON_SECRET=fixture-only\n",
        "README.md": "Fixture for controlled experimental execution, not real model quality.\n",
    }
    for name, content in files.items():
        (source / name).write_text(content, encoding="utf-8")
    command(["git", "add", "."], source)
    command(["git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.com", "commit", "-qm", "fixed input"], source)
    revision = command(["git", "rev-parse", "HEAD"], source)
    (source / "add.go").write_text("package example\n\nfunc Add(a, b int) int { return a * b }\n", encoding="utf-8")
    command(["git", "add", "."], source)
    command(["git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.com", "commit", "-qm", "moving main"], source)
    moved = command(["git", "rev-parse", "HEAD"], source)
    command(["git", "clone", "-q", "--bare", str(source), str(bare)])
    command(["git", "update-server-info"], bare)
    database("""
        DROP TABLE IF EXISTS demo;
        CREATE TABLE demo (id integer PRIMARY KEY, value text NOT NULL, mutation_count integer NOT NULL DEFAULT 0);
        CREATE OR REPLACE FUNCTION fixture_count_updates() RETURNS trigger LANGUAGE plpgsql AS $$
            BEGIN NEW.mutation_count := OLD.mutation_count + 1; RETURN NEW; END;
        $$;
        CREATE TRIGGER fixture_updates BEFORE UPDATE ON demo FOR EACH ROW EXECUTE FUNCTION fixture_count_updates();
        INSERT INTO demo(id,value) VALUES (1,'original');
    """)
    return {"repositoryUrl": "http://fixture:8000/repo.git", "sourceRevision": revision, "movedRevision": moved, "model": "fixture-only"}


INFO = initialize()


def tool_results(messages):
    results = []
    for message in messages:
        if message.get("role") != "tool":
            continue
        envelope = json.loads(message.get("content", "{}"))
        content = envelope.get("content", "")
        try:
            parsed = json.loads(content)
        except (ValueError, TypeError):
            parsed = {}
        results.append((envelope, parsed))
    return results


def tool_response(name, arguments):
    return {"tool": name, "arguments": arguments}


def data_response(messages):
    results = tool_results(messages)
    stage = len(results)
    if results and results[-1][0].get("isError"):
        return {"tool": None, "content": "The data operation was denied or failed; no retry requested."}
    if stage == 0:
        return tool_response("data_resources", {})
    resource = results[0][1][0]["id"]
    if stage == 1:
        return tool_response("data_schema", {"resourceId": resource})
    if stage == 2:
        return tool_response("data_plan", {
            "resourceId": resource,
            "schemaVersion": results[1][1]["schemaVersion"],
            "idempotencyKey": "approved-demo-row-v1",
            "ir": {"kind": "mutation", "schema": "public", "table": "demo", "operation": "update",
                   "values": {"value": "approved"}, "filters": [{"column": "id", "operator": "eq", "value": 1}],
                   "expectedRows": 1},
        })
    if stage == 3:
        plan = results[2][1]
        return tool_response("data_execute", {"planId": plan["id"], "digest": plan["digest"]})
    return {"tool": None, "content": "The durable data gateway confirmed execution."}


def model_response(request):
    messages = request.get("messages", [])
    system = next((m.get("content", "") for m in messages if m.get("role") == "system"), "")
    if "scout" in system.lower() or "verifier" in system.lower():
        return {"tool": None, "content": "Fixture repository reviewed."}
    current = next((m.get("content", "") for m in reversed(messages) if m.get("role") == "user"), "")
    if "PROOFCODE_CHAT_SMOKE" in current:
        if request.get("tools"):
            raise ValueError("chat unexpectedly received execution tools")
        if current.endswith("SECOND"):
            history = [m.get("content", "") for m in messages if m.get("role") == "assistant"]
            if "Chat fixture: FIRST" not in history:
                raise ValueError("chat lost its previous completed answer")
        return {"tool": None, "content": "Chat fixture: " + current.split()[-1]}
    if "PROOFCODE_SOURCE_SMOKE" in current:
        return source_response(messages, current)
    if any("PROOFCODE_DATA_SMOKE" in str(m.get("content", "")) for m in messages if m.get("role") == "user"):
        return data_response(messages)
    if not request.get("tools"):
        return {"tool": None, "content": "```diff\n" + DIFF + "```"}
    stage = len(tool_results(messages))
    if stage == 0:
        return tool_response("read_file", {"path": ".env"})
    if stage == 1:
        return tool_response("read_file", {"path": "add.go"})
    if stage == 2:
        return tool_response("apply_patch", {"edits": [{"path": "add.go", "old_text": "func Add(a, b int) int { return a - b }", "new_text": "func Add(a, b int) int { return a + b }"}]})
    return {"tool": None, "content": "Fixed Add; the independent evaluator must verify the result."}


def source_response(messages, current):
    results = tool_results(messages)
    if results and results[-1][0].get("isError"):
        raise ValueError("source smoke tool failed: " + str(results[-1]))
    stage = len(results)
    if current.endswith("CREATE"):
        if stage == 0:
            return tool_response("apply_patch", {"edits": [
                {"path": "app.py", "create": True, "new_text": "def add(a, b):\n    return a + b\n"},
                {"path": "test_app.py", "create": True, "new_text": "from app import add\n\ndef test_add():\n    assert add(2, 3) == 5\n"},
            ]})
        if stage == 1:
            return tool_response("run_command", {"program": "python3", "args": ["-m", "pytest", "-q"]})
        return {"tool": None, "content": "Created and tested the source snapshot."}
    if stage == 0:
        return tool_response("read_file", {"path": "app.py"})
    if current.endswith("READ"):
        return {"tool": None, "content": "Verified the unchanged source snapshot."}
    if stage == 1:
        return tool_response("apply_patch", {"edits": [{"path": "app.py", "old_text": "return a + b", "new_text": "return a + b + 0"}]})
    if stage == 2:
        return tool_response("run_command", {"program": "python3", "args": ["-m", "pytest", "-q"]})
    return {"tool": None, "content": "Updated and tested the source snapshot."}


class Handler(SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=str(ROOT), **kwargs)

    def send_json(self, data):
        body = json.dumps(data).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = urlsplit(self.path).path
        if path == "/healthz":
            self.send_json({"ok": True})
        elif path == "/fixture":
            self.send_json(INFO)
        elif path == "/fixture/data":
            self.send_json(json.loads(database("SELECT json_build_object('value',value,'mutationCount',mutation_count) FROM demo WHERE id=1")))
        elif path.startswith("/repo.git/"):
            super().do_GET()
        else:
            self.send_error(404)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        if not 0 < length <= 2 << 20:
            self.send_error(413)
            return
        request = json.loads(self.rfile.read(length))
        if self.path == "/v1/systemone":
            question = request["questions"]["next_tool"]
            # Tool routing sees the full transcript before arguments are generated.
            state = request.get("state", "")
            selected = "apply_patch" if 'return a - b' in state else "read_file"
            probabilities = {key: float(key == selected) for key in question["criteria"]}
            self.send_json({"model": "fixture-jev", "answers": {"next_tool": {"type": "choice", "choice": selected, "confidence": 1, "probabilities": probabilities}}})
            return
        if self.path != "/v1/chat/completions":
            self.send_error(404)
            return
        response = model_response(request)
        if response["tool"] is None:
            delta = {"content": response["content"]}
        else:
            delta = {"tool_calls": [{"index": 0, "id": "fixture-call-" + str(len(tool_results(request.get("messages", []))) + 1), "type": "function", "function": {"name": response["tool"], "arguments": json.dumps(response["arguments"])}}]}
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        self.wfile.write(("data: " + json.dumps({"choices": [{"delta": delta}]}) + "\n\n").encode())
        self.wfile.write(b'data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20}}\n\n')
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()


ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
