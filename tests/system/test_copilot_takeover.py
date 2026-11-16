"""Tests for PR48: GitHub Copilot can take over the project. The .github instruction files exist, are current, point to the
constitution and the rule files by repo-relative path (those paths exist), carry no copy of the constitution (PR42), and
name only test commands that exist. Offline; reads only.
Run: python -m unittest discover -s tests/system -p "test_copilot_takeover.py" -v"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import glob
import os
import re
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.normpath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
import constcheck  # noqa: E402
import constitution  # noqa: E402

# the rule files each Copilot file must name, by path relative to the repository root (PR48)
POINTERS = ["CONSTITUTION.md", "RULES.md", "STATUS.md", "HANDOVER.md", "AGENTS.md"]
EXPECTED = {
    ".github/copilot-instructions.md",
    ".github/agents/legion-chat.agent.md",
    ".github/agents/legion-guild.agent.md",
    ".github/agents/legion-life.agent.md",
    ".github/agents/legion-market.agent.md",
    ".github/prompts/legion-start.prompt.md",
}
# the commands the entry point names must exist (checked by path)
COMMAND_PATHS = ["tools/dev/gocheck.sh", "tools/system/gatecheck.py", "tools/system/constcheck.py", "tests/system"]


def copilot_files():
    out = []
    for pat in constcheck.COPILOT_SOURCES:
        out += glob.glob(os.path.join(REPO, pat))
    return sorted(out)


def read(p):
    with open(p, encoding="utf-8") as f:
        return f.read()


class CopilotTakeover(unittest.TestCase):
    def test_files_exist(self):
        found = {os.path.relpath(p, REPO).replace("\\", "/") for p in copilot_files()}
        self.assertEqual(found, EXPECTED)

    def test_each_file_names_the_pointers_and_they_exist(self):
        for p in copilot_files():
            text = read(p)
            rel = os.path.relpath(p, REPO).replace("\\", "/")
            for name in POINTERS:
                with self.subTest(file=rel, pointer=name):
                    self.assertRegex(text, r"(?<![\w/.-])%s(?![\w.-])" % re.escape(name))
                    self.assertTrue(os.path.isfile(os.path.join(REPO, name)), "%s does not exist" % name)

    def test_none_is_superseded(self):
        for p in copilot_files():
            with self.subTest(file=p):
                self.assertNotIn("SUPERSEDED", read(p))

    def test_no_copy_of_the_constitution(self):
        const = constitution.text()
        for p in copilot_files():
            with self.subTest(file=p):
                self.assertEqual(constcheck.copy_lines(read(p), const), [])

    def test_pointer_check_passes(self):
        rep = constcheck.Report()
        constcheck.check_pointers(rep)
        self.assertEqual(rep.bad, [])
        self.assertEqual(len([r for r in rep.rows if r[0] == "OK  "]), len(EXPECTED))

    def test_no_github_warning_in_constcheck(self):
        rep, _head = constcheck.run()
        self.assertEqual([w for w in rep.warn if ".github" in w], [])
        self.assertEqual([b for b in rep.bad if ".github" in b], [])
        self.assertFalse([u for u in constcheck.UNCOVERED if u[0].startswith(".github")])

    def test_named_test_commands_exist(self):
        for p in copilot_files():
            text = read(p)
            for path in re.findall(r"`((?:tools|tests)/[\w./-]+)`", text):
                with self.subTest(file=p, command=path):
                    self.assertTrue(os.path.exists(os.path.join(REPO, path.rstrip("/"))), "%s missing" % path)
        for path in COMMAND_PATHS:
            with self.subTest(command=path):
                self.assertTrue(os.path.exists(os.path.join(REPO, path)), "%s missing" % path)

    def test_a_bad_file_is_caught(self):
        """the checks fail on a superseded file, a file without a pointer, and a file with a copy (negative test)"""
        d = tempfile.mkdtemp(prefix="copilot-test-")
        sup = os.path.join(d, "sup.md")
        with open(sup, "w", encoding="utf-8") as f:
            f.write("SUPERSEDED. See CONSTITUTION.md\n")
        bare = os.path.join(d, "bare.md")
        with open(bare, "w", encoding="utf-8") as f:
            f.write("No pointer here.\n")
        const = constitution.text()
        long_line = next(ln.strip() for ln in const.split("\n") if len(ln.strip()) >= constcheck.COPY_MIN)
        copy = os.path.join(d, "copy.md")
        with open(copy, "w", encoding="utf-8") as f:
            f.write("Pointer: CONSTITUTION.md\n" + long_line + "\n")
        # the pointer check reads the Copilot globs; point it at this temp folder instead
        old = constcheck.COPILOT_SOURCES
        try:
            constcheck.COPILOT_SOURCES = [os.path.join(os.path.basename(d), "*.md")]
            rep = constcheck.Report()
            constcheck.check_pointers(rep, repo=os.path.dirname(d))
        finally:
            constcheck.COPILOT_SOURCES = old
        bad = " | ".join(rep.bad)
        self.assertIn("SUPERSEDED", bad)
        self.assertIn("does not point to CONSTITUTION.md", bad)
        rep2 = constcheck.Report()
        constcheck.check_copies(rep2, files=[copy])
        self.assertTrue(rep2.bad, "a copy of the constitution must fail")


if __name__ == "__main__":
    unittest.main()
