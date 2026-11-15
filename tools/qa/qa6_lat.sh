# latency logger: every 120 s, /api/bots and /api/overview time + uptime
while true; do
  b=$(curl -s -o /dev/null -w "%{time_total}" -m 30 http://127.0.0.1:8090/api/bots)
  o=$(curl -s -m 30 -w " %{time_total}" http://127.0.0.1:8090/api/overview | grep -o '"uptime_s": *[0-9]*\|[0-9.]*$' | tr '\n' ' ')
  echo "$(date +%H:%M:%S) bots=$b overview=$o" >> qa6_lat.txt
  sleep 120
done
