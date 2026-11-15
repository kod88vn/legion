package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Save points (owner 2026-11-15, EC05/PT03, PR16): the engine always knows each bot's save point (noteSaved keeps it
// in the bot's memory file under legion/data, so a deploy doesn't forget it), every party member saves at the leader's
// Kafra when she asks ("save here", "save at the kafra"), and on a party town run a member whose save point is
// another town wings home first when that is nearer the run town (town.go tripWingOK), then takes the Kafra / walks.

var saveAskRe = regexp.MustCompile(`(?i)\bsave\b[^.!?]*\bkafra\b|\bkafra\b[^.!?]*\bsave\b|\bsave here\b`)

// isSaveAsk: a party line asking everyone to save (not "saved at X", which is a member's report)
func isSaveAsk(line string) bool {
	low := strings.ToLower(line)
	if strings.Contains(low, "saved at") || strings.Contains(low, "saved here") {
		return false
	}
	return saveAskRe.MatchString(low)
}

// noteSaveAsk: the human leader asked the party to save - remember the town she stands in (its Kafra)
func noteSaveAsk(p *Party, line string, now time.Time) {
	if p == nil || p.Human == "" || !isSaveAsk(line) {
		return
	}
	h, ok := humanAt(p.Human)
	if !ok {
		return
	}
	town := h.Map
	if t := townOf(town); t != "" {
		town = t
	}
	if len(kafraSpots[town]) == 0 {
		return
	}
	parties.Lock()
	p.SaveAskTown, p.SaveAskAt = town, now
	parties.Unlock()
}

// saveAskThink (b.mu held): a member in the town the leader asked to save in, not saved there (or not known), goes to
// that town's Kafra and saves (kafra.go Save trip; noteSaved persists it)
func (b *Bot) saveAskThink(p *Party, now time.Time) {
	if p == nil || b.kafra != nil || !now.After(b.kafraNext) {
		return
	}
	parties.Lock()
	town, at := p.SaveAskTown, p.SaveAskAt
	parties.Unlock()
	if town == "" || now.Sub(at) > 3*time.Minute || b.MapName != town {
		return
	}
	if saved, known := b.knownSave(); known && saved == town {
		return
	}
	x, y, save, ok := rallySpot(town, town)
	if !ok || !save {
		return
	}
	b.kafra = &kafraTrip{Save: true, Spot: kafraSpot{x, y}, Town: town, Stage: "walk", Started: now, At: now}
	b.event("party", "", fmt.Sprintf("saving at the %s Kafra (the leader asked the party to save here)", town))
}

// saveWingHelps: a Butterfly Wing to the save town is worth it on a party town run when the save town is nearer the
// run town (in map changes) than the field the bot stands on
func saveWingHelps(hopsSaveToRun, hopsHereToRun int) bool {
	return hopsSaveToRun < 99 && hopsSaveToRun < hopsHereToRun
}
