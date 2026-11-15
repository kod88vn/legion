---
name: legion-ops
description: Operate and troubleshoot the AI-Lab Ragnarok stack (Docker in WSL) - start/stop, logs, re-seed bot profiles, rebuild Legion, check the game server. Use when legionctl can't reach the API or the world looks broken.
---

# Ops

All containers run in WSL (Ubuntu-24.04) Docker; compose project in this folder.
```
wsl -d Ubuntu-24.04 -- bash -c "cd /mnt/g/RO/legion && docker compose ps"
wsl -d Ubuntu-24.04 -- bash -c "cd /mnt/g/RO/legion && docker compose up -d"          # start everything
wsl -d Ubuntu-24.04 -- docker logs --tail 50 ro-legion                                # engine log (online/kills every 30s)
wsl -d Ubuntu-24.04 -- docker logs --tail 50 ro-rathena                               # game server
wsl -d Ubuntu-24.04 -- bash -c "cd /mnt/g/RO/legion && docker compose build legion && docker compose up -d legion"
```
- Legion settings: `.env` (LEGION_BOTS profiles, LEGION_MAX_ONLINE concurrent). Profiles log in on their own
  schedules, so "online" ramps up over a few minutes and follows the time of day.
- Re-seeding (wipes bot characters/progress and memory!) only if asked:
  stop legion, `docker compose run --rm legion seed -personas 4000 -guilds 10`, load `legion/data/seed.sql`
  into the `ragnarok` DB (`docker exec -i ro-db mariadb -uragnarok -pragnarok ragnarok < legion/data/seed.sql`),
  start legion.
- If WSL was restarted the stack comes back on its own (restart: unless-stopped); if not, `docker compose up -d`.
- Details: `README.md`, `legion/README.md`.
