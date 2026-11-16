> **Reference only: 15-mind era; current state is in HANDOVER.md and STATUS.md.**

<!-- Moved unchanged from HANDOVER.md lines 1486-1554 (as of commit 63ca923). Index: HANDOVER.md. -->

## 6i. Friendship system (2026-11-14, `legion/friends.go`; RULES.md 3 "Friendship system")
- Owner: "need a friendship system. bots that spend time together grow in friendship and more likely to continue to
  party together or be in the same guild. Bots can change guild because of their friends. This is a separate system
  we need to track." Its own module and data: `data/friends.json` (main) / `data/friends_core.json` (core), saved
  every 10 min (atomic), rotated to .bak1..3 hourly, a save that would halve the pair count is refused. Each instance
  scores pairs its own bots are in and reads the other's file as peer pairs (max).
- Score per character pair 0-100, gains shrink near the top (pts x (1 - s/110)): partied on the same map +0.30/min,
  both killing in that party +0.10/min, heal from the other +0.30 (1/min), party chat or whisper +0.20 / guild chat
  +0.05 (1 per 2 min), trade window done +1.5 per side, zeny mail +4, same guild + same map not partied +0.05/min.
  Ayla (human) counts as a member of her party when her status shows her on the member's map. Decay: after 6 h
  without contact, 7-day half-life; < 1 point and 3 days silent = dropped. Memory relations written by the minds
  (`legionctl remember --person X --relation friend|close friend|rival|enemy`) add +10/+20/-15/-30 (each side half).
  Tiers: acquaintance 10, friend 35, close friend 65. Seeded once (no file, 5 min after start): party mates 18, Ayla's
  party 30, guild mates 4 (big guilds: 5 random mates each).
- Hooks (one line each): bot.go party chat 0x0109, guild chat 0x017f (new case), whisper 0x09de, guild leave/expel
  0x015a/0x015c (new: `onGuildLeft` clears GuildID/GuildName/guildRoster); heal.go healRecv; economy.go marketDone;
  mail.go endMail; guilds.go guildThink (`friendMoveWants` accepts the move invite; `friendRecruit`: a Legion guild
  leader invites its unguilded friends (score >= 35) before party members); main.go `friendsManager`; api.go
  `registerFriends`.
- Effects: login greeting (a bot online 20 s-5 min greets its best online friend by party chat / guild chat /
  whisper, p 0.55 friend / 0.85 close friend, once per 2 h; the friend answers 85%; no greetings in the first 3 min
  after a restart); party banter between friends (<= 4 lines/min server-wide); diary entries on reaching friend /
  close friend. **Guild moves (main instance only):** a bot (not master, not guild leader, not pinned, not Starveil /
  Ayla's party, not its guild's alchemist or trader) with 2+ close friends in another Legion guild whose master bot
  is online, their scores > 1.5x its own guild's friends + 40, moves with ~12% chance per qualifying day (per-minute
  roll), max 3 moves a day server-wide, once a week per bot: goodbye line in guild chat ("gonna join my buddies in
  X, gl all"), CZ_REQ_LEAVE_GUILD 0x0159, the target master bot sends 0x0168 (up to 3 tries), hello + a friend's
  welcome in the new guild chat. Stages leaving -> invite -> done/failed with reasons, kept in the file (last 100).
- For the goal parties (lfg.go, not edited): `FriendScore(a, b string) float64`, `bestFriendsOnline(b *Bot, n int)
  []*Bot`, `friendPartyBonus(c *Bot, ms []*Bot) int` (0..40, add in lfgScore), `friendsFirst(leader *Bot, cands
  []*Bot)` (stable reorder). All safe with any b.mu held (own mutex, never takes a bot lock).
- `GET /api/friends[?n=30][&bot=Name]`: pairs_total, tiers, thresholds, top pairs (score, raw, tier, partied_min,
  fight_min, heals, chats, trades, guild_min, last contact, guilds), guild_moves, guild_changes_due_to_friends,
  counters, move_policy, one character's friends. Dashboard card "Friendships" (after the Leaderboard).

## 6i2. Guild loyalty and mutual help (2026-11-14, `legion/loyalty.go`; RULES.md 3b "Guild loyalty and mutual help")
- Built on friends.go, saved in the same friends files (`loyalty`, `help`, `loyalty_counters`). Starveil members may now
  move like anyone; only Ayla's six pinned party friends stay fixed.
- Loyalty per character to its current guild, 0-100 (50 when first seen or after a guild change). Gains: zeny mail from
  a guild mate +6, trade with one +1, heal from one +0.15 (1/min), a minute partied with guild mates +0.05 (+0.05 when
  carried, 10+ levels under them), help answered +8, a leader's keep gift +12; gains shrink near 100. Losses: -3/day
  after 24 h without any care, -5 per unanswered request. Hooked through friendNote (deferred, after the friends lock;
  lock order friends -> loyalty, never the other way).
- Guild move rule (friends.go): the friends' pull must beat 1.5x own-guild friends + 40 + 0.8 x loyalty, the chance is
  x 2(1 - loyalty/100), loyalty >= 85 never moves.
- At risk = loyalty < 40 and friends elsewhere summing >= 60. A bot guild leader (main instance) mails 2-20k (max 1/8
  of its purse, keeps 30k; 1 per leader per 2 h, 1 per member per day) with a guild-chat line; Starveil's at-risk
  members carry an `ayla.sh guild-fund "NAME" AMOUNT "keep: next gear"` action in /api/guild-loyalty for the ayla
  mind (NOT yet merged into /api/human-guild actions: that is the guild agent's file).
- Help requests: a member asks in guild chat when it needs gear (<= 30k short of its gear_next), pots (below half its
  potion minimum, < 1.5k zeny), a party (solo 30+ min, main instance) or rescue (3 deaths in 15 min); 10%/min x
  (0.5 + sociability) while the need holds; 1 per member per 3 h, 1 open per guild, 6/h per instance. 20-120 s later a
  guild mate of the same instance answers (asker's friends first, then the richest): RODEX zeny (helpGive, 14:30 fix for a
  poor server - the first rule "keep 25k + 3x" answered 0 of 6 live asks at a ~1.1k median purse: keep 3k, give at most
  half the rest and 30% of the purse a day; full help first, else a partial gift >= 1/3 of the ask and 500z; once per
  30 min), formParty (unpartied, +-15 levels), or rescue (5+ levels higher, solo, same map or <= 3 hops: hunts on the asker's map for 10 min, then its old goal back; else pots money). 10 min
  without help = unanswered (asker sometimes says "nvm"). Thanks line from the asker.
- `GET /api/guild-loyalty`: guilds (members, avg loyalty, low, help asked/answered), least_loyal, at_risk, help_recent,
  counters; `/api/friends` has a `loyalty` summary; the dashboard Friendships card shows help/at-risk/keep counts.

## 6i3. Memory/friendship name filter (2026-11-14, `legion/memory.go` memoryNameOK)
- 357 memories listed "AIBot" (the switched-off seed test bot) as a person; also "Observer" (old watcher), NPC
  speakers from before bot.go split NPC talk ("Resting Adventurer", "Trailmark") and channel-prefixed "[Global] Ayla".
  memoryNameOK blocks those, any "[..."-prefixed name and any non-Legion name containing "bot". Applied in meet(), on
  every memory load (memoryClean drops the people + diary lines naming them, marks the file dirty) and by a startup
  sweep (memoryCleanSweep: each instance loads its own accounts' files that contain a blocked name, so the next flush
  writes them clean); friends.go ignores such pairs and drops them on load. Files on disk are cleaned by the engine
  after the next deploy, not by hand (a hand edit would be overwritten by the running engine's flush).

