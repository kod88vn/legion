"""portcheck.py (PR47): offline and read-only. Every test works in a temp folder; the only live read is the
manifest and the tracked-file scan of this repo (read-only), in test_live_tree_has_no_new_host_paths.
Run: python -m unittest discover -s tests/system -v   (from the repo root)"""
import io
import json
import os
import shutil
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

ROOT = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
sys.path.insert(0, os.path.join(ROOT, "tools", "system"))
import portcheck as P  # noqa: E402


def write(root, rel, text):
    full = os.path.join(root, *rel.split("/"))
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "w", encoding="utf-8", newline="\n") as fh:
        fh.write(text)


class Tmp(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="portcheck-test-")
        self.repo = os.path.join(self.tmp, "legion")
        os.makedirs(self.repo)

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)


class ScanTests(Tmp):
    def test_finds_each_host_path_form_and_names_it(self):
        write(self.repo, "a.sh", "cd /mnt/g/RO/legion\n")
        write(self.repo, "b.py", 'X = r"G:\\RO\\ClientEN\\x.lua"\n')
        write(self.repo, "c.md", "git -C /g/RO/legion status\n")
        write(self.repo, "d.json", '{"u": "C:/Users/someone/x"}\n')
        hits = P.scan_host_paths(self.repo)
        self.assertEqual(hits["a.sh"], [(1, "wsl-path")])
        self.assertEqual(hits["b.py"], [(1, "drive-path")])
        self.assertEqual(hits["c.md"], [(1, "git-bash-path")])
        self.assertEqual(hits["d.json"], [(1, "user-profile")])

    def test_reports_names_not_matched_text(self):
        write(self.repo, "s.txt", "password = hunter2 at G:/RO/legion\n")
        hits = P.scan_host_paths(self.repo)
        self.assertNotIn("hunter2", repr(hits))

    def test_skips_prose_folders_skip_list_and_binaries(self):
        write(self.repo, "docs/guide.md", "G:/RO/legion\n")
        write(self.repo, "RESTART-NOTES/n.md", "G:/RO/legion\n")
        write(self.repo, "tools/system/portcheck.py", "G:/RO/legion\n")
        write(self.repo, "blob.bin", "G:/RO/legion\0\0")
        write(self.repo, "clean.py", "x = 1\n")
        self.assertEqual(P.scan_host_paths(self.repo, skip=("tools/system/portcheck.py",)), {})

    def test_relative_paths_are_clean(self):
        write(self.repo, "ok.sh", "cd ./legion && ls ../other\n")
        self.assertEqual(P.scan_host_paths(self.repo), {})


class EnvTests(Tmp):
    def test_parse_env_splits_required_and_optional(self):
        req, opt = P.parse_env("# note\nLEGION_BOTS=\n# ANTHROPIC_API_KEY=\nLEGION_CORE=a\n")
        self.assertEqual(req, {"LEGION_BOTS", "LEGION_CORE"})
        self.assertEqual(opt, {"ANTHROPIC_API_KEY"})

    def test_missing_env_and_missing_required_keys_are_reported(self):
        write(self.repo, "example.env", "LEGION_BOTS=\nLEGION_CORE=\n# ANTHROPIC_API_KEY=\n")
        res = P.check_env(os.path.join(self.repo, ".env"), os.path.join(self.repo, "example.env"))
        self.assertFalse(res["env_exists"])
        write(self.repo, ".env", "LEGION_BOTS=secret-value-123\n")
        res = P.check_env(os.path.join(self.repo, ".env"), os.path.join(self.repo, "example.env"))
        self.assertEqual(res["missing_required"], ["LEGION_CORE"])
        self.assertEqual(res["missing_optional"], ["ANTHROPIC_API_KEY"])
        self.assertNotIn("secret-value-123", repr(res))

    def test_commented_key_in_env_does_not_count_as_set(self):
        write(self.repo, "example.env", "LEGION_BOTS=\n")
        write(self.repo, ".env", "# LEGION_BOTS=4000\n")
        res = P.check_env(os.path.join(self.repo, ".env"), os.path.join(self.repo, "example.env"))
        self.assertEqual(res["missing_required"], ["LEGION_BOTS"])


class ToolTests(unittest.TestCase):
    def test_all_present_is_ok(self):
        out = P.check_tools(which=lambda n: "/x/" + n, version=(3, 12, 10))
        self.assertTrue(all(ok for _, ok, _, _ in out))

    def test_missing_git_or_wsl_is_blocking(self):
        out = {n: (ok, sev) for n, ok, sev, _ in P.check_tools(which=lambda n: None if n in ("git", "wsl") else "/x", version=(3, 12, 0), wsl=False)}
        self.assertEqual(out["git"], (False, "blocking"))
        self.assertEqual(out["wsl"], (False, "blocking"))

    def test_wsl_missing_inside_wsl_is_skipped_with_a_note(self):
        out = {n: (ok, sev, d) for n, ok, sev, d in P.check_tools(which=lambda n: None if n == "wsl" else "/x", version=(3, 12, 0), wsl=True)}
        self.assertEqual(out["wsl"][:2], (True, "skip"))
        self.assertIn("inside WSL", out["wsl"][2])

    def test_wsl_missing_outside_wsl_stays_blocking(self):
        out = {n: (ok, sev) for n, ok, sev, _ in P.check_tools(which=lambda n: None if n == "wsl" else "/x", version=(3, 12, 0), wsl=False)}
        self.assertEqual(out["wsl"], (False, "blocking"))

    def test_old_python_is_blocking_and_missing_docker_only_warns(self):
        out = {n: (ok, sev) for n, ok, sev, _ in P.check_tools(which=lambda n: None if n == "docker" else "/x", version=(3, 11, 9))}
        self.assertEqual(out["python"], (False, "blocking"))
        self.assertEqual(out["docker"], (False, "warn"))


class ManifestTests(Tmp):
    def test_present_and_missing_items_by_base(self):
        ro = os.path.join(self.tmp)
        write(self.repo, "data/a.json", "{}")
        write(ro, "kRO/client/RagexeRE.exe", "x")
        m = {"items": [
            {"path": "data/a.json", "base": "repo", "severity": "blocking"},
            {"path": "kRO/client/RagexeRE.exe", "base": "ro", "severity": "optional"},
            {"path": "data/gone.json", "base": "repo", "severity": "blocking", "note": "n"},
        ]}
        got = {p: (sev, present) for p, sev, present, _ in P.check_manifest(m, {"repo": self.repo, "ro": ro, "home": ro})}
        self.assertEqual(got["data/a.json"], ("blocking", True))
        self.assertEqual(got["kRO/client/RagexeRE.exe"], ("optional", True))
        self.assertEqual(got["data/gone.json"], ("blocking", False))


class RunTests(Tmp):
    def _tree(self, *, env_ok=True, known=(), extra_item=None, optional_missing=True):
        write(self.repo, "example.env", "LEGION_BOTS=\nLEGION_CORE=\n# ANTHROPIC_API_KEY=\n")
        if env_ok:
            write(self.repo, ".env", "LEGION_BOTS=4000\nLEGION_CORE=a\n")
        items = [{"path": "data/a.json", "base": "repo", "severity": "blocking"},
                 {"path": "data/opt.bin", "base": "repo", "severity": "optional"}]
        write(self.repo, "data/a.json", "{}")
        if not optional_missing:
            write(self.repo, "data/opt.bin", "x")
        if extra_item:
            items.append(extra_item)
        write(self.repo, "docs/NEW-MACHINE-MANIFEST.json",
              json.dumps({"known_path_files": list(known), "items": items}))
        return os.path.join(self.repo, "docs", "NEW-MACHINE-MANIFEST.json")

    def _ok_tools(self):
        return dict(which=lambda n: "/x/" + n, version=(3, 12, 0))

    def test_clean_tree_has_no_blocking_gap(self):
        manifest = self._tree()
        lines, blocking = P.run(self.repo, base=self.tmp, manifest_path=manifest, **self._ok_tools())
        self.assertEqual(blocking, 0, "\n".join(lines))

    def test_new_host_path_is_blocking_known_one_is_not(self):
        write(self.repo, "bad.sh", "cd /mnt/g/RO/legion\n")
        write(self.repo, "old.sh", "cd /mnt/g/RO/legion\n")
        manifest = self._tree(known=["old.sh"])
        lines, blocking = P.run(self.repo, base=self.tmp, manifest_path=manifest, **self._ok_tools())
        self.assertEqual(blocking, 1, "\n".join(lines))
        self.assertTrue(any("bad.sh:1" in l for l in lines))
        self.assertFalse(any("old.sh:1" in l and "BLOCKING" in l for l in lines))

    def test_missing_env_key_and_missing_blocking_item_are_blocking(self):
        manifest = self._tree(env_ok=False, extra_item={"path": "nope.json", "base": "repo", "severity": "blocking"})
        lines, blocking = P.run(self.repo, base=self.tmp, manifest_path=manifest, **self._ok_tools())
        self.assertEqual(blocking, 4, "\n".join(lines))   # .env, two required keys, nope.json

    def test_missing_optional_and_warn_items_do_not_block(self):
        manifest = self._tree(extra_item={"path": "later.zip", "base": "ro", "severity": "warn"})
        lines, blocking = P.run(self.repo, base=self.tmp, manifest_path=manifest, **self._ok_tools())
        self.assertEqual(blocking, 0, "\n".join(lines))
        self.assertTrue(any("later.zip" in l and "warn" in l for l in lines))

    def test_run_inside_wsl_without_wsl_on_path_is_not_a_gap(self):
        manifest = self._tree()
        lines, blocking = P.run(self.repo, base=self.tmp, manifest_path=manifest,
                                which=lambda n: None if n == "wsl" else "/x", version=(3, 12, 0), wsl=True)
        self.assertEqual(blocking, 0, "\n".join(lines))
        self.assertTrue(any(l.strip().startswith("skip") and "wsl" in l for l in lines), "\n".join(lines))

    def test_main_exit_codes(self):
        self._tree()
        with redirect_stdout(io.StringIO()) as out:
            code = P.main(["--repo", self.repo, "--base", self.tmp, "--manifest",
                           os.path.join(self.repo, "docs", "NEW-MACHINE-MANIFEST.json")])
        self.assertIn(code, (0, 1))
        self.assertIn("summary: blocking gaps", out.getvalue())
        with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
            self.assertEqual(P.main(["--no-such-flag"]), 2)

    def test_help_exits_zero(self):
        with redirect_stdout(io.StringIO()) as out:
            self.assertEqual(P.main(["-h"]), 0)
        self.assertIn("usage", out.getvalue())


class LiveManifestTests(unittest.TestCase):
    def test_manifest_is_valid_and_uses_known_fields(self):
        m = P.load_manifest(os.path.join(ROOT, P.MANIFEST_REL))
        self.assertTrue(m.get("items"), "manifest has no items")
        for item in m["items"]:
            self.assertIn(item.get("base", "repo"), ("repo", "ro", "home"), item)
            self.assertIn(item.get("severity", "warn"), ("blocking", "warn", "optional"), item)
            self.assertNotIn("\\", item["path"])

    def test_live_tree_has_no_new_host_paths(self):
        """a new hard-coded host path in a tracked file blocks: fix it or file it and add the file to known_path_files"""
        m = P.load_manifest(os.path.join(ROOT, P.MANIFEST_REL))
        hits = P.scan_host_paths(ROOT, skip=("tools/system/portcheck.py", "tests/system/test_portcheck.py"))
        new = sorted(set(hits) - set(m.get("known_path_files", [])))
        self.assertEqual(new, [], "new host paths (file them in the bug queue, then add to known_path_files)")


if __name__ == "__main__":
    unittest.main()
