"""Tests for tools/system/fieldcheck.py (map truth: OpenKore field files and portal lines against the server's map cache
and loaded warps) and its watchdog hook. Offline: synthetic caches, fields and npc warps in a temp dir; no docker.
Run: python -m unittest discover -s tests/system -v   (from the repo root)"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import gzip
import os
import shutil
import struct
import sys
import tempfile
import unittest
import zlib

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
sys.path.insert(0, os.path.join(REPO, "openkore"))

import fieldcheck  # noqa: E402

# server cell types: 0 walk, 1 wall, 5 shootable wall, 3 walkable water (rathena map_gat2cell)
TST1 = [0, 0, 1, 0,
        0, 5, 0, 0,
        3, 0, 0, 0]          # 4 x 3
TST2 = [0] * 12              # 4 x 3, all walkable
# the field values OpenKore stores for them (gen_fields_from_mapcache.GAT2FLD): 1 walk, 0 no-walk, 2 snipe, 5 water
FLD1 = [1, 1, 0, 1,
        1, 2, 1, 1,
        5, 1, 1, 1]
FLD2 = [1] * 12
T0 = 1_800_000_000.0


def cache_bytes(entries):
    """the rathena map_cache.dat layout that gen_fields_from_mapcache.load reads"""
    body = b""
    for name, xs, ys, cells in entries:
        z = zlib.compress(bytes(cells))
        body += name.encode().ljust(12, b"\0") + struct.pack("<hhi", xs, ys, len(z)) + z
    return struct.pack("<IH", 0, len(entries)) + b"\0\0" + body


def field_bytes(xs, ys, vals):
    return struct.pack("<HH", xs, ys) + bytes(vals)


def write(path, data):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "wb") as f:
        f.write(data)


def write_gz_field(fdir, name, xs, ys, vals):
    os.makedirs(fdir, exist_ok=True)
    with gzip.open(os.path.join(fdir, name + ".fld2.gz"), "wb") as f:
        f.write(field_bytes(xs, ys, vals))


class Synthetic(unittest.TestCase):
    """a server root with two 4x3 maps (tst1, tst2) and one warp tst1 1,1 -> tst2 2,2 loaded from npc/warps/t.txt"""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.root = os.path.join(self.tmp, "server")
        write(os.path.join(self.root, "db", "map_cache.dat"),
              cache_bytes([("tst1", 4, 3, TST1), ("tst2", 4, 3, TST2)]))
        write(os.path.join(self.root, "db", "re", "map_cache.dat"), cache_bytes([]))
        write(os.path.join(self.root, "npc", "scripts_custom.conf"), b"npc: npc/warps/t.txt\n")
        write(os.path.join(self.root, "npc", "warps", "t.txt"),
              b"tst1,1,1,4\twarp\tWarpA\t2,2,tst2,2,2\n")
        self.cache, self.warps, self.anynpc = fieldcheck.server_truth(self.root)
        self.fdir = os.path.join(self.tmp, "fields")
        write_gz_field(self.fdir, "tst1", 4, 3, FLD1)
        write_gz_field(self.fdir, "tst2", 4, 3, FLD2)

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def kinds(self, probs):
        return sorted((p["map"], p["kind"]) for p in probs)


class FieldCompare(Synthetic):
    def test_matching_fields_have_no_problem(self):
        probs, info = fieldcheck.check_fields("t", self.fdir, self.cache)
        self.assertEqual(probs, [])
        self.assertEqual(info["compared"], 2)

    def test_size_mismatch(self):
        write_gz_field(self.fdir, "tst1", 4, 2, FLD1[:8])   # 4 x 2 instead of 4 x 3
        probs, _ = fieldcheck.check_fields("t", self.fdir, self.cache)
        self.assertEqual(self.kinds(probs), [("tst1", "size")])
        self.assertIn("4x2", probs[0]["detail"])

    def test_cell_mismatch_on_the_walk_bit(self):
        vals = list(FLD1)
        vals[0] = 0                                         # walkable cell marked as a wall
        write_gz_field(self.fdir, "tst1", 4, 3, vals)
        probs, _ = fieldcheck.check_fields("t", self.fdir, self.cache)
        self.assertEqual(self.kinds(probs), [("tst1", "cells")])
        self.assertIn("1 of 12 cells differ, 1 on the walk bit", probs[0]["detail"])
        self.assertIn("x=0 y=0", probs[0]["detail"])

    def test_flag_only_mismatch_is_reported_apart(self):
        vals = list(FLD1)
        vals[0] = 5                                         # walk bit equal, water bit differs
        write_gz_field(self.fdir, "tst1", 4, 3, vals)
        probs, _ = fieldcheck.check_fields("t", self.fdir, self.cache)
        self.assertEqual(self.kinds(probs), [("tst1", "flags")])

    def test_higher_flag_bits_are_compared_too(self):
        vals = list(FLD1)
        vals[5] = 10                                        # shootable wall stored as 2 | 8: bit 8 is a difference now
        write_gz_field(self.fdir, "tst1", 4, 3, vals)
        probs, _ = fieldcheck.check_fields("t", self.fdir, self.cache)
        self.assertEqual(self.kinds(probs), [("tst1", "flags")])

    def test_uncompressed_shadow_and_unreadable_file(self):
        write(os.path.join(self.fdir, "tst2.fld2"), field_bytes(4, 3, FLD2))   # shadows tst2.fld2.gz
        write(os.path.join(self.fdir, "broken.fld2.gz"), b"not gzip")
        probs, _ = fieldcheck.check_fields("t", self.fdir, self.cache)
        self.assertEqual(sorted(p["kind"] for p in probs), ["shadow", "unreadable"])

    def test_map_without_server_entry_is_a_note_not_a_mismatch(self):
        write_gz_field(self.fdir, "nomap", 4, 3, FLD1)
        probs, info = fieldcheck.check_fields("t", self.fdir, self.cache)
        self.assertEqual(probs, [])
        self.assertTrue(any("nomap" in n for n in info["notes"]))


class Snapshot(unittest.TestCase):
    def test_refresh_rules(self):
        meta = {"t": 1000.0, "image": "sha256:a"}
        self.assertEqual(fieldcheck.snapshot_needed(None, "sha256:a", 1000.0)[0], True)
        self.assertEqual(fieldcheck.snapshot_needed(meta, "sha256:a", 1000.0 + 3600)[0], False)
        self.assertEqual(fieldcheck.snapshot_needed(meta, "sha256:a", 1000.0 + 86400)[0], True)
        self.assertEqual(fieldcheck.snapshot_needed(meta, "sha256:b", 1000.0 + 60)[0], True)
        self.assertEqual(fieldcheck.snapshot_needed(meta, None, 1000.0 + 60)[0], False)

    def test_no_docker_uses_the_last_snapshot_with_a_warning(self):
        tmp = tempfile.mkdtemp()
        try:
            snap = os.path.join(tmp, "mapsnap")
            write(os.path.join(snap, "npc", "x.txt"), b"")
            write(os.path.join(snap, "meta.json"), b'{"t": 0, "image": "sha256:a"}')
            root, label, warn = fieldcheck.ensure_snapshot(None, None, now=10.0 + 86400 * 2, snap=snap)
            self.assertEqual(root, snap)
            self.assertIn("using the snapshot", warn)
            root, _, warn = fieldcheck.ensure_snapshot(None, None, now=5.0, snap=snap)
            self.assertIsNone(warn)  # fresh: no docker is not a warning, the snapshot is used
            self.assertEqual(root, snap)
            root, _, warn = fieldcheck.ensure_snapshot(None, None, now=5.0, snap=os.path.join(tmp, "none"))
            self.assertIsNone(root)
            self.assertIn("no server data", warn)
        finally:
            shutil.rmtree(tmp, ignore_errors=True)


class PortalCompare(Synthetic):
    def test_correct_portal_has_no_problem(self):
        probs, info = fieldcheck.check_portals("c", ["tst1 1 1 tst2 2 2\n"], self.cache, self.warps, self.anynpc)
        self.assertEqual(probs, [])
        self.assertEqual(info["missing"], 0)

    def test_missing_portal(self):
        probs, info = fieldcheck.check_portals("c", [], self.cache, self.warps, self.anynpc)
        self.assertEqual(self.kinds(probs), [("tst1", "portal-missing")])
        self.assertIn("tst1 1 1 tst2 2 2", probs[0]["detail"])
        self.assertEqual(info["missing"], 1)

    def test_stale_portal_is_to_drop(self):
        lines = ["tst1 1 1 tst2 2 2\n", "tst2 0 0 tst1 0 0\n"]     # tst2 has no npc at 0,0: a removed warp
        probs, info = fieldcheck.check_portals("c", lines, self.cache, self.warps, self.anynpc)
        self.assertEqual(self.kinds(probs), [("tst2", "portal-drop")])
        self.assertEqual(info["drop"], 1)


class RepoPatch(Synthetic):
    def test_patch_block_and_srv_lines(self):
        ok = os.path.join(self.tmp, "ok")
        write(os.path.join(ok, "fix_re_portals.sh"), (
            b"set -e\nB=x\ncat > \"$B\" <<'EOT'\n# block\n"
            b"tst1 1 1 tst2 2 2\n"              # a server warp: fine
            b"tst2 0 0 tst1 0 0\n"              # no npc on tst2 at 0,0: the block would be dropped
            b"# end\nEOT\nrest\n"))
        write(os.path.join(ok, "portals-srv.txt"), (
            b"# patch\n"
            b"+ tst1 0 1 tst2 2 2\n"            # the generator's fix: source within 3 cells, the warp's landing
            b"+ tst1 1 1 tst2 0 0\n"            # landing is not the warp's: not a server warp
        ))
        probs, _ = fieldcheck.repo_portal_patch(ok, self.cache, self.warps, self.anynpc)
        self.assertEqual(sorted(p["detail"].split(":")[0] for p in probs),
                         ["+ line that is not a server warp", "fix_re_portals.sh line the server does not have"])


class RunAll(Synthetic):
    def test_run_with_a_field_dir_and_no_docker(self):
        bad = os.path.join(self.tmp, "bad")
        write_gz_field(bad, "tst1", 4, 3, [0] + FLD1[1:])
        probs, infos = fieldcheck.run_all(server_dir=self.root, repo=False, containers=[], field_dirs=[bad],
                                          no_docker=True)
        self.assertEqual(self.kinds(probs), [("tst1", "cells")])
        self.assertEqual(infos[0]["maps"], 2)
        self.assertEqual(infos[0]["warps"], 1)

    def test_clean_run_reports_ok(self):
        probs, _ = fieldcheck.run_all(server_dir=self.root, repo=False, containers=[], field_dirs=[self.fdir],
                                      no_docker=True)
        self.assertEqual(probs, [])

    def test_no_server_data_cannot_run(self):
        with self.assertRaises(OSError):
            fieldcheck.run_all(server_dir=os.path.join(self.tmp, "missing"), repo=False, containers=[],
                               no_docker=True)


class WatchdogHook(unittest.TestCase):
    """the hourly / container-start field pass files mismatches per source and closes them when they match again"""

    def setUp(self):
        self.d = tempfile.mkdtemp()
        os.environ["LEGION_DATA"] = self.d
        os.environ["LEGION_FAKE_NOW"] = str(T0)
        os.environ.pop("LEGION_BUGQ_EXTRA", None)

    def tearDown(self):
        shutil.rmtree(self.d, ignore_errors=True)
        for k in ("LEGION_DATA", "LEGION_FAKE_NOW"):
            os.environ.pop(k, None)

    class Ops:
        def start_agent(self, a):
            raise AssertionError("the field pass must never start anything")

        def process_running(self, a):
            return False

        def docker_start(self, name):
            raise AssertionError("the field pass must never restart a container")

    def fake(self, probs, calls):
        def run():
            calls.append(1)
            infos = [{"source": "server", "label": "t", "maps": 1, "warps": 0},
                     {"source": "ro-openkore-ayla", "files": 1, "compared": 1, "notes": []}]
            return probs, infos
        return run

    def run_at(self, t, containers, fields):
        import watchdog
        os.environ["LEGION_FAKE_NOW"] = str(t)
        return watchdog.run_pass(t, self.Ops(), containers=containers, fields=fields)

    def test_mismatch_is_filed_once_per_source_then_closed(self):
        import bugq
        probs = [{"source": "ro-openkore-ayla", "kind": "cells", "map": "umbala", "detail": "x"}]
        calls = []
        self.run_at(T0, {}, self.fake(probs, calls))
        items = [e for e in bugq.load().values() if e.get("key") == "watchdog:fieldcheck:ro-openkore-ayla"]
        self.assertEqual(len(items), 1)
        self.assertEqual(items[0]["priority"], "high")
        self.assertIn("umbala", items[0]["text"])
        self.run_at(T0 + 3700, {}, self.fake([], calls))            # an hour later and matching again
        self.assertEqual(len(calls), 2)
        open_items = [e for e in bugq.load().values() if e.get("key") == "watchdog:fieldcheck:ro-openkore-ayla"
                      and e.get("status", "open") == "open"]
        self.assertEqual(open_items, [])

    def test_hourly_not_more_often_and_container_start_runs_at_once(self):
        calls = []
        self.run_at(T0, {}, self.fake([], calls))
        self.run_at(T0 + 600, {}, self.fake([], calls))              # ten minutes later: not due
        self.assertEqual(len(calls), 1)
        started = {"ro-openkore-ayla": ("running", "Up 1 minute")}
        self.run_at(T0 + 900, started, self.fake([], calls))         # the container has just started
        self.assertEqual(len(calls), 2)
        self.run_at(T0 + 1000, started, self.fake([], calls))        # running already: not again
        self.assertEqual(len(calls), 2)

    def test_start_inside_the_debounce_stays_pending_then_runs(self):
        import watchdog
        calls = []
        self.run_at(T0, {}, self.fake([], calls))                     # the hourly pass
        started = {"ro-openkore-ayla": ("running", "Up 1 minute")}
        self.run_at(T0 + 600, started, self.fake([], calls))          # started 10 min later: inside the 15 min debounce
        self.assertEqual(len(calls), 1)
        self.assertIn("ro-openkore-ayla", self.last_state()["fc_pending"])
        self.run_at(T0 + 1000, started, self.fake([], calls))         # the debounce is over: the pending start runs
        self.assertEqual(len(calls), 2)
        self.assertEqual(self.last_state()["fc_pending"], [])
        self.assertEqual(watchdog.FIELD_DEBOUNCE, 900)

    def test_only_sources_in_use_are_filed(self):
        import bugq
        probs = [{"source": "ro-openkore-qa", "kind": "cells", "map": "umbala", "detail": "x"},
                 {"source": "ro-openkore-ayla", "kind": "cells", "map": "umbala", "detail": "y"}]

        def fake():
            return probs, [{"source": "server", "label": "t", "maps": 1, "warps": 0},
                           {"source": "ro-openkore-qa", "active": False, "state": "exited"},
                           {"source": "ro-openkore-ayla", "active": True, "state": "exited"}]
        self.run_at(T0, {}, fake)
        keys = {e.get("key") for e in bugq.load().values()}
        self.assertIn("watchdog:fieldcheck:ro-openkore-ayla", keys)
        self.assertNotIn("watchdog:fieldcheck:ro-openkore-qa", keys)

    def last_state(self):
        import json
        with open(os.path.join(self.d, "watchdog.state.json"), encoding="utf-8") as f:
            return json.load(f)

    def test_check_that_cannot_run_is_filed(self):
        def broken():
            raise OSError("no server data")
        self.run_at(T0, {}, broken)
        import bugq
        items = [e for e in bugq.load().values() if e.get("key") == "watchdog:fieldcheck:run"]
        self.assertEqual(len(items), 1)
        self.assertIn("could not run", items[0]["text"])

    def test_no_check_when_fields_is_none(self):
        import watchdog
        res = watchdog.run_pass(T0, self.Ops(), containers={})
        self.assertNotIn("fieldcheck", " ".join(f["key"] for f in res["filed"]))


class TownRouteTests(unittest.TestCase):
    """the town-route check (b1794721137499): the walk between two cells of a field, 8-way, no corner cut"""

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="townroute-")
        os.makedirs(os.path.join(self.tmp, "fields-re"))

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def write_field(self, name, w, h, rows):
        data = struct.pack("<HH", w, h) + bytes(int(c) for r in rows for c in r)
        with gzip.open(os.path.join(self.tmp, "fields-re", name + ".fld2.gz"), "wb") as f:
            f.write(data)

    def test_open_walk_is_found(self):
        fld = bytes([1] * 12)
        self.assertTrue(fieldcheck.field_route(fld, 4, 3, (0, 0), (3, 2)))

    def test_wall_cuts_the_walk(self):
        rows = [[1, 0, 1], [1, 0, 1], [1, 0, 1]]           # a wall down the middle: no way across
        fld = bytes(c for r in rows for c in r)
        self.assertFalse(fieldcheck.field_route(fld, 3, 3, (0, 0), (2, 0)))

    def test_no_corner_cut(self):
        rows = [[1, 0], [0, 1]]                             # only the diagonal is open: corners block it
        fld = bytes(c for r in rows for c in r)
        self.assertFalse(fieldcheck.field_route(fld, 2, 2, (0, 0), (1, 1)))

    def test_blocked_endpoint_is_no_route(self):
        fld = bytes([0, 1, 1, 1])
        self.assertFalse(fieldcheck.field_route(fld, 2, 2, (0, 0), (1, 1)))

    def test_repo_field_route_problem_is_filed(self):
        self.write_field("izlude", 4, 3, [[1, 0, 1, 1], [1, 0, 1, 1], [1, 0, 1, 1]])  # wall at x=1
        routes = [("izlude", (0, 0), (3, 0), "split by a wall"), ("izlude", (0, 0), (0, 2), "same side")]
        probs, info = fieldcheck.check_town_routes(self.tmp, routes)
        self.assertEqual([p["kind"] for p in probs], ["route"])
        self.assertIn("split by a wall", probs[0]["detail"])
        self.assertEqual(info["checked"], 2)

    def test_missing_field_file_is_a_problem(self):
        probs, _ = fieldcheck.check_town_routes(self.tmp, [("morocc", (0, 0), (1, 1), "x")])
        self.assertEqual(len(probs), 1)
        self.assertEqual(probs[0]["kind"], "route")

    def test_repo_town_routes_hold(self):
        """the real repo field files: the izlude start cell and the izlude and morocc town walks"""
        probs, info = fieldcheck.check_town_routes(os.path.join(REPO, "openkore"))
        self.assertEqual(probs, [], probs)
        self.assertEqual(info["checked"], len(fieldcheck.TOWN_ROUTES))


if __name__ == "__main__":
    unittest.main()
