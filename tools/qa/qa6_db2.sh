docker exec ro-rathena sh -c 'ls /rathena/db/re/ | grep item_db; ls /rathena/db/import 2>/dev/null | head'
docker exec ro-rathena cat /rathena/db/re/item_db_equip.yml > /mnt/c/Users/kod/AppData/Local/Temp/claude/9b0368b215c2c0f3/40046184-90b0-47ad-be6b-88bc200a3437/scratchpad/itemdb/equip.yml
wc -c /mnt/c/Users/kod/AppData/Local/Temp/claude/9b0368b215c2c0f3/40046184-90b0-47ad-be6b-88bc200a3437/scratchpad/itemdb/equip.yml
