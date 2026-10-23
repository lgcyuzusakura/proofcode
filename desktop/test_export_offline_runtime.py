import importlib.util
import hashlib
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location("offline_export", Path(__file__).with_name("export-offline-runtime.py"))
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
PACKAGE_SPEC = importlib.util.spec_from_file_location("offline_release", Path(__file__).with_name("package-offline-release.py"))
PACKAGE_MODULE = importlib.util.module_from_spec(PACKAGE_SPEC)
PACKAGE_SPEC.loader.exec_module(PACKAGE_MODULE)


class OfflineComposeTests(unittest.TestCase):
    def setUp(self):
        self.images = {name: "sha256:" + chr(97 + index) * 64
                       for index, name in enumerate(MODULE.SERVICES)}
        self.config = {"services": {name: {"image": "tag", "build": {"context": "."}}
                                     for name in MODULE.SERVICES}}

    def test_rewrites_exact_service_set_to_content_ids_and_never_pull(self):
        result = MODULE.offline_compose(self.config, self.images)
        for name, service in result["services"].items():
            self.assertEqual(service["image"], self.images[name])
            self.assertEqual(service["pull_policy"], "never")
            self.assertNotIn("build", service)

    def test_rejects_wrong_services_and_non_content_image_references(self):
        with self.assertRaises(ValueError):
            MODULE.offline_compose({"services": {"web": {}}}, self.images)
        invalid = dict(self.images, web="proofcode/web:latest")
        with self.assertRaises(ValueError):
            MODULE.offline_compose(self.config, invalid)


class OfflineReleaseTests(unittest.TestCase):
    def test_requires_exact_regular_image_parts_before_creating_release_assets(self):
        import json
        import tempfile
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "package"
            source.mkdir()
            output = root / "release"
            for name in PACKAGE_MODULE.BOOTSTRAP:
                (source / name).write_text("stub", encoding="utf-8")
            marker = {"archive": {"parts": [{"file": "images.tar.part001", "size": 5}]}}
            marker["archive"]["parts"][0]["sha256"] = hashlib.sha256(b"image").hexdigest()
            (source / "proofcode-offline.json").write_text(json.dumps(marker), encoding="utf-8")
            (source / "images.tar.part001").write_bytes(b"image")
            PACKAGE_MODULE.package(source, output)
            self.assertEqual({path.name for path in output.iterdir()}, {
                "ProofCode-Offline-Setup.zip", "ProofCode-Offline-Images-01-of-01.zip"})


if __name__ == "__main__":
    unittest.main()
