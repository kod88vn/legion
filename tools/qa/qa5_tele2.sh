docker exec -i ro-db mariadb -uragnarok -pragnarok ragnarok -N -B -e "
select c.name,c.class,c.base_level,c.online,c.zeny,i.equip from inventory i join \`char\` c using(char_id) where i.nameid=4040 or 4040 in (i.card0,i.card1,i.card2,i.card3);
select 'clip anywhere', count(*), sum(c.online) from inventory i join \`char\` c using(char_id) where i.nameid=2607;
select 'acc slots w/ card', count(*) from inventory i join \`char\` c using(char_id) where c.online=1 and i.equip & 136 and i.card0>0;"
