docker exec -i ro-db mariadb -uragnarok -pragnarok ragnarok -N -B -e "
select c.class, count(*), sum(c.online), sum(c.option&32>0), sum(c.online=1 and c.option&32>0), sum(s.id is not null), sum(s.id is not null and c.online=1), sum(s.id is not null and c.online=1 and c.zeny>=2700)
from \`char\` c left join skill s on s.char_id=c.char_id and s.id=63 where c.class in (7,14,4008,4015) and c.base_level>=40 group by c.class;"
