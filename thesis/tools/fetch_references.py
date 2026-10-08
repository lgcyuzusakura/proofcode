"""Preserve public bibliographic metadata and access evidence for the thesis."""

import concurrent.futures
import hashlib
import http.client
import json
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

from lxml import html


ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "sources" / "references"
PAPERS = {
    "rrf": "https://api.crossref.org/works/10.1145/1571941.1572114",
    "rag": "https://arxiv.org/abs/2005.11401",
    "react": "https://arxiv.org/abs/2210.03629",
    "repocoder": "https://aclanthology.org/2023.emnlp-main.151/",
    "llmlingua": "https://aclanthology.org/2023.emnlp-main.825/",
    "llmlingua2": "https://arxiv.org/abs/2403.12968",
    "lost_middle": "https://arxiv.org/abs/2307.03172",
    "swebench": "https://arxiv.org/abs/2310.06770",
    "sweagent": "https://arxiv.org/abs/2405.15793",
}
DOCUMENTS = {
    "postgres_transaction": "https://www.postgresql.org/docs/16/tutorial-transactions.html",
    "postgres_explain": "https://www.postgresql.org/docs/16/using-explain.html",
    "redis_transaction": "https://redis.io/docs/latest/develop/interact/transactions/",
    "git_worktree": "https://git-scm.com/docs/git-worktree",
    "spring_boot": "https://docs.spring.io/spring-boot/3.4/index.html",
    "amqp": "https://docs.oasis-open.org/amqp/core/v1.0/os/amqp-core-overview-v1.0-os.html",
    "outbox": "https://microservices.io/patterns/data/transactional-outbox.html",
    "prompt_injection": "https://cheatsheetseries.owasp.org/cheatsheets/LLM_Prompt_Injection_Prevention_Cheat_Sheet.html",
    "sql_injection": "https://cheatsheetseries.owasp.org/cheatsheets/SQL_Injection_Prevention_Cheat_Sheet.html",
    "aider_repomap": "https://aider.chat/docs/repomap.html",
    "codex": "https://github.com/openai/codex",
    "claude_code": "https://github.com/anthropics/claude-code",
    "opencode": "https://github.com/anomalyco/opencode",
    "cline": "https://github.com/cline/cline",
    "roo_code": "https://github.com/RooCodeInc/Roo-Code",
    "aider": "https://github.com/Aider-AI/aider",
    "openhands": "https://github.com/All-Hands-AI/OpenHands",
    "swe_agent_repo": "https://github.com/SWE-agent/SWE-agent",
    "continue": "https://github.com/continuedev/continue",
    "bytebase": "https://github.com/bytebase/bytebase",
    "chartdb": "https://github.com/chartdb/chartdb",
    "drawdb": "https://github.com/drawdb-io/drawdb",
    "dbeaver": "https://github.com/dbeaver/dbeaver",
    "react_flow": "https://github.com/xyflow/xyflow",
    "llmlingua_repo": "https://github.com/microsoft/LLMLingua",
    "mini_swe_agent": "https://github.com/SWE-agent/mini-swe-agent",
    "pi_mono": "https://github.com/badlogic/pi-mono",
    "crush": "https://github.com/charmbracelet/crush",
}


def fetch(item):
    name, url = item
    record = {"key": name, "requestedUrl": url, "retrievedAt": datetime.now(timezone.utc).isoformat()}
    saved = OUTPUT / f"{name}.json"
    if saved.exists():
        existing = json.loads(saved.read_text(encoding="utf-8"))
        if existing.get("status") == 200:
            return existing
    try:
        request = urllib.request.Request(url, headers={"User-Agent": "ProofCode-thesis-source-check/1.0"})
        with urllib.request.urlopen(request, timeout=18) as response:
            data = response.read(4 << 20)
            record.update(status=response.status, finalUrl=response.url, sha256=hashlib.sha256(data).hexdigest())
            content_type = response.headers.get("Content-Type", "")
        if "api.crossref.org" in url:
            message = json.loads(data)["message"]
            record["metadata"] = {k: message.get(k) for k in ("title", "author", "published", "DOI", "page", "container-title", "URL")}
        else:
            tree = html.fromstring(data.decode("utf-8", "replace"))
            record["title"] = " ".join(tree.xpath("//title/text()"))
            meta = {}
            for element in tree.xpath("//meta[@name or @property]"):
                key = element.get("name") or element.get("property")
                if key.startswith("citation_"):
                    meta.setdefault(key, []).append(element.get("content", ""))
            record["metadata"] = meta
        (OUTPUT / f"{name}.json").write_text(json.dumps(record, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    except (urllib.error.URLError, http.client.HTTPException, TimeoutError, OSError, ValueError) as error:
        record["error"] = str(error)
    return record


def main():
    import sys

    sys.stdout.reconfigure(encoding="utf-8")
    OUTPUT.mkdir(parents=True, exist_ok=True)
    with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
        records = list(pool.map(fetch, list(PAPERS.items()) + list(DOCUMENTS.items())))
    (OUTPUT / "manifest.json").write_text(json.dumps(records, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    for record in records:
        keys = ("key", "status", "metadata", "error") if record["key"] in PAPERS else ("key", "status", "title", "error")
        print(json.dumps({k: record.get(k) for k in keys}, ensure_ascii=False), flush=True)


if __name__ == "__main__":
    main()
