package main

// stayends (live 2026-11-15): Ayla said "coming to u" -> the priest stayed put ("waiting here, Ayla is coming"); she
// reached it, said "going on together" and walked off - and the priest kept standing until the 60 s order ran out, so
// she walked back for it, again and again. A member's "stay" / "wait" order now ends as soon as the leader is within 6
// cells, or when her distance starts growing (she walks on); the TTL (60 s / 2 min) stays as the last limit. Then the
// normal party follow takes over (the healer keeps within 8 of her).

const stayOverCells = 6

// stayOrderOver: d = the leader's distance now, last = at the previous tick (0 = unknown)
func stayOrderOver(d, last int) bool {
	return d <= stayOverCells || last > 0 && d > last+2
}
