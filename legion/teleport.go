package main

// teleport.go: the Teleport skill (AL_TELEPORT 26) - from a Creamy Card in an accessory or an Acolyte's own skill.
// Owner 2026-11-14 (RULES.md 3b): "every bot should aim to have [the Teleport] card so it can travel fast and avoid
// danger". A bot with Teleport:
//   - escapes a losing fight with it before spending a Fly Wing (danger.go escape);
//   - hunts by teleporting: solo, on its hunting map, with nothing to fight for a few seconds, it teleports to a random
//     cell instead of roaming on foot (ai.go roaming) - the classic "tele-farming" of a real player.
// Casting the skill (not the wing's itemskill) opens the server's warp list (0x0abe, "Random" first for level 1 and
// 2): bot.go answers it with CZ_SELECT_WARPPOINT 0x011b "Random". On a map that forbids teleport the server refuses
// (no position change): the map is noted for 30 minutes and the bot walks / uses wings there.

import (
	"time"
)

const (
	alTeleport   = 26
	itemCreamy   = 4040 // Creamy Card: accessory, Teleport lv1
	teleRoamWait = 6 * time.Second
)

// canSkillTele (b.mu held): Teleport is learned (card or skill), affordable and usable on this map right now.
func (b *Bot) canSkillTele(now time.Time) bool {
	si := b.skills[alTeleport]
	if si == nil || si.Lv <= 0 || b.SP < max(si.SP, 10) || b.Dead || isTown(b.MapName) {
		return false
	}
	if now.Before(b.buffAt["tele:cd"]) || now.Before(b.buffAt["tele:no:"+b.MapName]) || now.Before(b.skillFailUntil[alTeleport]) {
		return false
	}
	return true
}

// skillTele (b.mu held): cast Teleport (random cell on this map); bot.go picks "Random" from the warp list.
func (b *Bot) skillTele(now time.Time, why string) {
	b.buffAt["tele:cd"] = now.Add(4 * time.Second)
	b.teleFrom, b.teleAt = [2]int{b.X, b.Y}, now
	b.teleMap = b.MapName
	b.skillReady = now.Add(800 * time.Millisecond)
	b.target, b.path = 0, nil
	if b.tracing() {
		b.tracef("teleport skill (%s)", why)
	}
	b.event("info", "", "teleport skill: "+why+" on "+b.MapName) // run 6: countable by QA (not only in traces)
	aid, sitting := b.aid, b.Sitting
	go func() {
		if sitting {
			b.stand()
			time.Sleep(300 * time.Millisecond)
		}
		b.useSkill(alTeleport, 1, aid)
	}()
}

// teleCheck (b.mu held, from think): a skill Teleport that left us on the same cell 3 s later did not work (no-teleport
// map, or refused): don't try it on this map for 30 minutes.
func (b *Bot) teleCheck(now time.Time) {
	if b.teleAt.IsZero() || now.Sub(b.teleAt) < 3*time.Second {
		return
	}
	if b.MapName == b.teleMap && b.X == b.teleFrom[0] && b.Y == b.teleFrom[1] {
		b.buffAt["tele:no:"+b.MapName] = now.Add(30 * time.Minute)
	}
	b.teleAt = time.Time{}
}

// warpListReply: CZ_SELECT_WARPPOINT for the server's Teleport warp list (0x0abe: u16 op, u16 len, u16 skill, maps):
// always "Random" (never the save point: that is what Butterfly Wings are for).
func warpListReply(pk []byte) []byte {
	if len(pk) < 6 || int(le.Uint16(pk[4:])) != alTeleport {
		return nil
	}
	name := make([]byte, 16) // MAP_NAME_LENGTH_EXT
	copy(name, "Random")
	return append(newPkt(0x011b).u16(alTeleport).b, name...)
}

// teleHunt (b.mu held, from the roaming branch): a solo hunter with Teleport and nothing to fight for teleRoamWait
// teleports to a random cell of its hunting map instead of walking around. true = it teleported.
func (b *Bot) teleHunt(now time.Time) bool {
	if partyOf(b) != nil || b.roamSince.IsZero() || now.Sub(b.roamSince) < teleRoamWait || !b.canSkillTele(now) {
		return false
	}
	if b.Goal.Map != "" && b.Goal.Map != b.MapName {
		return false // travelling: the route walks on
	}
	if b.MaxSP > 0 && b.SP*100/b.MaxSP < 25 && !b.spCaster() {
		return false // keep the SP for a teleport out of trouble
	}
	b.roamSince = now // a fresh look around after landing
	b.skillTele(now, "looking for monsters")
	b.setState("teleporting to find monsters")
	return true
}
