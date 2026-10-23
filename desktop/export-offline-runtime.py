"""Build a code-only Docker runtime from a committed snapshot (Python 3.12+)."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

SERVICES = {"postgres", "redis", "artemis", "control-plane", "runner", "web"}
IMAGE_ID = re.compile(r"sha256:[a-f0-9]{64}\Z")
PART_SIZE = 600 * 1024 * 1024


def run(*args, cwd=None, capture=True):
    return subprocess.run(args, cwd=cwd, check=True, text=True,
                          stdout=subprocess.PIPE if capture else None).stdout


def sha256(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def offline_compose(config, images):
    if set(config.get("services", {})) != SERVICES or set(images) != SERVICES:
        raise ValueError("Expected exactly the six ProofCode services")
    for name, service in config["services"].items():
        if not IMAGE_ID.fullmatch(images[name]):
            raise ValueError("Invalid Docker image ID")
        service.pop("build", None)
        service["image"] = images[name]
        service["pull_policy"] = "never"
    return config


def export(source, output):
    if output.exists():
        raise ValueError("Output already exists; use a new directory")
    commit = run("git", "rev-parse", "HEAD", cwd=source).strip()
    # Archive explicit code roots, then apply the desktop packager's narrower
    # allowlist. Neither a working .env nor Git history enters a build context.
    paths = [".dockerignore", "agent-engine", "control-plane", "frontend",
             "desktop/prepare-runtime-bundle.ps1", "desktop/runtime-template/compose.yml",
             "desktop/import-offline-runtime.ps1"]
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="proofcode-offline-") as temporary:
        stage = Path(temporary)
        snapshot = (stage / "snapshot").resolve()
        snapshot.mkdir()
        archive = stage / "code.tar"
        run("git", "archive", "--format=tar", "-o", str(archive), commit, *paths, cwd=source)
        with tarfile.open(archive) as tar:
            for member in tar.getmembers():
                if not (member.isfile() or member.isdir()) or not (snapshot / member.name).resolve().is_relative_to(snapshot):
                    raise ValueError("Non-regular or escaping source entry")
            tar.extractall(snapshot, filter="data")
        shell = shutil.which("pwsh") or shutil.which("powershell")
        if not shell:
            raise ValueError("PowerShell is required on the build machine")
        bundle = stage / "runtime.zip"
        run(shell, "-NoProfile", "-File", str(snapshot / "desktop/prepare-runtime-bundle.ps1"),
            "-SourceRoot", str(snapshot), "-OutputPath", str(bundle), capture=False)
        context = stage / "runtime"
        context.mkdir()
        with zipfile.ZipFile(bundle) as zip_file:
            manifest = json.loads(zip_file.read("runtime-manifest.json"))
            for entry in manifest["files"]:
                path = context / entry["path"]
                if not path.resolve().is_relative_to(context.resolve()):
                    raise ValueError("Invalid source path")
                data = zip_file.read(entry["path"])
                if len(data) != entry["size"] or hashlib.sha256(data).hexdigest() != entry["sha256"]:
                    raise ValueError("Source checksum mismatch")
                if path.exists():
                    if not path.is_file() or path.read_bytes() != data:
                        raise ValueError("Conflicting runtime source")
                    continue
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(data)
        if run("docker", "version", "--format", "{{.Server.Os}}/{{.Server.Arch}}").strip() != "linux/amd64":
            raise ValueError("Use Docker Desktop Linux containers (linux/amd64)")
        compose = ["docker", "compose", "--project-directory", str(context), "-f", str(context / "compose.yml")]
        config = json.loads(run(*compose, "config", "--no-interpolate", "--no-normalize", "--no-path-resolution", "--format", "json"))
        images = {}
        for name in sorted(SERVICES):
            service = config["services"][name]
            image = service["image"]
            if "build" in service:
                build = service["build"]
                run("docker", "build", "--platform", "linux/amd64", "-t", image,
                    "-f", str(context / build["context"] / build.get("dockerfile", "Dockerfile")),
                    str(context / build["context"]), capture=False)
            else:
                # A cached upstream image is sufficient. No running container or
                # user volume is ever exported.
                check = subprocess.run(["docker", "image", "inspect", image], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                if check.returncode:
                    run("docker", "pull", "--platform", "linux/amd64", image, capture=False)
            info = json.loads(run("docker", "image", "inspect", image))[0]
            if info["Os"] != "linux" or info["Architecture"] != "amd64":
                raise ValueError("Image platform mismatch: " + name)
            images[name] = info["Id"]
        config = offline_compose(config, images)
        # Stage on the destination filesystem, publish only the complete folder.
        with tempfile.TemporaryDirectory(prefix=".proofcode-export-", dir=output.parent) as publish:
            package = Path(publish) / "package"
            package.mkdir()
            compose_file = package / "compose.yml"
            compose_file.write_text(json.dumps(config, indent=2) + "\n", encoding="utf-8")
            image_archive = package / "images.tar"
            run("docker", "save", "-o", str(image_archive), *sorted(set(images.values())), capture=False)
            parts = []
            with image_archive.open("rb") as source_image:
                part_index = 1
                while True:
                    part_name = f"images.tar.part{part_index:03d}"
                    part_path = package / part_name
                    digest = hashlib.sha256()
                    part_size = 0
                    with part_path.open("wb") as destination:
                        while part_size < PART_SIZE:
                            chunk = source_image.read(min(8 * 1024 * 1024, PART_SIZE - part_size))
                            if not chunk:
                                break
                            destination.write(chunk)
                            digest.update(chunk)
                            part_size += len(chunk)
                    if not part_size:
                        part_path.unlink()
                        break
                    parts.append({"file": part_name, "size": part_size, "sha256": digest.hexdigest()})
                    part_index += 1
            image_size = image_archive.stat().st_size
            image_hash = sha256(image_archive)
            image_archive.unlink()
            marker = {"version": 1, "platform": "linux/amd64", "sourceCommit": commit,
                      "composeSha256": sha256(compose_file), "images": images,
                      "archive": {"file": "images.tar", "size": image_size, "sha256": image_hash,
                                  "parts": parts}}
            (package / "proofcode-offline.json").write_text(json.dumps(marker, indent=2) + "\n", encoding="utf-8")
            shutil.copyfile(snapshot / "desktop/import-offline-runtime.ps1", package / "install.ps1")
            (package / "README.txt").write_text(
                "ProofCode offline services (Windows + Docker Desktop Linux containers)\n"
                "1. Install/start Docker Desktop. Download every matching ProofCode-Offline ZIP asset.\n"
                "2. Extract the Setup ZIP and every Images ZIP into the SAME new folder.\n"
                "3. In PowerShell opened in that folder, run:\n"
                "   powershell -NoProfile -ExecutionPolicy Bypass -File .\\install.ps1 -Package . -Destination D:\\ProofCode-runtime\n"
                "4. Open the latest ProofCode.exe, select the new deployment directory, click Start.\n"
                "Keep the generated .env private and backed up. Do not replace its data key after use.\n"
                "Model requests, package installation and external MCP still need their own network access.\n"
                "This package contains application images, not Docker Desktop or a model.\n", encoding="utf-8")
            os.rename(package, output)
    print("Offline runtime exported: " + str(output), flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=Path(__file__).resolve().parent.parent)
    parser.add_argument("--output", type=Path, required=True)
    arguments = parser.parse_args()
    export(arguments.source.resolve(), arguments.output.resolve())
