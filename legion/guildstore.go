package main

// guildstore.go (rule GD05, restored 2026-11-14): guild stock is kept in guild storage, or in the leader's or the
// treasurer's (the guild trader's) own Kafra storage while guild storage is not unlocked. Legion has no guild-storage
// I/O and no guild here has Guild Storage Expansion yet, so the fallback is the rule: what a guild leader or a guild
// vendor puts into its Kafra storage (storage.go onStorageOpen - the engine sends those exact items) is counted as
// guild stock held in storage, per character, and shown in GET /api/guild-stock ("storage"). Informational: the gaps
// and farmer assignments still count only what members can get now (open guild shops), because Legion cannot take
// items back out of storage yet. In memory only (a restart starts the ledger empty).

import (
	"sync"
	"time"
)

var gstore = struct {
	sync.Mutex
	held map[string]map[int]int // character -> item -> units it stored
}{held: map[string]map[int]int{}}

// guildStoreKeeper: may this character hold guild stock in its own storage (leader or treasurer)?
func guildStoreKeeper(inGuild, leader, vendor bool) bool { return inGuild && (leader || vendor) }

// noteGuildStored (b.mu held, storage.go onStorageOpen): the items at these bag indexes go into a guild leader's or
// guild vendor's storage - guild stock in storage (GD05).
func noteGuildStored(b *Bot, idxs []int) {
	if !guildStoreKeeper(b.GuildName != "", b.guildMaster, b.charRole() == "vendor" || b.isHumanGuildTrader(time.Now())) {
		return
	}
	add := map[int]int{}
	for _, idx := range idxs {
		if it := b.inv[idx]; it != nil && it.Count > 0 {
			add[it.ItemID] += it.Count
		}
	}
	guildStoreAdd(b.charName(), add)
}

func guildStoreAdd(char string, add map[int]int) {
	if char == "" || len(add) == 0 {
		return
	}
	gstore.Lock()
	defer gstore.Unlock()
	m := gstore.held[char]
	if m == nil {
		m = map[int]int{}
		gstore.held[char] = m
	}
	for id, n := range add {
		m[id] += n
	}
}

// guildStoreView: per character of this guild snapshot, the guild stock it holds in storage (item names).
func guildStoreView(names []string) map[string]map[string]int {
	gstore.Lock()
	defer gstore.Unlock()
	var out map[string]map[string]int
	for _, n := range names {
		for id, c := range gstore.held[n] {
			if c <= 0 {
				continue
			}
			if out == nil {
				out = map[string]map[string]int{}
			}
			if out[n] == nil {
				out[n] = map[string]int{}
			}
			out[n][itemName(id)] += c
		}
	}
	return out
}
