"""Create GitHub-sized ZIP assets for the offline runtime folder."""
import argparse
from pathlib import Path
import zipfile
import hashlib
import json

BOOTSTRAP = ("README.txt", "install.ps1", "compose.yml", "proofcode-offline.json")


def package(source: Path, output: Path):
    if output.exists():
        raise ValueError("Output already exists")
    output.mkdir(parents=True)
    for name in BOOTSTRAP:
        path = source / name
        if not path.is_file() or path.is_symlink():
            raise ValueError("Missing regular bootstrap file: " + name)
    manifest = json.loads((source / "proofcode-offline.json").read_text(encoding="utf-8"))
    expected = {entry["file"] for entry in manifest["archive"]["parts"]}
    if not expected or len(expected) > 16:
        raise ValueError("Invalid archive part list")
    for name in sorted(expected):
        path = source / name
        if not path.is_file() or path.is_symlink():
            raise ValueError("Missing image archive part: " + name)
        listed = next(item for item in manifest["archive"]["parts"] if item["file"] == name)
        with path.open("rb") as stream:
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
        if path.stat().st_size != listed["size"] or digest != listed["sha256"]:
            raise ValueError("Image part checksum mismatch: " + name)
    bootstrap_zip = output / "ProofCode-Offline-Setup.zip"
    with zipfile.ZipFile(bootstrap_zip, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=6) as archive:
        for name in BOOTSTRAP:
            archive.write(source / name, name)
    for index, name in enumerate(sorted(expected), start=1):
        target = output / f"ProofCode-Offline-Images-{index:02d}-of-{len(expected):02d}.zip"
        with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_STORED, allowZip64=True) as archive:
            archive.write(source / name, name)
    print("Created {} split GitHub release assets in {}".format(len(expected) + 1, output))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    options = parser.parse_args()
    package(options.source.resolve(), options.output.resolve())
