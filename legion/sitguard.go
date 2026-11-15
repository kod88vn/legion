package main

import "time"

// fightOn (b.mu held): a fight is going on for us - our target is still in sight, or a monster acted on us (hit or
// miss) in the last 6 s. QA 2026-11-14 12:39 (pay_fild01, half the deaths there were Acolytes lv7-19 "killed by
// Willow"): LilRinz, a party Acolyte, sat "resting for SP" twice with its target alive - once mid-fight with a Poring,
// once 2 s after a Willow's hit - and the party priest-sit rule only checked underAttack (a recent damaging hit).
func (b *Bot) fightOn(now time.Time) bool {
	if b.target != 0 {
		if e := b.ents[b.target]; e != nil && e.Type == objMob {
			return true
		}
	}
	for _, e := range b.ents {
		if e.Type == objMob && e.Target == b.aid && now.Sub(e.TargetAt) < 6*time.Second {
			return true
		}
	}
	return false
}
