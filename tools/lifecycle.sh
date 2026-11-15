cd /g/RO/server
P=tools/legionctl.py
python $P wait ${1:-40} >/dev/null
timeout 30 python $P orders life 2>&1 | tail -3
timeout 30 python $P inbox --role life 2>&1 | tail -6
echo "stats $(curl -s -m 6 -o /dev/null -w '%{http_code}' 127.0.0.1:8090/api/stats)"
timeout 40 python $P events --role life 2>&1 | grep -E "\[(death|jobchange)\]|Card into|sold .* for [0-9]{4,}z|refined my|base level [6-9][0-9]\b|Eddga|became good friends|joined the guild" | tail -14
