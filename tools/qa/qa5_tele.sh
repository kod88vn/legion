docker exec -i ro-db mariadb -uragnarok -pragnarok ragnarok -N -B -e "
select 'skill26 online', count(*) from skill s join \`char\` c using(char_id) where s.id=26 and c.online=1;
select 'creamy equipped online', count(*) from inventory i join \`char\` c using(char_id) where c.online=1 and i.equip>0 and 4040 in (i.card0,i.card1,i.card2,i.card3);
select 'creamy in bag online', count(*) from inventory i join \`char\` c using(char_id) where c.online=1 and i.nameid=4040;
select 'clip owned online', count(*) from inventory i join \`char\` c using(char_id) where c.online=1 and i.nameid=2607;"
