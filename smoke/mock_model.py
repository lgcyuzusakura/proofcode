"""Deterministic OpenAI-compatible stream for the Compose coding workflow smoke test."""

import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


SOURCE = "package smoke\n\nfunc Add(a, b int) int { return a + b }\n"
TEST = (
    'package smoke\n\nimport "testing"\n\n'
    "func TestAdd(t *testing.T) {\n"
    "    if got := Add(2, 3); got != 5 { t.Fatalf(\"Add(2, 3) = %d\", got) }\n"
    "}\n"
)


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/healthz":
            self.send_error(404)
            return
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok\n")

    def do_POST(self):
        if self.path != "/v1/chat/completions":
            self.send_error(404)
            return
        length = int(self.headers.get("Content-Length", "0"))
        request = json.loads(self.rfile.read(length))
        messages = request.get("messages", [])
        system = next((item.get("content", "") for item in messages if item.get("role") == "system"), "")
        if "independent read-only verifier" in system:
            response = {"content": "PASS: the focused Go test completed.", "tool": None}
        else:
            results = [item for item in messages if item.get("role") == "tool"]
            if results and json.loads(results[-1].get("content", "{}")).get("isError"):
                self.send_error(422, "smoke tool execution failed")
                return
            stage = len(results)
            if stage == 0:
                response = {"tool": "read_file", "arguments": {"path": "README.md"}}
            elif stage == 1:
                response = {"tool": "apply_patch", "arguments": {"edits": [
                    {"path": "agent-engine/smoke.go", "create": True, "new_text": SOURCE},
                    {"path": "agent-engine/smoke_test.go", "create": True, "new_text": TEST},
                ]}}
            elif stage == 2:
                response = {"tool": "run_command", "arguments": {
                    "program": "go", "args": ["-C", "agent-engine", "test", "."], "timeout_seconds": 120,
                }}
            else:
                response = {"content": "Created a small Go function and test; go test . passed.", "tool": None}

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        if response["tool"] is None:
            delta = {"content": response["content"]}
        else:
            delta = {"tool_calls": [{
                "index": 0,
                "id": "smoke-call-" + str(len([item for item in messages if item.get("role") == "tool"]) + 1),
                "type": "function",
                "function": {"name": response["tool"], "arguments": json.dumps(response["arguments"])},
            }]}
        self.wfile.write(("data: " + json.dumps({"choices": [{"delta": delta}]}) + "\n\n").encode())
        self.wfile.write(b'data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":10}}\n\n')
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()


ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
