# Legion bot engine. Game data (map cache, mob db, spawns, warps) is copied from the rAthena image built by the
# same compose project, so the bots always match the server.
FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod *.go *.html recvpackets.txt ./
RUN CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /legion .

FROM ro-rathena:20210107re AS rdata

FROM debian:bookworm-slim
COPY --from=build /legion /usr/local/bin/legion
COPY --from=rdata /rathena/db/map_cache.dat /rathena/db/map_cache.dat
COPY --from=rdata /rathena/db/re/map_cache.dat /rathena/db/re/map_cache.dat
COPY --from=rdata /rathena/db/re/mob_db.yml /rathena/db/re/mob_db.yml
COPY --from=rdata /rathena/db/re/mob_skill_db.txt /rathena/db/re/mob_skill_db.txt
COPY --from=rdata /rathena/npc/re/mobs /rathena/npc/re/mobs
COPY --from=rdata /rathena/npc/re/warps /rathena/npc/re/warps
COPY --from=rdata /rathena/npc/warps /rathena/npc/warps
COPY --from=rdata /rathena/npc/re/merchants /rathena/npc/re/merchants
COPY --from=rdata /rathena/npc/merchants /rathena/npc/merchants
COPY --from=rdata /rathena/db/re/item_db_usable.yml /rathena/db/re/item_db_etc.yml /rathena/db/re/item_db_equip.yml /rathena/db/re/
ENTRYPOINT ["legion"]
CMD ["run", "-n", "100"]
