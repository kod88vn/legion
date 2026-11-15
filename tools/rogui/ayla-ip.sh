#!/bin/bash
# ayla-ip.sh - prints the IP OpenKore's X-Kore 2 proxy listens on (= the rathena container IP; ro-openkore-ayla shares
# its network namespace via network_mode service:rathena, so inspecting ro-openkore-ayla itself returns nothing).
# The IP changes whenever the compose network is rebuilt (172.18.0.3 -> .4 after the 2026-11-14 folder move), so it is
# never hardcoded: 1) docker inspect ro-rathena  2) XKore_publicIp from Ayla's control/config.txt (written by her
# entrypoint at every start)  3) the old value. Read-only; usable as `. ayla-ip.sh; ayla_ip` or run directly.
ayla_ip() {
  local ip
  ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' ro-rathena 2>/dev/null | awk '{print $1}')
  [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || ip=$(awk '/^XKore_publicIp /{print $2}' /mnt/g/RO/legion/openkore-ayla/control/config.txt 2>/dev/null)
  [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || ip=172.18.0.3
  echo "$ip"
}
if [ "${BASH_SOURCE[0]}" = "$0" ]; then ayla_ip; fi
