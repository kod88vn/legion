package main

// Live tuning: behaviour numbers that used to be constants in the code, read from <data>/tuning.json and re-read
// every 30 s, so bot behaviour can be changed without restarting Legion (owner: "figure out a way to change bot
// behavior without restarting everything all the time"). A restart logs every bot out, the owner's party included.
//
// How it works
//   - tune() returns the current *Tuning: one atomic load, no lock, safe from every bot goroutine. A reload builds a
//     fresh Tuning and swaps the pointer; a Tuning is never modified after it is published.
//   - The file is decoded on top of the defaults, so it may hold only the fields you want to change; fields missing
//     from it keep their default. Unknown fields are ignored.
//   - A file that doesn't parse is reported in the log and ignored: the previous values stay in force.
//   - Every change is logged ("tuning: party_leash 8 -> 10").
//   - If the file doesn't exist it is written with the defaults (the values the code had before this file existed).
//   - Both Legion instances (main and core, see instances.go) read the same file.
//
// Fields (JSON name, default, meaning):
//
//   Pricing (pricemodel.go, pricedyn.go, EC48)
//   dynamic_price_on          false  true = asks follow scarcity (supply vs demand), the seller's urgency (zeny, bag
//                                    pressure), a time markdown (5% per 30 min after 45 min), and buyers' willingness
//                                    (need, budget) pulls a listing down to the best interested buyer; default: the fixed
//                                    formulas (vendPriceFixed, askPriceFixed, w.Each*11/10, priceOf*0.9)
//
//   Parties (party.go)
//   party_leash               8    members only start fights within this many cells of the leader, catch up beyond it
//   regroup_extra             4    a member farther than party_leash+regroup_extra cells walks back before anything else
//   tank_pull_radius          12   a party tank pulls monsters at most this far from the leader
//   tank_max_lv_over          5    ...and at most this many levels above its own base level
//   priest_buff_min_sp_pct    60   a party priest casts Blessing/Increase AGI only above this SP %
//   priest_sit_low_sp_pct     40   a standing party healer with nobody hurt sits under this SP % (then rests to 80%, 60% when the party moves)
//   priest_buff_renew_min_sec 220  a buff on a member is renewed after a random time between min and max seconds
//   priest_buff_renew_max_sec 260
//   priest_active_buffs       true PT29 (priestbuff.go): members ask for missing buffs, the priest buffs them on request and before expiry
//   priest_buff_hold_sp_pct   30   ...down to this SP % (kept for Heal); replaces the 70% floor while priest_active_buffs is on
//   party_cmd                 true partycmd.go: members obey the leader's commands in party chat (follow me, come to me at
//                                  <map> x,y, wait here, stay close, pull back, lets go to <map>, heal me / buffs pls)
//
//   Map choice by level AND gear (gearscore.go, AY07)
//   gear_score_on             true the effective level rates the whole kit (weapon+refine, all armor DEF/MDEF, cards, empty slots)
//   gear_perf_on              true measured kills / deaths step a solo bot or a party down (easier map) or up
//
//   Solo / general AI (ai.go)
//   beginner_lv               20   below this base level a bot stays near its home town and Kafra keeps more money
//   stay_crowd                70   a bot stays on a fitting map while it holds at most this many bots
//   crowd_spawns_per_bot      5    crowd.go: a map holds at most (non-boss spawns / this) bots, counting those heading there
//   crowd_roam_pct            30   crowd.go: a map where this % of its 12+ bots are roaming is full as well
//   roam_crowd_sec            75   a bot (or bot party leader) roaming this long on a full map moves to one with room
//   target_lv_over            2    highest monster level a bot attacks = base level + target_lv_over
//   target_lv_aggression      6      + persona aggression (0..1) * target_lv_aggression,
//   target_lv_min             8      but never less than target_lv_min
//   roam_give_up_sec          45   a solo bot that found nothing to fight for this long gives the map up
//   ghost_target_sec          10   a target neither reached nor hit for this long is dropped (ghost-target watchdog)
//   leader_collect_sec        90   a bot party leader collecting a straggler commits to it for at most this long
//   trip_urgent_weight_pct    90   bag weight % at which a party member may go to town alone (an emergency)
//   weight_regen_pct          50   rAthena stops natural HP/SP regen at this bag weight %: above it, sell the surplus
//   pot_weight_cap_pct        45   potion buys never push the bag above this weight % (room for loot, regen stays on)
//   pot_hp_acolyte            8    HP potions the acolyte line keeps (they heal themselves; zeny/weight go to SP pots)
//   pot_sp_target             15   SP potions an SP user (acolyte/mage line, skill users) tries to carry
//
//   Guild duty (guildduty.go)
//   guild_duty_on             false guild blocks, duty warnings, kicks, kicked bots' guild search (ledger always on)
//   market_directions_on      RETIRED (EC43 first version; design market-engine.md 4.3): parsed and ignored, always no effect. The
//                                  market sets no guild direction; by=market answers 410. Keep it false. Deleted in a later step
//   guild_duty_min_per_day    120  minutes a member owes its guild per played day (at most half its time online)
//   guild_duty_warn_days      3    short days before the leader's warning (/ the guild's strictness)
//   guild_duty_kick_days      2    short days after the warning before the kick (/ strictness)
//   guild_kicks_per_hour      2    server-wide expel cap (15 min between two, 2 h per guild)
//   guild_kick_cooldown_h     24   hours a kicked bot refuses every guild invitation
//   market_engine_on          false marketengine.go (WP2, EC43-EC47): computes needs, ranked gaps and farmer offers on each stock
//                                  round and serves GET /api/market/engine and /api/market/feed, data/market_feed.json.
//                                  Information only: no bot or guild is changed. Off: nothing is computed, endpoints say "off"
//   guild_market_feed_on      false guild leaders' autopilot and dutyMaps read the market feed as information (WP5, guildmarketfeed.go):
//                                  the wealth direction's item is the feed's best scored short item (24 h hold). Needs market_engine_on.
//                                  Off: the autopilot is byte-identical to before
//   farmer_roster_on          false the accounts in data/farmers.json are farmers (farmers.go, EC45; a missing file = none)
//   farmer_orders_on          false farmers take the market engine's offers (MoneyFarmGoal) and play hunter 70 / merchant 30
//                                  with a forced merchant session after a parcel (pickChar). Off: nothing changes for any bot
//   starveil_auto_lead        true Starveil is led with no AI (starveillead.go, AY19): duty warnings/kicks through Ayla's
//                                  plugin, guild-pool funding, keep gifts, member parties (main instance)
//
//   Kafra (kafra.go)
//   kafra_min_saving          3    teleport only if it saves at least this many map changes
//   kafra_keep_zeny_beginner  1000 zeny a beginner (below beginner_lv) keeps after paying for a teleport
//   kafra_keep_zeny           200  zeny everyone else keeps (for potions)
//
//   Gear (builds.go)
//   min_gear_level            10   no gear shopping trips below this base level
//   max_gear_hops             5    gear is only bought at a shop at most this many map changes away
//   gear_reserve_base         500  zeny kept back for potions when buying gear =
//   gear_reserve_per_lv       20     gear_reserve_base + base level * gear_reserve_per_lv,
//   gear_reserve_zeny_div     4      but at most zeny / gear_reserve_zeny_div
//   gear_trip_min_lv          15   from this base level a real upgrade sold only far away is worth one planned Kafra trip
//   gear_trip_cooldown_min    30   at most one planned far gear trip per bot / party in this many minutes
//   gear_pool_min_gift        200  smallest zeny gift a party mate hands over for the member whose gear turn it is
//   gear_turn_max_min         40   a member keeps the party's gear turn at most this long, then the next one gets it
//
//   Chatter (chatter.go): lines per minute = online bots / div, clamped to [min, max]
//   chat_global_div/min/max   300 / 1 / 3    #global and #trade
//   chat_local_div/min/max    35 / 6 / 28    public chat on maps with 3+ bots
//   chat_party_div/min/max    50 / 4 / 20    party chat
//   chat_guild_div/min/max    90 / 3 / 10    guild chat
//   (the core instance ignores the minimums, so its 3 bots don't flood their party: see chatRate)
//   chat_global_trade         0.45 share of #global picks that are a #trade line
//   chat_global_lfp           0.80 ...below this (and above trade) a looking-for-party line, if not in a party
//   chat_local_reply          0.6  chance someone answers a local line
//   chat_party_reply          0.5  chance a partymate answers a party line
//   chat_party_human          0.3  chance a party line in a human-led party is addressed to the human
//   chat_guild_leader         0.5  chance a guild leader's guild line is a leader line
//   chat_react_base           0.25 chance to announce a milestone = base + chattiness * chat_react_chattiness
//   chat_react_chattiness     0.5
//   chat_react_jobchange      0.25 ...then for a job change, this chance on top
//   chat_react_death          0.3  ...for a death, this chance on top
//   Ambient talk (convo.go): short 2-4 line conversations between two players near each other (town / party)
//   ambient_talk_on           true conversations on (system layer, CT02 "server feels alive")
//   ambient_talk_div          150  conversations per minute = online bots / div (at least 0.3)
//   ambient_talk_max_per_min  3    never more than this many started in any minute (and the rate's upper clamp)
//
//   Exchange and chat jobs (exchange.go, chatjobs.go, chatintent.go; docs/design/exchange.md)
//   exchange_tiers_on         false party free, guild mate / friend free for a spare else kin_discount_pct, ask order
//   kin_discount_pct          25   percent of the market price a guild mate / friend pays for a non-spare
//   exchange_gifts_per_hour   3    free / discounted hand-overs per giver per hour (then market price)
//   exchange_ask_stage_sec    180  a want is asked in party chat, then guild chat, then #trade, this long per stage
//   chat_jobs_on              false social lines wait for a chat agent's words (only while one polls /api/chat-jobs)
//   chat_job_wait_min_sec     45   how long a chat job waits for the agent (random between min and max)
//   chat_job_wait_max_sec     90
//   chat_job_listed_grace_sec 45   a job listed by the agent (GET /api/chat-jobs) gets at least this long from the listing
//                                  (capped 3 min after it was queued); 0 = off (chatjobs_wait.go)
//   chat_job_code_kinds       thanks,accept  kinds the code says at once without waiting for the agent (PR38)

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"time"
)

type Tuning struct {
	// parties
	PartyLeash   int `json:"party_leash"`
	RegroupExtra int `json:"regroup_extra"`
	// owner rule 2026-11-14: "the party is considered together within 20 steps" - nobody holds, collects or regroups
	// for a member within this many cells of the leader (member regroup, leader collect, rally radius, dashboard)
	TogetherCells int `json:"together_cells"`
	// hysteresis: a collect / regroup that started beyond together_cells keeps going until within this many cells
	// (stopping at 20 too made leaders oscillate at the boundary, QA 2026-11-14 pay_fild04)
	CollectStopCells  int  `json:"collect_stop_cells"`
	TankPullRadius    int  `json:"tank_pull_radius"`
	TankMaxLvOver     int  `json:"tank_max_lv_over"`
	PriestBuffMinSP   int  `json:"priest_buff_min_sp_pct"`
	PriestSitBelowSP  int  `json:"priest_sit_low_sp_pct"` // 2026-11-14: was priest_sit_below_sp_pct (95, "SP not full"); new key so the old file value is ignored
	PriestBuffRenewLo int  `json:"priest_buff_renew_min_sec"`
	PriestBuffRenewHi int  `json:"priest_buff_renew_max_sec"`
	PriestActiveBuffs bool `json:"priest_active_buffs"`     // PT29 priestbuff.go: members ask for buffs, the priest buffs down to the hold line
	PriestBuffHoldSP  int  `json:"priest_buff_hold_sp_pct"` // PT29: no buffs under this SP % (kept for Heal)
	PartyCmd          bool `json:"party_cmd"`               // partycmd.go: members obey the leader's party-chat commands (follow me, wait here, ...)
	// AI
	BeginnerLv          int     `json:"beginner_lv"`
	StayCrowd           int     `json:"stay_crowd"`
	TargetLvOver        int     `json:"target_lv_over"`
	TargetLvAggression  float64 `json:"target_lv_aggression"`
	TargetLvMin         int     `json:"target_lv_min"`
	RoamGiveUpSec       int     `json:"roam_give_up_sec"`
	CrowdSpawnsPerBot   int     `json:"crowd_spawns_per_bot"`
	CrowdRoamPct        int     `json:"crowd_roam_pct"`
	RoamCrowdSec        int     `json:"roam_crowd_sec"`
	GhostTargetSec      int     `json:"ghost_target_sec"`
	LeaderCollectSec    int     `json:"leader_collect_sec"`
	TripUrgentWeightPct int     `json:"trip_urgent_weight_pct"`
	TripBagPct          int     `json:"trip_bag_pct"`          // bag full (weight or slots) -> sell trip
	TripMinIntervalMin  int     `json:"trip_min_interval_min"` // min minutes between non-urgent town trips
	WeightRegenPct      int     `json:"weight_regen_pct"`
	PotWeightCapPct     int     `json:"pot_weight_cap_pct"`
	// rally.go kill switches (2026-11-14: bot-party rallies stalled guild leader parties legion-wide): bot parties
	// rally only with rally_enabled true; human-led parties (Ayla's) rally unless rally_human_disabled is true
	RallyEnabled       bool `json:"rally_enabled"`
	RallyHumanDisabled bool `json:"rally_human_disabled"`
	// partydest.go PT30: members head for the leader's destination by their own route (true = old trailing)
	PartyDestOff bool `json:"party_dest_off"`
	// healerstick.go: the party healer stays with the leader and keeps the leader buffed (true = old behaviour)
	HealerStickOff bool `json:"healer_stick_off"`
	// leadertalk.go PT10/PT26: bot party leaders say town runs, rally waits, late members, answer members (true = off)
	LeaderTalkOff bool `json:"leader_talk_off"`
	// kafrachain.go: the next hop of one Kafra trip (same goal) is not refused by the 3-in-10-min cap (true = plain cap)
	KafraChainOff bool `json:"kafra_chain_off"`
	// ferry.go: the Sailor boats (Izlude <-> Byalan Island) are route edges (true = no boats: iz_dun* unreachable)
	FerriesOff bool `json:"ferries_off"`
	// vendpitch.go: Vending refusals burn a pitch only for the cell, stale pitches reclaimed, stuck cart loads reset,
	// shop stock promised to walking buyers not offered twice (true = old behaviour)
	VendPitchFixOff bool `json:"vend_pitch_fix_off"`
	// staged rollout: with rally_enabled false, this percentage of bot parties (by party-name hash) rally
	RallyBotShare int `json:"rally_bot_share"`
	// rallypark.go: one owner of the rally/Kafra-wait sit, members leave a rally leg they can't travel, park cap.
	// ON by default (rally_park_off true switches it off); rally_park_cap_min 0 = 12 (Ayla's rally timeout)
	RallyParkOff    bool `json:"rally_park_off"`
	RallyParkCapMin int  `json:"rally_park_cap_min"`
	// farmshare.go: capped Kafra waits, no pointless waits, no-route loop guard, hourly Kafra cap lifted for long walks.
	// ON by default (farm_share_off true switches it off); 0 = defaults (60 s wait cap, 900 cells long walk)
	FarmShareOff       bool `json:"farm_share_off"`
	KafraWaitCapSec    int  `json:"kafra_wait_cap_sec"`
	KafraLongWalkCells int  `json:"kafra_long_walk_cells"`
	// farmshare.go kafraMemberWait: a party member past kafra_wait_cap_sec at the Kafra buys or flies a Butterfly Wing
	// (once per goal), walks a safe route, or asks the party to collect it and keeps waiting. After
	// kafra_member_wait_cap_sec (0 = 180) a stuck leader of a bot-led party moves the party once; a human-led party is
	// never moved. ON by default (kafra_member_wait_off true switches it off: the old open-ended member wait).
	KafraMemberWaitOff    bool `json:"kafra_member_wait_off"`
	KafraMemberWaitCapSec int  `json:"kafra_member_wait_cap_sec"`
	PotHPAcolyte    int  `json:"pot_hp_acolyte"`
	PotSPTarget     int  `json:"pot_sp_target"`
	// parcel.go (economy batch 17): surplus gear mailed to vendors. Off by default; parcel_test_bots (comma-separated
	// character names) turns it on for those bots only, for a live test before enabling it for everyone.
	ParcelsEnabled bool `json:"parcels_enabled"`
	// droprate.go (economy run 8): drop_rates_from_conf reads rathena's item_rate_* (flip when rathena restarts with
	// them); drop_rate_mul > 0 overrides every multiplier
	DropRatesFromConf bool `json:"drop_rates_from_conf"`
	// skillaudit.go (run 6): true = don't hold skills until the predicted can-act time (for a before/after measurement)
	SkillCanactOff bool `json:"skill_canact_off"`
	// casteractive.go (CL16): true = Mage-line casters back to the old single-target/no-help behaviour; bolt floor
	// and help threshold in % of max SP
	CasterActiveOff    bool `json:"caster_active_off"`
	CasterBoltFloorPct int  `json:"caster_bolt_floor_pct"`
	CasterHelpSPPct    int  `json:"caster_help_sp_pct"`
	// reset.go (run 7): true = no new natural build resets start (a running one still finishes / fetches its items)
	ResetsOff      bool    `json:"resets_off"`
	DropRateMul    float64 `json:"drop_rate_mul"`
	ParcelTestBots string  `json:"parcel_test_bots"`
	// dietrich.go (economy batch 19): refine visitors buy Phracon / Emveretarcon from Dietrich when no player shop
	// sells it at or below the NPC price. Off by default until tested live (dietrich_test_bots: comma-separated names).
	DietrichEnabled  bool   `json:"dietrich_enabled"`
	DietrichTestBots string `json:"dietrich_test_bots"`
	// pricing.go (economy batch 21): standardPrice uses the wealth x rarity table (off by default; the table is always
	// computed and shown at /api/economy/pricing)
	DynamicPrices bool `json:"dynamic_prices"`
	// pricedyn.go (EC48, economy batch 22): true = listing prices, undercut, markdown, seller asks and buy offers follow
	// the supply/demand, urgency and budget model in pricemodel.go. Off by default: the fixed formulas run as before
	DynamicPriceOn bool `json:"dynamic_price_on"`
	// mailtrade.go (economy run 7): far-apart sales of cards/gear worth 1,000z+ by RODEX (item mail + payment mail).
	// Off by default; mail_trade_test_bots (comma-separated names, seller or buyer) for a live test first.
	MailTrades        bool   `json:"mail_trades"`
	MailTradeTestBots string `json:"mail_trade_test_bots"`
	// guildduty.go (redesign G): guild_duty_on switches on guild blocks, warnings, kicks and the kicked bots' search
	// (default off; the ledger, rank, wealth and relations run regardless)
	GuildDutyOn        bool `json:"guild_duty_on"`
	// guildduty.go (EC43): market_directions_on is RETIRED (WP6, design 4.3). Parsed so old tuning files still load; no effect.
	MarketDirectionsOn bool `json:"market_directions_on"`
	// marketengine.go (EC43-EC47, WP2): market_engine_on computes the market needs and feed (off = computes nothing);
	// guild_market_feed_on lets guild leaders read the feed as information (WP5, guildmarketfeed.go)
	MarketEngineOn   bool `json:"market_engine_on"`
	GuildMarketFeedOn bool `json:"guild_market_feed_on"`
	// farmers.go (EC45, WP3): farmer_roster_on makes the accounts in data/farmers.json farmers (missing file = none);
	// farmer_orders_on lets farmers take the market engine's offers (their own checks decide) and play the 70/30
	// hunter/merchant split with a forced merchant session after a parcel. Both default off: nothing changes.
	FarmerRosterOn bool `json:"farmer_roster_on"`
	FarmerOrdersOn bool `json:"farmer_orders_on"`
	// engineparty.go (AY24, ayla-switch): comma-separated human names whose party runs bot-led on the engine, the human's
	// own bot as Leader through formPartyOpt (default empty: every human-led party stays as it is)
	EngineParties string `json:"engine_parties"`
	GuildDutyMinPerDay int  `json:"guild_duty_min_per_day"` // minutes a member owes per played day (never more than half its time online)
	GuildDutyWarnDays  int  `json:"guild_duty_warn_days"`   // short days before the warning (divided by the guild's strictness)
	GuildDutyKickDays  int  `json:"guild_duty_kick_days"`   // short days after the warning before the kick (same)
	GuildKicksPerHour  int  `json:"guild_kicks_per_hour"`   // server-wide expel cap (plus 15 min between two, 2 h per guild)
	GuildKickCooldownH int  `json:"guild_kick_cooldown_h"`  // hours a kicked bot refuses every guild invitation
	// starveillead.go (owner rule AY19): Starveil runs like a bot guild with no AI around (default on, owner order)
	StarveilAutoLead bool `json:"starveil_auto_lead"`
	// Kafra
	KafraMinSaving    int `json:"kafra_min_saving"`
	KafraKeepBeginner int `json:"kafra_keep_zeny_beginner"`
	KafraKeep         int `json:"kafra_keep_zeny"`
	// never a Kafra teleport straight back to the town the last one left from within this many minutes, and at most
	// KafraMaxPerHour teleports an hour, unless there is a real need (kafra.go kafraNeed) or walking is unsafe
	KafraCooldownMin int `json:"kafra_cooldown_min"`
	KafraMaxPerHour  int `json:"kafra_max_per_hour"`
	// take the Kafra when it saves at least this many seconds of walking (kafra.go walkCells), whatever the map changes
	KafraMinSaveSec int `json:"kafra_min_save_sec"`
	// gear
	MinGearLevel        int `json:"min_gear_level"`
	MaxGearHops         int `json:"max_gear_hops"`
	GearReserveBase     int `json:"gear_reserve_base"`
	GearReservePerLv    int `json:"gear_reserve_per_lv"`
	GearReserveZenyDiv  int `json:"gear_reserve_zeny_div"`
	GearTripMinLv       int `json:"gear_trip_min_lv"`
	GearTripCooldownMin int `json:"gear_trip_cooldown_min"`
	GearPoolMinGift     int `json:"gear_pool_min_gift"`
	GearTurnMaxMin      int `json:"gear_turn_max_min"`
	// chatter
	ChatGlobalDiv       float64 `json:"chat_global_div"`
	ChatGlobalMin       float64 `json:"chat_global_min"`
	ChatGlobalMax       float64 `json:"chat_global_max"`
	ChatLocalDiv        float64 `json:"chat_local_div"`
	ChatLocalMin        float64 `json:"chat_local_min"`
	ChatLocalMax        float64 `json:"chat_local_max"`
	ChatPartyDiv        float64 `json:"chat_party_div"`
	ChatPartyMin        float64 `json:"chat_party_min"`
	ChatPartyMax        float64 `json:"chat_party_max"`
	ChatGuildDiv        float64 `json:"chat_guild_div"`
	ChatGuildMin        float64 `json:"chat_guild_min"`
	ChatGuildMax        float64 `json:"chat_guild_max"`
	ChatGlobalTrade     float64 `json:"chat_global_trade"`
	ChatGlobalLFP       float64 `json:"chat_global_lfp"`
	ChatLocalReply      float64 `json:"chat_local_reply"`
	ChatPartyReply      float64 `json:"chat_party_reply"`
	ChatPartyHuman      float64 `json:"chat_party_human"`
	ChatGuildLeader     float64 `json:"chat_guild_leader"`
	ChatReactBase       float64 `json:"chat_react_base"`
	ChatReactChattiness float64 `json:"chat_react_chattiness"`
	ChatReactJobChange  float64 `json:"chat_react_jobchange"`
	ChatReactDeath      float64 `json:"chat_react_death"`
	// lfg.go need-driven parties: goal_parties_on true switches them on (default off until measured; off = the old
	// permanent grouping: partyManager same-map role parties, guild-leader parties without goals);
	// lfg_global_per_min caps the LFG/LFM lines on #global
	GoalPartiesOn   bool `json:"goal_parties_on"`
	LfgGlobalPerMin int  `json:"lfg_global_per_min"`
	// exchange.go / chatjobs.go (redesign package X): price by relationship and chat jobs, both default off
	ExchangeTiersOn      bool `json:"exchange_tiers_on"`
	KinDiscountPct       int  `json:"kin_discount_pct"`
	ExchangeGiftsPerHour int  `json:"exchange_gifts_per_hour"`
	ExchangeAskStageSec  int  `json:"exchange_ask_stage_sec"`
	ChatJobsOn           bool `json:"chat_jobs_on"`
	ChatJobWaitMinSec    int  `json:"chat_job_wait_min_sec"`
	ChatJobWaitMaxSec    int  `json:"chat_job_wait_max_sec"`
	// shopannounce.go (EC35): vendors announce their shop's rare items on #trade (default on, owner's rule);
	// shop_announce_gap_sec = server-wide seconds between two shop lines
	ShopAnnounceOn     bool `json:"shop_announce_on"`
	ShopAnnounceGapSec int  `json:"shop_announce_gap_sec"`
	// gearscore.go (AY07, 2026-11-14): the effective level rates the whole kit (gear_score_on) and measured kills /
	// deaths step a solo bot or a party down / up (gear_perf_on); both default ON
	GearScoreOn bool `json:"gear_score_on"`
	GearPerfOn  bool `json:"gear_perf_on"`
	// buildmilestones.go (EC39, 2026-11-15): a bot whose worn gear misses its band's milestone is capped at the level
	// its gear allows; default ON
	GearGateOn bool `json:"gear_gate_on"`
	// fairgate.go (bugq b1794751012196-928): a solo hunting spot needs fair_spot_share of its monsters at or above base
	// level minus meter_fair_lv_gap (0 or less = 0.5). fair_spot_gate_on default ON.
	FairSpotGateOn bool    `json:"fair_spot_gate_on"`
	FairSpotShare  float64 `json:"fair_spot_share"`
	// lfgstats.go / lfg.go (bug b1794773272q31): count the map check of every fitting join pair, not only the pairs that
	// would beat the best candidate (costs one partyMapOK per fitting pair; on by default for the first live run: the
	// per-tick cost is in /api/lfg tick_ms_*, so it can be switched off if it is too slow)
	LfgCountMaps bool `json:"lfg_count_maps"`
	// partystick.go (PT04/PT06/PT31, 2026-11-14): no move / target / loot takes a grinding party member beyond
	// together_cells of its party (party_stick_on), and no solo random Fly Wing / Teleport escape while party mates are
	// on the map (party_stick_escape_on): run to the party, call for help, Butterfly Wing only when about to die. Both ON.
	PartyStickOn       bool `json:"party_stick_on"`
	PartyStickEscapeOn bool `json:"party_stick_escape_on"`
	// pt32.go (PT32/PR16, 2026-11-14): a party member fights in place - a new target within 8 cells of it or a party
	// mate, no other aggressive monster on the way, none while 2+ monsters are on it. ON (owner rule).
	PartyFightInPlaceOn bool `json:"party_fight_in_place_on"`
	// chatjobs_wait.go: a listed job's grace, and the kinds the code says by itself (comma list)
	ChatJobListedGraceSec int    `json:"chat_job_listed_grace_sec"`
	ChatJobCodeKinds      string `json:"chat_job_code_kinds"`
	// chatjobs.go: an ask for an item already waiting in the same channel is not queued twice (default on)
	ChatJobDedupeOn bool `json:"chat_job_dedupe_on"`
	// party / guild get first refusal on a want for this long in all (split over the buyer's kin stages), then anyone
	ExchangeFirstRefusalSec int `json:"exchange_first_refusal_sec"`
	// convo.go ambient talk (default on: an older tuning.json without the keys keeps these defaults)
	TownDropJunk         bool    `json:"town_drop_junk"` // regendrop.go: over the regen line in town with nothing else to do, drop junk (EC37)
	AmbientTalkOn        bool    `json:"ambient_talk_on"`
	AmbientTalkDiv       float64 `json:"ambient_talk_div"`
	AmbientTalkMaxPerMin float64 `json:"ambient_talk_max_per_min"`
	// idleerrand.go (EC36): a bot waiting in a town sells / restocks (default on; idle_errand_off switches it off)
	IdleErrandOff         bool `json:"idle_errand_off"`
	IdleErrandWaitSec     int  `json:"idle_errand_wait_sec"`
	IdleErrandWeightPct   int  `json:"idle_errand_weight_pct"`
	IdleErrandSlots       int  `json:"idle_errand_slots"`
	IdleErrandCooldownMin int  `json:"idle_errand_cooldown_min"`
	// aylarotate.go (AY15, 2026-11-15): Ayla's party swaps one non-priest member every ayla_rotation_hours (0 = 3)
	// for an online bot of an untested class/build in the same role. ON by default (ayla_rotation_off true stops it).
	AylaRotationOff   bool    `json:"ayla_rotation_off"`
	AylaRotationHours float64 `json:"ayla_rotation_hours"`
	// lfg.go (PT35, 2026-11-15): the widest base-level spread (strongest - weakest) a bot party takes. Default 99 =
	// the server's party_share_level (rathena/conf/import/inter_conf.txt), so even exp share always works; the map
	// (survivable for the weakest, real exp for the strongest) is what keeps parties sensible. 15 = stock rAthena.
	PartyLvSpread int `json:"party_lv_spread"`
	// huntfit.go / town.go (2026-11-15, Constitution 3.2): the hunting-map fit of potions, arrows and cards. ON by default;
	// fit_buy_off true = the plain pre-fit behaviour (one potion tier with a Red fallback, plain Arrows, no card fit).
	// pot_budget_pct: the potion spend beyond the survival minimum, as a share of the bot's recent zeny per hour, with
	// pot_budget_floor zeny when there is no measured hour yet. pot_min_heal_pct: a potion must heal this share of max HP
	// to be a bulk buy. arrow_gain_per_z: percent of damage an elemental arrow must gain per extra zeny per arrow.
	FitBuyOff       bool `json:"fit_buy_off"`
	PotBudgetPct    int  `json:"pot_budget_pct"`
	PotBudgetFloor  int  `json:"pot_budget_floor"`
	PotMinHealPct   int  `json:"pot_min_heal_pct"`
	ArrowGainPerZ   int  `json:"arrow_gain_per_z"`
}

// defaultTuning: exactly the values the code used before tuning.json existed.
var defaultTuning = Tuning{
	GearScoreOn: true, GearPerfOn: true, GearGateOn: true, FairSpotGateOn: true, FairSpotShare: 0.5, TownDropJunk: true, LfgCountMaps: true,
	PartyLeash: 8, RegroupExtra: 4, TogetherCells: 20, CollectStopCells: 16, TankPullRadius: 12, TankMaxLvOver: 5,
	PriestBuffMinSP: 60, PriestSitBelowSP: 40, PriestBuffRenewLo: 220, PriestBuffRenewHi: 260,
	PriestActiveBuffs: true, PriestBuffHoldSP: 30, PartyCmd: true,
	CasterBoltFloorPct: 35, CasterHelpSPPct: 60,

	BeginnerLv: 20, StayCrowd: 70, TargetLvOver: 2, TargetLvAggression: 6, TargetLvMin: 8,
	RoamGiveUpSec: 120, CrowdSpawnsPerBot: 5, CrowdRoamPct: 30, RoamCrowdSec: 75, GhostTargetSec: 10, LeaderCollectSec: 90, TripUrgentWeightPct: 90, TripBagPct: 70, TripMinIntervalMin: 45,
	WeightRegenPct: 50, PotWeightCapPct: 45, PotHPAcolyte: 8, PotSPTarget: 15,

	GuildDutyMinPerDay: 120, GuildDutyWarnDays: 3, GuildDutyKickDays: 2, GuildKicksPerHour: 2, GuildKickCooldownH: 24, StarveilAutoLead: true,

	KafraMinSaving: 3, KafraKeepBeginner: 1000, KafraKeep: 200, KafraCooldownMin: 50, KafraMaxPerHour: 3, KafraMinSaveSec: 120,

	MinGearLevel: 10, MaxGearHops: 5, GearReserveBase: 500, GearReservePerLv: 20, GearReserveZenyDiv: 4,
	GearTripMinLv: 15, GearTripCooldownMin: 30, GearPoolMinGift: 200, GearTurnMaxMin: 40,

	ChatGlobalDiv: 300, ChatGlobalMin: 1, ChatGlobalMax: 3,
	ChatLocalDiv: 35, ChatLocalMin: 6, ChatLocalMax: 28,
	ChatPartyDiv: 50, ChatPartyMin: 4, ChatPartyMax: 20,
	ChatGuildDiv: 90, ChatGuildMin: 3, ChatGuildMax: 10,
	ChatGlobalTrade: 0.45, ChatGlobalLFP: 0.80, ChatLocalReply: 0.6, ChatPartyReply: 0.5, ChatPartyHuman: 0.3,
	ChatGuildLeader: 0.5, ChatReactBase: 0.25, ChatReactChattiness: 0.5, ChatReactJobChange: 0.25, ChatReactDeath: 0.3,
	LfgGlobalPerMin: 6,
	KinDiscountPct:  25, ExchangeGiftsPerHour: 3, ExchangeAskStageSec: 180, ExchangeFirstRefusalSec: 40, ChatJobWaitMinSec: 45, ChatJobWaitMaxSec: 90,
	ShopAnnounceOn: true, ShopAnnounceGapSec: 45,
	PartyStickOn: true, PartyStickEscapeOn: true, PartyFightInPlaceOn: true,
	ChatJobListedGraceSec: 45, ChatJobCodeKinds: "thanks,accept", ChatJobDedupeOn: true,
	AmbientTalkOn: true, AmbientTalkDiv: 150, AmbientTalkMaxPerMin: 3,
	IdleErrandWaitSec: 45, IdleErrandWeightPct: 35, IdleErrandSlots: 85, IdleErrandCooldownMin: 10,
	PartyLvSpread: 99,
	PotBudgetPct: 15, PotBudgetFloor: 1000, PotMinHealPct: 2, ArrowGainPerZ: 4,
}

var curTuning atomic.Value // *Tuning

// tune: the tuning in force right now (cheap: one atomic load). Never modify the result.
func tune() *Tuning {
	if t, ok := curTuning.Load().(*Tuning); ok {
		return t
	}
	return &defaultTuning
}

// accessors for values that used to be named constants (kept as names so call sites stay readable)
func partyLeash() int    { return tune().PartyLeash }
func togetherCells() int { return tune().TogetherCells }
func collectStopCells() int {
	return min(tune().CollectStopCells, tune().TogetherCells)
}
func beginnerLv() int     { return tune().BeginnerLv }
func stayCrowd() int      { return tune().StayCrowd }
func kafraMinSaving() int { return tune().KafraMinSaving }
func minGearLevel() int   { return tune().MinGearLevel }
func maxGearHops() int    { return tune().MaxGearHops }

// chatRate: chatter lines per minute for this many online bots. The core instance (a handful of bots) skips the
// minimum, which is meant for the whole world: otherwise its one party would get 4 party lines a minute.
func chatRate(online int, div, lo, hi float64) float64 {
	if coreInstance() {
		lo = 0
	}
	return clampF(float64(online)/div, lo, hi)
}

func tuningFile() string { return filepath.Join(*dataDir, "tuning.json") }

// sanitize replaces values that would break the code (divide by zero, empty ranges) with the defaults.
func (t *Tuning) sanitize() {
	d := defaultTuning
	pos := func(v *int, def int) {
		if *v <= 0 {
			*v = def
		}
	}
	posF := func(v *float64, def float64) {
		if *v <= 0 {
			*v = def
		}
	}
	pos(&t.PartyLeash, d.PartyLeash)
	pos(&t.PartyLvSpread, d.PartyLvSpread)
	pos(&t.IdleErrandWaitSec, d.IdleErrandWaitSec)
	pos(&t.IdleErrandWeightPct, d.IdleErrandWeightPct)
	pos(&t.IdleErrandSlots, d.IdleErrandSlots)
	pos(&t.IdleErrandCooldownMin, d.IdleErrandCooldownMin)
	pos(&t.CasterBoltFloorPct, d.CasterBoltFloorPct)
	pos(&t.CasterHelpSPPct, d.CasterHelpSPPct)
	pos(&t.TogetherCells, d.TogetherCells)
	pos(&t.CollectStopCells, d.CollectStopCells)
	pos(&t.GearReserveZenyDiv, d.GearReserveZenyDiv)
	pos(&t.MaxGearHops, d.MaxGearHops)
	pos(&t.GearTurnMaxMin, d.GearTurnMaxMin)
	pos(&t.GuildDutyMinPerDay, d.GuildDutyMinPerDay)
	pos(&t.GuildDutyWarnDays, d.GuildDutyWarnDays)
	pos(&t.GuildDutyKickDays, d.GuildDutyKickDays)
	pos(&t.GuildKicksPerHour, d.GuildKicksPerHour)
	pos(&t.GuildKickCooldownH, d.GuildKickCooldownH)
	posF(&t.ChatGlobalDiv, d.ChatGlobalDiv)
	posF(&t.ChatLocalDiv, d.ChatLocalDiv)
	posF(&t.ChatPartyDiv, d.ChatPartyDiv)
	posF(&t.ChatGuildDiv, d.ChatGuildDiv)
	posF(&t.AmbientTalkDiv, d.AmbientTalkDiv)
	posF(&t.AmbientTalkMaxPerMin, d.AmbientTalkMaxPerMin)
	if t.PriestBuffRenewHi <= t.PriestBuffRenewLo {
		t.PriestBuffRenewHi = t.PriestBuffRenewLo + 1
	}
	if t.ChatGlobalMax < t.ChatGlobalMin {
		t.ChatGlobalMax = t.ChatGlobalMin
	}
	if t.ChatLocalMax < t.ChatLocalMin {
		t.ChatLocalMax = t.ChatLocalMin
	}
	if t.ChatPartyMax < t.ChatPartyMin {
		t.ChatPartyMax = t.ChatPartyMin
	}
	if t.ChatGuildMax < t.ChatGuildMin {
		t.ChatGuildMax = t.ChatGuildMin
	}
}

// loadTuning reads tuning.json (writing the defaults if it's missing) and publishes it if it changed.
func loadTuning() {
	raw, err := os.ReadFile(tuningFile())
	if os.IsNotExist(err) {
		b, _ := json.MarshalIndent(defaultTuning, "", "  ")
		tmp := tuningFile() + ".tmp"
		if os.WriteFile(tmp, append(b, '\n'), 0644) == nil {
			os.Rename(tmp, tuningFile())
			log.Printf("tuning: wrote the defaults to %s", tuningFile())
		}
		raw, err = b, nil
	}
	if err != nil {
		log.Printf("tuning: %v (keeping the current values)", err)
		return
	}
	nt := defaultTuning
	if err := json.Unmarshal(raw, &nt); err != nil {
		log.Printf("tuning: %s doesn't parse: %v (keeping the current values)", tuningFile(), err)
		return
	}
	nt.sanitize()
	old := tune()
	if changes := tuningDiff(old, &nt); len(changes) > 0 || curTuning.Load() == nil {
		curTuning.Store(&nt)
		if len(changes) > 0 {
			log.Printf("tuning: %s", strings.Join(changes, ", "))
		}
	}
}

func tuningDiff(a, b *Tuning) []string {
	var out []string
	va, vb := reflect.ValueOf(*a), reflect.ValueOf(*b)
	ty := va.Type()
	for i := 0; i < ty.NumField(); i++ {
		if x, y := va.Field(i).Interface(), vb.Field(i).Interface(); x != y {
			name := strings.Split(ty.Field(i).Tag.Get("json"), ",")[0]
			out = append(out, fmt.Sprintf("%s %v -> %v", name, x, y))
		}
	}
	return out
}

// startTuning loads tuning.json now and re-reads it every 30 s.
func startTuning(stop <-chan struct{}) {
	loadTuning()
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				loadTuning()
			}
		}
	}()
}
