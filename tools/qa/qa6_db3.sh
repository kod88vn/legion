O=/mnt/c/Users/kod/AppData/Local/Temp/claude/9b0368b215c2c0f3/40046184-90b0-47ad-be6b-88bc200a3437/scratchpad/itemdb
docker exec -i ro-db mariadb -uragnarok -pragnarok ragnarok -N -B -e "
select c.char_id,c.name,c.class,c.base_level,c.zeny from \`char\` c where c.online=1 and c.base_level>=20 and c.name<>'Ayla';" > $O/chars.tsv
docker exec -i ro-db mariadb -uragnarok -pragnarok ragnarok -N -B -e "
select i.char_id,i.nameid,i.amount,i.equip,i.identify,i.refine,i.card0,i.card1,i.card2,i.card3 from inventory i join \`char\` c using(char_id) where c.online=1 and c.base_level>=20 and c.name<>'Ayla';" > $O/inv.tsv
wc -l $O/chars.tsv $O/inv.tsv
