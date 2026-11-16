"""Rule-based sentinel (tools/system/sentinel_algo.py): fake heartbeats, fake docker, fake spawn and fake bug filing.
Every test runs in its own temp dir; nothing touches ro-db, rathena, Ayla or the live legion/data files.
Run: python -m unittest discover -s tests/system -v   (from the repo root)"""
import json
import os
import shutil
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..', 'tools', 'system'))
import sentinel_algo as SA  # noqa: E402

T0 = 1_800_000_000.0
ROLES = {'guild01': 25 * 60, 'build': 24 * 3600, 'ayla': 10 * 60}


class Fakes:
    def __init__(self):
        self.containers = {'ro-legion': 'running|false|0', 'ro-legion-core': 'running|false|0',
                           'ro-openkore-ayla': 'running|false|0'}
        self.docker_calls = []
        self.spawned = []
        self.bugs = []
        self.api_ok = True
        self.party_online = True

    def docker(self, args):
        self.docker_calls.append(list(args))
        if args[0] == 'inspect':
            name = args[-1]
            if name not in self.containers:
                return 1, ''
            return 0, self.containers[name]
        if args[0] == 'start':
            name = args[1]
            self.containers[name] = 'running|false|0'
            return 0, name
        return 1, 'unexpected'

    def spawn(self, cmd, cwd):
        self.spawned.append((cmd, cwd))
        return 4242

    def bug(self, text, evidence):
        self.bugs.append((text, evidence))
        return True, 'ok'

    def api(self, path):
        if path == '/api/parties/detail':
            if not self.party_online:
                return [{'human': 'Ayla', 'online': False}]
            return [{'human': 'Ayla', 'online': True, 'members': []}]
        if not self.api_ok:
            raise OSError('connection refused')
        return {'runtime': {'uptime_s': 9999}}


class SentinelTest(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp(prefix='sentinel_algo_test_')
        self.f = Fakes()
        self.ayla_log = os.path.join(self.d, 'console_player_2.txt')
        open(self.ayla_log, 'w').write('x')
        self.env = self.make_env()
        self.fresh_all(T0)
        self.stack(True)
        self.write_pup()

    def tearDown(self):
        shutil.rmtree(self.d, ignore_errors=True)

    def make_env(self):
        return SA.Env(data=self.d, root=self.d, docker=self.f.docker, api=self.f.api, spawn=self.f.spawn,
                      bug=self.f.bug, role_intervals=ROLES, ayla_log=self.ayla_log)

    def hb(self, name, t):
        p = os.path.join(self.d, name)
        open(p, 'w').write(str(int(t)))
        os.utime(p, (t, t))

    def fresh_all(self, t):
        for n in ('dispatch.heartbeat', 'partycheck.heartbeat', 'aylalog.heartbeat', 'bugq-watch.heartbeat',
                  'unwall.heartbeat'):
            self.hb(n, t)
        os.utime(self.ayla_log, (t, t))

    def stack(self, up, hold=False):
        for n, on in (('stack.up', up), ('stack.hold', hold)):
            p = os.path.join(self.d, n)
            if on:
                open(p, 'w').write('x')
            elif os.path.exists(p):
                os.remove(p)

    def write_pup(self, roles=None, state='ok', pid=7):
        now = T0
        r = roles if roles is not None else {'guild01': {'last': now - 60}, 'build': {'last': now - 3600},
                                             'ayla': {'last': now - 60}}
        with open(os.path.join(self.d, 'puppet_heartbeat.json'), 'w') as f:
            json.dump({'pid': pid, 'state': state, 'roles': r, 't': now}, f)

    def cycle(self, t):
        return SA.run_cycle(self.env, now=t)

    def tick(self, t, stale=None):
        """A cycle at time t where every loop heartbeat is fresh except `stale` (a loop name -> its heartbeat file)."""
        self.fresh_all(t)
        if stale:
            self.hb(stale, t - 7200)
        return self.cycle(t)

    def rows(self):
        p = os.path.join(self.d, SA.REPORT)
        if not os.path.exists(p):
            return []
        return [json.loads(x) for x in open(p).read().splitlines() if x.strip()]

    def bad(self, row):
        return [c['name'] for c in row['checks'] if not c['ok']]

    def only_allowed_docker(self):
        for c in self.f.docker_calls:
            ok = c[0] == 'inspect' or (c[0] == 'start' and len(c) == 2 and c[1] in ('ro-legion', 'ro-legion-core'))
            self.assertTrue(ok, 'forbidden docker call %s' % c)


class TestHealthy(SentinelTest):
    def test_all_ok_no_actions_and_outputs_written(self):
        row = self.cycle(T0)
        self.assertEqual(self.bad(row), [])
        self.assertEqual(row['actions'], [])
        self.assertEqual(self.f.spawned, [])
        self.assertEqual(self.f.bugs, [])
        self.assertEqual(len(self.rows()), 1)
        status = open(os.path.join(self.d, SA.STATUS)).read()
        self.assertIn('OK', status)
        self.assertTrue(os.path.exists(os.path.join(self.d, SA.HEARTBEAT)))
        self.only_allowed_docker()

    def test_report_row_has_every_check(self):
        row = self.cycle(T0)
        names = {c['name'] for c in row['checks']}
        for n in ('container:ro-legion', 'container:ro-legion-core', 'container:ro-openkore-ayla', 'engine_api',
                  'loop:dispatcher', 'loop:partycheck', 'loop:aylalog', 'loop:bugq_watch', 'loop:unwall',
                  'puppet:roles', 'ayla_log'):
            self.assertIn(n, names)


class TestLoopRestarts(SentinelTest):
    def test_dead_dispatcher_restarted_with_its_command(self):
        self.hb('dispatch.heartbeat', T0 - 3600)
        row = self.cycle(T0)
        self.assertEqual(len(self.f.spawned), 1)
        cmd, cwd = self.f.spawned[0]
        self.assertEqual(cmd[1:], ['tools/puppet/dispatch.py'])
        self.assertEqual(cwd, self.d)
        self.assertEqual([a['type'] for a in row['actions']], ['restart'])
        self.assertTrue(row['actions'][0]['ok'])

    def test_stale_partycheck_restarted(self):
        self.hb('partycheck.heartbeat', T0 - 1200)
        self.cycle(T0)
        self.assertEqual(len(self.f.spawned), 1)

    def test_fresh_heartbeats_not_restarted(self):
        self.hb('partycheck.heartbeat', T0 - 599)
        self.cycle(T0)
        self.assertEqual(self.f.spawned, [])

    def test_no_restart_while_stack_not_up(self):
        self.stack(False)
        self.hb('aylalog.heartbeat', T0 - 3600)
        row = self.cycle(T0)
        self.assertEqual(self.f.spawned, [])
        self.assertEqual(self.f.bugs, [])
        self.assertIn('STANDBY', open(os.path.join(self.d, SA.STATUS)).read())
        self.assertFalse(row['stack_up'])

    def test_no_restart_while_hold_present(self):
        self.stack(True, hold=True)
        self.hb('bugq-watch.heartbeat', T0 - 3600)
        self.cycle(T0)
        self.assertEqual(self.f.spawned, [])

    def test_unwall_without_heartbeat_file_not_restarted(self):
        os.remove(os.path.join(self.d, 'unwall.heartbeat'))
        row = self.cycle(T0)
        self.assertEqual(self.f.spawned, [])
        self.assertTrue(any('loop:unwall' == k or 'unwall' in t for k, t in
                            [(p, p) for p in row['problems']]))

    def test_unwall_stale_heartbeat_is_restarted(self):
        self.hb('unwall.heartbeat', T0 - 900)
        self.cycle(T0)
        self.assertEqual(len(self.f.spawned), 1)
        self.assertEqual(self.f.spawned[0][0][1:], ['tools/unwall.py', '--loop'])

    def test_restart_limit_three_per_hour_then_bug(self):
        t = T0
        for i in range(3):
            self.tick(t, stale='aylalog.heartbeat')
            t += SA.LOOP_BACKOFF_S * (i + 1) + 1   # clear the backoff each time
        self.assertEqual([c[1:] for c, _ in self.f.spawned], [['tools/system/aylalog.py']] * 3)
        # fourth: still dead, limit reached within the hour -> bug queue, no spawn
        self.tick(t, stale='aylalog.heartbeat')
        self.assertEqual(len(self.f.spawned), 3)
        self.assertTrue(any('loop:aylalog' in b[0] and 'restart limit' in b[0] for b in self.f.bugs))

    def test_backoff_blocks_immediate_second_restart(self):
        self.hb('partycheck.heartbeat', T0 - 3600)
        self.cycle(T0)
        self.assertEqual(len(self.f.spawned), 1)
        self.cycle(T0 + 120)   # 2 min later: gap is 5 min x 1
        self.assertEqual(len(self.f.spawned), 1)
        self.cycle(T0 + 301)
        self.assertEqual(len(self.f.spawned), 2)

    def test_limit_resets_after_an_hour(self):
        t = T0
        for i in range(3):
            self.tick(t, stale='partycheck.heartbeat')
            t += SA.LOOP_BACKOFF_S * (i + 1) + 1
        self.tick(t, stale='partycheck.heartbeat')
        self.assertEqual(len(self.f.spawned), 3)
        later = T0 + 3600 + 1000   # the three restarts are now older than an hour
        self.tick(later, stale='partycheck.heartbeat')
        self.assertEqual(len(self.f.spawned), 4)

    def test_unknown_loop_cannot_be_spawned(self):
        with self.assertRaises(PermissionError):
            self.env.spawn('rathena')
        with self.assertRaises(PermissionError):
            self.env.spawn('ayla')
        self.assertEqual(self.f.spawned, [])


class TestContainers(SentinelTest):
    def test_stopped_legion_started_once_an_hour(self):
        self.f.containers['ro-legion'] = 'exited|false|1'
        row = self.cycle(T0)
        self.assertIn(['start', 'ro-legion'], self.f.docker_calls)
        self.assertEqual([a['type'] for a in row['actions']], ['docker_start'])
        self.f.containers['ro-legion'] = 'exited|false|2'
        self.cycle(T0 + 120)
        self.assertEqual(self.f.docker_calls.count(['start', 'ro-legion']), 1)
        self.f.containers['ro-legion'] = 'exited|false|3'
        self.cycle(T0 + 3601)
        self.assertEqual(self.f.docker_calls.count(['start', 'ro-legion']), 2)
        self.only_allowed_docker()

    def test_stopped_ayla_never_started_bug_filed(self):
        self.f.containers['ro-openkore-ayla'] = 'exited|false|1'
        self.cycle(T0)
        self.assertNotIn(['start', 'ro-openkore-ayla'], self.f.docker_calls)
        self.assertTrue(any('ro-openkore-ayla' in b[0] for b in self.f.bugs))
        self.only_allowed_docker()

    def test_restart_loop_is_a_bug_not_an_action(self):
        self.f.containers['ro-legion-core'] = 'restarting|true|1'
        self.cycle(T0)
        self.f.containers['ro-legion-core'] = 'running|false|5'
        self.cycle(T0 + 60)
        self.f.containers['ro-legion-core'] = 'running|false|9'
        row = self.cycle(T0 + 120)
        self.assertIn('container:ro-legion-core', self.bad(row))
        self.assertEqual(self.f.docker_calls.count(['start', 'ro-legion-core']), 0)
        self.assertTrue(any('ro-legion-core' in b[0] for b in self.f.bugs))

    def test_missing_container_is_a_bug(self):
        del self.f.containers['ro-legion']
        self.cycle(T0)
        self.assertNotIn(['start', 'ro-legion'], self.f.docker_calls[-1:])
        self.assertTrue(any('ro-legion' in b[0] for b in self.f.bugs))

    def test_docker_guard_refuses_rathena_and_recreate(self):
        with self.assertRaises(PermissionError):
            self.env.docker(['start', 'ro-rathena'])
        with self.assertRaises(PermissionError):
            self.env.docker(['start', 'ro-openkore-ayla'])
        with self.assertRaises(PermissionError):
            self.env.docker(['rm', '-f', 'ro-legion'])
        with self.assertRaises(PermissionError):
            self.env.docker(['restart', 'ro-legion'])
        self.assertEqual(self.f.docker_calls, [])


class TestApiAndPuppet(SentinelTest):
    def test_api_down_is_a_bug(self):
        self.f.api_ok = False
        row = self.cycle(T0)
        self.assertIn('engine_api', self.bad(row))
        self.assertTrue(any('engine_api' in b[0] for b in self.f.bugs))

    def test_stalled_guild_role_is_a_bug(self):
        self.write_pup({'guild01': {'last': T0 - 3 * 3600}, 'build': {'last': T0 - 3600},
                        'ayla': {'last': T0 - 60}})
        row = self.cycle(T0)
        self.assertIn('puppet:roles', self.bad(row))
        self.assertTrue(any('guild01' in b[0] for b in self.f.bugs))

    def test_daily_build_inside_its_day_not_stalled(self):
        self.write_pup({'guild01': {'last': T0 - 600}, 'build': {'last': T0 - 20 * 3600},
                        'ayla': {'last': T0 - 60}})
        row = self.cycle(T0)
        self.assertNotIn('puppet:roles', self.bad(row))

    def test_build_silent_past_25h_is_stalled(self):
        self.write_pup({'guild01': {'last': T0 - 600}, 'build': {'last': T0 - 26 * 3600},
                        'ayla': {'last': T0 - 60}})
        row = self.cycle(T0)
        self.assertIn('puppet:roles', self.bad(row))

    def test_roles_skipped_while_paused(self):
        self.write_pup({'guild01': {'last': T0 - 9 * 3600}}, state='paused')
        row = self.cycle(T0)
        self.assertNotIn('puppet:roles', self.bad(row))

    def test_role_never_run_judged_from_dispatcher_first_seen(self):
        # the sentinel has seen dispatcher pid 7 since 25.5 h ago (a report row from then), and build never ran
        self.write_pup({'build': {'last': None}})
        first = {'t': T0 - 25.5 * 3600, 'iso': '', 'stack_up': True, 'actions': [],
                 'checks': [{'name': 'loop:dispatcher', 'ok': True, 'detail': '', 'pid': 7}]}
        with open(os.path.join(self.d, SA.REPORT), 'a') as f:
            f.write(json.dumps(first) + '\n')
        row = self.cycle(T0)
        self.assertIn('puppet:roles', self.bad(row))
        self.assertIn('build', [c['detail'] for c in row['checks'] if c['name'] == 'puppet:roles'][0])


class TestAylaLog(SentinelTest):
    def test_silent_log_with_party_online_is_bug(self):
        os.utime(self.ayla_log, (T0 - 700, T0 - 700))
        row = self.cycle(T0)
        self.assertIn('ayla_log', self.bad(row))
        self.assertTrue(any('ayla_log' in b[0] for b in self.f.bugs))
        self.assertEqual([a for a in row['actions'] if a['type'] == 'restart'], [])

    def test_silent_log_party_offline_not_judged(self):
        self.f.party_online = False
        os.utime(self.ayla_log, (T0 - 3000, T0 - 3000))
        row = self.cycle(T0)
        self.assertNotIn('ayla_log', self.bad(row))


class TestBugDedupe(SentinelTest):
    def test_same_key_filed_once_per_30_min(self):
        self.f.api_ok = False
        self.cycle(T0)
        self.cycle(T0 + 60)
        self.assertEqual(len([b for b in self.f.bugs if 'engine_api' in b[0]]), 1)
        self.cycle(T0 + 1801)
        self.assertEqual(len([b for b in self.f.bugs if 'engine_api' in b[0]]), 2)

    def test_bug_text_carries_sentinel_key(self):
        self.f.api_ok = False
        self.cycle(T0)
        self.assertTrue(self.f.bugs[0][0].startswith('[sentinel:engine_api]'))


class TestReport(SentinelTest):
    def test_every_cycle_appends_a_row_with_actions(self):
        self.tick(T0, stale='partycheck.heartbeat')
        self.tick(T0 + 60)
        rows = self.rows()
        self.assertEqual(len(rows), 2)
        self.assertEqual([a['type'] for a in rows[0]['actions']], ['restart'])
        self.assertEqual(rows[1]['actions'], [])   # backoff: silent, no bug, no action

    def test_status_line_is_one_line(self):
        self.f.api_ok = False
        self.cycle(T0)
        text = open(os.path.join(self.d, SA.STATUS)).read()
        self.assertEqual(len(text.strip().splitlines()), 1)
        self.assertIn('PROBLEM', text)
        self.assertIn('engine_api', text)

    def test_no_llm_or_network_outside_fakes(self):
        # the cycle only calls the injected functions; with fakes nothing real is reached
        self.cycle(T0)
        self.assertTrue(all(c[0] in ('inspect', 'start') for c in self.f.docker_calls))


if __name__ == '__main__':
    unittest.main()
