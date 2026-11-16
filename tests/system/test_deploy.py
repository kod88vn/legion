"""The deploy batch script (tools/deploy/deploy.py, rule PR47). Every gate and step is tested with a fake docker and a
temp repo and data folder: no live container, image or data file is touched (rule G23).
Run: python -m unittest discover -s tests/system -p test_deploy.py -v   (from the repo root)"""
import importlib.util
import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

REPO = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
import common as C  # noqa: E402

_spec = importlib.util.spec_from_file_location("deploy_batch", os.path.join(REPO, "tools", "deploy", "deploy.py"))
DP = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(DP)

T0 = 1800000000  # pinned clock (LEGION_FAKE_NOW)
SHA_A = "a" * 40


def fake_resolve(name):
    return {"fix/a": SHA_A, SHA_A: SHA_A}.get(name)


class FakeDocker:
    """Answers every command the batch runs and records it. Knobs set by the tests."""

    def __init__(self):
        self.calls = []
        self.ps = ""              # docker ps lines (id, name, image, compose project)
        self.hash_ok = True       # compose config hash equals the running container's label
        self.dry_out = {}         # compose dry-run output: "deps" (with deps) / "nodeps"
        self.dry_rc = 0
        self.test_rc = []         # rc of each `docker run` (go vet/test); empty = 0
        self.up_rc = 0
        self.logs = ""            # docker logs of Ayla
        self.ayla = {"online": True, "map": "prontera", "ts": T0 + 100}

    def __call__(self, argv, cwd=None):
        a = list(argv)
        self.calls.append(a)
        j = " ".join(a)
        if a[:3] == ["docker", "compose", "config"]:
            return 0, "%s %s" % (a[-1], "HASH1" if self.hash_ok else "HASHX")
        if a[:2] == ["docker", "inspect"] and "config-hash" in j:
            return 0, "HASH1"
        if a[:2] == ["docker", "inspect"] and "{{.Image}}" in a:
            return 0, "sha256:IMG"
        if a[:3] == ["docker", "image", "inspect"]:
            return 0, "sha256:IMG"
        if a[:2] == ["docker", "ps"]:
            return 0, self.ps
        if a[:2] == ["docker", "compose"] and "--dry-run" in a:
            return self.dry_rc, self.dry_out.get("nodeps" if "--no-deps" in a else "deps", "")
        if a[:2] == ["docker", "run"]:
            return (self.test_rc.pop(0) if self.test_rc else 0), "go test ok"
        if a[:2] == ["docker", "build"]:
            return 0, "built"
        if a[:2] == ["docker", "compose"] and "up" in a:
            return self.up_rc, "Container x  Recreated"
        if a[:2] == ["docker", "logs"]:
            return 0, self.logs
        if a[:2] == ["docker", "stop"]:
            return 0, ""
        if len(a) > 1 and a[1].endswith("league.py"):
            return 0, "league output\n"
        return 0, ""

    def acts(self):
        """Calls that change something (not reads)."""
        return [c for c in self.calls if (c[:2] == ["docker", "run"] or c[:2] == ["docker", "build"]
                                          or c[:2] == ["docker", "stop"] or ("compose" in c and "up" in c and "--dry-run" not in c))]

    def ups(self):
        return [c for c in self.calls if "compose" in c and "up" in c and "--dry-run" not in c]


class DeployTests(unittest.TestCase):
    def setUp(self):
        self.saved_env = {k: os.environ.get(k) for k in ("LEGION_DATA", "LEGION_FAKE_NOW")}
        self.tmp = tempfile.mkdtemp(prefix="deploy-test-")
        self.data = os.path.join(self.tmp, "data")
        os.makedirs(self.data)
        os.environ["LEGION_DATA"] = self.data
        os.environ["LEGION_FAKE_NOW"] = str(T0)
        self.root = os.path.join(self.tmp, "repo")
        os.makedirs(os.path.join(self.root, "legion"))
        os.makedirs(os.path.join(self.root, "openkore-ayla", "logs"))
        with open(os.path.join(self.root, "legion", "Dockerfile"), "w") as f:
            f.write("FROM scratch\n")
        with open(os.path.join(self.root, "legion", "main.go"), "w") as f:
            f.write("package main\n")
        with open(os.path.join(self.root, "legion", "notes.txt"), "w") as f:
            f.write("not copied\n")
        self.rev = os.path.join(self.tmp, "reviews")
        os.makedirs(self.rev)
        with open(os.path.join(self.rev, SHA_A + ".md"), "w") as f:
            f.write("# review\n\nverdict: merge\n")
        self.write_status({"online": True, "map": "prontera", "ts": T0 + 100})
        self.lines = []
        self.sleeps = []

    def tearDown(self):
        for k, v in self.saved_env.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v
        testenv.isolate()  # restore the process-wide temp data folder
        import shutil
        shutil.rmtree(self.tmp, ignore_errors=True)

    def write_status(self, s):
        with open(os.path.join(self.root, "openkore-ayla", "logs", "status.json"), "w") as f:
            json.dump(s, f)

    def go(self, fixes=("the fix|kpm|solo",), reviewed=("fix/a",), critical=None, services="legion,legion-core",
           lifted=False, apply=False, fake=None):
        fake = fake or FakeDocker()
        d = DP.Deploy(root=self.root, apply=apply, execute_fn=fake, sleep=lambda s: self.sleeps.append(s),
                      reviews_dir=self.rev, resolve=fake_resolve, out=self.lines.append)
        argv = []
        for f in fixes:
            argv += ["--fix", f]
        for r in reviewed:
            argv += ["--reviewed", r]
        if critical is not None:
            argv += ["--critical", critical]
        argv += ["--services", services]
        if apply:
            argv.append("--apply")
        if lifted:
            argv.append("--owner-lifts-hold")
        code = DP.main(argv, deploy=d)
        return code, fake

    def record(self, epoch, tag="ro-legion:deploy-old", fixes=None, **extra):
        rec = {"epoch": int(epoch), "tag": tag, "services": ["legion", "legion-core"], "critical": False, "reason": "",
               "fixes": fixes or []}
        rec.update(extra)
        C.append_jsonl(C.dpath("deploys.jsonl"), rec)

    def text(self):
        return "\n".join(self.lines)

    # ---------------------------------------------------------------- default and review gate
    def test_dry_run_is_the_default_and_changes_nothing(self):
        code, fake = self.go()
        self.assertEqual(code, DP.OK, self.text())
        self.assertEqual(fake.acts(), [], "a dry run must not change anything")
        self.assertEqual(fake.ups(), [])
        self.assertIn("DRY RUN", self.text())
        self.assertFalse(os.path.exists(C.dpath("deploys.jsonl")))
        self.assertFalse(os.path.exists(C.dpath("deploy.lock")))
        self.assertEqual(self.sleeps, [])

    def test_no_merge_review_is_refused(self):
        code, fake = self.go(reviewed=("fix/b",))  # not a commit with a review
        self.assertEqual(code, DP.GATE, self.text())
        self.assertIn("review", self.text())
        self.assertEqual(fake.ups(), [])
        code, fake = self.go(apply=True, reviewed=("fix/b",))
        self.assertEqual(code, DP.GATE)
        self.assertEqual(fake.acts(), [], "nothing built or stopped when the review is missing")

    def test_review_without_merge_verdict_is_refused(self):
        with open(os.path.join(self.rev, SHA_A + ".md"), "w") as f:
            f.write("verdict: do not merge\n")
        code, _ = self.go()
        self.assertEqual(code, DP.GATE)

    def test_one_reviewed_commit_per_fix(self):
        code, fake = self.go(fixes=("one|kpm|x", "two|kpm|x"), reviewed=("fix/a",))
        self.assertEqual(code, DP.GATE)
        self.assertIn("2 fix(es) but 1", self.text())
        self.assertEqual(fake.ups(), [])

    def test_no_fix_is_refused(self):
        code, _ = self.go(fixes=(), reviewed=())
        self.assertEqual(code, DP.GATE)
        self.assertIn("no --fix", self.text())

    def test_metric_must_be_measurable(self):
        code, _ = self.go(fixes=("a|fps|x",))
        self.assertEqual(code, DP.GATE)
        self.assertIn("kpm, zph or deaths", self.text())

    # ---------------------------------------------------------------- G17: 3-hour rule and critical override
    def test_three_hour_rule_refuses_a_deploy_too_soon(self):
        self.record(T0 - 3600)
        code, fake = self.go(apply=True)
        self.assertEqual(code, DP.GATE, self.text())
        self.assertIn("min ago", self.text())
        self.assertEqual(fake.acts(), [], "too soon: nothing built or stopped")

    def test_three_hour_rule_allows_a_deploy_after_the_gap(self):
        self.record(T0 - 4 * 3600)
        code, _ = self.go()
        self.assertEqual(code, DP.OK, self.text())

    def test_critical_override_skips_the_gap_with_a_reason(self):
        self.record(T0 - 600)
        code, _ = self.go(critical="api wedge, unblocks every bot")
        self.assertEqual(code, DP.OK, self.text())
        self.assertIn("critical deploy allowed", self.text())

    def test_critical_without_reason_is_refused(self):
        self.record(T0 - 600)
        code, _ = self.go(critical="   ")
        self.assertEqual(code, DP.GATE)

    def test_critical_still_needs_a_merge_review(self):
        code, _ = self.go(critical="wedge", reviewed=("fix/b",))
        self.assertEqual(code, DP.GATE)

    # ---------------------------------------------------------------- G21, lock, G15 services
    def test_stack_hold_refuses_unless_the_owner_lifts_it(self):
        open(C.dpath("stack.hold"), "w").close()
        code, fake = self.go(apply=True)
        self.assertEqual(code, DP.HOLD, self.text())
        self.assertIn("G21", self.text())
        self.assertEqual(fake.acts(), [])
        code, _ = self.go(lifted=True)
        self.assertEqual(code, DP.OK, self.text())

    def test_a_running_deploy_lock_refuses(self):
        open(C.dpath("deploy.lock"), "w").close()
        code, fake = self.go()
        self.assertEqual(code, DP.LOCKED)
        self.assertEqual(fake.calls, [], "refused before any check ran")

    def test_only_legion_and_legion_core_may_be_deployed(self):
        for svc in ("rathena", "legion,ro-rathena", "db", "openkore-ayla", ""):
            code, fake = self.go(services=svc)
            self.assertEqual(code, DP.SERVICE, svc + ": " + self.text())
            self.assertEqual(fake.ups(), [])

    # ---------------------------------------------------------------- G15: rathena and db never change
    def test_rathena_or_db_config_change_refuses(self):
        code, fake = self.go(apply=True, fake=self._fake(hash_ok=False))
        self.assertEqual(code, DP.DRIFT, self.text())
        self.assertIn("rathena", self.text())
        self.assertEqual(fake.acts(), [])

    def test_compose_would_recreate_rathena_refuses(self):
        fake = FakeDocker()
        fake.dry_out["deps"] = " Container ro-rathena  Recreate\n Container ro-legion  Recreated\n"
        code, fake = self.go(fake=fake)
        self.assertEqual(code, DP.DRIFT, self.text())
        self.assertIn("would change", self.text())

    def test_compose_would_touch_another_container_refuses(self):
        fake = FakeDocker()
        fake.dry_out["nodeps"] = " Container ro-openkore-ayla  Recreate\n"
        code, _ = self.go(fake=fake)
        self.assertEqual(code, DP.DRIFT, self.text())

    def test_compose_dry_run_error_fails_closed(self):
        fake = FakeDocker()
        fake.dry_rc = 1
        code, _ = self.go(fake=fake)
        self.assertEqual(code, DP.DRIFT, "a compose error must refuse, not allow")

    def _fake(self, **kw):
        fake = FakeDocker()
        for k, v in kw.items():
            setattr(fake, k, v)
        return fake

    def test_up_command_is_only_legion_or_legion_core_with_no_deps(self):
        for svc in DP.ALLOWED:
            argv = DP.up_argv(svc, "/tmp/override.yml")
            self.assertIn("--no-deps", argv)
            self.assertEqual(argv[-1], svc)
            for bad in ("rathena", "ro-rathena", "db", "ro-db", "openkore-ayla", "ro-openkore-ayla", "agency"):
                self.assertNotIn(bad, argv, "%s up must not name %s" % (svc, bad))

    def test_act_refuses_a_compose_up_that_could_take_rathena_along(self):
        d = DP.Deploy(root=self.root, apply=True, execute_fn=FakeDocker())
        with self.assertRaises(RuntimeError):
            d.act(["docker", "compose", "up", "-d", "ro-rathena"])
        with self.assertRaises(RuntimeError):
            d.act(["docker", "compose", "up", "-d", "--no-deps", "ro-rathena"])
        with self.assertRaises(RuntimeError):
            d.act(["docker", "compose", "up", "-d", "legion"])  # no --no-deps: compose would start rathena too

    # ---------------------------------------------------------------- the apply path
    def _stock_ps(self):
        return "\n".join([
            "aaa111\tro-rathena\tro-rathena:latest\tro",
            "bbb222\tro-openkore-ayla\topenkore:latest\tro",
            "ccc333\tro-legion\tro-legion:latest\tro",
            "ddd444\tro-db\tpostgres\tro",
            "eee555\tbusy_mclean\tgolang:1.22\t",        # stray golang test container: stopped
            "fff666\tro-legion-core\tro-legion:latest\tro",
        ])

    def test_apply_runs_the_whole_batch(self):
        fake = FakeDocker()
        fake.ps = self._stock_ps()
        code, fake = self.go(apply=True, fake=fake)
        self.assertEqual(code, DP.OK, self.text())
        stops = [c for c in fake.calls if c[:2] == ["docker", "stop"]]
        self.assertEqual(stops, [["docker", "stop", "eee555"]], "only the stray golang container is stopped (G24)")
        self.assertTrue(any(c[:2] == ["docker", "run"] and "go vet" in " ".join(c) for c in fake.calls))
        builds = [c for c in fake.calls if c[:2] == ["docker", "build"]]
        self.assertEqual(len(builds), 1)
        tag = builds[0][builds[0].index("-t") + 1]
        self.assertTrue(tag.startswith("ro-legion:deploy-"), tag)
        ups = fake.ups()
        self.assertEqual([u[-1] for u in ups], ["legion", "legion-core"], "legion first, then legion-core")
        for u in ups:
            self.assertIn("--no-deps", u)
            self.assertNotIn("ro-rathena", u)
            self.assertNotIn("rathena", u)
        recs = C.read_jsonl(C.dpath("deploys.jsonl"))
        self.assertEqual(len(recs), 1)
        self.assertEqual(recs[0]["services"], ["legion", "legion-core"])
        self.assertEqual(recs[0]["fixes"][0]["text"], "the fix")
        self.assertEqual(recs[0]["tag"], tag)
        run_dir = os.path.join(self.data, "deploy-runs", "deploy-" + tag.split("deploy-")[-1])
        self.assertTrue(os.path.exists(os.path.join(run_dir, "run.log")))
        self.assertTrue(os.path.exists(os.path.join(run_dir, "pre-league.md")))
        with open(os.path.join(run_dir, "override.yml")) as f:
            ov = f.read()
        self.assertIn("legion:", ov)
        self.assertIn("legion-core:", ov)
        self.assertNotIn("rathena", ov)
        self.assertFalse(os.path.exists(C.dpath("deploy.lock")), "lock released")

    def test_stray_stop_never_touches_protected_containers(self):
        fake = FakeDocker()
        fake.ps = "\n".join(["aaa\tro-rathena\tgolang:1.22\t", "bbb\tro-legion\tgolang:1.22\t",
                             "ccc\tlegion-test\tgolang:1.22\tro"])
        code, fake = self.go(apply=True, fake=fake)
        self.assertEqual(code, DP.OK, self.text())
        self.assertEqual([c for c in fake.calls if c[:2] == ["docker", "stop"]], [])

    def test_failed_go_test_twice_deploys_nothing(self):
        fake = FakeDocker()
        fake.test_rc = [1, 1]
        code, fake = self.go(apply=True, fake=fake)
        self.assertEqual(code, DP.FAILED, self.text())
        self.assertEqual(fake.ups(), [], "nothing deployed")
        self.assertEqual([c for c in fake.calls if c[:2] == ["docker", "build"]], [])
        self.assertIn(60, self.sleeps, "a fresh snapshot is tried after 60 s")
        self.assertFalse(os.path.exists(C.dpath("deploys.jsonl")))
        self.assertFalse(os.path.exists(C.dpath("deploy.lock")))

    def test_failed_go_test_once_then_passes_on_a_fresh_snapshot(self):
        fake = FakeDocker()
        fake.test_rc = [1]
        code, fake = self.go(apply=True, fake=fake)
        self.assertEqual(code, DP.OK, self.text())
        self.assertEqual(len(fake.ups()), 2)

    def test_up_failure_stops_before_the_second_service(self):
        fake = FakeDocker()
        fake.up_rc = 1
        code, fake = self.go(apply=True, fake=fake)
        self.assertEqual(code, DP.FAILED, self.text())
        self.assertEqual(len(fake.ups()), 1)
        self.assertFalse(os.path.exists(C.dpath("deploys.jsonl")), "a failed up is not recorded")

    def test_ayla_not_back_within_five_minutes_is_flagged(self):
        self.write_status({"online": False, "map": "", "ts": T0 - 9999})
        code, fake = self.go(apply=True)
        self.assertEqual(code, DP.AYLA_LATE, self.text())
        self.assertIn("G19", self.text())
        self.assertEqual(self.sleeps.count(DP.AYLA_POLL_S), DP.AYLA_WINDOW_S // DP.AYLA_POLL_S,
                         "polled for the full 5 minutes")
        self.assertEqual(len(C.read_jsonl(C.dpath("deploys.jsonl"))), 1, "the deploy stays recorded")
        for u in fake.ups():
            self.assertNotIn("ai", u)

    def test_ayla_back_from_the_log_line_counts(self):
        self.write_status({"online": False, "map": "", "ts": 0})
        fake = FakeDocker()
        fake.logs = "... You are now in the game ..."
        code, _ = self.go(apply=True, fake=fake)
        self.assertEqual(code, DP.OK, self.text())

    # ---------------------------------------------------------------- compare and the revert list
    def test_compare_calls_league_and_writes_the_revert_list(self):
        self.record(T0 - 4 * 3600, tag="ro-legion:deploy-20261114-000000",
                    fixes=[{"text": "bad fix", "metric": "kpm", "cohort": ""}])
        self.record(T0 - 4 * 3600, tag="ro-legion:deploy-20261114-000000", kind="verdict", partial=False,
                    items=[{"text": "bad fix", "metric": "kpm", "cohort": "", "change_pct": -5.0,
                            "verdict": "REVERT (kpm -5.0%)"},
                           {"text": "good fix", "metric": "kpm", "cohort": "", "change_pct": 4.0,
                            "verdict": "KEEP (kpm +4.0%)"}])
        d = DP.Deploy(root=self.root, apply=False, execute_fn=FakeDocker(), out=self.lines.append)
        code = DP.main(["compare"], deploy=d)
        self.assertEqual(code, DP.OK, self.text())
        out =os.path.join(self.data, "deploy-runs", "deploy-20261114-000000", "revert.txt")
        self.assertTrue(os.path.exists(out))
        with open(out) as f:
            text = f.read()
        self.assertIn("bad fix", text)
        self.assertNotIn("good fix", text, "only REVERT items are listed")

    def test_compare_runs_league_with_compare_deploy(self):
        self.record(T0 - 4 * 3600, tag="ro-legion:deploy-20261114-000000")
        fake = FakeDocker()
        d = DP.Deploy(root=self.root, apply=False, execute_fn=fake, out=self.lines.append)
        DP.main(["compare"], deploy=d)
        cmp_calls = [c for c in fake.calls if len(c) > 1 and c[1].endswith("league.py")]
        self.assertEqual(len(cmp_calls), 1)
        self.assertIn("--compare-deploy", cmp_calls[0])

    def test_compare_without_a_deploy_says_so(self):
        fake = FakeDocker()
        d = DP.Deploy(root=self.root, apply=False, execute_fn=fake, out=self.lines.append)
        self.assertEqual(DP.main(["compare"], deploy=d), DP.FAILED)
        self.assertIn("no deploy recorded", self.text())


class TestStaticRules(unittest.TestCase):
    def test_batch_scripts_are_gone_and_guard_keeps_only_the_recreate_helpers(self):
        for name in ("dep3.sh", "depcore.sh", "depcore_nobuild.sh"):
            self.assertFalse(os.path.exists(os.path.join(REPO, "tools", "deploy", name)), name)
        with open(os.path.join(REPO, "tools", "deploy", "guard.sh"), encoding="utf-8") as f:
            g = f.read()
        for gone in ("snapshot_build", "guard_core", "deploy_up", "record_deploy", "parse_deploy_args"):
            self.assertNotIn(gone + "()", g)
        for kept in ("rathena_gate()", "svc_changed()", "svc_image_drift()", "svc_field()"):
            self.assertIn(kept, g)

    def test_deploy_script_never_names_a_rathena_recreate(self):
        with open(os.path.join(REPO, "tools", "deploy", "deploy.py"), encoding="utf-8") as f:
            src = f.read()
        self.assertNotIn("--force-recreate", src)  # a rathena recreate is never a flag of this script


if __name__ == "__main__":
    unittest.main()
