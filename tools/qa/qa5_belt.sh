docker exec -i ro-db mariadb -uragnarok -pragnarok ragnarok -N -B -e "
select 'belt owned', count(*), sum(c.online) from inventory i join \`char\` c using(char_id) where i.nameid=2627;
select 'creamy compounded', count(*) from inventory i join \`char\` c using(char_id) where 4040 in (i.card0,i.card1,i.card2,i.card3);
select 'creamy loose', count(*) from inventory i join \`char\` c using(char_id) where i.nameid=4040;
select c.name,c.zeny from inventory i join \`char\` c using(char_id) where i.nameid=4040 and c.online=1;"
