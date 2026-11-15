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
# the whole npc tree: srvwarps.go reads the warps of exactly the files npc/re/scripts_main.conf loads (quest/city
# scripts included), like the map server; compose mounts scripts_custom.conf, functions_kafras.txt and custom-local
COPY --from=rdata /rathena/npc /rathena/npc
COPY --from=rdata /rathena/db/re/item_db_usable.yml /rathena/db/re/item_db_etc.yml /rathena/db/re/item_db_equip.yml /rathena/db/re/
ENTRYPOINT ["legion"]
CMD ["run", "-n", "100"]
