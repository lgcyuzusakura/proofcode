"""Archive measured fixture evidence without exposing service credentials."""
from datetime import datetime, timezone
from pathlib import Path
import hashlib
import json
import shutil
import subprocess
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
DEST = ROOT / "thesis" / "evidence"


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    DEST.mkdir(parents=True, exist_ok=True)
    original_dir = ROOT / ".tools" / "experiment-e2e"
    copied = []
    for original, target in (("summary.json", "fixture-summary.json"), ("compare.csv", "fixture-compare.csv"), ("compare.json", "fixture-compare.json")):
        source = original_dir / original
        output = DEST / target
        shutil.copyfile(source, output)
        copied.append({"source": ".tools/experiment-e2e/" + original, "archive": "thesis/evidence/" + target, "sha256": digest(output)})
    reports = []
    totals = {"tests": 0, "failures": 0, "errors": 0, "skipped": 0}
    for path in sorted((ROOT / "control-plane" / "target" / "surefire-reports").glob("TEST-*.xml")):
        element = ET.parse(path).getroot()
        report = {"suite": element.get("name"), "source": path.relative_to(ROOT).as_posix(), "sha256": digest(path)}
        for key in totals:
            report[key] = int(element.get(key, "0"))
            totals[key] += report[key]
        report["testNames"] = [{"name": e.get("name"), "skipped": e.find("skipped") is not None} for e in element.findall("testcase")]
        reports.append(report)
    totals["executed"] = totals["tests"] - totals["skipped"]
    assert totals == {"tests": 37, "failures": 0, "errors": 0, "skipped": 2, "executed": 35}, totals
    test_file = DEST / "test-report-summary.json"
    test_file.write_text(json.dumps({"kind": "archived Maven Surefire summary, not model task scores", "totals": totals, "suites": reports}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    baseline = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    summary = json.loads((DEST / "fixture-summary.json").read_text(encoding="utf-8"))
    assert summary["ok"] and summary["modelQualityClaim"] is False and summary["researchQualityClaim"] is False
    provenance = {
        "archivedAt": datetime.now(timezone.utc).isoformat(),
        "backendCommit": baseline,
        "fixtureSourceCommit": summary["experiment"]["sourceRevision"],
        "fixtureKind": summary["kind"],
        "originalAuditRowsArchived": False,
        "realModelResearchPerformed": False,
        "sourceFiles": copied,
        "otherEvidence": [{"path": path.relative_to(ROOT).as_posix(), "sha256": digest(path)} for path in (DEST / "mechanism-results.json", test_file, ROOT / "thesis" / "tools" / "mechanism_bench.go", ROOT / "smoke" / "experiments" / "verify.py")],
        "limitations": ["Fixture model/Jev responses are predetermined.", "Surefire live PostgreSQL/Redis adapter tests are skipped in this report.", "The fixture uses real PostgreSQL for approval mutation verification.", "Source report XML is summarized without credentials or process environments."],
    }
    (DEST / "provenance.json").write_text(json.dumps(provenance, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"archived": len(copied), "testTotals": totals, "backendCommit": baseline}))


if __name__ == "__main__":
    main()
