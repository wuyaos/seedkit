"""Offline regression for versioned sprite generation; no live CDN requests."""
import hashlib
import importlib.util
import io
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("icons", Path(__file__).with_name("icons.py"))
icons = importlib.util.module_from_spec(spec)
spec.loader.exec_module(icons)


class IconsTest(unittest.TestCase):
    def test_versioned_inputs_and_content_invalidation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "web" / "static"
            (root / "vendor").mkdir(parents=True)
            sources = [root / "index.html", root / "app.js"]
            sources[0].write_text("vendor/lucide.svg?v=2#house", encoding="utf-8")
            sources[1].write_text("vendor/lucide.svg#file", encoding="utf-8")
            output = root / "vendor" / "lucide.svg"
            previous = None
            for shape in ("M1 2", "M3 4"):
                upstream = (f'<svg><symbol id="house"><path d="{shape}"/></symbol>'
                            '<symbol id="file"><path d="M0 0"/></symbol></svg>').encode()
                with patch.object(icons, "ROOT", root), patch.object(icons, "SRC", sources), \
                     patch.object(icons, "OUT", output), patch.object(icons.sys, "argv", ["icons.py"]), \
                     patch.object(icons.urllib.request, "urlopen", return_value=io.BytesIO(upstream)):
                    self.assertEqual(icons.main(), 0)
                version = hashlib.sha256(output.read_bytes()).hexdigest()[:12]
                self.assertNotEqual(version, previous)
                for source in sources:
                    self.assertIn(f"vendor/lucide.svg?v={version}#", source.read_text("utf-8"))
                previous = version


if __name__ == "__main__":
    unittest.main()
