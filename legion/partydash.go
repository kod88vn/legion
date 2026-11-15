package main

// GET /api/parties/detail: every party Legion runs (bot parties and human-led ones such as Ayla's core test party),
// with each member's class, level, role, map, position, HP, what they're doing and whether they're with the leader.
// Used by the dashboard "Parties" card.

import (
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"
)

var roleNames = map[int]string{roleTank: "tank", roleRanged: "ranged", roleSupport: "support", roleMelee: "melee", roleNone: "-"}

type partyMemberView struct {
	Name     string    `json:"name"`
	Class    string    `json:"class"`
	Level    int       `json:"level"`
	JobLevel int       `json:"job_level"`
	Role     string    `json:"role"`
	Online   bool      `json:"online"`
	Map      string    `json:"map"`
	X        int       `json:"x"`
	Y        int       `json:"y"`
	HP       float64   `json:"hp"`
	SP       float64   `json:"sp"`
	Zeny     int       `json:"zeny"`
	Weight   int       `json:"weight,omitempty"`     // current weight, raw server units (1/10 of what the client shows)
	MaxWt    int       `json:"max_weight,omitempty"` // weight limit, same units; 0 = unknown (e.g. a human leader)
	State    string    `json:"state"`
	Leader   bool      `json:"leader,omitempty"`
	Human    bool      `json:"human,omitempty"`
	HasStats bool      `json:"has_stats,omitempty"` // a human whose own HP/SP/zeny/weight are known (humanstats.go)
	Waiting  bool      `json:"waiting,omitempty"`   // waiting at the checkpoint town (leader's map too dangerous)
	WithLead bool      `json:"with_leader"`         // on the same map as the leader
	Kills    int       `json:"kills"`
	Deaths   int       `json:"deaths"`
	GearNext *gearStep `json:"gear_next,omitempty"` // gearpool.go: next gear step, cost, funded by, party turn
}

type partyView struct {
	Name       string                 `json:"name"`
	Human      string                 `json:"human,omitempty"`
	Leader     string                 `json:"leader"`
	Map        string                 `json:"map"`
	Checkpoint string                 `json:"checkpoint"`
	FormedMin  int                    `json:"formed_min"`
	EvenShare  bool                   `json:"even_share"`
	Online     int                    `json:"online"`
	Together   int                    `json:"together"` // online members on the leader's map
	AvgLevel   float64                `json:"avg_level"`
	Members    []partyMemberView      `json:"members"`
	Instance   string                 `json:"instance,omitempty"`  // "core": run by the core Legion instance (instances.go)
	Rally      map[string]interface{} `json:"rally,omitempty"`     // rally.go: meeting at the Kafra nearest the destination
	GearTurn   string                 `json:"gear_turn,omitempty"` // gearpool.go: member whose gear step the party funds now
	GearLog    []string               `json:"gear_log,omitempty"`  // recent gear pool events (gifts, turns done)
	Need       bool                   `json:"need,omitempty"`      // lfg.go: formed around goals (ends with them)
	Purpose    string                 `json:"purpose,omitempty"`   // lfg.go: what the party farms
}

func registerPartyDash(mux *http.ServeMux) {
	mux.HandleFunc("/api/parties/detail", func(w http.ResponseWriter, r *http.Request) {
		parties.Lock()
		ps := make([]*Party, 0, len(parties.byName))
		for _, p := range parties.byName {
			ps = append(ps, p)
		}
		parties.Unlock()
		now := time.Now()
		res := []partyView{}
		for _, p := range ps {
			parties.Lock()
			need, purpose := p.Need, p.Purpose
			parties.Unlock()
			v := partyView{Name: p.Name, Human: p.Human, Map: p.Map, Checkpoint: p.Checkpoint,
				FormedMin: int(now.Sub(p.Formed).Minutes()), EvenShare: p.ShareSet, Rally: rallyView(p, now), Need: need, Purpose: purpose}
			leadMap, leadX, leadY := "", 0, 0
			if p.Human != "" {
				v.Leader = p.Human
				hm := partyMemberView{Name: p.Human, Class: "human", Role: "leader", Leader: true, Human: true, State: "played by a person / OpenKore", HP: -1, SP: -1, Zeny: -1}
				if h, ok := humanAt(p.Human); ok {
					hm.Online = now.Sub(h.At) < 5*time.Minute
					if !h.PartyAt.IsZero() {
						hm.Online = h.PartyOnline // the server's party data is authoritative
					}
					hm.Map, hm.X, hm.Y, hm.HP = h.Map, h.X, h.Y, -1
					leadMap = h.Map
					leadX, leadY = h.X, h.Y
					v.Map = h.Map
				}
				hm.Level = humanLevel(p.Human)
				if s, ok := humanStatusOf(p.Human); ok { // her own stats from OpenKore (humanstats.go)
					fillHumanStats(&hm, s)
				}
				hm.WithLead = true
				v.Members = append(v.Members, hm)
			} else if p.Leader != nil {
				v.Leader = p.Leader.charName()
				p.Leader.mu.Lock()
				leadMap, leadX, leadY = p.Leader.MapName, p.Leader.X, p.Leader.Y
				p.Leader.mu.Unlock()
			}
			lvSum, lvN := 0, 0
			for _, m := range p.Members {
				m.mu.Lock()
				mv := partyMemberView{Name: m.charName(), Class: className(m.Class), Level: m.BaseLvOr(), JobLevel: m.JobLv,
					Role: roleNames[partyRole(m)], Online: m.Online, Map: m.MapName, X: m.X, Y: m.Y, State: m.State,
					Leader: p.Human == "" && m == p.Leader, Waiting: m.waitingAtCheckpoint, Kills: m.Kills, Deaths: m.Deaths}
				if m.Online {
					mv.HP = m.hpFrac()
					if m.MaxSP > 0 {
						mv.SP = float64(m.SP) / float64(m.MaxSP)
					}
				}
				mv.Zeny = m.Zeny
				mv.Weight, mv.MaxWt = m.Weight, m.MaxWeight
				if m.Online {
					mv.GearNext = m.gearStepView()
				}
				m.mu.Unlock()
				mv.WithLead = mv.Online && leadMap != "" && mv.Map == leadMap
				if mv.WithLead && leadX+leadY > 0 && dist(mv.X, mv.Y, leadX, leadY) > togetherCells() {
					mv.WithLead = false // "together" means close to the leader, not just the same map
				}
				if mv.Online {
					lvSum += mv.Level
					lvN++
				}
				v.Members = append(v.Members, mv)
			}
			for _, m := range v.Members {
				if m.Online {
					v.Online++
					if m.WithLead {
						v.Together++
					}
				}
			}
			if lvN > 0 {
				v.AvgLevel = float64(lvSum) / float64(lvN)
			}
			v.GearTurn, v.GearLog = gearPoolView(p.Name)
			res = append(res, v)
		}
		// the main instance (-exclude) adds the human-led parties the core instance runs (Ayla's), unless it has them
		if r.URL.Query().Get("local") == "" {
			res = mergeCoreParties(res)
		}
		// human-led parties first, then the biggest groups online
		sort.Slice(res, func(i, j int) bool {
			if (res[i].Human != "") != (res[j].Human != "") {
				return res[i].Human != ""
			}
			if res[i].Online != res[j].Online {
				return res[i].Online > res[j].Online
			}
			return res[i].Name < res[j].Name
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})
}

// core instance parties (instances.go): fetched from its API (?local=1 so it doesn't merge in turn), cached 5 s;
// the last good answer is kept for 2 minutes so a core redeploy doesn't make Ayla's party vanish from the dashboard.
var coreParties struct {
	sync.Mutex
	at, ok time.Time
	list   []partyView
}

var coreClient = &http.Client{Timeout: 1500 * time.Millisecond}

func fetchCoreParties() []partyView {
	base := coreAPIURL()
	if base == "" {
		return nil
	}
	coreParties.Lock()
	defer coreParties.Unlock()
	now := time.Now()
	if now.Sub(coreParties.at) < 5*time.Second {
		return coreParties.list
	}
	coreParties.at = now
	var got []partyView
	resp, err := coreClient.Get(base + "/api/parties/detail?local=1")
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
	}
	if err == nil {
		coreParties.list, coreParties.ok = got, now
	} else if now.Sub(coreParties.ok) > 2*time.Minute {
		coreParties.list = nil
	}
	return coreParties.list
}

// mergeCoreParties adds the core instance's human-led parties that this instance doesn't run itself.
func mergeCoreParties(res []partyView) []partyView {
	have := map[string]bool{}
	for _, v := range res {
		have[v.Name] = true
	}
	for _, v := range fetchCoreParties() {
		if v.Human != "" && !have[v.Name] {
			v.Instance = "core"
			res = append(res, v)
		}
	}
	return res
}
