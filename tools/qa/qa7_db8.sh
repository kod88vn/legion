docker exec ro-rathena sh -c 'grep -n "^1008,\|^1007,\|^2836," /rathena/db/re/mob_skill_db.txt | grep -i "METAMORPH\|SUMMON" | head'
