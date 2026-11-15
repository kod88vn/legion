# Guild minds 06-10: restart state (run 17, cycle ~12/50, 16:40)

Status: /api/guild-leaders timing out on every call since about 15:30 (not yet verified after the 16:35-16:40 deploy).
Heartbeats (`legionctl think`) work and show all five guilds active on the minds bus.

Last known party state (run 16/17, unverified since 15:30):
- guild06 Kitty Cafe: Stormdragon (Blacksmith 67) on pay_fild10 with priest Silent Heals (68) and Knight xXFrostroseXx (80).
  Healer present. Named-map override earlier: moved to gef_fild03 by me at 14:37, engine put it on pay_fild10.
- guild07 Obsidian Edge: Sunheart (Monk ~70) leader. Priest gap. Tanks Yukix (83) and ghost.fang38 are OFFLINE (schedule).
  PENDING: re-form with Mintchan (Priest 65, online, gef_fild02, guild07). Taco z (Priest 61, gef_fild02), Ashcat (Priest 64,
  prt_fild09) and SilentdancerPrist (Priest 63, prt_fild09) are also online. Read the current member list first
  (`legionctl leader guild07`), since leader-party replaces the whole group.
- guild08 Dawnbreakers: suncat82 (Knight ~80) on prt_fild09/mjolnir_07 with priest xXAshleafXx (Priest ~71), Yuki blade12 and Rin.cat.
  Healer present when last checked.
- guild09 Thorned Rose: ryu hunter (Knight ~81). PENDING: check HolyleafPrist (Priest 65) is still in the party (last seen on
  mjolnir_07). No other priest within 15 levels (Zenclaw79 is 61).
- guild10 Pudding Patrol: Star.soul (Hunter 64) with priests Holy_sage (62) and FrostPriest (Acolyte 50), plus TheGhostshot.
  Healers present.

Owner rules to keep: parties re-form only after the leader is back and settled (about 10 min); read the reason before
re-forming when the engine places a party on a different map; named-map overrides reported to main with times.

Resume order: 1) `python tools/legionctl.py leader guild07` (until it answers, retry each cycle); 2) guild07 Mintchan re-form;
3) guild09 HolyleafPrist check; 4) heartbeats every 1-2 min; 5) normal duties.
