# Watchdog status (restart notes), written 2026-11-14 16:38 local

1. Run 11 in progress, cycle 19/60. Run 10 finished (60 cycles) and was handed back to main.
2. Legion API (127.0.0.1:8090) DOWN since about 20:19Z (about 16:00 local). /api/overview times out or resets every probe. Only brief 200s during run 10 (cycles 34, 42).
3. Coordinator says the API should answer normally again after the deploy (about 16:55 local). First answer under 2 s is to be reported.
4. Minds (last check 16:34): ayla, chat, life, market, watcher active. guild01-10 active at last full check (run 10 cycle 57). econ and qa idle (about 6 min since last post at 16:34). build and deploy idle (silence expected while the API is wedged, per coordinator).
5. Ayla online: openkore-ayla/logs/status.json reports online true at every check.
6. Watch client (WSL /tmp/roai-restarts.log, old Wine): login failed 1/3 at 16:09:12 and 16:16:14, then in game as Ayla at 16:20:30. No later entry at 16:38.
7. Savecheck: last good read at run 10 cycle 20 (102 mismatched of 841 hunting). Not read since (API down).
8. Deaths: about 1.4/min, no spikes over 8/min seen in run 10.
9. Watchdog restart: run as a fresh agent with the same brief (check minds each cycle, heartbeat `think watchdog`, flag API DOWN each cycle over 5 min, report Ayla state).
10. Standing reminders: report API back when /api/overview answers under 2 s; keep SendMessage to main short; never send commands to bots or Ayla.
