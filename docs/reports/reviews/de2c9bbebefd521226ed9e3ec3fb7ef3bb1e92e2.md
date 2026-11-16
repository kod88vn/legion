# Retroactive review of rest split / kafra chain de2c9bb (merge e8ad043, fix/rest-kafra)

Reviewer: Sonnet. Constitution 4.2 (retroactive). Diff `e8ad043^1...e8ad043^2`, compared with main HEAD 3aca736 (heal.go
gained a later unrelated hunk). Scratch copy of main, gocheck on rest / kafra chain tests: vet ok, pass.

Change: no low-HP rest at full HP after a death, SP wait split out (restsplit.go, heal.go), post-death entry line 0.8;
party-follow Kafra chain across a moved leader (kafrachain.go kafraChainOK, kafra.go).

Findings
- restsplit.go:29-47 spRestBeforeMoving returns false when underAttack or poisoned, so no sitting under fire; the rest clears
  on the existing stand-up path in lowHPGuard once SP is back. A caster may sit up to the 10-min rejoin window after a death
  while the party moves on; intended and bounded.
- minor, restsplit.go:29: no check of the leader's distance; a short SP rest while the party leaves lengthens the gap. The
  Kafra chain and the normal follow close it. Not harmful.
- kafrachain.go:33-43 kafraChainOK: the member-follow exception keeps the cap kafraChainMax, blocks X -> Y -> X, and needs a
  goal; solo bots keep the plain cap. TestKafraChainFollowsMovedLeader covers the four cases and passes.
- The raw merge diff of this branch shows deletions of savepoints.go / humanrosterdrift.go only because the branch predates
  them; both exist on main (checked), so no regression.
- G18, G20, G25: no chat lines, no kicks or resets, same rules for every leader.

No defect remains on main.

verdict: merge
