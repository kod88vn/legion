FROM debian:bookworm
ARG OPENKORE_COMMIT=edbd385b0b715b23849968c2b542de226477926c
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      git ca-certificates perl make g++ scons python3 libreadline-dev libncurses-dev zlib1g-dev \
      libcurl4-openssl-dev libtime-hires-perl libcompress-raw-zlib-perl libdigest-sha-perl \
      libhtml-parser-perl libjson-perl libtext-balanced-perl libnet-ssleay-perl procps tini \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/*
RUN git init /openkore && cd /openkore \
    && git remote add origin https://github.com/OpenKore/openkore.git \
    && git fetch --depth 1 origin ${OPENKORE_COMMIT} && git checkout FETCH_HEAD
WORKDIR /openkore
RUN scons -j"$(nproc)" && ls -la src/auto/XSTools/*.so
# upstream ships a few table files with stray non-UTF-8 bytes; OpenKore refuses to start on them
RUN find tables -name '*.txt' | while read f; do iconv -f utf-8 -t utf-8 "$f" >/dev/null 2>&1 || { iconv -f utf-8 -t utf-8 -c "$f" > /tmp/fix && mv /tmp/fix "$f" && echo "fixed $f"; }; done
# extra serverType profile(s): kRO_Ragexe_2021_06_02
COPY serverTypes/ /openkore/src/Network/
RUN mkdir -p tables/kRO/Ragexe_2021_06_02 \
    && sed 's/^0436 23$/0436 19/' tables/kRO/Ragexe_2021_11_03/recvpackets.txt > tables/kRO/Ragexe_2021_06_02/recvpackets.txt \
    && grep -n '^0436' tables/kRO/Ragexe_2021_06_02/recvpackets.txt
# local server definition
COPY servers-local.txt /tmp/servers-local.txt
RUN cat /tmp/servers-local.txt tables/servers.txt > /tmp/s && mv /tmp/s tables/servers.txt \
    && cp -r control /openkore/control.default
# Server-exact fields baked into the image (2026-11-15): ALL 1296 maps, every bit, generated from the server's map cache
# (rathena db/map_cache.dat with db/re/map_cache.dat over it: openkore/gen_fields_from_mapcache.py --all). The classic
# OpenKore fields this replaces (izlude, morocc, umbala, ...) are gone.
# The stale ro-openkore image of 2026-11-12 had the classic fields: rebuilt images are right without the overlay too.
# tools/system/fieldcheck.py checks every file here and every container's /openkore/fields against the server.
# Renewal portals patched (2026-11-14, Ayla stuck in Renewal Izlude).
COPY fields-re/ /tmp/fields-re/
COPY fix_re_portals.sh /tmp/fix_re_portals.sh
RUN cd /openkore && for f in /tmp/fields-re/*.fld2.gz; do b=$(basename "$f" .fld2.gz); cp "$f" fields/ && rm -f "fields/$b.dist" "fields/$b.weight" "fields/$b.fld2"; done \
    && sh /tmp/fix_re_portals.sh tables/portals.txt
# Kafra teleport lines matching this server's Kafras (2026-11-14 travel QA; keep it the LAST portals.txt patch:
# it owns every Kafra "c r2 c rN" line, fix_re_portals.sh and others must not write Kafra lines)
COPY fix_srv_portals.py portals-srv.txt /tmp/
RUN python3 -I /tmp/fix_srv_portals.py tables/portals.txt /tmp/portals-srv.txt
COPY fix_kafra_portals.py /tmp/fix_kafra_portals.py
RUN python3 /tmp/fix_kafra_portals.py tables/portals.txt
# packet layouts of the 20210107 client that stock OpenKore misparses (RefineUI 0AA2/0AA3, NPC market 09D5/09D6, RODEX
# read 0B63) - load with loadPlugins_list ...,pkt2021fix (2026-11-14, travel/services QA)
COPY plugins-extra/ /openkore/plugins/extra/
COPY entrypoint.sh /entrypoint.sh
RUN sed -i 's/\r$//' /entrypoint.sh && chmod +x /entrypoint.sh
ENTRYPOINT ["/usr/bin/tini", "--", "/entrypoint.sh"]
