docker exec ro-rathena cat /rathena/db/re/item_db_etc.yml > /mnt/c/Users/kod/AppData/Local/Temp/claude/9b0368b215c2c0f3/40046184-90b0-47ad-be6b-88bc200a3437/scratchpad/itemdb/etc.yml
docker exec ro-rathena sh -c 'ls -la /rathena/db/import/item_db*.yml; grep -c "Id:" /rathena/db/import/item_db.yml' 2>&1
