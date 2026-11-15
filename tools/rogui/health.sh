#!/bin/bash
# health.sh - one-shot health report for the watchdog agent (run inside WSL).
cd /mnt/g/RO/legion
clean() { sed -e 's/\x1b\][0-9];[^\x07]*\x07//g' -e 's/\x1b\[[0-9;?]*[a-zA-Z]//g' | grep -v '^\s*$'; }
echo "== containers"; docker ps --format '{{.Names}}: {{.Status}}' | sort
echo "== Ayla (OpenKore) last lines"; docker logs --tail 6 ro-openkore-ayla 2>&1 | clean | tail -4
echo "== watch client"; pgrep -f RO-AILab.exe >/dev/null && echo "running" || echo "NOT running"; tail -3 /tmp/roai-restarts.log 2>/dev/null
echo "== keeper actions (last 5)"; tail -5 legion/data/watchdog.log 2>/dev/null
echo "== Ayla's party (Legion)"
curl -s -m 5 '127.0.0.1:8090/api/human-party?human=Ayla' | python3 -c '
import json,sys
try: d=json.load(sys.stdin)
except Exception as e: print("legion API error", e); sys.exit()
h=d.get("human_last_seen",{}); print("Ayla last seen:", h.get("map"), h.get("x"), h.get("y"), "ago", h.get("ago_s"), "s; checkpoint:", d.get("checkpoint"))
for m in d.get("members",[]):
    print("  %-12s lv%-3s %-9s online=%s party=%s waiting=%s state=%s map=%s hp=%s" % (m["name"], m["level"], m["class"], m["online"], m["in_party"], m.get("waiting_at_checkpoint"), m["state"][:40], m["map"], m["hp"]))'
echo "== world"
curl -s -m 20 127.0.0.1:8090/api/overview | python3 -c '
import json,sys
try: d=json.load(sys.stdin)
except Exception as e: print("overview error", e); sys.exit()
p=d["population"]; a=d["activity"]["m15"]["per_min"]
print("online", p["online"], "avg lv", round(p["avg_base_level"],1), "| per min (15m): kills", a["kills"], "deaths", a["deaths"], "levelups", a["levelups"])
st=sorted(p["states"].items(), key=lambda x:-x[1])[:8]; print("states", st)
print("deadliest", d.get("death_maps", [])[:3])'
echo "== errors last 10 min"
echo -n "rathena errors: "; docker logs --since 10m ro-rathena 2>&1 | grep -ciE "\[error\]|\[warning\]"
docker logs --since 10m ro-rathena 2>&1 | grep -iE "\[error\]" | sed 's/^.*\[Error\]//I' | sort | uniq -c | sort -rn | head -5
echo -n "legion panics/errors: "; docker logs --since 10m ro-legion 2>&1 | grep -ciE "panic|error|fatal"
docker logs --since 10m ro-legion 2>&1 | grep -iE "panic|fatal" | head -3
echo -n "openkore-ayla errors: "; docker logs --since 10m ro-openkore-ayla 2>&1 | clean | grep -ciE "error|You have died|You are dead"
docker logs --since 10m ro-openkore-ayla 2>&1 | clean | grep -iE "error|You have died|You are dead" | tail -3
