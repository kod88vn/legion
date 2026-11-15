"""Wake limits: the bug-queue watcher wakes main for urgent items at most once per 15 min (outages always), and a
party split wakes only after 10 min apart without walking back, or 2 min dead.
Run: python -m unittest discover -s tests/system -v   (from the repo root)"""
import io
import os
import sys
import unittest
from contextlib import redirect_stdout

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from test_system import Env, T0  # noqa: E402


class TestUrgentGap(Env):
    def test_urgent_at_most_every_15_min(self):
        import bugq
        bugq.add("partycheck", "high", "[party apart] Knight apart", key="partycheck:apart:Knight", wake=True)
        r = bugq.wake_reasons()
        self.assertEqual([k for k, _, _ in r], ["urgent"])
        bugq.mark_woken(r)
        self.assertTrue(os.path.exists(os.path.join(self.d, "bugq.wake.state")))
        # a new urgent item one minute later is filed but held back
        self.clock(T0 + 60)
        i2, new = bugq.add("partycheck", "high", "[party apart] Priest apart", key="partycheck:apart:Priest", wake=True)
        self.assertTrue(new)
        self.assertEqual(bugq.wake_reasons(), [])
        self.clock(T0 + 14 * 60)
        self.assertEqual(bugq.wake_reasons(), [])
        # after 15 min it shows
        self.clock(T0 + 15 * 60 + 1)
        r = bugq.wake_reasons()
        self.assertEqual([k for k, _, _ in r], ["urgent"])
        self.assertEqual(r[0][2], i2)

    def test_outage_always_wakes(self):
        import bugq
        bugq.add("partycheck", "high", "[party idle] x", key="partycheck:idle:x", wake=True)
        bugq.mark_woken(bugq.wake_reasons())
        self.clock(T0 + 30)
        bugq.add("partycheck", "high", "[party idle] y", key="partycheck:idle:y", wake=True)
        bugq.add("sentinel", "critical", "[OUTAGE] api down")
        kinds = sorted(k for k, _, _ in bugq.wake_reasons())
        self.assertEqual(kinds, ["outage", "urgent"])      # the outage wakes and shows the held urgent item too
        self.assertTrue(bugq.is_outage({"priority": "high", "text": "[OUTAGE] ayla offline"}))

    def test_watch_cli_respects_gap(self):
        import bugq
        bugq.add("sentinel", "high", "[party] split", key="sentinel:party", wake=True)
        with redirect_stdout(io.StringIO()):
            self.assertEqual(bugq.main(["watch", "--once"]), 0)
        self.clock(T0 + 120)
        bugq.add("sentinel", "high", "[stuck] stuck", key="sentinel:stuck", wake=True)
        with redirect_stdout(io.StringIO()) as buf:
            self.assertEqual(bugq.main(["watch", "--once"]), 1)
        self.assertIn("nothing to wake for", buf.getvalue())


class TestApartWake(unittest.TestCase):
    L = {"name": "Ayla", "map": "pay_fild09", "x": 100, "y": 100, "human": True, "leader": True, "online": True}

    def m(self, **kw):
        d = {"name": "Knight", "map": "pay_fild09", "x": 150, "y": 100, "state": "fighting", "online": True}
        d.update(kw)
        return d

    def test_needs_10_min(self):
        import apartwake
        aw = apartwake.ApartWake()
        aw.update([self.L, self.m()], self.L, ["Knight"], T0)
        self.assertFalse(aw.wake("Knight"))
        aw.update([self.L, self.m()], self.L, ["Knight"], T0 + 9 * 60)
        self.assertFalse(aw.wake("Knight"))
        aw.update([self.L, self.m()], self.L, ["Knight"], T0 + 11 * 60)
        self.assertTrue(aw.wake("Knight"))
        # back together resets the timer
        aw.update([self.L, self.m()], self.L, [], T0 + 12 * 60)
        aw.update([self.L, self.m()], self.L, ["Knight"], T0 + 13 * 60)
        self.assertFalse(aw.wake("Knight"))

    def test_walking_back_does_not_wake(self):
        import apartwake
        aw = apartwake.ApartWake()
        far = self.m(map="payon", state="travel -> pay_fild09")
        aw.update([self.L, far], self.L, ["Knight"], T0)
        aw.update([self.L, far], self.L, ["Knight"], T0 + 20 * 60)
        self.assertFalse(aw.wake("Knight"))
        aw.update([self.L, self.m(map="payon", state="going to Ayla")], self.L, ["Knight"], T0 + 21 * 60)
        self.assertFalse(aw.wake("Knight"))
        # same map, distance shrinking = walking back
        aw.update([self.L, self.m(x=160)], self.L, ["Knight"], T0 + 22 * 60)
        aw.update([self.L, self.m(x=150)], self.L, ["Knight"], T0 + 23 * 60)
        self.assertFalse(aw.wake("Knight"))
        aw.update([self.L, self.m(x=150)], self.L, ["Knight"], T0 + 24 * 60)
        self.assertTrue(aw.wake("Knight"))
        # travelling somewhere else is not walking back
        aw.update([self.L, self.m(map="payon", state="travel -> moc_fild01")], self.L, ["Knight"], T0 + 25 * 60)
        self.assertTrue(aw.wake("Knight"))
        self.assertFalse(apartwake.heading_to("travel -> pay_fild091", self.L))

    def test_dead_wakes_after_2_min(self):
        import apartwake
        aw = apartwake.ApartWake()
        dead = self.m(state="dead")
        aw.update([self.L, dead], self.L, ["Knight"], T0)
        aw.update([self.L, dead], self.L, ["Knight"], T0 + 90)
        self.assertFalse(aw.wake("Knight"))
        aw.update([self.L, dead], self.L, ["Knight"], T0 + 125)
        self.assertTrue(aw.wake("Knight"))


class TestPartyCheckWake(Env):
    def party(self, **knight):
        k = {"name": "Knight", "map": "payon", "state": "travel -> pay_fild09", "with_leader": False, "online": True,
             "save_map": "payon"}
        k.update(knight)
        return {"members": [{"name": "Ayla", "map": "pay_fild09", "state": "fighting", "human": True, "leader": True,
                             "online": True}, k], "events": []}

    def test_apart_filed_but_wakes_only_when_due(self):
        import bugq
        import partycheck as P
        pc = P.PartyCheck()
        pc.evaluate(self.party(), T0)
        v = pc.evaluate(self.party(), T0 + 300)
        self.assertEqual([x[0] for x in v], ["apart"])
        P.file_violations(v, pc)
        live = bugq.rank(list(bugq.load().values()))
        self.assertEqual(len(live), 1)                      # still filed
        self.assertFalse(live[0].get("wake"))
        self.assertEqual(bugq.wake_reasons(), [])
        # 11 min apart and standing still on another map: wakes
        v = pc.evaluate(self.party(state="idle"), T0 + 660)
        P.file_violations([x for x in v if x[0] == "apart"], pc)
        self.assertEqual([k for k, _, _ in bugq.wake_reasons()], ["urgent"])

    def test_walking_back_never_wakes(self):
        import bugq
        import partycheck as P
        pc = P.PartyCheck()
        for t in (0, 300, 900, 1500):
            v = pc.evaluate(self.party(), T0 + t)
            P.file_violations(v, pc)
        self.assertEqual(bugq.wake_reasons(), [])
        self.assertFalse(P.wants_wake("apart", "Knight"))
        self.assertTrue(P.wants_wake("save", "Knight"))


class TestSentinelPartyWake(Env):
    def test_split_wakes_only_when_due(self):
        import apartwake
        import bugq
        import sentinel
        sentinel.DATA = self.d
        ap = {"members": [{"name": "Ayla", "map": "pay_fild09", "human": True, "online": True, "with_leader": True},
                          {"name": "Knight", "map": "payon", "state": "travel -> pay_fild09", "online": True,
                           "with_leader": False}]}
        aw = apartwake.ApartWake()
        self.assertFalse(sentinel.party_split_wake(aw, ap, T0))
        self.assertFalse(sentinel.party_split_wake(aw, ap, T0 + 900))
        sentinel.file_problem("party", "split: Knight(payon: travel -> pay_fild09)", wake=False)
        self.assertEqual(bugq.wake_reasons(), [])
        self.assertEqual(len(bugq.rank(list(bugq.load().values()))), 1)
        ap["members"][1]["state"] = "dead"
        self.assertFalse(sentinel.party_split_wake(aw, ap, T0 + 960))
        self.assertTrue(sentinel.party_split_wake(aw, ap, T0 + 1100))
        sentinel.file_problem("party", "split: Knight(payon: dead)", wake=True)
        self.assertEqual([k for k, _, _ in bugq.wake_reasons()], ["urgent"])


if __name__ == "__main__":
    unittest.main()
