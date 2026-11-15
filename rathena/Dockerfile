FROM ubuntu:24.04 AS build
ARG RATHENA_COMMIT=1ca3e7700939ea40120cb51a777f8681ba5e84ae
ARG PACKETVER=20211103
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      git ca-certificates make gcc g++ libmariadb-dev libmariadb-dev-compat zlib1g-dev libpcre3-dev \
    && rm -rf /var/lib/apt/lists/*
RUN git init /rathena && cd /rathena \
    && git remote add origin https://github.com/rathena/rathena.git \
    && git fetch --depth 1 origin ${RATHENA_COMMIT} && git checkout FETCH_HEAD
WORKDIR /rathena
# FORCE_MAIN=1: treat the client as main "Ragexe" (rAthena otherwise forces RagexeRE for 20200902..20211118).
# Needed for the official 2021-06-02 kRO Ragexe (from Gravity's Ragnarok_210607.zip full client).
ARG FORCE_MAIN=0
RUN if [ "$FORCE_MAIN" = "1" ]; then \
      sed -i 's/|| ( PACKETVER >= 20200902 \&\& PACKETVER <= 20211118 )//' src/config/packets.hpp \
      && grep -n 'PACKETVER > 20151104' src/config/packets.hpp; fi
# Smooth hold-to-walk: OFFICIAL_WALKPATH rejects walk requests longer than 14 cells whenever the straight line is
# blocked (trees, walls). The client has already started walking such a path, so the rejection shows up as the
# character snapping back. Disabled (see also max_walk_path in conf/import/battle_conf.txt).
RUN sed -i 's|^#define OFFICIAL_WALKPATH|// #define OFFICIAL_WALKPATH  (disabled: causes rubber-banding with hold-to-walk)|' src/config/core.hpp \
    && grep -n 'OFFICIAL_WALKPATH' src/config/core.hpp
RUN ./configure --enable-packetver=${PACKETVER} --enable-prere=no --enable-64bit \
    && make -j"$(nproc)" server

FROM ubuntu:24.04
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      libmariadb3 zlib1g libpcre3 mariadb-client ca-certificates tini \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /rathena /rathena
WORKDIR /rathena
COPY start.sh /start.sh
RUN chmod +x /start.sh && mkdir -p /rathena/npc/custom-local
EXPOSE 6900 6121 5121
ENTRYPOINT ["/usr/bin/tini", "--", "/start.sh"]
