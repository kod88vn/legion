package main

// A human-played party leader's own stats (Ayla, via OpenKore). Legion's bots only see a partymate's position and
// HP fraction, so the party dashboard showed her HP/SP/zeny as unknown. Her OpenKore plugin
// openkore-ayla/plugins/cmdfile/statusout.pl writes logs/status.json every ~3 s; docker-compose mounts
// openkore-ayla/logs read-only at /ayla in legion and legion-core. A file older than 30 s is ignored.

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

type humanStatus struct {
	Name      string        `json:"name"`
	Online    bool          `json:"online"`
	HP        int           `json:"hp"`
	MaxHP     int           `json:"max_hp"`
	SP        int           `json:"sp"`
	MaxSP     int           `json:"max_sp"`
	Zeny      int           `json:"zeny"`
	Weight    float64       `json:"weight"`     // OpenKore keeps the client's figure (server units / 10)
	MaxWeight float64       `json:"max_weight"` // same
	BaseLevel int           `json:"base_level"`
	JobLevel  int           `json:"job_level"`
	Map       string        `json:"map"`
	LockMap   string        `json:"lock_map"` // her hunting map (partylead.pl $huntMap): the party's destination
	X         int           `json:"x"`
	Y         int           `json:"y"`
	TS        int64         `json:"ts"`
	SaveMap   string        `json:"save_map"` // statusout.pl: her save point (partylead.pl sets it on a verified Kafra save)
	Sitting   bool          `json:"sitting"`  // statusout.pl: she is sitting (resting) - the friends hold position
	Guild     string        `json:"guild"`    // statusout.pl: her guild name ("" = none) - humanguild.go
	JobID     int           `json:"job_id"`   // statusout.pl: her job (class id) - humanguild.go recruits the jobs she lacks
	Dest      string        `json:"dest"`     // statusout.pl: where she is heading (goto map, rally town, lockMap; "" = there) - partydest.go
	TownRun   bool          `json:"town_run"` // statusout.pl: she is on a town run (sell/buy/storage, the Kafra meet after it)
	Effects   []humanEffect `json:"effects"`  // statusout.pl: OpenKore $char->{statuses} (effects.go)
}

var humanStats struct {
	sync.Mutex
	at  time.Time
	got humanStatus
	ok  bool
}

func humanStatusPath() string {
	if p := os.Getenv("LEGION_HUMAN_STATUS"); p != "" {
		return p
	}
	return "/ayla/status.json"
}

// humanStatusOf returns the human's own stats if the status file is about this character and fresh (< 30 s).
func humanStatusOf(name string) (humanStatus, bool) {
	humanStats.Lock()
	defer humanStats.Unlock()
	now := time.Now()
	if now.Sub(humanStats.at) >= 2*time.Second {
		humanStats.at = now
		humanStats.ok = false
		if raw, err := os.ReadFile(humanStatusPath()); err == nil {
			var s humanStatus
			if json.Unmarshal(raw, &s) == nil {
				humanStats.got, humanStats.ok = s, true
			}
		}
	}
	s := humanStats.got
	if !humanStats.ok || !strings.EqualFold(s.Name, name) || now.Sub(time.Unix(s.TS, 0)) > 30*time.Second {
		return humanStatus{}, false
	}
	return s, true
}

// fillHumanStats puts a human leader's own stats into the party dashboard row (partydash.go).
func fillHumanStats(hm *partyMemberView, s humanStatus) {
	if s.MaxHP > 0 {
		hm.HP = float64(s.HP) / float64(s.MaxHP)
	}
	if s.MaxSP > 0 {
		hm.SP = float64(s.SP) / float64(s.MaxSP)
	}
	hm.Zeny = s.Zeny
	hm.Weight, hm.MaxWt = int(s.Weight*10+0.5), int(s.MaxWeight*10+0.5) // back to server units like the bots
	if s.BaseLevel > 0 {
		hm.Level = s.BaseLevel
	}
	if s.JobLevel > 0 {
		hm.JobLevel = s.JobLevel
	}
	hm.HasStats = true
	hm.Effects = humanEffectsView(s, time.Now()) // effects.go
	if hm.Map == "" && s.Map != "" {
		hm.Map, hm.X, hm.Y = s.Map, s.X, s.Y
	}
}
