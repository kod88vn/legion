"""Tests for the constitution loader, the dispatcher's refusal and constcheck (PR42). Offline; no model is called.
Run: python -m unittest discover -s tests/system -p "test_constitution.py" -v"""
import json
import os
import subprocess
import sys
import tempfile
import types
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.normpath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
sys.path.insert(0, os.path.join(REPO, "tools", "puppet"))
# never read or write the live owner-orders file from tests (G23)
_TMP = tempfile.mkdtemp(prefix="const-test-")
os.environ.setdefault("AYLA_ORDERS", os.path.join(_TMP, "ayla_orders.jsonl"))
os.environ.setdefault("AYLA_ORDERS_LOG", os.path.join(_TMP, "owner_orders.log"))
import constcheck  # noqa: E402
import constitution  # noqa: E402
import dispatch  # noqa: E402

CONST = constitution.text()


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8", newline="\n") as f:
        f.write(text)
    return path


class LoaderTests(unittest.TestCase):
    def test_text_is_the_file(self):
        with open(os.path.join(REPO, "CONSTITUTION.md"), encoding="utf-8") as f:
            raw = f.read()
        self.assertEqual(CONST, raw.replace("\r\n", "\n").strip("\n"))
        self.assertTrue(CONST.startswith("# The Constitution"))
        self.assertIn("## Article 1. The main session", CONST)
        self.assertIn("## Article 3. The self-improving loop", CONST)
        self.assertTrue(CONST.endswith("except getting Ayla back online."))

    def test_line_ends_do_not_matter(self):
        self.assertEqual(constitution.digest(CONST.replace("\n", "\r\n") + "\r\n\r\n"), constitution.digest())
        self.assertEqual(constitution.count_in("x\r\n" + CONST.replace("\n", "\r\n") + "\r\ny"), 1)

    def test_with_constitution_puts_it_first_once(self):
        p = constitution.with_constitution("You are the market mind.")
        self.assertTrue(p.startswith(CONST))
        self.assertTrue(p.endswith("You are the market mind."))
        self.assertEqual(constitution.count_in(p), 1)

    def test_body_that_already_holds_it_is_refused(self):
        with self.assertRaises(constitution.ConstitutionError):
            constitution.with_constitution(CONST + "\n\nbrief")

    def test_require(self):
        constitution.require(CONST + "\n\nbrief")
        for bad in ("brief only", "", CONST[:-40] + "\n\nbrief", CONST[:4000] + "\n\nbrief",
                    CONST.replace("It does no work\nitself.", "It does little work itself.") + "\n\nbrief",
                    "brief first\n\n" + CONST, CONST + "\n\n" + CONST + "\n\nbrief",
                    "Summary of the constitution: the main session assigns.\n\nbrief"):
            with self.assertRaises(constitution.ConstitutionError, msg=bad[:40]):
                constitution.require(bad)

    def test_missing_or_empty_file(self):
        for content in (None, "", "# not it\n"):
            d = tempfile.mkdtemp(prefix="const-test-")
            p = os.path.join(d, "CONSTITUTION.md")
            if content is not None:
                write(p, content)
            with mock.patch.dict(os.environ, {"LEGION_CONSTITUTION": p}):
                with self.assertRaises(constitution.ConstitutionError):
                    constitution.text()


class SubagentHookTests(unittest.TestCase):
    def test_explore_and_plan_get_the_full_text(self):
        for t in ("Explore", "Plan"):
            out = constitution.subagent_start({"agent_type": t, "agent_id": "a1"}, [])
            self.assertEqual(out["hookSpecificOutput"]["hookEventName"], "SubagentStart")
            self.assertEqual(out["hookSpecificOutput"]["additionalContext"], CONST)

    def test_agents_that_load_claude_md_get_nothing_from_the_hook(self):
        for t in ("general-purpose", "fixer", "hard-fixer", "deploy", "qa", "watcher", "legion-chat", "", None):
            self.assertIsNone(constitution.subagent_start({"agent_type": t}, [os.path.join(REPO, ".claude", "agents")]), t)

    def test_agent_with_omit_claude_md_is_served(self):
        d = tempfile.mkdtemp(prefix="const-test-")
        write(os.path.join(d, "lean.md"), "---\nname: lean\ndescription: x\nomitClaudeMd: true\n---\nbody\n")
        write(os.path.join(d, "full.md"), "---\nname: full\ndescription: x\n---\nbody\n")
        self.assertIsNotNone(constitution.subagent_start({"agent_type": "lean"}, [d]))
        self.assertIsNone(constitution.subagent_start({"agent_type": "full"}, [d]))

    def test_text_fits_the_hook_cap(self):
        self.assertLessEqual(len(CONST), constitution.HOOK_CAP)

    def test_over_the_cap_is_refused_not_shortened(self):
        d = tempfile.mkdtemp(prefix="const-test-")
        p = write(os.path.join(d, "CONSTITUTION.md"), CONST + "\n\n" + "More law. " * 300)
        with mock.patch.dict(os.environ, {"LEGION_CONSTITUTION": p}):
            with self.assertRaises(constitution.ConstitutionError):
                constitution.subagent_start({"agent_type": "Explore"}, [])

    def test_hook_process(self):
        script = os.path.join(REPO, "tools", "system", "constitution.py")
        for t, want in (("Explore", True), ("Plan", True), ("general-purpose", False), ("fixer", False)):
            p = subprocess.run([sys.executable, script, "subagent-start"], input=json.dumps({"agent_type": t}),
                               capture_output=True, text=True, encoding="utf-8", timeout=30)
            self.assertEqual(p.returncode, 0, p.stderr)
            if want:
                ctx = json.loads(p.stdout)["hookSpecificOutput"]["additionalContext"]
                self.assertEqual(ctx, CONST)
                self.assertLessEqual(len(ctx), 10000)
            else:
                self.assertEqual(p.stdout, "")
        p = subprocess.run([sys.executable, script, "subagent-start"], input=json.dumps({"agent_type": "Explore"}),
                           capture_output=True, text=True, timeout=30,
                           env=dict(os.environ, LEGION_CONSTITUTION=os.path.join(_TMP, "nope.md")))
        self.assertEqual(p.returncode, 2)
        self.assertEqual(p.stdout, "")


class DispatcherTests(unittest.TestCase):
    def test_every_role_prompt_starts_with_it_once(self):
        for role in dispatch.ROLES:
            cmd = dispatch.build_command(role, claude="claude")
            sp = cmd[cmd.index("--append-system-prompt") + 1]
            self.assertTrue(sp.startswith(CONST), role)
            self.assertEqual(constitution.count_in(sp), 1, role)
            self.assertIn(dispatch.load_brief(role).strip()[:200], sp)
            self.assertIn("--safe-mode", cmd)
            self.assertLess(sum(len(c) + 3 for c in cmd), 30000, role)   # Windows command line limit 32767

    def test_briefs_hold_no_copy(self):
        for role in dispatch.ROLES:
            self.assertEqual(constitution.count_in(dispatch.load_brief(role)), 0)
            self.assertEqual(constcheck.copy_lines(dispatch.load_brief(role), CONST), [])

    def test_no_file_no_command(self):
        with mock.patch.dict(os.environ, {"LEGION_CONSTITUTION": os.path.join(_TMP, "missing.md")}):
            with self.assertRaises(constitution.ConstitutionError):
                dispatch.build_command("market")

    def test_check_command_refuses(self):
        good = dispatch.build_command("chat")
        i = good.index("--append-system-prompt") + 1
        for bad in (good[:i] + [dispatch.load_brief("chat")] + good[i + 1:],                 # brief only
                    good[:i] + [good[i][:5000] + dispatch.load_brief("chat")] + good[i + 1:],  # shortened
                    good[:i] + [CONST + "\n\n" + good[i]] + good[i + 1:],                    # twice
                    good[:i] + ["intro\n\n" + good[i]] + good[i + 1:],                       # not first
                    [c for c in good if c != "--safe-mode"],                                 # CLAUDE.md would add it again
                    good[:i - 1]):                                                           # no system prompt at all
            with self.assertRaises(constitution.ConstitutionError):
                dispatch.check_command(bad)
        self.assertIs(dispatch.check_command(good), good)

    def test_run_call_refuses_and_never_starts_the_cli(self):
        args = types.SimpleNamespace(model=None, claude="claude", python="python", call_cap=None)
        rows = []
        with mock.patch.dict(os.environ, {"LEGION_CONSTITUTION": os.path.join(_TMP, "missing.md")}), \
                mock.patch.object(dispatch, "make_briefing", return_value=("briefing", {})), \
                mock.patch.object(dispatch, "append_log", side_effect=lambda row, path=None: rows.append(row)), \
                mock.patch.object(dispatch, "save_snap") as snap, \
                mock.patch.object(dispatch.subprocess, "run") as run:
            row = dispatch.run_call("market", "test", args)
        run.assert_not_called()
        snap.assert_not_called()
        self.assertEqual(row["exit"], "refused")
        self.assertIn("CONSTITUTION", row["error"])
        self.assertEqual(rows, [row])

    def test_run_call_refuses_a_tampered_command(self):
        args = types.SimpleNamespace(model=None, claude="claude", python="python", call_cap=None)
        cmd = ["claude", "-p", "--safe-mode", "--append-system-prompt", "just a brief"]
        with mock.patch.object(dispatch, "make_briefing", return_value=("briefing", {})), \
                mock.patch.object(dispatch, "build_command", return_value=cmd), \
                mock.patch.object(dispatch, "append_log"), mock.patch.object(dispatch, "save_snap"), \
                mock.patch.object(dispatch.subprocess, "run") as run:
            row = dispatch.run_call("market", "test", args)
        run.assert_not_called()
        self.assertEqual(row["exit"], "refused")


class ConstcheckTests(unittest.TestCase):
    def tree(self, root_md, repo_md=None, extra=None):
        """a session root with a repo folder below it, like G:\\RO and G:\\RO\\legion"""
        root = tempfile.mkdtemp(prefix="const-test-")
        repo = os.path.join(root, "legion")
        const = write(os.path.join(repo, "CONSTITUTION.md"), CONST + "\n")
        write(os.path.join(repo, "RULES.md"), "# Rules core\nG01 ...\n")
        write(os.path.join(root, "CLAUDE.md"), root_md)
        if repo_md is not None:
            write(os.path.join(repo, "CLAUDE.md"), repo_md)
        for relp, text in (extra or {}).items():
            write(os.path.join(root, relp), text)
        return root, repo, const

    def claude_md(self, root_md, repo_md=None, extra=None):
        root, repo, const = self.tree(root_md, repo_md, extra)
        rep = constcheck.Report()
        with mock.patch.object(os.path, "expanduser", side_effect=lambda p: p.replace("~", os.path.join(root, "home"))):
            constcheck.check_claude_md(rep, root, repo, const)
        return rep

    def test_import_first_once_passes(self):
        rep = self.claude_md("@legion/CONSTITUTION.md\n\n# Project\n\n@legion/RULES.md\n", "<!-- nothing here -->\n")
        self.assertEqual(rep.bad, [])

    def test_missing_import_fails(self):
        rep = self.claude_md("# Project\n\n@legion/RULES.md\n")
        self.assertTrue(any("no CLAUDE.md imports" in b for b in rep.bad), rep.bad)

    def test_mention_in_backticks_is_not_an_import(self):
        rep = self.claude_md("# Project\nRead `@legion/CONSTITUTION.md` first.\n\n@legion/RULES.md\n")
        self.assertTrue(any("no CLAUDE.md imports" in b for b in rep.bad), rep.bad)

    def test_not_first_fails(self):
        rep = self.claude_md("# Project\n\n@legion/RULES.md\n\n@legion/CONSTITUTION.md\n")
        self.assertTrue(any("does not start with" in b for b in rep.bad), rep.bad)

    def test_double_load_through_nested_claude_md_fails(self):
        rep = self.claude_md("@legion/CONSTITUTION.md\n\n@legion/RULES.md\n", "@CONSTITUTION.md\n")
        self.assertTrue(any("loaded 2 times" in b for b in rep.bad), rep.bad)

    def test_double_load_through_an_imported_file_fails(self):
        rep = self.claude_md("@legion/CONSTITUTION.md\n\n@legion/RULES.md\n", None,
                             {"legion/RULES.md": "# Rules\n@CONSTITUTION.md\n"})
        self.assertTrue(any("loaded 2 times" in b for b in rep.bad), rep.bad)

    def test_double_load_through_a_rules_file_fails(self):
        rep = self.claude_md("@legion/CONSTITUTION.md\n", None,
                             {"legion/.claude/rules/law.md": "@../../CONSTITUTION.md\n"})
        self.assertTrue(any("loaded 2 times" in b for b in rep.bad), rep.bad)

    def test_other_file_loaded_twice_fails(self):
        rep = self.claude_md("@legion/CONSTITUTION.md\n\n@legion/RULES.md\n", "@RULES.md\n")
        self.assertTrue(any("rules.md is loaded more than once" in b.lower() for b in rep.bad), rep.bad)

    def test_copy_in_a_prompt_file_fails(self):
        d = tempfile.mkdtemp(prefix="const-test-")
        para = "\n".join(CONST.split("\n")[15:18])
        files = [write(os.path.join(d, "fixer.md"), "---\nname: fixer\n---\n" + para + "\nYou fix one item.\n"),
                 write(os.path.join(d, "clean.md"), "---\nname: clean\n---\nSee Constitution 1.3.\n"),
                 write(os.path.join(d, "whole.md"), CONST + "\n\nbrief\n")]
        rep = constcheck.Report()
        constcheck.check_copies(rep, files)
        self.assertEqual(len(rep.bad), 2, rep.bad)
        self.assertTrue(any("fixer.md" in b for b in rep.bad) and any("whole.md" in b for b in rep.bad))

    def test_unregistered_sender_fails(self):
        d = tempfile.mkdtemp(prefix="const-test-")
        write(os.path.join(d, "tools", "newmind.py"), "import anthropic\nc = anthropic.Anthropic()\nc.messages.create(model='x')\n")
        write(os.path.join(d, "tools", "runner.sh"), "claude -p --model haiku 'do it'\n")
        write(os.path.join(d, "tools", "plain.py"), "print('claude is mentioned, nothing is sent')\n")
        rep = constcheck.Report()
        found = constcheck.check_senders(rep, d)
        self.assertEqual(sorted(found), ["tools/newmind.py", "tools/runner.sh"])
        self.assertEqual(len(rep.bad), 2)

    def hooks(self, settings):
        root = tempfile.mkdtemp(prefix="const-test-")
        repo = os.path.join(root, "legion")
        for d in (root, repo):
            write(os.path.join(d, ".claude", "settings.json"), json.dumps(settings))
        rep = constcheck.Report()
        constcheck.check_hooks(rep, root, repo)
        return rep

    def hook(self, script, arg=""):
        return {"hooks": [{"type": "command", "command": ("python " + os.path.join(REPO, "tools", "system", script).replace("\\", "/") + " " + arg).strip()}]}

    def test_hooks_wired_once(self):
        good = {"hooks": {"SubagentStart": [self.hook("constitution.py", "subagent-start")],
                          "PreToolUse": [self.hook("mainguard.py")]}}
        self.assertEqual(self.hooks(good).bad, [])

    def test_second_injection_fails(self):
        twice = {"hooks": {"SubagentStart": [self.hook("constitution.py", "subagent-start")],
                           "SessionStart": [self.hook("constitution.py", "subagent-start")],
                           "PreToolUse": [self.hook("mainguard.py")]}}
        self.assertEqual(len(self.hooks(twice).bad), 2)

    def test_missing_hooks_fail(self):
        self.assertEqual(len(self.hooks({"hooks": {"PreToolUse": [self.hook("mainguard.py")]}}).bad), 2)
        self.assertEqual(len(self.hooks({"hooks": {"SubagentStart": [self.hook("constitution.py", "subagent-start")]}}).bad), 2)

    def test_the_live_tree_passes(self):
        rep, _head = constcheck.run()
        self.assertEqual(rep.bad, [])


if __name__ == "__main__":
    unittest.main()
