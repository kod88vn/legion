# Parties

One line per rule: ID, statement, date. Owner words in `archive.md`. Class rules: `docs/classes/`.

## Forming and goals
- PT01. Parties come from need. Every non-priest bot has a current goal from its build (gear piece, card, zeny) and a farm map;
  it looks for a party with the same goal in #global. Priests join parties of similar level and share loot and EXP. A forming
  party wants a priest or Monk first. Parties dissolve when the goal is met or goals diverge. Ayla's party follows this too;
  it is the bug-finding example, except that it never dissolves; its roster is fixed (AY11); and it may move to farm another member's gear when it is that member's turn. (2026-11-14)
- PT02. Parties are built from tank, ranged and priest roles; bait-and-carry when levels differ; EXP is always shared. (2026-11-13)
- PT25. The party is one mind for gear: every member gets the ideal gear for its build one step at a time; the party pools money
  and effort for the weakest member's next step and spends equal time on each member's turn; a member's turn lasts about 2 h (PT16). (2026-11-13)
- PT16. A party picks one hunting map and stays about 2 hours. It changes only for a real reason (repeated deaths, a map with
  nothing to kill, a goal met, a real-need town run after which it returns). (2026-11-14)
- AY07 / EC05 apply to every party: farm the best map for level and gear; save the respawn point at the Kafra nearest the ground.

## Moving and keeping together
- EC05. No bot or party member enters a hunting ground until its save point is the Kafra nearest that ground; the party waits
  at that Kafra until all (leader included) have saved, and the leader confirms in party chat. A death that respawns elsewhere
  is a bug. (2026-11-14)
- PT03. A party heading out or regrouping meets at the Kafra nearest the destination, saves there, then sets out together. (2026-11-13)
- PT04. Rule number one for every party (owner, since day one): stay close while grinding; a member far from the leader regroups first, fighting only what hits it. (2026-11-13)
- PT06. "Together" means within 20 cells of the leader; nobody holds, collects or regroups for a member inside 20. Applies to the engine, Ayla's plugin, the dashboard and the sentinel. (2026-11-14)
- PT05. The leader is decisive: collects stragglers one by one, never switches target halfway, never hovers in the middle (low-level parties; lv50+ see PT07; Ayla's priest see AY22). Bots
  bouncing between the same places are a bug. (2026-11-13)
- PT07. In a party whose weakest member is lv50+ and not on a map far above it, a separated member runs straight to the leader and
  the leader keeps farming. Low-level parties keep leader-collects. In every party tanks and melee go to a support member at
  once when it calls for help, is attacked or is low on HP, and take the monsters off it. (2026-11-14)
- PT08. The leader never turns back for a member that is not lost, in danger or asking for help. (2026-11-14)
- PT12. A separated member says so in party chat; the leader fetches it, on its own map or another map, one member at a time (low-level parties; lv50+ see PT07; Ayla's priest see AY22). (2026-11-13)
- PT15. When the area is empty, the leader sweeps the map in a steady planned loop toward monsters, not circling a corner or
  bouncing back and forth; the party moves together and rests as one. (2026-11-14)

## Fighting and resting
- PT17. Once the leader has the healer with it on the hunting map, the party starts farming; the others catch up. (2026-11-14)
- PT18. A leader or member waiting for stragglers kills monsters nearby instead of standing; sitting is only for real SP/HP
  rests. (2026-11-14)
- PT19. When the party rests, everyone stays put (sit, or fight what comes near to protect the resting). Nobody walks about unless
  stuck or someone needs help. Side-steps only to get unstuck. (2026-11-14)
- PT20. A member who needs rest says so in party chat ("need sp, resting") and says "ok, ready" when done; the party moves on when
  all resting members are ready. (2026-11-14)
- PT21. Fighters rarely rest (skills above an SP floor, auto-attack below). A non-healer leader rests only under 10% SP or 50% HP,
  briefly, says so, and the others keep farming within 20 cells of it. (2026-11-14)
- PT13. Tanks are active and every class uses AoE on crowds: `docs/classes/all.md` (CL01 to CL03). (2026-11-14)
- PT14. Priests heal first and sit only for low SP when nobody needs a heal or buff (CL17, PT29); Acolyte/Priest builds max INT: `docs/classes/acolyte.md` (CL09, CL10). (2026-11-14)

## Town runs, errands, loot
- PT22. On a party town run every member uses a Butterfly Wing, shops, meets at the Kafra, and the party returns together. Party
  loot is free for all: any member may pick up any drop (party pickup open to everyone; bots loot their party's kills, not only their own). (2026-11-14)
- PT24. Town runs are done together; nobody shops alone at random; only an emergency goes alone. (2026-11-13)
- PT23. A member may leave briefly for a necessary build/class step (job change, Peco/falcon rental, class weapon or arrows,
  quest skill): it says so, goes alone by Butterfly Wing/Kafra, and returns; nobody waits for it. This overrides PT24. (2026-11-14)

## Leaders and talk
- PT09. The leader leads: protects and guides the party, never runs ahead, talks to the party. (2026-11-13)
- PT10 (also PT30). Before every move the leader says in party chat where, why (whose gear/turn, what to farm) and how (route, regroup
  point); members act on that plan instead of trailing the leader. Applies to every leader, Ayla included. (2026-11-14)
- PT26. Members announce what they do (brb selling, back, need pots) and the leader answers. (2026-11-13)
- PT27. All bots talk, leaders most. (2026-11-13)

## Friendship
- PT28. A tracked friendship score grows with time together (party, fighting side by side, chat, trade, guild) and fades without
  contact. Friends prefer to party again and share a guild; a bot may leave its guild to follow close friends. It is its own
  system with its own data and dashboard view. (2026-11-14)
- PT29. Party members ask for buffs when they lack them, and the priest buffs them (Blessing, Increase Agility and the like) unless its SP is low; the priest is never passive. Applies to every party, Ayla's included. (2026-11-14)
- PT34. A character taken off a party's roster leaves the in-game party right away, Ayla's party and every other party alike. (2026-11-15)
- PT33. A town-run request from a party member is answered (leader or helper); a member at 50% weight is a critical town run and goes even if the leader does not respond, and catches up with the party later. (2026-11-15)
- PT32b. Party members catch up with the party leader first and engage monsters second: a member who is behind moves to the leader before it fights anything that is not already on it. (2026-11-15)
- PT31. (Part of PT04/PT06.) Party members stay with their party. Nobody escapes alone with a random Teleport while in a party; in danger a member retreats to the tank or priest, asks for help in party chat, and only when it would die uses a Butterfly Wing to the party's save Kafra and says so. The leader never leaves its party behind. Applies to every party, Ayla's included. A Butterfly Wing on a party town run (PT22) or a build errand (PT23) is not an escape. (2026-11-14)
- PT32. Fight in place, near the party: hit the first monster that comes into reach (the nearest one) instead of walking past it into the middle of a pack (only monsters far below her level are ignored); don't run around pulling more attackers; take monsters one or a few at a time where the party (priest) can cover, and stay back rather than charging into groups. Applies to Ayla and every bot. While regrouping or travelling, fight only what attacks or blocks the path (PT04); far-below-level monsters are ignored. (2026-11-14)
