#!/bin/bash
# Start login/char/map servers; initialise the DB on first run.
set -u
DB="mariadb -h db -u ragnarok -pragnarok --skip-ssl ragnarok"
echo "[start] waiting for database..."
until $DB -e "SELECT 1" >/dev/null 2>&1; do sleep 1; done

if ! $DB -e "SELECT 1 FROM login LIMIT 1" >/dev/null 2>&1; then
  echo "[start] first run: importing schema"
  $DB < sql-files/main.sql
  $DB < sql-files/logs.sql
  [ -f sql-files/web.sql ] && $DB < sql-files/web.sql
  echo "[start] seeding accounts/characters"
  $DB < /seed.sql
fi

# custom NPC scripts dropped into ./npc-custom are loaded via conf/import/npc.txt (optional)
cd /rathena
run() { # run <name> : start a server with a log prefix, stdin detached
  ./$1-server < /dev/null > >(sed -u "s/^/[$1] /") 2>&1 &
}
run login; LP=$!
sleep 2
run char;  CP=$!
sleep 2
run map;   MP=$!
trap 'kill $LP $CP $MP 2>/dev/null' TERM INT
wait -n $LP $CP $MP; RC=$?
for n in LP CP MP; do kill -0 ${!n} 2>/dev/null || echo "[start] $n (pid ${!n}) exited rc=$RC"; done
echo "[start] a server process exited; stopping container"
kill $LP $CP $MP 2>/dev/null
exit 1
