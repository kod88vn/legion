package main

// meterclass.go: which time bucket an engine state belongs to (pure; definitions in docs/design/meter.md).

import "strings"

// meterCtx: the facts beside the free-text state that the classification needs (read under b.mu by meterCtx()).
type meterCtx struct {
	dead      bool
	town      bool // the bot stands in a town
	onHunt    bool // a field/dungeon that is its goal map (or it has no goal map)
	hasTarget bool
	stuck     bool // the stuck watchdog's clock (rally.go stuckWatch) has run 30 s+
}

// state prefixes per bucket, checked in this order (the first match wins)
var meterRules = []struct {
	bucket int
	pre    []string // prefixes
	sub    []string // substrings
}{
	{mbDead, []string{"dead"}, nil},
	{mbStuck, []string{"walled in", "no route to"}, []string{"(can't get there)"}},
	{mbIdle, []string{"afk", "hanging out", "idle in", "offline", "login failed"}, nil},
	// trading and the merchant's work before "waiting" ("waiting for X to trade" is a trade)
	{mbTown, []string{"town trip", "shopping", "vending", "vendor", "trading", "going to trade", "storing", "saving at",
		"renting", "refining", "at the refiner", "going to the refiner", "brewing", "compounding", "identifying",
		"loading the cart", "unloading", "talking to the Hypnotist", "closing shop", "buying ", "setting up shop",
		"restocking", "walking to market", "going to Dietrich", "going to the Alchemist", "at the Alchemist",
		"going to the Peco"}, []string{" to trade", " to buy from ", "'s shop"}},
	{mbFight, []string{"standing up to "}, nil}, // to heal / cure a member
	{mbRest, []string{"resting", "sitting", "guarding ", "low HP", "poisoned (", "curing myself", "drinking",
		"standing up", "rest over"}, nil},
	{mbFight, []string{"fighting", "engaging", "tanking", "kiting", "pulling", "provoking", "taking ", "baiting",
		"defending", "assisting", "casting", "area skill", "Heal on", "playing", "Grand Cross", "Fury", "Asura",
		"Mental Strength", "luring", "holding ", "bringing ", "letting ", "protecting", "coming to help",
		"coming to protect", "looting", "healing", "Kyrie", "Assumptio", "Aid Potion", "buffing", "curing "}, nil},
	{mbWait, []string{"waiting", "rally:", "collecting", "gathering", "regrouping", "catching up", "looking for",
		"running to", "leading the way", "going back to"}, []string{" to cross "}},
	{mbTravel, []string{"travel ->", "going to the Kafra", "teleported away", "fled to town", "running away",
		"keeping away", "poisoned with no cure"}, nil},
	{mbSearch, []string{"roaming", "teleporting to find monsters"}, nil},
}

// meterBucketOf: the time bucket for a state string in its context.
func meterBucketOf(state string, c meterCtx) int {
	if c.dead {
		return mbDead
	}
	if c.stuck {
		return mbStuck
	}
	for _, r := range meterRules {
		for _, p := range r.pre {
			if strings.HasPrefix(state, p) {
				return r.bucket
			}
		}
		for _, s := range r.sub {
			if strings.Contains(state, s) {
				return r.bucket
			}
		}
	}
	// with the party / following the leader: on the hunting map that is the party looking for monsters
	if strings.HasPrefix(state, "with ") || strings.HasPrefix(state, "following ") {
		switch {
		case c.town:
			return mbTown
		case c.onHunt:
			return mbSearch
		}
		return mbTravel
	}
	// anything else (skill names, new states): by context
	switch {
	case c.hasTarget:
		return mbFight
	case c.town:
		return mbTown
	case c.onHunt:
		return mbSearch
	}
	return mbTravel
}
