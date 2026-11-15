// Legion: a high-volume Ragnarok Online bot engine for a local rAthena server (PACKETVER 20210107 RE).
//
//	legion seed -personas 1000 -guilds 10     generate personas.json + seed.sql (accounts, chars, items)
//	legion run  -n 1000                       run bots (first N personas) + HTTP API
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	dataDir   = flag.String("data", "/data", "directory with personas.json")
	keepNames = flag.String("keep", "", "seed: comma-separated character names whose accounts are left untouched")
	rathena   = flag.String("rathena", "/rathena", "rAthena tree (db/, npc/)")
	loginAddr = flag.String("login", "127.0.0.1:6900", "login server")
	nBots     = flag.Int("n", 100, "number of bots to run")
	offset    = flag.Int("offset", 0, "skip the first N personas")
	apiAddr   = flag.String("api", ":8090", "HTTP API listen address")
	stagger   = flag.Duration("stagger", 150*time.Millisecond, "delay between bot starts")
	logins    = flag.Int("logins", 8, "max concurrent logins")
	schedule  = flag.Bool("schedule", false, "bots log in/out following their persona's play schedule")
	simSpeed  = flag.Float64("simspeed", 1, "schedule clock speed (24 = one simulated day per real hour)")
	nPersona  = flag.Int("personas", 1000, "seed: number of personas")
	nGuilds   = flag.Int("guilds", 10, "seed: number of guilds (mastermind groups)")
	seedVal   = flag.Int64("seed", 42, "seed: random seed")
	verbose   = flag.Bool("v", false, "verbose logging")
	maxOnline = flag.Int("max-online", 1000, "max concurrent sessions (profiles beyond this wait for a slot)")
	reserve   = flag.Int("reserve", 40, "online slots kept free under -max-online for guild leaders, pinned players, legion-core, Ayla and humans (the map server holds ~1013 clients: rAthena select() build, FD_SETSIZE 1024)")
)

func loadGameData() {
	t := time.Now()
	// base map cache, then the Renewal one (re-done maps such as prontera) on top
	for _, f := range []string{filepath.Join(*rathena, "db", "map_cache.dat"), filepath.Join(*rathena, "db", "re", "map_cache.dat")} {
		if err := loadMapCache(f); err != nil {
			log.Fatal("map cache: ", err)
		}
	}
	if err := loadMobDB(filepath.Join(*rathena, "db", "re", "mob_db.yml")); err != nil {
		log.Fatal("mob db: ", err)
	}
	loadMobSkillPoison(filepath.Join(*rathena, "db", "re", "mob_skill_db.txt")) // poison.go: which monsters poison
	loadNPCs(filepath.Join(*rathena, "npc", "re", "mobs"), filepath.Join(*rathena, "npc", "re", "warps"),
		filepath.Join(*rathena, "npc", "warps"))
	loadItemDB(filepath.Join(*rathena, "db", "re", "item_db_usable.yml"), filepath.Join(*rathena, "db", "re", "item_db_etc.yml"),
		filepath.Join(*rathena, "db", "re", "item_db_equip.yml"))
	loadItemOverrides(filepath.Join(*rathena, "db", "import", "item_db.yml")) // rathena db-import (docker-compose mount)
	loadNPCs(filepath.Join(*rathena, "npc", "re", "merchants"), filepath.Join(*rathena, "npc", "merchants"),
		filepath.Join(*rathena, "npc", "custom-local")) // legion_shops.txt: every shop item at Prontera's south gate
	applyBarterDealers(filepath.Join(*rathena, "npc", "re", "merchants", "Dealer_Update.txt")) // ammo.go: the shops the server really runs
	buildHuntSpots()
	initMarket()
	ns := 0
	for _, s := range shopsByMap {
		ns += len(s)
	}
	log.Printf("items: %d, NPC shops: %d, market pitches: %d", len(items), ns, len(market.free))
	nw := 0
	for _, w := range warpsFrom {
		nw += len(w)
	}
	log.Printf("game data: %d maps, %d monsters, %d spawn maps, %d warps, %d hunting grounds (%v)",
		len(maps), len(mobs), len(spawnsByMap), nw, len(huntSpots), time.Since(t).Round(time.Millisecond))
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: legion seed|run [flags]")
		os.Exit(2)
	}
	cmd := os.Args[1]
	flag.CommandLine.Parse(os.Args[2:])
	loadGameData()

	switch cmd {
	case "route": // debug: legion route <map> <x> <y> <toMap> [level]
		a := flag.Args()
		x, _ := strconv.Atoi(a[1])
		y, _ := strconv.Atoi(a[2])
		lv := 99
		if len(a) > 4 {
			lv, _ = strconv.Atoi(a[4])
		}
		m := maps[a[0]]
		if m == nil {
			fmt.Println("no map", a[0])
			return
		}
		fmt.Printf("region at %d,%d = %d, walkable=%v, warps from %s: %d\n", x, y, m.Region(x, y), m.Walkable(x, y), a[0], len(warpsFrom[a[0]]))
		for _, w := range warpsFrom[a[0]] {
			fmt.Printf("  warp %d,%d region %d -> %s %d,%d\n", w.X, w.Y, m.Region(w.X, w.Y), w.To, w.ToX, w.ToY)
		}
		r := RouteMaps(a[0], x, y, a[3], lv)
		fmt.Println("route:", r)
		if len(r) > 0 {
			p := m.FindPath(x, y, r[0].X, r[0].Y, 80000)
			hits := 0
			for _, c := range p {
				if m.OnWarp(c[0], c[1]) && !inWarpBoxes([]Warp{r[0]}, c[0], c[1]) {
					hits++
				}
			}
			fmt.Printf("path to %d,%d: %d cells, %d on other warps\n", r[0].X, r[0].Y, len(p), hits)
			tx, ty := portalCell(m, x, y, r[0])
			p2 := m.FindPath(x, y, tx, ty, 80000)
			fmt.Printf("portal walkable=%v region=%d; walk target %d,%d: %d cells\n", m.Walkable(r[0].X, r[0].Y), m.Region(r[0].X, r[0].Y), tx, ty, len(p2))
		}
		return
	case "seed":
		ps := generatePersonas(*nPersona, *seedVal, *nGuilds)
		// -keep "Name1,Name2": those characters' accounts keep their characters, progress and persona
		if *keepNames != "" {
			old, err := loadPersonas(filepath.Join(*dataDir, "personas.json"))
			if err != nil {
				log.Fatal("-keep needs the current personas.json: ", err)
			}
			want := map[string]bool{}
			for _, n := range strings.Split(*keepNames, ",") {
				want[strings.ToLower(strings.TrimSpace(n))] = true
			}
			oldBy := map[int]*Persona{}
			for _, p := range old {
				for _, c := range p.Chars {
					if want[strings.ToLower(c.Name)] {
						seedKeep[p.AccountID] = true
						oldBy[p.AccountID] = p
					}
				}
			}
			for i, p := range ps {
				if o := oldBy[p.AccountID]; o != nil {
					ps[i] = o
				}
			}
			log.Printf("keeping %d accounts as they are", len(seedKeep))
		}
		sql := seedSQL(ps, rand.New(rand.NewSource(*seedVal)))
		os.MkdirAll(*dataDir, 0755)
		if err := savePersonas(filepath.Join(*dataDir, "personas.json"), ps); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(*dataDir, "seed.sql"), []byte(sql), 0644); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %d personas and seed.sql to %s", len(ps), *dataDir)

	case "run":
		ps, err := loadPersonas(filepath.Join(*dataDir, "personas.json"))
		if err != nil {
			log.Fatal("personas: ", err, " (run 'legion seed' first)")
		}
		all := ps
		if *offset > len(ps) {
			*offset = len(ps)
		}
		ps = ps[*offset:]
		if *nBots < len(ps) {
			ps = ps[:*nBots]
		}
		ps = selectProfiles(all, ps) // instances.go: -only / -exclude (core instance for the owner's party)
		if *onlyNames != "" || *excludeNames != "" {
			log.Printf("profiles: %d (only %q, exclude %q)", len(ps), *onlyNames, *excludeNames)
		}
		startTuning(nil) // tuning.go: data/tuning.json, re-read every 30 s
		loginGate = make(chan struct{}, *logins)
		onlineSlots = make(chan struct{}, slotCapacity(*maxOnline, *reserve)) // leaderroom.go
		memDir = filepath.Join(*dataDir, "memory")
		for _, p := range all { // every Legion character, also those another instance runs (bot vs human)
			for _, c := range p.Chars {
				allChars[c.Name] = true
			}
		}
		scheduler.enabled, scheduler.speed, scheduler.start = *schedule, *simSpeed, time.Now()
		bots := make([]*Bot, len(ps))
		for i, p := range ps {
			bots[i] = NewBot(p)
		}
		go serveAPI(*apiAddr, bots)
		if *pprofAddr != "" {
			go http.ListenAndServe(*pprofAddr, nil) // net/http/pprof (profiling)
		}
		stop := make(chan struct{})
		go partyManager(bots, stop)
		go lfgManager(bots, stop) // lfg.go: farm goals, LFG/LFM on #global, need-driven parties (main instance)
		go humanPartyManager(bots, stop)
		go rallyManager(stop)         // rally.go: parties regroup at the Kafra nearest their destination
		go econManager(bots, stop)    // economy.go: #trade wants/offers, bot-to-bot trades
		go alchemyManager(bots, stop) // alchemy.go: every guild's alchemist, brewing
		go gearPoolManager(stop)      // gearpool.go: party gear turns, zeny gifts, planned gear runs
		go memoryFlusher(stop)
		go memoryCleanSweep(bots)     // memory.go: drop test accounts (AIBot) / NPC speakers from old memories
		go friendsManager(bots, stop) // friends.go: friendship scores, greetings, guild moves
		reactBots = bots
		go chatterLoop(bots, stop) // chatter.go: #global and local talk
		go func() {
			for i, b := range bots {
				go b.run(*loginAddr, stop)
				if i%50 == 49 {
					log.Printf("started %d bots", i+1)
				}
				time.Sleep(*stagger)
			}
		}()
		go func() {
			for range time.Tick(30 * time.Second) {
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				log.Printf("online %d/%d  kills %d  deaths %d  loots %d  loginfail %d  goroutines %d  heap %dMB",
					stats.online, len(bots), stats.kills, stats.deaths, stats.loots, stats.loginFail,
					runtime.NumGoroutine(), m.HeapAlloc>>20)
			}
		}()
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Printf("shutting down")
		close(stop)
		time.Sleep(2 * time.Second)
	default:
		log.Fatal("unknown command ", cmd)
	}
}
