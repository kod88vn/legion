package main

// One Bot = one game session (login -> char -> map) plus its world view. Network reading runs in its own
// goroutine; the AI runs in another (ai.go). Shared state is guarded by b.mu.

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	objPC   = 0x0
	objNPC  = 0x6
	objMob  = 0x5
	objItem = 0x2
)

type Entity struct {
	ID        uint32
	Type      byte
	Class     int
	Name      string
	X, Y      int
	DX, DY    int // walking destination
	HP, MaxHP int
	Seen      time.Time
	LastHitMe time.Time
	Target    uint32 // who this unit last attacked
	TargetAt  time.Time
	MoveAt    time.Time // when its current walk (X,Y -> DX,DY) started
	Opt2      uint16    // poison.go: healthState from 0x0229 (bit 0x1 = poisoned)
	Opt2At    time.Time
	Sitting   bool      // together.go: a player seen sitting (0x08c8 action 2/3) - fallback when no rest was announced
	SitAt     time.Time // when it sat down
}

// pos: where the unit is now. A walk packet only says from where to where; X,Y used to stay at the start forever, so
// a monster that had walked up to a tank still looked several cells away and Bash/Magnum Break (range 1/2) were
// never cast. Assumes ~150 ms per cell (most monsters walk at 150-200).
func (e *Entity) pos(now time.Time) (int, int) {
	if e.MoveAt.IsZero() || (e.DX == e.X && e.DY == e.Y) {
		return e.X, e.Y
	}
	steps := max(abs(e.DX-e.X), abs(e.DY-e.Y))
	done := int(now.Sub(e.MoveAt) / (150 * time.Millisecond))
	if done >= steps {
		return e.DX, e.DY
	}
	return e.X + (e.DX-e.X)*done/steps, e.Y + (e.DY-e.Y)*done/steps
}

type GroundItem struct {
	ID     uint32
	ItemID int
	X, Y   int
	Count  int
	Seen   time.Time
}

type InvItem struct {
	Index    int
	ItemID   int
	Count    int
	Loc      uint32 // equip items: where it can go
	Equipped bool
	Cards    [4]int // equip items: card slots (cards.go; 0 = empty)
	Refine   int    // refine level (equipment list at login, refine.go 0x0188)
	// identify.go: a monster-dropped item is unidentified (the server won't equip it); NoEquip = the server refused
	// to equip this very item (or never answered twice), so it is skipped without blacklisting the item ID
	Unidentified bool `json:",omitempty"`
	NoEquip      bool `json:",omitempty"`
	equipTries   int
}

type Event struct {
	T      time.Time `json:"t"`
	Bot    string    `json:"bot"`              // character name
	Player string    `json:"player,omitempty"` // persona (account owner)
	Map    string    `json:"map,omitempty"`
	Kind   string    `json:"kind"` // chat, whisper, party, death, levelup, info
	From   string    `json:"from,omitempty"`
	Text   string    `json:"text"`
	// party lines are logged once: the other members who heard it (ai.go eventRing.add; 2026-11-14 every party line
	// was logged once per member, ~3,800 party events per 10 min)
	HeardBy []string `json:"heard_by,omitempty"`
}

type Bot struct {
	P   *Persona
	rng *rand.Rand

	mu      sync.Mutex
	conn    *Conn       // current session's conn (guarded by wmu); nil between sessions
	wmu     sync.Mutex  // serializes writes
	running atomic.Bool // a run() loop is active for this Bot (only one may be)
	// leaderroom.go: holdsSlot = this session holds an onlineSlots token; bump asks the session to end so a guild
	// leader can get onto a full map server
	holdsSlot atomic.Bool
	bump      chan struct{}

	aid, cid, lid1, lid2 uint32
	sex                  byte

	Online    bool
	State     string // ai state for display
	MapName   string
	X, Y      int
	DestX     int
	DestY     int
	MoveUntil time.Time
	Speed     int
	HP, MaxHP int
	SP, MaxSP int
	Weight    int
	MaxWeight int
	BaseLv    int
	JobLv     int
	BaseExp   int64
	Zeny      int
	StatPts   int
	Dead      bool
	Sitting   bool

	ents  map[uint32]*Entity
	items map[uint32]*GroundItem
	inv   map[int]*InvItem
	cart  map[int]*InvItem

	// AI
	Goal Goal
	// lfg.go: the farm goal from the build (next gear piece / card / zeny / level, or heal for healers) and the
	// goal just met (a party member leaves politely when its next goal is elsewhere)
	fgoal        *farmGoal
	fgoalDone    *farmGoal
	fgoalDoneAt  time.Time
	target       uint32
	targetAt     time.Time
	path         [][2]int
	route        []Warp
	lastMoveTx   time.Time
	restUntil    time.Time
	afkUntil     time.Time
	lastPos      [3]interface{}
	lastPosAt    time.Time
	replanAt     time.Time
	skillReady   time.Time
	progressAt   time.Time
	skillUpAt    time.Time // last skill-up request: one at a time, retried if the server doesn't confirm (progress.go)
	sitSentAt    time.Time
	wantSit      bool
	restSitAt    time.Time // together.go restGuard: seated on purpose during a party rest (hunt must not stand us up)
	songID       int       // song.go: the song being played (0 = none)
	songAt       time.Time // song.go: when it was cast
	songUntil    time.Time // song.go: when it ends
	errand       *errand   // errand.go: a necessary build/class step away from the party (nil = none)
	restOn       bool      // together.go restAnnounce: announced "need rest", no "ready" yet (published as Resting)
	restSaidAt   time.Time // when the last "need rest" line went out (one per 45 s at most)
	restSaid     bool      // the current rest was announced in chat: its end gets a "ready" line too
	restSince    time.Time // together.go restAnnounce: resting (by state) since - a rest counts after restMinDur
	deferAt      time.Time // tanklead.go leaderDefer: start of the window in which a non-tank leader holds its pull for the tank
	noSitUntil   time.Time // party.go stood us up for the leader: no new rest sit before this (no sit/stand flicker)
	potAt        time.Time
	tripAgain    time.Time
	classAskAt   time.Time // human-led party: last "need a Rod/Bow" ask in party chat (once per 30 min)
	lastTripAt   time.Time // last town trip start (min interval: tune().TripMinIntervalMin)
	cur          *CharSpec // character played this session
	sessionEnd   time.Time
	hangoutUntil time.Time

	// progression / skills
	Class    int
	SkillPts int
	skills   map[int]*SkillInfo
	stats    [6]int // str agi vit int dex luk (base values)

	// party
	PartyID         uint32
	PartyName       string
	partyInvite     uint32
	stripTries      map[string]int // variantskill.go: Divest tries per skill and monster
	reset           *resetTask     // reset.go: a natural build reset underway
	comboWait       time.Duration  // the last combo delay the server announced (0x01d2) and when
	comboWaitAt     time.Time
	canactAt        time.Time            // skillaudit.go: when the server lets us act again (skills sent before are refused silently)
	buffAt          map[string]time.Time // "skill:target" -> next allowed
	partyHP         map[uint32][2]int    // party member aid -> hp, maxhp
	aoeWhyAt        time.Time            // aoe.go: last "AOE none" trace line
	aoeCastUntil    time.Time            // aoe.go: an area spell is being cast until then (published: the tank holds the pack)
	roster          map[string]string    // party member name -> map
	partyLeader     string               // in-game party leader (only they can invite)
	pin             *Pin                 // pinned online on one character (human-led party)
	equipped        map[uint32]*InvItem  // slot -> worn item
	gearFailed      map[int]bool         // items the server refused to equip
	invSeen         bool                 // the server sent the inventory this session: gear/trip decisions wait for it
	invWaitFrom     time.Time            // session start (invReady falls back to 20 s after it)
	farGearAt       time.Time            // last planned far gear trip (builds.go farGearOK)
	gearNext        *gearStep            // the party gear pool's view of this bot's next step (gearpool.go)
	equipWanted     int
	equipIdx        int // inventory index of the equip request in flight (equipWanted is its item ID)
	identAt         time.Time
	magNext         time.Time // identify.go: next vendor Magnifier trip
	identIdx        int       // identify.go: the unidentified item a Magnifier is being used for
	equipAt         time.Time
	ammoAt          time.Time       // ammo.go: next arrow-equip / melee-fallback attempt
	arrowFailed     map[string]bool // ammo.go: arrow sellers (shopKey) whose buy list had no arrows this session
	arrowNeedAt     time.Time       // ammo.go: no "no arrows" need before this (20 min after a trip or a failed visit)
	gearItem        int             // item a gear trip is buying
	kafra           *kafraTrip      // Kafra teleport in progress (kafra.go)
	kafraNext       time.Time       // no Kafra plans before this
	kafraLast       kafraHop        // the last Kafra teleport taken (kafra.go kafraHold: no back-and-forth trips)
	kafraHops       []time.Time     // Kafra teleports in the last hour (kafraHold: at most kafra_max_per_hour)
	kafraHardUntil  time.Time       // kafraHoldQ: the hard limit (3 in 10 min / X->Y->X) refuses teleports until then
	escWingAt       time.Time       // last Fly / Butterfly Wing used (escapeitem.go wingEvent)
	diedAt          time.Time       // last own death (a respawn is a real reason for another Kafra teleport)
	savedTown       string          // where the respawn point is saved ("" = unknown, assume home)
	viaTown, viaFor string          // heading to this Kafra town first, for the trip to viaFor
	tgtID           uint32          // ghost-target watchdog: the target being watched
	waitSince       time.Time       // party leader: waiting for a straggler since
	collectName     string          // party leader: the straggler being collected
	// escort.go: escorted crossings of a dangerous approach map
	escKey, escSafe, escDanger string    // cached crossingDanger for "map|dest"
	escWaitAt                  string    // member: waiting at this safe point for an escort (published)
	escUntil                   time.Time // escorted (member) / leading across (leader) until then
	escSaidAt, escLeadAt       time.Time
	escNear                    bool      // the cached danger is on the destination's approach (escortApproachHops)
	escWaitSince               time.Time // member: waiting at the safe point since
	gearPick                   int       // party town run: item chosen for this town's shops
	gearStick                  int       // gearpool.go stickyStep: the gear target kept until bought (no flip-flopping)
	earmarkZ                   int       // gearpool.go giftableZeny: zeny received by mail (a gift "for X"), never given on
	earmarkUntil               time.Time // ... until it is spent on gear or this time passes
	awaySince                  time.Time // separated from the human leader since (help call after 2 min)
	lastWrongRoom              time.Time // trip whose shop room couldn't be reached
	wrongRoomTries             int
	wrongRoomMap               string
	collectAt                  time.Time
	tradeFrom                  string      // a party/guild mate asked to trade (tradeThink)
	mkt                        *marketSide // economy.go: our part in a bot-to-bot trade deal
	prevMap                    string      // warp-loop detector (ai.go)
	opt2                       uint16      // poison.go: our healthState (0x0229), opt2At when it last came
	optionSeen                 bool
	pecoFails                  int    // peco.go: breeder talks in a row that ended without a ride
	option                     uint32 // peco.go: our effectState (0x0229 option bits: 0x20 riding a Peco Peco); optionSeen once one came
	opt2At, poisonSince        time.Time
	poisonSeen, cureAt         time.Time
	mapHist                    []mapVisit
	lastWarp                   Warp
	prevLegIntended            bool          // warp-loop detector: the previous map change went through b.lastWarp
	attackSentFor              uint32        // target the continuous auto-attack was sent for
	atkRange                   int           // our attack range from the server (0x013a); 0 = not told yet (melee, 1)
	chaseAt                    time.Time     // last walk sent to close in on the target (fight)
	tooFarAt                   time.Time     // last "target too far" (0x0139) from the server
	tooFarID                   uint32        // ... and for which target
	tradeAt, tradeOpen         time.Time     // when they asked / when we accepted
	roamSince, roamAt          time.Time     // roaming without a target since / last roaming tick
	roamWin                    time.Time     // ai.go hunt: start of the current roam-share window
	roamAcc                    time.Duration // time spent roaming in that window
	seenMap                    string        // seen.go: the map noteMobSight is sampling
	seenMapAt, seenAt          time.Time     // ... arrived on it / last sample
	noMobSince                 time.Time     // ... no monster at all in view since
	roamDest                   [2]int        // explore(): the far cell being walked to on this map
	roamDestAt                 time.Time
	patrolKey                  string    // patrol.go: map#region of the loop being walked
	patrolIdx                  int       // next waypoint
	patrolDir                  int       // +1 / -1 along the loop
	patrolAt                   time.Time // when the current leg started
	patrolTick                 time.Time // last sweep tick
	badMap                     string    // a map where we found nothing to fight
	badMapUntil                time.Time
	townRunSeen                time.Time // the party town run this bot already joined
	forceTown                  string    // town for the trip being started (party town run)
	hasCart                    bool      // a pushcart is equipped (vendors rent one at the Kafra)
	cartLoadAt                 time.Time // last time items were moved into the cart
	tgtSince                   time.Time // since when we've had it
	hitTargetAt                time.Time // last time the server showed us acting on our target
	wingAt                     time.Time // last Butterfly Wing used for a Kafra trip
	noPathLog                  time.Time // last "no path" event (ai.go stepAlong)
	planNoteAt                 time.Time
	baited                     map[uint32]time.Time // monsters this character tagged once for its party (when)
	skillFailUntil             map[int]time.Time    // attack skill the server refused (0x0110: wrong weapon, no ammo, zeny...): skipped until then
	teleAt                     time.Time            // teleport.go: last skill Teleport cast, its cell and map (teleCheck)
	teleFrom                   [2]int               //
	teleSaveAt                 time.Time            // travelwing.go: a Teleport cast for the save point (warpReply)
	teleMap                    string               //
	castUntil                  time.Time            // our own cast bar ends (0x07fb): rAthena refuses a wing (item and cast) while casting
	opt1                       uint16               // our bodyState (0x0229): stone/freeze/stun/sleep - no item can be used
	fleeTo                     [2]int               // danger.go escape: running TO the party (not just away) until fleeUntil
	escAll                     []time.Time          // danger.go escape: every escape of the last 2 min (the wing throttle; b.escapes = map danger only)
	spiritCasts                int                  // spirit spheres the server reports for us (0x01d0/0x01e1); skillbuff.go Call Spirits
	buffDeaths                 int                  // skillbuff.go: Deaths when the self-buff timers were last valid
	saleRoom                   string               // title of the "S> ..." chat room we opened in town
	waitingAtCheckpoint        bool
	waitDanger                 string    // humanparty.go: the leader's map we wait out at the checkpoint town (too dangerous)
	rallyID                    time.Time // rally.go: the party rally this bot is in (its start time)
	rallySaid, rallySat        bool
	rallyCell                  [2]int
	rallyRouteOK               time.Time // the rally (start time) whose route to the town was checked
	collectWaitAt              time.Time // waiting for the leader to collect us since / last time it got 5+ cells closer
	collectWaitD               int
	regrouping                 bool           // party member walking back to its leader until within collect_stop_cells
	regroupBestD               int            // together.go: closest distance to the leader reached on this regroup ...
	regroupBestAt              time.Time      // ... and when (progressing = closer within regroupProgressWin)
	attackersOnly              bool           // hunt: only fight monsters hitting us (a party member on its way to the leader)
	leadOff                    bool           // party.go partyGoalSync: the bot leader is on a detour / behind (members wait in town)
	leadPassedCP               bool           // the leader has reached the party's checkpoint town (or the party map) ...
	leadCPFor                  string         // ... for this party map
	lootShareAt                time.Time      // together.go partyLootShare: last time the party's pickup-share option was sent
	travelPos                  [3]interface{} // think travel branch: last position seen while travelling ...
	travelMovedAt              time.Time      // ... and when it last changed (a party member walks on while it progresses)
	lostSince                  time.Time      // party.go: the human leader's whereabouts unknown since (asks "where r u")
	leadPrevMap                string         // partyGoalSync: the leader's map before its current one (follow it through portals)
	holdTown, holdFor          string         // partyhold.go: the detour hold point chosen, for which party map
	holdAt                     time.Time      // ... and when (kept 5 min: no flipping)
	saveKnown                  bool           // together.go: savedTown is the real save point (Kafra save, respawn or memory), not a guess
	leadWaitMap                string         // a travelling bot leader waiting on this map for members to catch up ...
	leadWaitSince              time.Time      // ... since (at most leaderWaitMax per map)
	lostSeenAt                 time.Time
	humanAskAt                 time.Time // ai.go: first "need a town run" to a human leader not yet answered by a party run
	idleLeadPos                [2]int    // heal.go healerIdleSit: the leader's position as last seen, still since ...
	idleLeadStill              time.Time
	idleSitAt                  time.Time // ... and when the idle sit went down (hysteresis / minimum sit time)
	kafraVirtual               bool      // kafra.go kafraWouldTake: planning from a town we are not in (no fare requests)
	escortHold                 string    // humanparty.go escortGate: why we wait in the entrance town ("" = not waiting)
	leadMap                    string    // partyGoalSync: the leader's map as last seen, since when, and when our goal changed
	leadMapSince               time.Time
	goalSyncAt                 time.Time
	stuckPos                   [3]interface{} // stuck watchdog (rally.go unstick): position not changing since stuckSince
	stuckSince                 time.Time
	unstuckAt                  time.Time
	stuckTries                 int
	dialogNPC                  uint32    // last NPC that opened a dialog with us (closed when stuck)
	unwallUntil                time.Time // walled in: stay logged out until then so tools/unwall.py can move the char (nav.go unwall)
	escapeAt                   time.Time
	escapes                    []time.Time
	hpHist                     []hpSample // danger.go survive: recent HP fractions (how fast HP drops)
	fleeUntil                  time.Time  // danger.go: running away on foot from a losing fight until then
	deathLog                   []mapVisit // own deaths (map, time): 2 on one map in 10 min -> an easier map
	GuildID                    uint32
	GuildName                  string
	guildMaster                bool
	guildInvite                uint32
	guildInviteName            string
	gadm                       *guildAdmin // guildtax.go: a guild master's positions / tax / skill state
	traderNext                 time.Time   // humanguildtrader.go: next guild-shop shift / end of the current one
	traderUntil                time.Time
	guildJoinGID               uint32 // guilds.go: the invitation we accepted, until the server confirms the membership
	guildJoinName              string
	guildActAt                 time.Time
	saleTried                  bool
	wake                       chan struct{} // nudges an offline bot to re-check pin/schedule now

	// town trips / vending
	trip    *Trip
	vend    *VendState
	npcDeal *npcDeal

	// stats
	Kills, Deaths, Loots int
	meter                meterCounters // meter.go: kills, income, deaths, time budget (guarded by mu)
	ConnectedAt          time.Time
	LastErr              string
	PrevErr              string    // loginready.go: LastErr moved here at the next good login
	PrevErrAt            time.Time // when that login happened
	pktsIn               int64
}

type Goal struct {
	Mode string `json:"mode"` // hunt, idle, town, follow
	Map  string `json:"map"`
	X    int    `json:"x,omitempty"`
	Y    int    `json:"y,omitempty"`
	Who  string `json:"who,omitempty"`
	Set  string `json:"set_by,omitempty"`
}

func NewBot(p *Persona) *Bot {
	return &Bot{P: p, rng: rand.New(rand.NewSource(int64(p.AccountID))), Speed: 150, State: "offline", Class: p.Class,
		ents: map[uint32]*Entity{}, items: map[uint32]*GroundItem{}, inv: map[int]*InvItem{},
		skills: map[int]*SkillInfo{}, buffAt: map[string]time.Time{}, cart: map[int]*InvItem{},
		partyHP: map[uint32][2]int{}, roster: map[string]string{}, wake: make(chan struct{}, 1),
		equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}, bump: make(chan struct{}, 1)}
}

func (b *Bot) send(p []byte) error {
	if b.escBlocks(p) { // escapeitem.go: a wing is pending - no ground skill, no other item use in between
		return nil
	}
	b.traceSend(p)
	b.auditSend(p) // skillaudit.go
	b.wmu.Lock()
	defer b.wmu.Unlock()
	if b.conn == nil {
		return fmt.Errorf("not connected")
	}
	return b.conn.Send(p)
}

func tick() uint32 { return uint32(time.Now().UnixMilli()) }

// ---------------------------------------------------------------- login / char / map handshake

func (b *Bot) connect(loginAddr string) error {
	// 1. login server
	c, err := dial(loginAddr)
	if err != nil {
		return err
	}
	p := newPkt(0x0064).u32(55).str(b.P.Account, 24).str(b.P.Password, 24).u8(0x0a)
	c.Send(p.b)
	pk, err := c.Read(10 * time.Second)
	c.Close()
	if err != nil {
		return fmt.Errorf("login: %v", err)
	}
	op := le.Uint16(pk)
	if op != 0x0ac4 && op != 0x0069 {
		return loginRefusedErr(op, pk[2]) // loginready.go
	}
	b.lid1, b.aid, b.lid2 = le.Uint32(pk[4:]), le.Uint32(pk[8:]), le.Uint32(pk[12:])
	b.sex = pk[46]
	srvOff := 64
	if op == 0x0069 {
		srvOff = 47
	}
	if len(pk) < srvOff+6 {
		return fmt.Errorf("login: no char servers")
	}
	charAddr := fmt.Sprintf("%s:%d", ip4(le.Uint32(pk[srvOff:])), le.Uint16(pk[srvOff+4:]))

	// 2. char server
	c, err = dial(charAddr)
	if err != nil {
		return err
	}
	c.Send(newPkt(0x0065).u32(b.aid).u32(b.lid1).u32(b.lid2).u16(0).u8(b.sex).b)
	if _, err := c.ReadRaw(4, 10*time.Second); err != nil {
		c.Close()
		return fmt.Errorf("char greet: %v", err)
	}
	slot := -1
	deadline := time.Now().Add(15 * time.Second)
	for slot < 0 && time.Now().Before(deadline) {
		pk, err := c.Read(10 * time.Second)
		if err != nil {
			c.Close()
			return fmt.Errorf("char: %v", err)
		}
		switch le.Uint16(pk) {
		case 0x09a0: // number of char list pages
			c.Send(newPkt(0x09a1).b)
		case 0x099d, 0x006b: // char list page (block size 155: char_id at 0, name at 88, stats 112-117, slot at 118)
			start := 4
			if le.Uint16(pk) == 0x006b {
				start = 27
			}
			for o := start; o+155 <= len(pk); o += 155 {
				// alchemy.go: levels of every character on the account (the guild alchemist is picked among them)
				noteCharLevels(int(le.Uint32(pk[o:])), int(le.Uint16(pk[o+64:])), int(le.Uint16(pk[o+72:])), int(le.Uint32(pk[o+24:])))
				if le.Uint32(pk[o:]) == uint32(b.charSpec().CharID) || slot < 0 {
					slot = int(pk[o+118])
					if le.Uint32(pk[o:]) == uint32(b.charSpec().CharID) {
						// the map server never re-sends levels at login, so take them from the char list
						b.mu.Lock()
						b.BaseLv, b.JobLv = int(le.Uint16(pk[o+72:])), int(le.Uint32(pk[o+24:]))
						b.Zeny = int(int32(le.Uint32(pk[o+12:])))
						b.Class = int(le.Uint16(pk[o+64:]))
						// peco.go: CHARACTER_INFO.effectstate (the saved option, 0x20 = riding a Peco Peco). The map
						// server sends ZC_STATE_CHANGE3 (0x0229) to us at login only when opt2 is set (poison...),
						// so without this optionSeen stayed false and no unmounted rider ever wanted its Peco
						// (0 of 125 Knights lv40+ riding, 2026-11-14)
						b.option, b.optionSeen = le.Uint32(pk[o+36:]), true
						b.mu.Unlock()
						break
					}
				}
			}
		case 0x006c:
			c.Close()
			return fmt.Errorf("char server refused")
		}
	}
	if slot < 0 {
		c.Close()
		return fmt.Errorf("no character on account")
	}
	c.Send(newPkt(0x0066).u8(byte(slot)).b)
	var mapAddr string
	for mapAddr == "" {
		pk, err := c.Read(10 * time.Second)
		if err != nil {
			c.Close()
			return fmt.Errorf("char select: %v", err)
		}
		switch le.Uint16(pk) {
		case 0x0ac5, 0x0071:
			b.cid = le.Uint32(pk[2:])
			b.MapName = trimGat(cstr(pk[6:22]))
			mapAddr = fmt.Sprintf("%s:%d", ip4(le.Uint32(pk[22:])), le.Uint16(pk[26:]))
		case 0x0840: // loginready.go: map server not up yet; the server sends nothing else, so stop waiting
			c.Close()
			return accessibleMapsErr(pk)
		case 0x006c, 0x0081:
			c.Close()
			return fmt.Errorf("char select refused (%04x)", le.Uint16(pk))
		}
	}
	c.Close()

	// 3. map server
	c, err = dial(mapAddr)
	if err != nil {
		return err
	}
	c.Send(newPkt(0x0436).u32(b.aid).u32(b.cid).u32(b.lid1).u32(tick()).u8(b.sex).b)
	for {
		pk, err := c.Read(15 * time.Second)
		if err != nil {
			c.Close()
			return fmt.Errorf("map: %v", err)
		}
		op := le.Uint16(pk)
		if op == 0x02eb || op == 0x0a18 || op == 0x073 {
			b.mu.Lock()
			b.X, b.Y, _ = decodePos(pk[6:9])
			b.DestX, b.DestY, b.MoveUntil = b.X, b.Y, time.Time{}
			b.mu.Unlock()
			break
		}
		if op == 0x0081 {
			c.Close()
			return fmt.Errorf("map refused (code %d)", pk[2])
		}
	}
	b.wmu.Lock()
	b.conn = c
	b.wmu.Unlock()
	b.send(newPkt(0x007d).b) // map loaded
	b.mu.Lock()
	b.Online, b.Dead, b.Sitting = true, false, false
	b.ConnectedAt = time.Now()
	b.ents = map[uint32]*Entity{}
	b.items = map[uint32]*GroundItem{}
	b.mu.Unlock()
	return nil
}

func trimGat(s string) string {
	if len(s) > 4 && s[len(s)-4:] == ".gat" {
		return s[:len(s)-4]
	}
	return s
}

// ---------------------------------------------------------------- packet handling

// errSessionEnded: the read loop noticed its session was over (quit closed) before the conn reported an error.
var errSessionEnded = errors.New("session ended")

// readLoop reads one session's packets from c. The conn is passed in, never re-read from b.conn: 2026-11-14 11:34Z a
// session ended through logoutSignal/stop while this loop was still handling a packet buffered in bufio; it then
// re-read b.conn, which run() had just set to nil, and the SIGSEGV in (*Conn).Read(nil) killed legion (1,000 bots
// logged out). It also stops at quit, so an old session's buffered packets are never fed into the next session's
// state, and it sends exactly one value on done (buffered, cap 1) so run() can wait for it to exit.
func (b *Bot) readLoop(c *Conn, quit <-chan struct{}, done chan<- error) {
	defer func() {
		if r := recover(); r != nil { // safety.go: a crash in the read path ends this session, never the service
			name := "?"
			if b.P != nil {
				name = b.P.Name
			}
			log.Printf("PANIC (recovered) bot %s in read loop: %v\n%s", name, r, debug.Stack())
			done <- fmt.Errorf("read loop panic: %v", r)
		}
	}()
	if c == nil {
		done <- errors.New("no connection")
		return
	}
	for {
		select {
		case <-quit:
			done <- errSessionEnded
			return
		default:
		}
		pk, err := c.Read(60 * time.Second)
		if err != nil {
			done <- err
			return
		}
		select {
		case <-quit:
			done <- errSessionEnded
			return
		default:
		}
		atomic.AddInt64(&b.pktsIn, 1)
		atomic.AddInt64(&stats.pktsIn, 1)
		b.safely("packet handler", func() { b.handle(pk) }) // safety.go
	}
}

func (b *Bot) handle(pk []byte) {
	op := le.Uint16(pk)
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.traceRecv(op, pk, now)
	b.auditRecv(op, pk, now) // skillaudit.go: skill answers and the can-act delay
	b.healRecv(op, pk)       // heal.go
	b.buffRecv(op, pk, now)  // priestbuff.go PT29
	b.efstRecv(op, pk, now)  // effects.go: status icons for the party dashboard
	switch op {
	case 0x0087: // own walk accepted: tick, movedata
		x0, y0, x1, y1 := decodeMove(pk[6:12])
		b.X, b.Y, b.DestX, b.DestY = x0, y0, x1, y1
		steps := max(abs(x1-x0), abs(y1-y0))
		b.MoveUntil = now.Add(time.Duration(steps*b.Speed) * time.Millisecond)
	case 0x013a: // our attack range (sent on login and on weapon change)
		if len(pk) >= 4 {
			b.atkRange = int(le.Uint16(pk[2:]))
		}
	case 0x0139: // attack refused, target too far: target id, target x/y, our x/y, our range. rAthena does NOT chase
		// for a player (unit_attack_timer_sub -> clif_movetoattack): the continuous attack ends here and the client
		// must walk into range and attack again. Take the server's positions as the truth and let fight() close in.
		if len(pk) >= 16 {
			id := le.Uint32(pk[2:])
			tx, ty := int(le.Uint16(pk[6:])), int(le.Uint16(pk[8:]))
			if e := b.ents[id]; e != nil && (e.DX == e.X && e.DY == e.Y || now.Sub(e.MoveAt) > 3*time.Second) {
				e.X, e.Y, e.DX, e.DY = tx, ty, tx, ty
			}
			if now.After(b.MoveUntil) {
				b.X, b.Y = int(le.Uint16(pk[10:])), int(le.Uint16(pk[12:]))
				b.DestX, b.DestY = b.X, b.Y
			}
			if r := int(le.Uint16(pk[14:])); r > 0 {
				b.atkRange = r
			}
			b.tooFarAt, b.tooFarID = now, id
			if id == b.attackSentFor {
				b.attackSentFor = 0 // the swing never started: fight() walks in and sends it again
			}
		}
	case 0x0088: // stop / fix position
		id := le.Uint32(pk[2:])
		x, y := int(le.Uint16(pk[6:])), int(le.Uint16(pk[8:]))
		if id == b.aid {
			b.X, b.Y, b.DestX, b.DestY, b.MoveUntil = x, y, x, y, now
		} else if e := b.ents[id]; e != nil {
			e.X, e.Y, e.DX, e.DY = x, y, x, y
		}
	case 0x0086: // other unit moved
		id := le.Uint32(pk[2:])
		if e := b.ents[id]; e != nil {
			x0, y0, x1, y1 := decodeMove(pk[6:12])
			e.X, e.Y, e.DX, e.DY, e.Seen, e.MoveAt = x0, y0, x1, y1, now, now
			if e.Type == objPC && e.Name != "" && !allChars[e.Name] {
				noteHuman(e.Name, b.MapName, x1, y1, id)
			}
		}
	case 0x09ff, 0x09fe, 0x09fd: // unit appears (idle / spawn / walking)
		id := le.Uint32(pk[5:])
		if id == b.aid {
			return
		}
		e := b.ents[id]
		if e == nil {
			e = &Entity{ID: id}
			b.ents[id] = e
		}
		e.Type, e.Class, e.Seen = pk[4], int(int16(le.Uint16(pk[23:]))), now
		var nameOff, hpOff int
		switch op {
		case 0x09fd:
			x0, y0, x1, y1 := decodeMove(pk[67:73])
			e.X, e.Y, e.DX, e.DY = x0, y0, x1, y1
			hpOff, nameOff = 79, 90
		case 0x09ff:
			e.X, e.Y, _ = decodePos(pk[63:66])
			e.DX, e.DY = e.X, e.Y
			hpOff, nameOff = 73, 84
		default:
			e.X, e.Y, _ = decodePos(pk[63:66])
			e.DX, e.DY = e.X, e.Y
			hpOff, nameOff = 72, 83
		}
		if len(pk) >= hpOff+8 {
			e.MaxHP, e.HP = int(int32(le.Uint32(pk[hpOff:]))), int(int32(le.Uint32(pk[hpOff+4:])))
		}
		if len(pk) > nameOff {
			e.Name = cstr(pk[nameOff:])
		}
		if e.Type == objPC && e.Name != "" && !allChars[e.Name] { // a human player came into view
			noteHuman(e.Name, b.MapName, e.DX, e.DY, e.ID)
			memoryOf(b.P).meet(e.Name, "", b.MapName, true)
		}
	case 0x0080: // vanish: id, type (0 out of sight, 1 died, 2 logged out, 3 teleported)
		id := le.Uint32(pk[2:])
		if id == b.aid {
			if pk[6] == 1 {
				b.Dead = true
				b.Deaths++
				b.meterDeath(now) // meter.go
				atomic.AddInt64(&stats.deaths, 1)
				b.event("death", "", fmt.Sprintf("died on %s", b.MapName))
				reportDeath(b.MapName)
				reportDeathLv(b.MapName, b.BaseLvOr()) // beginner.go: deaths per level band
				noteDeathAt(b.MapName)                 // danger.go: real deaths in a 10-minute window
				b.noteBossDeath(time.Now())            // bossnote.go: a boss killed us here: the spot is shared for 10 min
				b.deathLog = append(b.deathLog, mapVisit{m: b.MapName, at: time.Now()})
				leaderNoteDeath(partyLeaderName(b), b.MapName) // party.go: 2 party deaths on a map keep the leader's parties off it 1 h
				b.diedAt = time.Now()
				// QA 2026-11-14: killed by a monster far above our effective level (TheLilychan, Rogue lv48 with a Knife,
				// by an Orc Lady on gef_fild11, then walked straight back): ONE such death bans the map for 30 min
				if _, maxLv, _, _ := b.attackers(time.Now()); maxLv >= b.powerLv()+8 && !isTown(b.MapName) {
					b.badMap, b.badMapUntil = b.MapName, time.Now().Add(30*time.Minute)
					if b.Goal.Set == "auto" || b.Goal.Set == "" {
						b.Goal, b.route, b.path = Goal{}, nil, nil
					}
					b.event("info", "", fmt.Sprintf("killed by a level %d monster on %s: avoiding it for 30 min", maxLv, b.MapName))
				}
			}
			return
		}
		if pk[6] == 1 && id == b.target {
			b.Kills++
			b.meterKill(b.ents[id], now) // meter.go: fair kill by the monster's level
			atomic.AddInt64(&stats.kills, 1)
		}
		if e := b.ents[id]; pk[6] == 1 && e != nil && e.Type == objMob && bossHuntsLive.Load() > 0 {
			bossKilled(b, e.Class) // bosshunt.go: a hunted boss died in sight
		}
		delete(b.ents, id)
		claims.release(b.MapName, id, b.aid)
	case 0x08c8: // damage / action (also sit/stand confirmations: action 2 = sit, 3 = stand)
		src, dst := le.Uint32(pk[2:]), le.Uint32(pk[6:])
		if src == b.aid && len(pk) > 29 {
			switch pk[29] {
			case 2:
				b.Sitting = true
			case 3:
				b.Sitting = false
			}
		} else if e := b.ents[src]; e != nil && len(pk) > 29 && (pk[29] == 2 || pk[29] == 3) {
			e.Sitting = pk[29] == 2
			if e.Sitting {
				e.SitAt = now
			}
		}
		if dst == b.aid {
			if e := b.ents[src]; e != nil {
				e.LastHitMe = now
			}
		}
		if src == b.aid && dst != 0 && dst == b.target {
			b.hitTargetAt = now // we really landed an action on our target (ghost-target watchdog in think)
		}
		if e := b.ents[src]; e != nil && dst != 0 && dst != src {
			e.Target, e.TargetAt = dst, now
		}
	case 0x0229: // ZC_STATE_CHANGE3: id, bodyState, healthState (poison...), effectState, pk (poison.go)
		b.statusRecv(pk, now)
	case 0x0110: // skill failed: skill id, btype, result, cause. bestAttack skips the skill for a while instead of
		// re-sending it every cast (QA 2026-11-14: Assassins without a katar sent Sonic Blow every 2 s, never Envenom)
		if len(pk) >= 10 && pk[8] == 0 {
			if b.skillFailUntil == nil {
				b.skillFailUntil = map[int]time.Time{}
			}
			b.skillFailUntil[int(le.Uint16(pk[2:]))] = now.Add(60 * time.Second)
		}
		if len(pk) >= 4 && int(le.Uint16(pk[2:])) == mcVending {
			b.vendPitchRefused() // vend.go: the pitch is too close to an NPC (or a no-vending cell)
		}
	case 0x01d2: // ZC_COMBODELAY: aid, delay - weaponskill.go comboNext casts the next step after it (run 6)
		if len(pk) >= 10 && le.Uint32(pk[2:]) == b.aid {
			b.comboWait, b.comboWaitAt = time.Duration(le.Uint32(pk[6:]))*time.Millisecond, now
		}
	case 0x01d0, 0x01e1: // spirit spheres: aid, count (Call Spirits is refused at max - QA 2026-11-14)
		if len(pk) >= 8 && le.Uint32(pk[2:]) == b.aid {
			b.spiritCasts = int(le.Uint16(pk[6:]))
		}
	case 0x0abe: // ZC_WARPLIST: our Teleport skill's menu - teleport.go answers "Random"
		if r := b.warpReply(pk); r != nil { // travelwing.go: "Random", or the save point after flySave
			go b.send(r)
		}
	case 0x0afb: // ZC_AUTOSPELLLIST (Hindsight): len, int32 skill ids - answer with the spell to auto-cast
		if id := autoSpellPick(pk, b.skills); id != 0 {
			go b.send(newPkt(0x01ce).u32(uint32(id)).b) // CZ_SELECTAUTOSPELL
		}
	case 0x01de: // skill damage: skill, src, dst
		if len(pk) >= 12 {
			src, dst := le.Uint32(pk[4:]), le.Uint32(pk[8:])
			if src == b.aid && dst != 0 && dst == b.target {
				b.hitTargetAt = now
			}
			if src == b.aid && dst != 0 {
				b.comboNext(int(le.Uint16(pk[2:])), dst, now) // weaponskill.go: Monk Triple Attack -> Chain Combo -> Combo Finish
			}
			if e := b.ents[src]; e != nil && dst != 0 && dst != src {
				e.Target, e.TargetAt = dst, now
				if dst == b.aid {
					e.LastHitMe = now
				}
			}
		}
	case 0x080e: // party member hp: aid, hp, maxhp
		b.partyHP[le.Uint32(pk[2:])] = [2]int{int(int32(le.Uint32(pk[6:]))), int(int32(le.Uint32(pk[10:])))}
	case 0x0107: // party member position: aid, x, y (sent for members on our map, visible or not)
		aid := le.Uint32(pk[2:])
		x, y := int(le.Uint16(pk[6:])), int(le.Uint16(pk[8:]))
		if e := b.ents[aid]; e != nil && e.Name != "" && !allChars[e.Name] {
			noteHuman(e.Name, b.MapName, x, y, aid)
		} else if x > 0 && x < 1000 {
			// not on our screen: find the human by account id (party data). Before, a human who wasn't visible had
			// no position at all and the party "roamed" instead of walking to the leader.
			humanLocs.Range(func(k, v interface{}) bool {
				if h := v.(humanSeen); h.AID == aid {
					noteHuman(k.(string), b.MapName, x, y, aid)
					return false
				}
				return true
			})
		}
	case 0x00b0: // own status
		v := int(int32(le.Uint32(pk[4:])))
		switch le.Uint16(pk[2:]) {
		case 0:
			b.Speed = max(v, 20)
		case 5:
			b.HP = v
		case 6:
			b.MaxHP = v
		case 7:
			b.SP = v
		case 8:
			b.MaxSP = v
		case 9:
			b.StatPts = v
		case 12:
			b.SkillPts = v
		case 11:
			if b.BaseLv != 0 && v > b.BaseLv {
				b.event("levelup", "", fmt.Sprintf("reached base level %d", v))
			}
			b.BaseLv = v
		case 24:
			b.Weight = v
		case 25:
			b.MaxWeight = v
		case 55:
			b.JobLv = v
		}
	case 0x00b1:
		if le.Uint16(pk[2:]) == 20 {
			b.meterZeny(int(int32(le.Uint32(pk[4:])))) // meter.go
			b.Zeny = int(int32(le.Uint32(pk[4:])))
		}
	case 0x0acb:
		if le.Uint16(pk[2:]) == 1 {
			b.BaseExp = int64(le.Uint64(pk[4:]))
		}
	case 0x0add, 0x009d, 0x084b, 0x009e: // item on the floor
		gi := &GroundItem{ID: le.Uint32(pk[2:]), ItemID: int(le.Uint32(pk[6:])), Seen: now}
		switch op {
		case 0x0add:
			gi.X, gi.Y, gi.Count = int(le.Uint16(pk[13:])), int(le.Uint16(pk[15:])), int(le.Uint16(pk[19:]))
		case 0x009d:
			gi.X, gi.Y, gi.Count = int(le.Uint16(pk[11:])), int(le.Uint16(pk[13:])), int(le.Uint16(pk[15:]))
		default:
			return
		}
		b.items[gi.ID] = gi
	case 0x00a1:
		delete(b.items, le.Uint32(pk[2:]))
	case 0x0b41: // pickup ack
		if len(pk) > 33 && pk[33] == 0 {
			idx := int(le.Uint16(pk[2:]))
			it := b.inv[idx]
			if it == nil {
				// 0x0b41 (2021 layout): identified at 10, location at 28. Loc was left 0 for bought/looted gear, so
				// the API showed an equipped Rod as a non-equipment item and QA counted its owner as weaponless
				it = &InvItem{Index: idx, ItemID: int(le.Uint32(pk[6:])), Loc: le.Uint32(pk[28:]), Unidentified: pk[10] == 0}
				b.inv[idx] = it
			} else if id := int(le.Uint32(pk[6:])); it.ItemID != id || it.Count <= 0 {
				// a stale entry at this index (item traded/sold away): take the new item's id, location and identify
				// flag (a Club received in a trade kept the old entry's "identified" and the equip was refused)
				it.ItemID, it.Loc, it.Unidentified, it.Count, it.Equipped, it.NoEquip = id, le.Uint32(pk[28:]), pk[10] == 0, 0, false, false
			}
			it.Count += int(le.Uint16(pk[4:]))
			b.Loots++
			b.meterLoot(it.ItemID, int(le.Uint16(pk[4:]))) // meter.go: market value of the loot
			atomic.AddInt64(&stats.loots, 1)
		}
	case 0x0b39: // equipment list (68 bytes each after invType): idx, itemid, type, location, wearstate, ...
		if len(pk) > 5 && pk[4] == 0 {
			b.invSeen = true // the last list before 0x0b0b (normal items come first)
			for o := 5; o+68 <= len(pk); o += 68 {
				idx := int(le.Uint16(pk[o:]))
				// EQUIPITEM_INFO (2021, refine moved to the end): idx 0, id 2, type 6, location 7, wear 11, cards 15-30,
				// ..., refine 65, grade 66, flags 67 (bit 0 = identified)
				it := &InvItem{Index: idx, ItemID: int(le.Uint32(pk[o+2:])), Count: 1, Loc: le.Uint32(pk[o+7:]),
					Unidentified: pk[o+67]&1 == 0, Refine: int(pk[o+65])}
				for c := 0; c < 4; c++ {
					it.Cards[c] = int(le.Uint32(pk[o+15+4*c:]))
				}
				if wear := le.Uint32(pk[o+11:]); wear != 0 {
					it.Equipped = true
					b.equipped[gearSlot(wear)] = it
				}
				b.inv[idx] = it
			}
		} else if len(pk) > 5 && pk[4] == 2 { // the storage's equipment (reset.go takes its things back)
			for o := 5; o+68 <= len(pk); o += 68 {
				b.resetStorageItem(int(le.Uint16(pk[o:])), &InvItem{Index: int(le.Uint16(pk[o:])), ItemID: int(le.Uint32(pk[o+2:])), Count: 1})
			}
		} else if len(pk) > 5 && pk[4] == 1 { // the cart's equipment (vend.go: vendors must know what is unidentified)
			for o := 5; o+68 <= len(pk); o += 68 {
				idx := int(le.Uint16(pk[o:]))
				it := &InvItem{Index: idx, ItemID: int(le.Uint32(pk[o+2:])), Count: 1, Loc: le.Uint32(pk[o+7:]),
					Unidentified: pk[o+67]&1 == 0}
				for c := 0; c < 4; c++ {
					it.Cards[c] = int(le.Uint32(pk[o+15+4*c:]))
				}
				b.cart[idx] = it
			}
		}
	case 0x0999: // equip ack: idx, location, sprite, result (0 = ok)
		idx, loc := int(le.Uint16(pk[2:])), le.Uint32(pk[4:])
		it := b.inv[idx]
		if it != nil {
			if pk[10] == 0 && loc != 0 {
				if old := b.equipped[gearSlot(loc)]; old != nil && old != it {
					old.Equipped = false
				}
				// economy run 9: a piece over two head slots (Goggles: upper+mid) takes off whatever sat in either one
				for s, e := range b.equipped {
					if e != it && s != eqpAmmo && e.Loc&loc != 0 && gearSlot(e.Loc) != 0x88 {
						e.Equipped = false
						delete(b.equipped, s)
					}
				}
				it.Equipped = true
				b.equipped[gearSlot(loc)] = it
				if x := items[it.ItemID]; x != nil {
					b.remember("equipped %s", x.Name)
				}
			} else {
				// refused: skip this item. Only an identified item's refusal means the ID itself can't be worn (it
				// used to blacklist the ID always, so a refused unidentified drop stopped the bot BUYING that weapon)
				it.NoEquip = true
				if !it.Unidentified {
					b.gearFailed[it.ItemID] = true
				}
			}
		}
		b.equipWanted = 0
	case 0x099a: // unequip ack: idx, location, result
		if it := b.inv[int(le.Uint16(pk[2:]))]; it != nil && pk[8] == 0 {
			it.Equipped = false
			for s, e := range b.equipped {
				if e == it {
					delete(b.equipped, s)
				}
			}
		}
	case 0x0125: // item left the cart: index, amount (vend.go unloads NPC junk back to the bag)
		if len(pk) >= 8 {
			if it := b.cart[int(le.Uint16(pk[2:]))]; it != nil {
				if it.Count -= int(le.Uint32(pk[4:])); it.Count <= 0 {
					delete(b.cart, it.Index)
				}
			}
		}
	case 0x017d: // card compounded (cards.go)
		b.onCardAck(pk)
	case 0x0121: // cart info (count, max, weight, max weight): we have a cart
		b.hasCart = true
	case 0x0b45, 0x0a0b: // item added to the cart: index.W amount.L itemid.L ...
		if len(pk) >= 12 {
			idx, n, id := int(le.Uint16(pk[2:])), int(le.Uint32(pk[4:])), int(le.Uint32(pk[8:]))
			if it := b.cart[idx]; it != nil && it.ItemID == id {
				it.Count += n
			} else {
				// identified at 13: rAthena refuses a whole shop when one item in it is unidentified (vending.cpp
				// "i != j"), and the vendor believed its shop was open (economy batch 10)
				b.cart[idx] = &InvItem{Index: idx, ItemID: id, Count: n, Unidentified: len(pk) >= 14 && pk[13] == 0}
			}
		}
	case 0x0b09: // item list (normal items, 34 bytes each); byte 4 = 0 inventory, 1 cart, 2 storage
		dst := b.inv
		switch pk[4] {
		case 1:
			dst, b.hasCart = b.cart, true
		case 2:
			for o := 5; o+34 <= len(pk); o += 34 { // reset.go: what the Kafra storage holds (to take things back)
				b.resetStorageItem(int(le.Uint16(pk[o:])), &InvItem{Index: int(le.Uint16(pk[o:])), ItemID: int(le.Uint32(pk[o+2:])), Count: int(le.Uint16(pk[o+7:]))})
			}
			return
		}
		// invSeen waits for the inventory END (0x0b0b): the equipment list comes after this one, and a bot that
		// decided between the two saw no weapon in its bag and left on a "buying Rod" trip 0.6 s after login
		// NORMALITEM_INFO's WearState is NOT "worn": rAthena's clif_item_normal fills it with the item db's equip
		// LOCATION (id->equip), so every arrow stack carries 0x8000 whether or not it is in the ammo slot. Treating it
		// as worn left archers "loaded" forever with nothing in the slot (0 kills, arrows never drop; QA 2026-11-14
		// 06:05). The truth is the 0x013c the server sends right after this list only when the ammo slot is filled.
		if pk[4] == 0 {
			delete(b.equipped, eqpAmmo)
		}
		for o := 5; o+34 <= len(pk); o += 34 {
			idx := int(le.Uint16(pk[o:]))
			it := &InvItem{Index: idx, ItemID: int(le.Uint32(pk[o+2:])), Count: int(le.Uint16(pk[o+7:]))}
			dst[idx] = it
		}
	case 0x0b32: // skill list: id, inf, level, sp, range, upgradable, level2 (15 bytes each)
		for o := 4; o+15 <= len(pk); o += 15 {
			id := int(le.Uint16(pk[o:]))
			b.skills[id] = &SkillInfo{ID: id, Lv: int(le.Uint16(pk[o+6:])), SP: int(le.Uint16(pk[o+8:])),
				Range: int(le.Uint16(pk[o+10:])), Upgradable: pk[o+12] != 0}
		}
	case 0x0b31: // skill added
		if len(pk) >= 17 {
			id := int(le.Uint16(pk[2:]))
			b.skills[id] = &SkillInfo{ID: id, Lv: int(le.Uint16(pk[8:])), SP: int(le.Uint16(pk[10:])),
				Range: int(le.Uint16(pk[12:])), Upgradable: pk[14] != 0}
		}
	case 0x010e: // skill level changed: id, lv, sp, range, upgradable
		id := int(le.Uint16(pk[2:]))
		s := b.skills[id]
		if s == nil {
			s = &SkillInfo{ID: id}
			b.skills[id] = s
		}
		s.Lv, s.SP, s.Range, s.Upgradable = int(le.Uint16(pk[4:])), int(le.Uint16(pk[6:])), int(le.Uint16(pk[8:])), pk[10] != 0
	case 0x0141: // base stat value: type (13..18), base, bonus
		if t := int(le.Uint32(pk[2:])); t >= 13 && t <= 18 {
			b.stats[t-13] = int(le.Uint32(pk[6:]))
		}
	case 0x00bd: // all stats
		b.StatPts = int(le.Uint16(pk[2:]))
		for i := 0; i < 6; i++ {
			b.stats[i] = int(pk[4+2*i])
		}
	case 0x00bc: // stat raise result: id, ok, new value
		if pk[4] == 1 {
			if id := int(le.Uint16(pk[2:])); id >= 13 && id <= 18 {
				b.stats[id-13] = int(pk[5])
			}
		}
	case 0x01d7: // look change; type 0 on ourselves = job change
		if le.Uint32(pk[2:]) == b.aid && pk[6] == 0 {
			nc := int(le.Uint32(pk[7:]))
			if nc != b.Class {
				b.event("jobchange", "", fmt.Sprintf("became a %s", className(nc)))
				b.Class = nc
				b.Goal = Goal{} // new class: re-plan where to train
			}
		}
	case 0x02c6: // party invitation: party id, party name
		b.partyInvite = le.Uint32(pk[2:])
		b.PartyName = cstr(pk[6:30])
	case 0x00fa: // party created (result 0 = ok)
		if pk[2] == 0 {
			b.event("party", "", "created party "+b.PartyName)
		} else { // 1 = name taken or refused, 2 = already in a party
			b.event("party", "", fmt.Sprintf("could not create party %s (result %d)", b.PartyName, pk[2]))
		}
	case 0x0a44, 0x00fb, 0x0ae5: // party member list => we're in a party
		if len(pk) > 28 {
			b.PartyName = cstr(pk[4:28])
			b.PartyID = 1
			if op == 0x0ae5 {
				b.roster = map[string]string{}
				for o := 28; o+54 <= len(pk); o += 54 { // aid, gid, name[24], map[16], leader, offline, class, lv
					n, m := cstr(pk[o+8:o+32]), strings.TrimSuffix(cstr(pk[o+32:o+48]), ".gat")
					b.roster[n] = m
					if pk[o+48] == 0 { // 0 = leader (sic)
						b.partyLeader = n
					}
					if !allChars[n] {
						humanLevels.Store(n, int(le.Uint16(pk[o+52:])))
					}
					if !allChars[n] {
						notePartyMember(n, m, pk[o+49] == 0, le.Uint32(pk[o:]))
					}
				}
			}
		}
	case 0x07d8: // ZC_REQ_GROUPINFO_CHANGE_V2: exp option.L (1 = even share), item pick, item share - sent on login
		// and on change; the dashboard's even_share showed false for 258 of 259 parties while the DB had exp=1
		if len(pk) >= 6 {
			even := le.Uint32(pk[2:]) == 1
			go func() { // parties lock first, never while holding b.mu
				parties.Lock()
				if p := parties.ofBot[b]; p != nil {
					p.ShareSet = even
				}
				parties.Unlock()
			}()
		}
	case 0x0abd: // party member job/level changed: aid, job, level
		aid, lv := le.Uint32(pk[2:]), int(le.Uint16(pk[8:]))
		humanLocs.Range(func(k, v interface{}) bool {
			if v.(humanSeen).AID == aid {
				humanLevels.Store(k.(string), lv)
				return false
			}
			return true
		})
	case 0x0ae4: // member info (join / map change / log in-out): aid, gid, leader, class, lv, x, y, offline, party[24], name[24], map[16]
		if len(pk) >= 87 {
			n, m := cstr(pk[47:71]), strings.TrimSuffix(cstr(pk[71:87]), ".gat")
			b.roster[n] = m
			b.PartyID = 1
			if !allChars[n] {
				humanLevels.Store(n, int(le.Uint16(pk[16:])))
				// sent on join, map change and log in/out: the server's word on a human member's state
				notePartyMember(n, m, pk[22] == 0, le.Uint32(pk[2:]))
				if pk[22] == 0 {
					noteHuman(n, m, int(le.Uint16(pk[18:])), int(le.Uint16(pk[20:])), le.Uint32(pk[2:]))
				}
			}
		}
	case 0x016c: // our guild: id, emblem, mode, isMaster, interSID, name[24]
		if len(pk) >= 43 {
			old := b.GuildName
			b.GuildID, b.guildMaster, b.GuildName = le.Uint32(pk[2:]), pk[14] != 0, cstr(pk[19:43])
			if b.GuildName != "" {
				guildRoster.Store(b.P.Account, b.GuildName)
				if b.guildMaster {
					noteGuildMaster(b, b.GuildName) // the in-game master is the guild's one leader (guilds.go)
				}
				if old != b.GuildName {
					if b.guildMaster {
						b.event("guild", "", "founded the guild "+b.GuildName)
						b.remember("founded the guild %s", b.GuildName)
					} else {
						b.event("guild", "", "joined the guild "+b.GuildName)
						b.remember("joined the guild %s", b.GuildName)
						b.humanGuildJoined(b.GuildName) // humanguild.go: hello in Ayla's guild chat
					}
				}
			}
		}
	case 0x0b7b, 0x0166, 0x0160, 0x0162: // guild info / positions / skills (guildtax.go)
		b.onGuildAdmin(op, pk)
	case 0x0a30: // ZC_ACK_REQNAMEALL2: id, name[24], party[24], guild[24], position[24], title id
		// rAthena sends a new member no 0x016c when it joins (guild_member_added: clif_guild_belonginfo needs the
		// member's position, not known yet), only this name update to the area, us included: without it a bot that
		// accepted an invitation didn't know its guild until it relogged (Starveil 12:42: three friends joined in the
		// DB, the engine still listed them as recruits).
		if len(pk) >= 78 && le.Uint32(pk[2:]) == b.aid && b.GuildID == 0 && b.guildJoinName != "" {
			if g := cstr(pk[54:78]); g != "" && strings.EqualFold(g, b.guildJoinName) {
				b.noteGuildJoined(b.guildJoinGID, g)
			}
		}
	case 0x017f: // guild chat: len.W "Name : text" (friends.go)
		if len(pk) > 4 {
			friendHeard(b, cstr(pk[4:]), true)
		}
	case 0x015a, 0x015c, 0x0839: // a member left / was expelled (0x0839 = ZC_ACK_BAN_GUILD_SSO, PACKETVER >= 20100803): name[24] ... - us: out of the guild (friends.go)
		if len(pk) >= 26 && cstr(pk[2:26]) == b.charName() {
			b.onGuildLeft(map[uint16]string{0x015a: "left", 0x015c: "was expelled from", 0x0839: "was expelled from"}[op])
		}
	case 0x016a: // guild invitation: guild id, name[24]
		if len(pk) >= 30 {
			b.guildInvite, b.guildInviteName = le.Uint32(pk[2:]), cstr(pk[6:30])
		}
	case 0x0109: // party chat
		if len(pk) > 8 {
			b.event("party", "", cstr(pk[8:]))
			friendHeard(b, cstr(pk[8:]), false) // friends.go
			b.buffAskHeard(cstr(pk[8:]), now)   // priestbuff.go PT29: a party mate asks the priest for buffs
			b.partyCmdHeard(cstr(pk[8:]), now)  // partycmd.go: the leader's command (follow me, come to me at <map> x,y, wait here...)
			if b.pin != nil {
				txt := cstr(pk[8:])
				go func() { rallyHeard(partyOf(b), txt) }()                 // the human called a rally (rally.go)
				go func() { humanRestHeard(partyOf(b), txt, time.Now()) }() // together.go: "resting" / "ready" from the human leader
			}
		}
	case 0x00c4, 0x00c6, 0x00c7, 0x00ca, 0x00cb, 0x012d:
		b.onDeal(op, pk)
	case 0x0b7a, 0x09d5, 0x0b4e, 0x09d7: // NPC market shop (dietrich.go: refine materials)
		b.onMarket(op, pk)
	case 0x00b4: // NPC dialog text: len.W id.L msg
		if len(pk) >= 8 {
			b.dialogNPC = le.Uint32(pk[4:])
		}
	case 0x00b5, 0x00b6, 0x00b7: // NPC dialog: next / close / menu
		if op == 0x00b7 && len(pk) >= 8 {
			b.dialogNPC = le.Uint32(pk[4:])
		} else if len(pk) >= 6 {
			b.dialogNPC = le.Uint32(pk[2:])
		}
		b.onKafraDialog(op, pk)
		b.onAlchemyDialog(op, pk) // alchemy.go: the Alchemist Union's Guild Dealer
		b.onResetDialog(op, pk)   // reset.go: the Hypnotist (natural build reset)
		b.answerStrayDialog(op)   // nav.go: a dialog nobody drives would freeze walking and item use (pc_cant_act)
	case 0x0142: // NPC asks for a number (alchemy.go: how many Medicine Bowls)
		b.onAlchemyDialog(op, pk)
	case 0x018d, 0x018f: // pharmacy: makeable list / result (alchemy.go)
		b.onPharmacy(op, pk)
	case 0x0b3d, 0x0135: // a vending shop's list / failed purchase (vendbuy.go)
		b.onVendList(op, pk)
	case 0x09e5, 0x0137: // our vending shop sold something (vendbuy.go)
		b.onVendSold(op, pk)
	case 0x0a12, 0x09ed, 0x09e7, 0x0ac2, 0x09f2, 0x0a05, 0x0b3f, 0x09f4: // RODEX mail (mail.go; items: parcel.go)
		b.onMail(op, pk)
	case 0x0131, 0x0132: // a shop sign shown / taken down (shopcheck.go)
		onStoreSign(op, pk)
	case 0x0aa0, 0x0aa2, 0x0188: // refine window / materials / result (refine.go)
		b.onRefine(op, pk)
	case 0x0a28: // ZC_ACK_OPENSTORE2: result 0 = our shop is open, 1 = refused (vend.go)
		b.onOpenStoreAck(pk)
	case 0x00f2: // ZC_NOTIFY_STOREITEM_COUNTINFO: the Kafra storage window opened (storage.go)
		b.onStorageOpen()
	case 0x0b0b: // inventory list end: invType, flag
		if len(pk) >= 3 && pk[2] == 0 {
			b.invSeen = true
		}
	case 0x013c: // arrow equipped (ammo.go)
		b.onArrowEquip(pk)
	case 0x013b: // ZC_ACTION_FAILURE: 0 = no/wrong ammo equipped, 3 = ammo equipped OK (ammo.go)
		b.onActionFailure(pk, now)
	case 0x0177, 0x0179: // Magnifier: identify list / result (identify.go)
		b.onIdentify(op, pk)
	case 0x00af, 0x07fa: // item removed / used
		b.meterRemoved(op, pk) // meter.go: ammo / skill materials used up
		idx := int(le.Uint16(pk[2:]))
		amt := int(le.Uint16(pk[4:]))
		if op == 0x07fa {
			idx, amt = int(le.Uint16(pk[4:])), int(le.Uint16(pk[6:]))
		}
		if it := b.inv[idx]; it != nil {
			it.Count -= amt
			if it.Count <= 0 {
				delete(b.inv, idx)
			}
		}
	case 0x0147: // ZC_AUTORUN_SKILL: an item skill (Fly / Butterfly Wing) - escapeitem.go
		b.onAutorunSkill(pk)
	case 0x07fb, 0x0b1a: // ZC_USESKILL_ACK: src, dst, x, y, skill, element, cast time ms - our own cast bar (escapeitem.go)
		// run 6: this server (PACKETVER 20211103) sends 0x0b1a (same fields + attackMT), never 0x07fb - castUntil was never set
		if len(pk) >= 24 && le.Uint32(pk[2:]) == b.aid {
			b.castUntil = now.Add(time.Duration(le.Uint32(pk[20:])) * time.Millisecond)
		}
	case 0x01c8: // use item ack: index, itemid u32, id, amount left, result
		if len(pk) >= 15 && le.Uint32(pk[8:]) == b.aid && pk[14] == 0 {
			b.escRefused(int(le.Uint32(pk[4:])), now) // escapeitem.go: a wing refused right away
		}
		// a refused use (result 0) carries amount 0: it is NOT "none left" (rAthena clif_useitemack(sd,n,0,false))
		if len(pk) >= 15 && le.Uint32(pk[8:]) == b.aid && pk[14] == 1 {
			idx := int(le.Uint16(pk[2:]))
			if it := b.inv[idx]; it != nil {
				b.meterUsed(it.ItemID, it.Count-int(le.Uint16(pk[12:]))) // meter.go: potions, wings
				it.Count = int(le.Uint16(pk[12:]))
				if it.Count <= 0 {
					delete(b.inv, idx)
				}
			}
		}
	case 0x0091: // map change: mapname, x, y
		b.noteKafraHop(trimGat(cstr(pk[2:18])), now) // kafra.go: a teleport taken, before kafraThink drops the trip
		b.MapName = trimGat(cstr(pk[2:18]))
		b.X, b.Y = int(le.Uint16(pk[18:])), int(le.Uint16(pk[20:]))
		b.DestX, b.DestY, b.MoveUntil = b.X, b.Y, now
		b.ents = map[uint32]*Entity{}
		b.items = map[uint32]*GroundItem{}
		b.target, b.path = 0, nil
		if len(b.route) > 1 && b.route[0].To == b.MapName && b.route[1].From == b.MapName {
			// came through the route's first portal: keep the rest of the plan. Re-planning on every map with RouteMaps
			// (any region) undid detours that must come back into ANOTHER region of a map (nav.go regroupStep: the
			// leader across prt_fild04's river; wrong-room shop trips) - the bot walked back to the side it came from
			b.route = b.route[1:]
		} else {
			b.route = nil
		}
		memoryOf(b.P).visit(b.MapName)
		// tell the server we finished "loading" the new map, after a client-like pause
		go func() {
			time.Sleep(time.Duration(300+b.rng.Intn(700)) * time.Millisecond)
			b.send(newPkt(0x007d).b)
		}()
	case 0x008d: // someone talks nearby: len, id, "name : text"
		if len(pk) > 8 {
			// NPCs (npctalk: "Resting Adventurer" tips on prt_fild08) and mobs use the same packet; their ids are
			// object ids (>= 100000000), players' are account ids. NPC lines are never player chat (inbox, memory).
			gid := le.Uint32(pk[4:])
			if e := b.ents[gid]; gid >= 100000000 || (e != nil && e.Type != objPC) {
				b.hear("npc", cstr(pk[8:]))
			} else {
				b.hear("chat", cstr(pk[8:]))
			}
		}
	case 0x09de, 0x0097: // whisper
		if op == 0x09de && len(pk) > 34 {
			b.hearFrom("whisper", cstr(pk[8:32]), cstr(pk[33:]))
			go friendNote(b.charName(), cstr(pk[8:32]), fkChat) // friends.go
		}
	case 0x01f4: // trade request: nick[24], char id, level. Accept from party and guild mates (real players hand
		// each other zeny and gear; the leader of a party funds its members' weapons)
		if len(pk) >= 26 {
			from := cstr(pk[2:26])
			ok := false
			if p := partyOf(b); p != nil {
				ok = strings.EqualFold(p.Human, from)
				for _, m := range p.Members {
					ok = ok || strings.EqualFold(m.charName(), from)
				}
			}
			if o, found := botNames.Load(from); found && b.GuildName != "" {
				ok = ok || o.(*Bot).GuildName == b.GuildName
			}
			mp := b.marketPartner(from) // economy.go: the seller of our current deal
			ok = ok || mp
			if !ok || !b.tradeSafe(now) {
				if mp && b.mkt != nil {
					b.mkt.d.busy(now) // economy.go run 8: the seller waits for us instead of failing the deal
				}
				go b.send(newPkt(0x00e6).u8(4).b) // refuse strangers, and any trade while a monster hits us / HP < 70%
			} else if mp {
				// a market partner only asks from within 2 cells: accept at once (walking over first lost half the
				// deals: the partner's name is often unknown in b.ents, so tradeThink never found it and refused)
				b.tradeFrom, b.tradeAt, b.tradeOpen = from, now, now
				b.event("info", "", "accepted a trade from "+from)
				go b.send(newPkt(0x00e6).u8(3).b)
			} else {
				// the server only starts a trade within 2 cells: stop, walk next to them, then accept (tradeThink)
				b.tradeFrom, b.tradeAt = from, now
			}
		}
	case 0x00ec: // trade: someone pressed OK (1 = the other side) -> confirm ours and trade
		if b.onMarketTrade(op, pk) { // economy.go: market deals verify the window before locking
			break
		}
		if len(pk) >= 3 && pk[2] == 1 {
			go func() {
				time.Sleep(600 * time.Millisecond)
				b.send(newPkt(0x00eb).b)
				time.Sleep(700 * time.Millisecond)
				b.send(newPkt(0x00ef).b)
			}()
		}
	case 0x00f0: // trade finished (0 = success)
		b.onMarketTrade(op, pk) // economy.go
		if len(pk) >= 3 && pk[2] == 0 {
			b.event("info", "", "trade completed")
		}
		b.tradeFrom, b.tradeOpen = "", time.Time{}
	case 0x00ee: // trade cancelled
		b.onMarketTrade(op, pk) // economy.go
		b.tradeFrom, b.tradeOpen = "", time.Time{}
	case 0x01f5, 0x00ea, 0x0b42, 0x0a96, 0x0a09: // trade opened / add-item ack / partner added an item (economy.go)
		b.onMarketTrade(op, pk)
	case 0x02c1: // channel message (#global etc.)
		b.onChannel(pk)
	case 0x0081: // disconnected by server
		b.LastErr = fmt.Sprintf("kicked (code %d)", pk[2])
	}
}

func (b *Bot) event(kind, from, text string) {
	events.add(Event{T: time.Now(), Bot: b.charName(), Player: b.P.Name, Kind: kind, From: from, Text: text, Map: b.MapName})
	mem := memoryOf(b.P)
	switch kind {
	case "death":
		killer := ""
		for _, e := range b.ents {
			if time.Since(e.LastHitMe) < 5*time.Second && e.Type == objMob {
				killer = " (killed by " + mobName(e.Class) + ")"
			}
		}
		b.remember("%s%s", text, killer)
		b.chatterReact(kind, text)
	case "levelup", "jobchange", "market", "info":
		b.remember("%s", text)
		b.chatterReact(kind, text)
	case "party":
		if strings.HasPrefix(text, "formed") || strings.HasPrefix(text, "created") {
			b.remember("%s", text)
		}
	case "whisper":
		mem.meet(from, "", b.MapName, !allChars[from])
		b.remember("%s whispered me: %s", from, text)
	case "chat":
		if i := strings.Index(text, " : "); i > 0 && !allChars[text[:i]] {
			mem.meet(text[:i], "", b.MapName, true)
		}
	}
}

// allChars: names of every Legion character (to tell bots from humans)
var allChars = map[string]bool{}

func (b *Bot) hear(kind, line string) { b.event(kind, "", line) }
func (b *Bot) hearFrom(kind, from, line string) {
	b.event(kind, from, line)
}

// ---------------------------------------------------------------- actions

func (b *Bot) walkTo(x, y int) {
	x, y, ok := b.stickDest(x, y) // partystick.go PT04/PT06/PT31: a grinding party member never walks beyond 20 cells of its party
	if !ok {
		return
	}
	x, y, ok = b.safeWalkDest(x, y) // nav.go: never walk into / across a warp that isn't the portal we mean to take
	if !ok {
		return
	}
	b.send(newPkt(0x035f).bytes(encodePos(x, y, 0)).b)
	b.lastMoveTx = time.Now()
}
func (b *Bot) attack(id uint32) { b.send(newPkt(0x0437).u32(id).u8(7).b) }
func (b *Bot) sit() {
	if b.escHolding(0) {
		return // a wing is being used: sitting would void it (escapeitem.go)
	}
	b.send(newPkt(0x0437).u32(0).u8(2).b)
}
func (b *Bot) stand()           { b.send(newPkt(0x0437).u32(0).u8(3).b) }
func (b *Bot) pickup(id uint32) { b.send(newPkt(0x0362).u32(id).b) }
func (b *Bot) respawn()         { b.send(newPkt(0x00b2).u8(0).b) }
func (b *Bot) useItem(idx int)  { b.send(newPkt(0x0439).u16(uint16(idx)).u32(b.aid).b) }
func (b *Bot) keepalive()       { b.send(newPkt(0x0360).u32(tick()).b) }
func (b *Bot) say(text string) {
	msg := b.charName() + " : " + text
	b.send(newPkt(0x00f3).u16(0).str(msg, len(msg)+1).varlen())
	b.event("said", b.charName(), text)
}
func (b *Bot) whisper(to, text string) {
	b.send(newPkt(0x0096).u16(0).str(to, 24).str(text, len(text)+1).varlen())
	b.event("whispered", b.charName(), to+" <- "+text)
}

// ---------------------------------------------------------------- lifecycle

// endSession closes this session's conn and clears b.conn - only if b.conn is still that conn.
func (b *Bot) endSession(conn *Conn) {
	b.wmu.Lock()
	if b.conn == conn {
		b.conn = nil
	}
	b.wmu.Unlock()
	if conn != nil {
		conn.Close() // unblocks readLoop's Read at once
	}
}

// waitSessionLoops waits (bounded) for the session's readLoop and aiLoop to exit after quit was closed.
func (b *Bot) waitSessionLoops(done <-chan error, readExited bool, aiDone <-chan struct{}) {
	limit := time.After(15 * time.Second)
	if !readExited {
		select {
		case <-done:
		case <-limit:
			log.Printf("%s: read loop still running 15 s after its session ended", b.P.Name)
		}
	}
	select {
	case <-aiDone:
	case <-limit:
		log.Printf("%s: AI loop still running 15 s after its session ended", b.P.Name)
	}
}

func (b *Bot) run(loginAddr string, stop <-chan struct{}) {
	// one run loop per Bot, ever: a second one would share b.conn and the per-session state
	if !b.running.CompareAndSwap(false, true) {
		log.Printf("%s: run loop already active, not starting a second one", b.P.Name)
		return
	}
	defer b.running.Store(false)
	backoff := 5 * time.Second
	for {
		select {
		case <-stop:
			return
		default:
		}
		pin := b.pinned()
		if pin != nil && time.Now().Before(pin.Hold) {
			b.mu.Lock()
			b.State = "offline (joining a party soon)"
			b.mu.Unlock()
			select {
			case <-stop:
				return
			case <-time.After(time.Until(pin.Hold)):
			}
			continue
		}
		lead := b.alwaysOnline() // guild leaders: always online, no schedule, no slot wait (guildlead.go)
		if pin == nil && !lead && !scheduler.ShouldBeOnline(b) {
			b.mu.Lock()
			b.State = "offline (schedule)"
			b.mu.Unlock()
			select {
			case <-stop:
				return
			case <-time.After(time.Duration(30+b.rng.Intn(60)) * time.Second):
			case <-b.wake:
			}
			continue
		}
		// wait for a free online slot (more profiles than concurrent players); pinned players don't wait
		slot := pin == nil && !lead
		if slot {
			select {
			case onlineSlots <- struct{}{}:
			case <-stop:
				return
			}
			b.holdsSlot.Store(true)
		}
		release := func() {
			if slot {
				b.holdsSlot.Store(false)
				<-onlineSlots
			}
		}
		select { // a bump left over from the previous session must not end this one
		case <-b.bump:
		default:
		}
		// which of the player's characters to play this session; reset per-character state
		b.mu.Lock()
		b.cur = b.pickChar()
		b.Class, b.skills, b.stats = b.cur.Class, map[int]*SkillInfo{}, [6]int{}
		b.inv, b.cart, b.hasCart = map[int]*InvItem{}, map[int]*InvItem{}, false
		b.vend, b.trip, b.npcDeal, b.Goal = nil, nil, nil, Goal{}
		b.BaseLv, b.JobLv, b.PartyName, b.PartyID = 0, 0, "", 0
		b.partyHP, b.roster = map[uint32][2]int{}, map[string]string{}
		b.equipped, b.gearFailed, b.equipWanted = map[uint32]*InvItem{}, map[int]bool{}, 0
		b.invSeen, b.invWaitFrom, b.gearNext = false, time.Now(), nil
		b.saleRoom, b.saleTried = "", false
		b.mu.Unlock()
		loginGate <- struct{}{} // limit concurrent logins
		err := b.connect(loginAddr)
		<-loginGate
		if err != nil {
			release()
			b.mu.Lock()
			b.LastErr, b.State = err.Error(), "login failed"
			b.mu.Unlock()
			atomic.AddInt64(&stats.loginFail, 1)
			if lead && mapServerFull(err) && makeRoomFor(b) { // leaderroom.go: a slot holder steps aside
				backoff = 10 * time.Second
			}
			select {
			case <-stop:
				return
			case <-time.After(backoff + time.Duration(b.rng.Intn(5000))*time.Millisecond):
			}
			if backoff < 2*time.Minute && !(serverStarting(err) && backoff >= 20*time.Second) { // loginready.go
				backoff *= 2
			}
			continue
		}
		backoff = 5 * time.Second
		atomic.AddInt64(&stats.online, 1)
		// session routine: sociable players hang around town after logging in; everyone heads back to town
		// shortly before they log off
		b.mu.Lock()
		now := time.Now()
		b.sessionEnd = scheduler.sessionEnd(b)
		b.hangoutUntil = time.Time{}
		if pin != nil || lead {
			b.sessionEnd = time.Time{} // leaders level instead of hanging out in town
		} else if isTown(b.MapName) && b.rng.Float64() < (0.08+0.3*b.P.Sociability)*b.tactics().Hangout {
			// players log in, chat a little, then go grind
			b.hangoutUntil = now.Add(time.Duration(float64(30+b.rng.Intn(int(30+150*b.P.Sociability)))*b.tactics().Hangout) * time.Second)
		}
		b.remember("logged in on %s", b.MapName)
		b.noteLoginOK(now)        // loginready.go: an old error is no longer the current one
		b.kafraLastFromDiary(now) // kafra.go: an hourly restart doesn't reset the "no Kafra back-and-forth" memory
		b.meterLogin(now)         // meter.go: purse baseline, new counters for another character
		b.mu.Unlock()
		b.wmu.Lock()
		conn := b.conn // this session's conn, captured once (readLoop never re-reads b.conn)
		b.wmu.Unlock()
		done := make(chan error, 1)
		quit := make(chan struct{})
		aiDone := make(chan struct{})
		go b.readLoop(conn, quit, done)
		go func() {
			defer close(aiDone)
			b.aiLoop(quit)
		}()
		var why string
		readExited := false
		select {
		case err := <-done:
			why = fmt.Sprint(err)
			readExited = true
		case <-stop:
			why = "stopped"
		case <-scheduler.logoutSignal(b):
			why = "session over"
		case <-b.bump:
			why = bumpedWhy
		}
		close(quit)
		b.endSession(conn)
		// one session per Bot: the old read and AI loops are gone before anything else (or the next login) runs
		b.waitSessionLoops(done, readExited, aiDone)
		releasePitch(b)
		if p := partyOf(b); p != nil && p.Human == "" && why != "stopped" {
			// one member's logout no longer ends the party (QA 2026-11-14 15:53: guild06's leader party vanished with
			// no event when a member's play session ended; the leader wandered off solo): the member leaves, the party
			// goes on while 2+ remain (drop disbands below that, or when the leader leaves). Not at shutdown: the party
			// is saved and restored after the restart (partypersist.go)
			p.drop(b)
		}
		if why == bumpedWhy {
			yieldSlot(b, stop) // leaderroom.go: keep the slot a while so no other bot takes the freed map connection
		}
		release()
		b.mu.Lock()
		b.remember("logged off (%s), level %d %s", why, b.BaseLvOr(), className(b.Class))
		b.mu.Unlock()
		atomic.AddInt64(&stats.online, -1)
		b.mu.Lock()
		b.Online, b.State = false, "offline"
		if why != "stopped" && why != "session over" {
			b.LastErr = why
		}
		b.mu.Unlock()
		if why == "stopped" {
			return
		}
		if lead {
			noteLeaderRelog(b, why) // logged straight back in below (guildlead.go)
		}
		if *verbose {
			log.Printf("%s disconnected: %s", b.P.Name, why)
		}
		time.Sleep(time.Duration(3+b.rng.Intn(5)) * time.Second)
		b.mu.Lock()
		hold := time.Until(b.unwallUntil)
		if hold > 0 {
			b.State = "offline (moving off a blocked cell)"
		}
		b.mu.Unlock()
		if hold > 0 {
			select {
			case <-stop:
				return
			case <-time.After(hold):
			}
		}
	}
}
