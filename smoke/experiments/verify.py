#!/usr/bin/env python3
"""End-to-end smoke for experiment profiles and approved project data writes.

Uses only Python's standard library. Results describe fixture plumbing, never
model quality or research measurements.
"""

from __future__ import annotations

import csv
import base64
import hashlib
import io
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any


CONTROL = os.environ.get("CONTROL_PLANE_URL", "http://control-plane:8080").rstrip("/")
FIXTURE = os.environ.get("FIXTURE_URL", "http://fixture:8000").rstrip("/")
USER_TOKEN = os.environ.get("PROOFCODE_AUTH_TOKEN", "experiment-smoke-user")
RUNNER_TOKEN = os.environ.get("PROOFCODE_RUNNER_TOKEN", "experiment-smoke-runner")
TOTAL_TIMEOUT = 300.0
POLL_INTERVAL = 1.0
ARTIFACTS = Path(os.environ.get("RESULTS_DIR", "/results"))
START = time.monotonic()


class SmokeFailure(RuntimeError):
    pass


class HttpFailure(SmokeFailure):
    def __init__(self, status: int, message: str):
        super().__init__(message)
        self.status = status


def require(condition: bool, message: str) -> None:
    if not condition:
        raise SmokeFailure(message)


def request(method: str, url: str, body: Any = None, token: str | None = None) -> tuple[Any, str, int]:
    headers = {"Accept": "application/json"}
    data = None
    if body is not None:
        data = json.dumps(body, separators=(",", ":")).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        remaining = START + TOTAL_TIMEOUT - time.monotonic()
        require(remaining > 0, "total smoke deadline exceeded")
        with urllib.request.urlopen(req, timeout=min(8.0, remaining)) as response:
            raw = response.read(4 * 1024 * 1024 + 1)
            if len(raw) > 4 * 1024 * 1024:
                raise SmokeFailure(f"{method} {url}: response exceeds 4 MiB")
            text = raw.decode("utf-8", "replace")
            content_type = response.headers.get("Content-Type", "")
            if "json" in content_type:
                try:
                    return json.loads(text), text, response.status
                except json.JSONDecodeError as error:
                    raise SmokeFailure(f"{method} {url}: invalid JSON response: {error}") from error
            return text, text, response.status
    except urllib.error.HTTPError as error:
        detail = error.read(8192).decode("utf-8", "replace")
        raise HttpFailure(error.code, f"{method} {url}: HTTP {error.code}: {detail}") from error
    except (urllib.error.URLError, TimeoutError) as error:
        raise SmokeFailure(f"{method} {url}: request failed: {error}") from error


def api(method: str, path: str, body: Any = None, *, internal: bool = False) -> Any:
    token = RUNNER_TOKEN if internal else USER_TOKEN
    return request(method, CONTROL + path, body, token)[0]


def fixture(path: str) -> Any:
    return request("GET", FIXTURE + path)[0]


def poll(label: str, fetch, accept, *, timeout: float = TOTAL_TIMEOUT) -> Any:
    deadline = min(START + TOTAL_TIMEOUT, time.monotonic() + timeout)
    last = None
    while time.monotonic() < deadline:
        last = fetch()
        if accept(last):
            return last
        time.sleep(min(POLL_INTERVAL, max(0.0, deadline - time.monotonic())))
    elapsed = time.monotonic() - START
    raise SmokeFailure(f"timeout waiting for {label} after {elapsed:.1f}s; last state: {json.dumps(last, ensure_ascii=False)[:1400]}")


def save_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def run_experiment(project: dict[str, Any], fixture_info: dict[str, Any]) -> dict[str, Any]:
    source_revision = fixture_info.get("sourceRevision")
    require(isinstance(source_revision, str) and len(source_revision) in (40, 64), "fixture sourceRevision must be a full Git commit hash")
    require(all(c in "0123456789abcdefABCDEF" for c in source_revision), "fixture sourceRevision is not hexadecimal")
    moved_revision = fixture_info.get("movedRevision")
    require(isinstance(moved_revision, str) and moved_revision != source_revision,
        "fixture main must have moved away from the pinned experiment commit")
    payload = {
        "projectId": project["id"],
        "name": "fixture experiment profiles A-F",
        "prompt": "PROOFCODE_EXPERIMENT_SMOKE: Fix Add(a,b) in add.go so it sums numbers; keep the tests meaningful.",
        "model": fixture_info.get("model", "fixture-only"),
        "baseCommit": source_revision,
        "maxSteps": 10,
        "testCommand": "go test ./...",
        "temperature": 0.1,
        "repetitions": 1,
    }
    created = api("POST", "/api/experiments", payload)
    experiment = created.get("experiment", {})
    experiment_id = experiment.get("id")
    require(experiment_id, "experiment create response has no experiment id")
    qs = urllib.parse.urlencode({"projectId": project["id"]})
    path = f"/api/experiments/{experiment_id}/compare?{qs}"

    comparison = poll("six terminal experiment runs", lambda: api("GET", path),
        lambda value: len(value.get("groups", [])) == 6
            and all(g.get("plannedRuns") == 1 and g.get("terminalRuns") == 1 for g in value["groups"]))
    save_json(ARTIFACTS / "compare.json", comparison)
    groups = comparison["groups"]
    require({g.get("group") for g in groups} == set("ABCDEF"), "comparison must contain groups A through F exactly")
    require(comparison.get("runCount") == 6, "experiment run count must be six")
    seen_profiles: set[str] = set()
    seen_task_ids: set[str] = set()
    feature_keys = ("toolsEnabled", "deterministicSafety", "jevRouting", "jevRequired", "ragEnabled", "contextCompressionEnabled", "feedbackRetrieval")
    features = {
        "A": (False, False, False, False, False, False, False),
        "B": (True, True, False, False, False, False, False),
        "C": (True, False, True, True, False, False, False),
        "D": (True, False, False, False, True, False, False),
        "E": (True, False, False, False, True, True, False),
        "F": (True, True, True, True, True, True, True),
    }
    for group in groups:
        profile = group.get("profile", {})
        signature = json.dumps({key: profile.get(key) for key in feature_keys}, sort_keys=True)
        seen_profiles.add(signature)
        letter = group.get("group")
        require(tuple(profile.get(key) for key in feature_keys) == features[letter], f"group {letter} has incorrect feature flags: {profile}")
        require(profile.get("mandatorySafety") is True, f"group {letter} disabled mandatory safety")
        require(group.get("validRuns") == group.get("profileAppliedRuns") == group.get("succeededRuns") == 1, f"group {letter} does not have one valid, applied, successful run")
        results = group.get("results", [])
        require(len(results) == 1, f"group {group.get('group')} must have exactly one result")
        result = results[0]
        require(result.get("terminal") is True and result.get("valid") is True, f"group {group.get('group')} result is not terminal and valid")
        require(result.get("status") == "SUCCEEDED", f"group {group.get('group')} did not succeed: {result.get('status')} {result.get('reason')}")
        require(result.get("profileApplied") is True, f"group {group.get('group')} profile was not applied")
        require(result.get("attempt") == 1, f"group {group.get('group')} expected attempt 1")
        require(result.get("taskId") not in seen_task_ids, "experiment groups unexpectedly reused a task")
        seen_task_ids.add(result.get("taskId"))
        observed = result.get("observedResult", {})
        require(observed.get("testsPassed") is True, f"group {group.get('group')} testsPassed must be true")
        require(observed.get("taskCompleted") is True, f"group {group.get('group')} taskCompleted must be true")
        terminal = poll(f"group {letter} terminal event", lambda: api("GET", f"/api/tasks/{result['taskId']}/events"),
            lambda events: any(e.get("type") == "task.completed" and e.get("attempt") == 1 for e in events))
        event = next(e for e in reversed(terminal) if e.get("type") == "task.completed" and e.get("attempt") == 1)
        raw_result = event.get("payload", {}).get("experimentResult", {})
        require(raw_result.get("profileVersion") == comparison["experiment"].get("profileVersion"), f"group {letter} profile version mismatch")
        require(raw_result.get("sourceRevision") == source_revision, f"group {letter} source revision differs from fixture commit")
        require(raw_result.get("experimentGroup") == letter, f"group {letter} was attributed to the wrong profile")
        require(raw_result.get("profileApplied") is True, f"group {letter} terminal event missing applied profile")
        measured = ("durationMs", "inputTokens", "outputTokens", "totalTokens", "toolCalls", "toolErrors", "invalidToolCalls", "highRiskBlocked", "retrievalCalls", "retrievalCacheHits", "jevDecisions", "jevApplied")
        for key in measured:
            metric = group.get("metrics", {}).get(key, {})
            require(metric.get("sampleCount") == 1 and metric.get("missingCount") == 0 and metric.get("mean") is not None, f"group {letter} measured metric {key} is absent")
        require(observed.get("usageReported") is True, f"group {letter} model usage was not reported")
        if letter in "DEF":
            require(observed.get("retrievalCalls", 0) > 0 and observed.get("snapshotId"), f"group {letter} did not perform actual retrieval")
        else:
            require(observed.get("retrievalCalls") == 0, f"group {letter} incorrectly performed retrieval")
        if letter in "CF":
            require(observed.get("jevDecisions", 0) > 0 and observed.get("jevApplied", 0) > 0, f"group {letter} did not apply Jev routing")
        else:
            require(observed.get("jevDecisions") == 0, f"group {letter} incorrectly applied Jev")
        if letter in "EF":
            require(observed.get("tokensBeforeCompression", 0) > 0 and observed.get("tokensAfterCompression", 0) > 0, f"group {letter} did not execute compression")
            require(observed.get("compressionTokenBasis") == "estimated", f"group {letter} compression token basis is not disclosed")
        else:
            require("tokensBeforeCompression" not in observed, f"group {letter} incorrectly reported compression")
    require(len(seen_profiles) == 6, "A-F profiles are not six distinct configurations")

    _, csv_text, status = request("GET", CONTROL + path + "&format=csv", token=USER_TOKEN)
    require(status == 200, "CSV comparison request failed")
    rows = list(csv.DictReader(io.StringIO(csv_text)))
    require(len(rows) == 6, f"CSV should have six rows, got {len(rows)}")
    require({row.get("group") for row in rows} == set("ABCDEF"), "CSV does not contain all A-F groups")
    for row in rows:
        require(row.get("valid") == "true" and row.get("testsPassed") == "true", f"CSV row {row.get('group')} is invalid or tests failed")
        require(row.get("sourceRevision") == source_revision, f"CSV row {row.get('group')} has wrong source revision")
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    save_json(ARTIFACTS / "compare.json", comparison)
    (ARTIFACTS / "compare.csv").write_text(csv_text, encoding="utf-8")
    return {"experimentId": experiment_id, "sourceRevision": source_revision, "groups": groups, "runCount": 6}


def task_state(task_id: str) -> dict[str, Any]:
    return api("GET", f"/api/tasks/{task_id}")


def approvals(task_id: str) -> list[dict[str, Any]]:
    return api("GET", f"/api/tasks/{task_id}/approvals")


def wait_approval(task_id: str) -> tuple[dict[str, Any], dict[str, Any]]:
    def fetch():
        task = task_state(task_id)
        values = approvals(task_id)
        pending = next((a for a in values if a.get("tool") == "data_execute" and a.get("status") == "PENDING"), None)
        if task.get("status") in ("FAILED", "CANCELLED", "SUCCEEDED"):
            raise SmokeFailure(f"database task became terminal before approval: {task.get('status')} {task.get('error')}")
        return {"task": task, "approval": pending}

    state = poll("database write approval request", fetch, lambda value: value.get("approval") is not None and value["task"].get("status") == "WAITING_APPROVAL")
    return state["task"], state["approval"]


def fixture_data() -> dict[str, Any]:
    return fixture("/fixture/data")


def assert_fixture_data(expected_value: str, expected_count: int, label: str) -> dict[str, Any]:
    current = fixture_data()
    require(current.get("value") == expected_value, f"{label}: database value {current.get('value')!r}, expected {expected_value!r}")
    require(current.get("mutationCount") == expected_count, f"{label}: mutationCount {current.get('mutationCount')!r}, expected {expected_count}")
    return current


def create_data_task(project_id: str, prompt: str) -> dict[str, Any]:
    return api("POST", "/api/tasks", {
        "projectId": project_id,
        "prompt": prompt,
        "model": "fixture-only",
    })


def run_database_approval(project_id: str) -> dict[str, Any]:
    before = assert_fixture_data("original", 0, "before approved workflow")
    resource = api("POST", f"/api/projects/{project_id}/data/resources", {
        "name": "experiment smoke postgres",
        "provider": "postgres",
        "environment": "dev",
        "secretRef": "SMOKE_DB",
        "allowedSchema": "public",
    })
    require(resource.get("provider") == "postgres", "registered data source is not PostgreSQL")
    task = create_data_task(project_id, "PROOFCODE_DATA_SMOKE: read demo row, prepare update, wait for user approval, then execute the exact approved plan")
    task_id = task.get("id")
    require(task_id, "database task response has no id")
    waiting_task, approval = wait_approval(task_id)
    pending_data = assert_fixture_data("original", 0, "before granting approval")
    require(waiting_task.get("status") == "WAITING_APPROVAL", f"expected WAITING_APPROVAL, got {waiting_task.get('status')}")

    arguments = approval.get("arguments") or {}
    plan_id, digest = arguments.get("planId"), arguments.get("digest")
    require(plan_id and digest, "data_execute approval is missing its immutable plan id/digest")
    pending_plan = api("GET", f"/internal/tasks/{task_id}/data/plans/{plan_id}?attempt=1", internal=True)
    require(pending_plan.get("status") == "PENDING" and pending_plan.get("digest") == digest, "approval plan is not pending or its digest differs")
    ir = pending_plan.get("ir", {})
    require(ir.get("operation") == "update" and ir.get("expectedRows") == 1, "approval plan is not the expected single-row update")
    require(ir.get("values", {}).get("value") == "approved", "approval plan does not set the approved value")
    require({f.get("column"): f.get("value") for f in ir.get("filters", [])}.get("id") == 1, "approval plan does not target row id=1")

    try:
        api("POST", f"/internal/tasks/{task_id}/data/plans/{plan_id}/execute-approved", {"attempt": 1, "digest": digest}, internal=True)
        raise SmokeFailure("pending plan unexpectedly accepted execution without approval")
    except HttpFailure as error:
        require(error.status == 409, f"pending plan execution returned unexpected HTTP {error.status}")
        unapproved = {"rejected": True, "httpStatus": error.status, "reason": "plan has not been approved"}
    assert_fixture_data("original", 0, "after unauthorized execution attempt")
    still_pending = api("GET", f"/internal/tasks/{task_id}/data/plans/{plan_id}?attempt=1", internal=True)
    require(still_pending.get("status") == "PENDING", "unauthorized execution changed the plan state")

    api("POST", f"/api/tasks/{task_id}/approvals/{approval['id']}", {"approved": True})
    completed = poll("approved database task completion", lambda: task_state(task_id), lambda value: value.get("status") in ("SUCCEEDED", "FAILED", "CANCELLED"))
    require(completed.get("status") == "SUCCEEDED", f"approved data task failed: {completed.get('error')}")
    after = assert_fixture_data("approved", 1, "after approval and runner execution")

    try:
        api("POST", f"/internal/tasks/{task_id}/data/plans/{plan_id}/execute-approved", {"attempt": 1, "digest": digest}, internal=True)
        raise SmokeFailure("terminal task unexpectedly accepted duplicate database execution")
    except HttpFailure as error:
        require(error.status == 409, f"terminal duplicate execution returned unexpected HTTP {error.status}")
        replay = {"rejected": True, "httpStatus": error.status, "reason": "terminal task cannot execute"}
    replay_state = assert_fixture_data("approved", 1, "after duplicate execute-approved")

    denied_task = create_data_task(project_id, "PROOFCODE_DATA_SMOKE_DENY: prepare the same demo update to approved, then wait for approval")
    denied_id = denied_task.get("id")
    require(denied_id, "denied workflow task response has no id")
    _, denied = wait_approval(denied_id)
    assert_fixture_data("approved", 1, "before denying approval")
    api("POST", f"/api/tasks/{denied_id}/approvals/{denied['id']}", {"approved": False})
    denied_done = poll("denied task completion", lambda: task_state(denied_id), lambda value: value.get("status") in ("SUCCEEDED", "FAILED", "CANCELLED"))
    require(denied_done.get("status") == "SUCCEEDED", f"denial response task did not finish cleanly: {denied_done.get('status')} {denied_done.get('error')}")
    denied_state = assert_fixture_data("approved", 1, "after denied approval")
    plans = api("GET", f"/api/projects/{project_id}/data/plans")
    approved_plan = next((p for p in plans if p.get("id") == plan_id), {})
    denied_plan = next((p for p in plans if p.get("id") == denied.get("arguments", {}).get("planId")), {})
    require(approved_plan.get("status") == "COMMITTED", "approved plan has no durable COMMITTED ledger")
    require(denied_plan.get("status") == "DENIED", "denied plan has no durable DENIED ledger")
    ledger = api("GET", f"/api/projects/{project_id}/data/audit")
    actions = [entry.get("action") for entry in ledger if entry.get("operationId") == plan_id]
    require(actions.count("EXECUTION_STARTED") == actions.count("EXECUTION_FINISHED") == 1 and "USER_APPROVED" in actions, "approved plan audit is missing or execution was duplicated")
    denied_actions = [entry.get("action") for entry in ledger if entry.get("operationId") == denied_plan.get("id")]
    require("USER_DENIED" in denied_actions and "EXECUTION_STARTED" not in denied_actions, "denied plan executed or denial audit is missing")
    require(before.get("mutationCount") == pending_data.get("mutationCount") == 0, "mutation occurred before approval")
    require(after.get("mutationCount") == replay_state.get("mutationCount") == denied_state.get("mutationCount") == 1, "mutation count changed during replay or denial")
    return {
        "resourceId": resource.get("id"),
        "approvedTaskId": task_id,
        "approvedPlanId": plan_id,
        "deniedTaskId": denied_id,
        "beforeApproval": before,
        "unapprovedExecution": unapproved,
        "afterApproval": after,
        "afterDuplicateExecution": replay_state,
        "terminalReplay": replay,
        "afterDenial": denied_state,
        "approvalWasRequired": waiting_task.get("status") == "WAITING_APPROVAL",
        "mutationCountStayedOneAfterReplayAndDenial": True,
    }


def finish_task(payload: dict[str, Any]) -> dict[str, Any]:
    task = api("POST", "/api/tasks", payload)
    completed = poll("source or chat task completion", lambda: task_state(task["id"]),
        lambda value: value.get("status") in ("SUCCEEDED", "FAILED", "CANCELLED"))
    require(completed.get("status") == "SUCCEEDED", f"task failed: {completed.get('error')}")
    return completed


def run_project_sources() -> dict[str, Any]:
    project = api("POST", "/api/projects", {"name": "Scratch source smoke", "sourceKind": "SCRATCH",
        "defaultBranch": "main", "bootstrapId": "smoke-" + str(time.time_ns())})
    scope = api("POST", f"/api/projects/{project['id']}/default-scope", {})
    require(scope == api("POST", f"/api/projects/{project['id']}/default-scope", {}), "default scope is not idempotent")
    chat_payload = {**scope, "model": "fixture-only", "executionMode": "CHAT"}
    first = finish_task({**chat_payload, "prompt": "PROOFCODE_CHAT_SMOKE FIRST"})
    second = finish_task({**chat_payload, "prompt": "PROOFCODE_CHAT_SMOKE SECOND"})
    require(first.get("result") == "Chat fixture: FIRST" and second.get("result") == "Chat fixture: SECOND", "chat replies are incorrect")
    messages_path = f"/api/projects/{scope['projectId']}/workspaces/{scope['workspaceId']}/conversations/{scope['conversationId']}/messages"
    messages = api("GET", messages_path)
    require(len(messages) == 4 and [m['role'] for m in messages] == ['user', 'assistant', 'user', 'assistant'], "chat transcript did not persist both turns")
    require(first.get("resultSourceSnapshotId") is None and second.get("resultSourceSnapshotId") is None, "chat produced code snapshots")
    code_payload = {**scope, "model": "fixture-only", "executionMode": "CODE", "maxSteps": 6}
    created = finish_task({**code_payload, "prompt": "PROOFCODE_SOURCE_SMOKE CREATE"})
    source_id = created.get("resultSourceSnapshotId")
    require(source_id, "scratch code result did not persist source")
    updated = finish_task({**code_payload, "prompt": "PROOFCODE_SOURCE_SMOKE UPDATE"})
    require(updated.get("sourceSnapshotId") == source_id, "next scratch task did not restore previous successful source")
    archive = api("GET", f"/internal/tasks/{updated['id']}/source?attempt=1", internal=True)
    app = next((entry for entry in archive["files"] if entry["path"] == "app.py"), None)
    require(app and base64.b64decode(app["content"]) == b"def add(a, b):\n    return a + b\n", "restored scratch input has incorrect bytes")
    unchanged = finish_task({**code_payload, "prompt": "PROOFCODE_SOURCE_SMOKE READ"})
    require(unchanged.get("sourceSnapshotId") == updated.get("resultSourceSnapshotId")
        and unchanged.get("resultSourceSnapshotId") == unchanged.get("sourceSnapshotId"), "unchanged source result was not preserved")
    for task in (created, updated):
        events = api("GET", f"/api/tasks/{task['id']}/events")
        require(any(e.get("type") == "tool.completed" and e.get("payload", {}).get("tool") == "run_command" for e in events), "source task did not execute its real tests")
        artifact = api("GET", f"/api/tasks/{task['id']}/artifacts/checkpoint")
        require(artifact.get("patch") and "app.py" in artifact["patch"], "source task produced no reviewable code patch")

    local = api("POST", "/api/projects", {"name": "Dirty local source smoke", "sourceKind": "LOCAL_FOLDER",
        "defaultBranch": "main", "localHandle": "desktop:smoke-source", "bootstrapId": "local-" + str(time.time_ns())})
    local_scope = api("POST", f"/api/projects/{local['id']}/default-scope", {})
    content = b"def add(a, b):\n    return a + b\n"
    file_hash = hashlib.sha256(content).hexdigest()
    manifest = hashlib.sha256(("app.py\0" + file_hash + "\0" + "0\n").encode()).hexdigest()
    upload = {"workspaceId": local_scope["workspaceId"], "manifestHash": manifest,
        "files": [{"path": "app.py", "sha256": file_hash, "content": base64.b64encode(content).decode(), "executable": False}]}
    metadata = api("POST", f"/api/projects/{local['id']}/sources", upload)
    require("content" not in metadata, "public source metadata leaked file contents")
    local_done = finish_task({**local_scope, "prompt": "PROOFCODE_SOURCE_SMOKE READ", "model": "fixture-only",
        "executionMode": "CODE", "sourceSnapshotId": metadata["id"]})
    require(local_done.get("sourceSnapshotId") == local_done.get("resultSourceSnapshotId") == metadata["id"], "unchanged local source was not recorded")
    cross_scope_rejected = False
    try:
        api("POST", "/api/tasks", {**scope, "prompt": "must reject foreign source", "model": "fixture-only",
            "executionMode": "CODE", "sourceSnapshotId": metadata["id"]})
    except HttpFailure as error:
        require(error.status == 404, f"foreign snapshot returned unexpected HTTP {error.status}")
        cross_scope_rejected = True
    require(cross_scope_rejected, "cross-project source snapshot was accepted")
    return {"projectId": project["id"], "chatTaskIds": [first["id"], second["id"]],
        "sourceTaskIds": [created["id"], updated["id"], unchanged["id"], local_done["id"]],
        "chatHistoryPersisted": True, "realTestsExecuted": True, "unchangedSourcePersisted": True,
        "crossProjectSourceRejected": True}


def main() -> int:
    require(USER_TOKEN, "PROOFCODE_AUTH_TOKEN is required")
    fixture_info = fixture("/fixture")
    require(isinstance(fixture_info, dict), "fixture /fixture must return a JSON object")
    project = api("POST", "/api/projects", {
        "name": "ProofCode experiment smoke",
        "repositoryUrl": fixture_info.get("repositoryUrl"),
        "defaultBranch": "main",
    })
    require(project.get("id"), "project create response has no id")
    experiment_result = run_experiment(project, fixture_info)
    data_result = run_database_approval(project["id"])
    project_result = run_project_sources()
    summary = {
        "ok": True,
        "kind": "fixture-only end-to-end plumbing validation",
        "researchQualityClaim": False,
        "modelQualityClaim": False,
        "elapsedSeconds": round(time.monotonic() - START, 3),
        "projectId": project["id"],
        "experiment": experiment_result,
        "databaseApproval": data_result,
        "projectSources": project_result,
    }
    save_json(ARTIFACTS / "summary.json", summary)
    print(json.dumps({"ok": True, "summary": str(ARTIFACTS / "summary.json"), "elapsedSeconds": summary["elapsedSeconds"]}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        try:
            save_json(ARTIFACTS / "summary.json", {
                "ok": False,
                "kind": "fixture-only end-to-end plumbing validation",
                "researchQualityClaim": False,
                "elapsedSeconds": round(time.monotonic() - START, 3),
                "failure": str(error),
            })
        except OSError:
            pass
        print(f"SMOKE FAILED: {error}", file=sys.stderr)
        sys.exit(1)
