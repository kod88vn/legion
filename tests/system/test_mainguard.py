"""Tests for tools/system/mainguard.py (Constitution 1.3 / 1.9). Offline: sample hook inputs only.
Run: python -m unittest discover -s tests/system -p "test_mainguard.py" -v"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import json
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.normpath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
import mainguard  # noqa: E402

ALLOW = mainguard.load_allow()
SCRATCH = r"C:\Users\kod\AppData\Local\Temp\claude\abc\sess\scratchpad"
MSG = "Constitution 1.3: the main session does not do this. Assign an agent."


def inp(tool, tool_input, agent=None, cwd=r"G:\RO"):
    d = {"session_id": "s1", "cwd": cwd, "scratchpad_dir": SCRATCH, "hook_event_name": "PreToolUse",
         "tool_name": tool, "tool_input": tool_input}
    if agent:
        d["agent_id"] = agent
        d["agent_type"] = "general-purpose"
    return d


def ok(tool, tool_input, agent=None, cwd=r"G:\RO"):
    # resolve_real=False: the tests judge the path logic alone and never touch the disk
    return mainguard.decide(inp(tool, tool_input, agent, cwd), ALLOW, resolve_real=False)[0]


def sh(cmd, **kw):
    return ok("Bash", {"command": cmd}, **kw)


def ps(cmd, **kw):
    return ok("PowerShell", {"command": cmd}, **kw)


class WhoTests(unittest.TestCase):
    def test_subagent_is_never_checked(self):
        for tool, ti in (("Bash", {"command": "git commit -am x && docker restart rathena"}),
                         ("Write", {"file_path": r"G:\RO\legion\legion\bot.go", "content": "x"}),
                         ("Edit", {"file_path": r"G:\RO\legion\tools\system\mainguard.allow.json"}),
                         ("SomethingNew", {})):
            self.assertTrue(ok(tool, ti, agent="a4e719bd1f05626df"), tool)

    def test_main_session_is_the_call_without_agent_id(self):
        self.assertTrue(mainguard.is_main_session({"tool_name": "Bash"}))
        self.assertTrue(mainguard.is_main_session({"agent_id": ""}))
        self.assertTrue(mainguard.is_main_session({"agent_id": None}))
        self.assertTrue(mainguard.is_main_session({"agent_type": "fixer"}))   # --agent session: still the main one
        self.assertFalse(mainguard.is_main_session({"agent_id": "a1"}))

    def test_same_call_main_denied_subagent_allowed(self):
        ti = {"command": "git -C /g/RO/legion commit -am x"}
        self.assertFalse(ok("Bash", ti))
        self.assertTrue(ok("Bash", ti, agent="a1"))


class ToolTests(unittest.TestCase):
    def test_always_allowed(self):
        for t in ("Read", "Grep", "Glob", "Agent", "SendMessage", "AskUserQuestion", "ToolSearch", "TaskStop"):
            self.assertTrue(ok(t, {"anything": 1}), t)

    def test_unknown_tool_denied(self):
        for t in ("EnterWorktree", "mcp__x__write", "Artifact", "", None, "NewTool"):
            self.assertFalse(ok(t, {}), t)

    def test_garbage_input_denied(self):
        self.assertFalse(mainguard.decide(None, ALLOW)[0])
        self.assertFalse(mainguard.decide([], ALLOW)[0])
        self.assertFalse(mainguard.decide({"tool_name": "Bash", "tool_input": "x"}, ALLOW)[0])
        self.assertFalse(mainguard.decide({"tool_name": "Bash", "tool_input": {"command": 5}}, ALLOW)[0])


class WriteTests(unittest.TestCase):
    def w(self, p, tool="Write"):
        return ok(tool, {"file_path": p, "content": "x"})

    def test_rule_records_allowed(self):
        for p in (r"G:\RO\legion\CONSTITUTION.md", r"G:\RO\legion\RULES.md", r"G:\RO\legion\STATUS.md",
                  r"G:\RO\legion\docs\rules\process.md", r"G:\RO\legion\docs\rules\sub\x.md",
                  r"G:\RO\legion\docs\classes\mage.md", r"G:\RO\legion\docs\reports\league-1.md",
                  r"C:\Users\kod\.claude\projects\G--RO\memory\MEMORY.md",
                  SCRATCH + r"\note.txt", "g:/ro/legion/rules.md", "G:/RO/legion/docs/rules/ayla.md"):
            self.assertTrue(self.w(p), p)
            self.assertTrue(self.w(p, "Edit"), p)

    def test_everything_else_denied(self):
        for p in (r"G:\RO\legion\legion\bot.go", r"G:\RO\legion\tools\system\mainguard.py",
                  r"G:\RO\legion\tools\system\mainguard.allow.json", r"G:\RO\legion\tools\system\mainguard.off",
                  r"G:\RO\.claude\settings.json", r"G:\RO\legion\.claude\settings.json", r"G:\RO\CLAUDE.md",
                  r"G:\RO\legion\CLAUDE.md", r"G:\RO\legion\.env", r"G:\RO\legion\legion\data\tuning.json",
                  r"G:\RO\legion\openkore-ayla\control\config.txt", r"G:\RO\legion\docs\design\REDESIGN.md",
                  r"G:\RO\legion\docs\rules", r"G:\RO\legion\docs\rulesx\a.md", r"G:\RO\legion\RULES.md.bak",
                  r"G:\RO\legion\logs\constitution-denied.log", r"C:\Users\kod\.claude\settings.json",
                  r"C:\Users\kod\.claude\projects\G--RO\x.jsonl", r"C:\Windows\x", "relative.md", "", None):
            self.assertFalse(self.w(p), p)

    def test_path_tricks(self):
        for p in (r"G:\RO\legion\docs\rules\..\..\legion\bot.go", "G:/RO/legion/docs/rules/../../.env",
                  "G:/RO/legion/docs/rules/../design/x.md", r"G:\RO\legion\docs\rules\..\..\tools\x.py",
                  "/g/RO/legion/docs/rules/../../tools/system/mainguard.py",
                  "/mnt/g/RO/legion/docs/reports/../../docker-compose.yml",
                  r"G:/RO\legion/docs\rules/..\..\legion/data/x.json",
                  r"\\?\G:\RO\legion\legion\bot.go", r"\\localhost\G$\RO\legion\RULES.md",
                  r"G:\RO\legion\RULES.md:stream", r"G:\RO\legion\docs\rules\x.md.", "G:RULES.md",
                  r"G:\RO\legion\docs\rules\~\x", r"G:\RO\LEGION~1\RULES.md", SCRATCH + r"\..\..\..\..\x.txt",
                  r"C:\Users\kod\.claude\projects\G--RO\memory\..\..\..\settings.json"):
            self.assertFalse(self.w(p), p)

    def test_same_file_other_spellings_allowed(self):
        for p in ("/g/RO/legion/RULES.md", "/mnt/g/RO/legion/docs/rules/process.md", r"g:\ro\LEGION\Docs\Rules\x.md",
                  r"G:\RO\legion\tools\..\docs\rules\x.md", "G:/RO//legion/./STATUS.md"):
            self.assertTrue(self.w(p), p)

    def test_notebook_edit_checked(self):
        self.assertFalse(ok("NotebookEdit", {"notebook_path": r"G:\RO\legion\x.ipynb"}))


class ShellTests(unittest.TestCase):
    def test_read_only_inspection_allowed(self):
        for c in ("git status", "git -C /g/RO/legion log --oneline -5", "git diff HEAD~1 -- RULES.md", "git show dce4c68",
                  "git branch -a", "git branch", "git worktree list", "git --no-pager log -3",
                  "ls -la /g/RO/legion", "cat STATUS.md", "head -20 RULES.md", "tail -n 50 x.log", "wc -l a b",
                  "grep -rn 'G14' docs/rules", 'grep -n "foo$" RULES.md', "find . -name '*.md' -newer x",
                  "docker ps", "docker ps -a --format '{{.Names}} {{.Status}}'", "docker logs --tail 50 ro-legion",
                  "wsl -d Ubuntu-24.04 -- docker ps", "wsl -d Ubuntu-24.04 -- docker logs --tail 20 ro-rathena",
                  "wsl -d Ubuntu-24.04 -- bash -lc \"docker ps --format '{{.Names}}' | head -5\"",
                  "python tools/system/bugq.py list", "python /g/RO/legion/tools/system/bugq.py close b1 --note 'dup'",
                  "python G:/RO/legion/tools/system/league.py", "python tools/system/leaderboard.py --top 10",
                  "python tools/system/gatecheck.py --coverage", "python tools/system/constcheck.py",
                  "python tools/legionctl.py status", "python tools/legionctl.py meter bots --window 1h",
                  "/c/Users/kod/AppData/Local/Programs/Python/Python312/python tools/system/bugq.py rank",
                  "wsl -d Ubuntu-24.04 --cd G:/RO/legion -- python3 tools/system/bugq.py list",
                  "git status 2>&1", "git log -3 2>/dev/null", "ls >/dev/null 2>&1", "echo ---"):
            self.assertTrue(sh(c, cwd=r"G:\RO\legion"), c)

    def test_chains_need_every_part(self):
        self.assertTrue(sh("cd /g/RO/legion && git status && git log --oneline | head -5", cwd=r"G:\RO"))
        self.assertTrue(sh("git status; git diff --stat | tail -3", cwd=r"G:\RO\legion"))
        self.assertTrue(sh("git status\ngit branch -a", cwd=r"G:\RO\legion"))
        for c in ("git status && git commit -am x", "git log | tee out.txt", "git status; docker restart rathena",
                  "git status || rm -rf x", "ls\nrm x", "cat a | xargs rm", "git status && git push",
                  "ls & rm x", "git status |& tee x", "ls | sh", "cat x | bash", "ls | python",
                  "cd /g/RO/legion && git checkout main", "git log && wsl -d Ubuntu-24.04 -- docker stop legion"):
            self.assertFalse(sh(c, cwd=r"G:\RO\legion"), c)

    def test_system_changing_commands_denied(self):
        for c in ("git commit -m x", "git merge fix/a", "git checkout main", "git reset --hard", "git push",
                  "git add -A", "git stash", "git branch new", "git branch -D x", "git branch -m a b",
                  "git worktree add ../x", "git worktree remove x", "git -c core.pager=evil log",
                  "git --exec-path=/x log", "git log --output=x.txt", "git diff --output x", "git tag v1", "git",
                  "git config user.name x", "git rebase main", "git cherry-pick x", "git restore .", "git clean -fd",
                  "docker start rathena", "docker stop legion", "docker restart rathena", "docker compose up -d",
                  "docker-compose up", "docker cp a b", "docker exec ro-db mysql", "docker kill x", "docker rm x",
                  "docker", "docker run x", "docker build .", "docker compose ps",
                  "wsl -d Ubuntu-24.04 -- docker restart rathena", "wsl --shutdown", "wsl -t Ubuntu-24.04", "wsl",
                  "wsl -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/deploy/dep3.sh",
                  "wsl -d Ubuntu-24.04 -- bash -lc 'docker ps; docker restart rathena'",
                  "wsl -u root -- docker ps", "wsl -d Ubuntu-24.04 -- docker ps ';' reboot",
                  "bash tools/deploy/dep3.sh", "bash tools/rogui/ayla.sh map pay_dun01", "sh x.sh", "./x.sh",
                  "tools/rogui/keeper.sh", "bash tools/rogui/ayla.sh log", "source x", ". x",
                  "python tools/system/stackctl.py up", "python tools/system/deploylog.py", "python -c 'import os'",
                  "python -m pip install x", "python", "python tools/puppet/dispatch.py", "python x.py",
                  "python tools/legionctl.py say guild01 hi", "python tools/legionctl.py goal x",
                  "python tools/legionctl.py", "python tools/legionctl.py shop a b", "python tools/legionctl.py think x y",
                  "python tools/system/../legionctl.py say x", "python tools/system/../../tools/puppet/dispatch.py",
                  "python /tmp/tools/system/bugq.py", "python C:/x/tools/system/bugq.py list",
                  "rm x", "mv a b", "cp a b", "touch x", "mkdir x", "chmod +x a", "sed -i s/a/b/ x", "tee x", "dd if=a",
                  "curl -X POST http://127.0.0.1:8091/api/x", "kill 1", "pkill python", "npm i", "go build ./...",
                  "make", "nohup x", "xargs rm", "env X=1 git status", "X=1 git status", "sudo ls", "eval ls",
                  "exec ls", "/usr/bin/git commit", "C:/tools/git.exe status", "find . -delete",
                  "find . -name x -exec rm {} ;", "find . -fprint out", "sort -o out in", "sort -uo out in",
                  "sort --output=out in", "date -s 2020-01-01", "awk 'BEGIN{system(\"x\")}'", "docker logs x; reboot",
                  "powershell -Command Remove-Item x", "cmd /c del x", "start x"):
            self.assertFalse(sh(c, cwd=r"G:\RO\legion"), c)

    def test_substitution_and_syntax_tricks_denied(self):
        for c in ("ls $(rm x)", "ls `rm x`", 'echo "$(rm x)"', 'echo "`rm x`"', "cat <(rm x)", "ls > >(tee x)",
                  "ls $HOME", 'cat "${X}"', "git $X", "(git commit)", "{ git commit; }", "ls && (rm x)",
                  "cat <<EOF\nx\nEOF", "cat < x", "ls \\\n -la", "ls # x", "git status &", "'unclosed", '"unclosed',
                  "$'rm' x", "ls;$(x)", "a=$(x)", "", "   ", "echo $((1+1))", "git log --format=$x",
                  "g''it commit", "gi\\t commit", '"git" commit', "'git' 'commit'", "\\git commit",
                  "cd /tmp && python tools/system/bugq.py list", "cd $X && python tools/system/bugq.py list",
                  "cd .. && python tools/system/bugq.py list", "cd && python tools/system/bugq.py list"):
            self.assertFalse(sh(c, cwd=r"G:\RO\legion"), c)

    def test_quoting_does_not_hide_a_split(self):
        self.assertTrue(sh("grep -n 'a && b; c | d > e' RULES.md", cwd=r"G:\RO\legion"))
        self.assertTrue(sh('grep -n "git commit; rm x" RULES.md', cwd=r"G:\RO\legion"))
        self.assertFalse(sh("grep -n 'a' RULES.md; rm x", cwd=r"G:\RO\legion"))
        self.assertFalse(sh("grep -n 'a' RULES.md' ; rm x", cwd=r"G:\RO\legion"))

    def test_redirection_only_into_the_scratchpad(self):
        s = SCRATCH.replace("\\", "/")
        self.assertTrue(sh("git log -5 > %s/log.txt" % s))
        self.assertTrue(sh("git log -5 >> '%s/log.txt' 2>&1" % s))
        self.assertTrue(sh("git diff > /c/Users/kod/AppData/Local/Temp/claude/abc/sess/scratchpad/d.txt"))
        for c in ("git log > out.txt", "git log > /g/RO/legion/RULES.md", "echo x > /g/RO/legion/docs/rules/a.md",
                  "echo x >> STATUS.md", "echo x > %s/../../../../x" % s, "echo x >", "echo x > $F",
                  "echo '{}' > /g/RO/legion/legion/data/tuning.json", "echo 1 > tools/system/mainguard.off",
                  "echo x &> out", "echo x 2> err.txt", "> x", "echo x >&file", "ls > ~/x"):
            self.assertFalse(sh(c, cwd=r"G:\RO\legion"), c)

    def test_relative_script_follows_cd(self):
        self.assertTrue(sh("cd /g/RO/legion && python tools/system/bugq.py list", cwd=r"C:\x"))
        self.assertTrue(sh("cd legion && python tools/system/bugq.py list", cwd=r"G:\RO"))
        self.assertTrue(sh("python legion/tools/system/bugq.py list", cwd=r"G:\RO"))
        self.assertFalse(sh("python tools/system/bugq.py list", cwd=r"G:\RO"))
        self.assertFalse(sh("python tools/system/bugq.py list", cwd=SCRATCH))
        self.assertFalse(sh("cd %s && python tools/system/bugq.py list" % SCRATCH.replace("\\", "/"), cwd=r"G:\RO\legion"))
        self.assertFalse(sh("python tools/system/bugq.py list", cwd=None))

    def test_powershell(self):
        for c in ("Get-ChildItem G:\\RO\\legion", "Get-Content STATUS.md -Tail 20", "git status", "git log --oneline -5",
                  "Get-Content x.log | Select-String error | Select-Object -First 5", "docker ps", "ls; git status",
                  "wsl -d Ubuntu-24.04 -- docker ps", "python tools/system/bugq.py list", "git status 2>$null",
                  "get-childitem . | sort-object Name"):
            self.assertTrue(ps(c, cwd=r"G:\RO\legion"), c)
        for c in ("Remove-Item x", "Set-Content x y", "git commit -m x", "Get-Content a | Set-Content b",
                  "Get-Content a > b", "Get-Content a | Out-File b", "docker restart rathena", "& git commit",
                  "Invoke-Expression 'git commit'", "iex x", "$x = 1", "Get-ChildItem | ForEach-Object { rm $_ }",
                  "Get-ChildItem | % { x }", "Start-Process x", "git status; git push", "ls `\n x", "$(git commit)",
                  "Get-Content \"$(rm x)\"", "New-Item x", "Copy-Item a b", "Stop-Process -Name python",
                  "wsl -d Ubuntu-24.04 -- docker stop legion", "[IO.File]::WriteAllText('a','b')", ". .\\x.ps1",
                  ".\\x.ps1", "powershell -c x", "cmd /c x", "Restart-Service x", "@(1)", "git status 2>err.txt"):
            self.assertFalse(ps(c, cwd=r"G:\RO\legion"), c)


class HookProcessTests(unittest.TestCase):
    """the script as Claude Code runs it: JSON on stdin, a deny decision or silence on stdout, exit 0"""

    def run_hook(self, payload, raw=None):
        log = os.path.join(tempfile.mkdtemp(prefix="mainguard-test-"), "denied.log")
        env = dict(os.environ, MAINGUARD_LOG=log)
        p = subprocess.run([sys.executable, os.path.join(REPO, "tools", "system", "mainguard.py")],
                           input=raw if raw is not None else json.dumps(payload), capture_output=True, text=True,
                           env=env, timeout=30)
        rows = []
        if os.path.exists(log):
            with open(log, encoding="utf-8") as f:
                rows = [json.loads(x) for x in f if x.strip()]
        return p, rows

    @unittest.skipIf(os.path.exists(mainguard.OFF_FILE), "the guard is switched off (mainguard.off)")
    def test_deny_output_and_log(self):
        p, rows = self.run_hook(inp("Bash", {"command": "git commit -am x"}))
        self.assertEqual(p.returncode, 0, p.stderr)
        out = json.loads(p.stdout)["hookSpecificOutput"]
        self.assertEqual(out["hookEventName"], "PreToolUse")
        self.assertEqual(out["permissionDecision"], "deny")
        self.assertTrue(out["permissionDecisionReason"].startswith(MSG))
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["tool"], "Bash")
        self.assertIn("git commit", rows[0]["what"])

    def test_allowed_is_silent(self):
        p, rows = self.run_hook(inp("Read", {"file_path": "x"}))
        self.assertEqual((p.returncode, p.stdout, rows), (0, "", []))
        p, rows = self.run_hook(inp("Bash", {"command": "git status"}))
        self.assertEqual((p.returncode, p.stdout, rows), (0, "", []))

    def test_subagent_is_silent(self):
        p, rows = self.run_hook(inp("Bash", {"command": "git commit -am x"}, agent="a1"))
        self.assertEqual((p.returncode, p.stdout, rows), (0, "", []))

    @unittest.skipIf(os.path.exists(mainguard.OFF_FILE), "the guard is switched off (mainguard.off)")
    def test_unreadable_input_fails_closed(self):
        p, rows = self.run_hook(None, raw="not json")
        self.assertEqual(json.loads(p.stdout)["hookSpecificOutput"]["permissionDecision"], "deny")

    def test_message_in_allow_file(self):
        self.assertEqual(ALLOW["deny_message"], MSG)
        self.assertEqual(mainguard.DEFAULT_MESSAGE, MSG)


class RealPathTests(unittest.TestCase):
    """with the disk consulted (as in the live hook): the plain cases still pass"""

    @unittest.skipUnless(os.name == "nt", "Windows paths")
    def test_live_resolution(self):
        # the expected answers are for the live checkout's paths (G:\RO\legion); a copy elsewhere would decide differently
        if os.path.normcase(os.path.abspath(REPO)) != os.path.normcase(r"G:\RO\legion"):
            self.skipTest("live-checkout paths only (REPO is %s)" % REPO)
        d = lambda t, ti: mainguard.decide(inp(t, ti, cwd=REPO), ALLOW)[0]
        self.assertTrue(d("Write", {"file_path": os.path.join(REPO, "RULES.md")}))
        self.assertTrue(d("Write", {"file_path": os.path.join(REPO, "docs", "reports", "new-report.md")}))
        self.assertFalse(d("Write", {"file_path": os.path.join(REPO, "tools", "system", "mainguard.py")}))
        self.assertTrue(d("Bash", {"command": "python tools/system/bugq.py list"}))
        self.assertTrue(d("Bash", {"command": "python tools/system/gatecheck.py --coverage"}))


if __name__ == "__main__":
    unittest.main()
