# Protect the support: audit against AY26 (2026-11-15)

Rule AY26 (owner, 2026-11-15): the party always protects its support. The frontlines stay between the support and the
monsters, the ranged keep the support out of harm's way, and no one leaves the support behind. Branch
`fix/protect-support` from main `a33f812`. Flag `party_protect_support_on` (default OFF); an engine party
(`ovEngineParty`) has it always.

## What the engine already does (main, before this branch)

| Mechanism | Where | What it does for the support |
|---|---|---|
| Support need | together.go `supportNeed` | Names a support in need: "called for help", "being attacked" (Mobbed > 0), "low hp" (< 50%). Humans only when hurt and calling for help. |
| Tank / melee protect | together.go `protectSupport` (party.go ~1048, ~1103) | A tank (melee only when no tank is nearer) walks to a support in need within 28 cells, or takes the monster on it off it. |
| Peel | tanklead.go `tankPeelTarget`, `tankTakeAggro` (Provoke when known); together.go `meleePeel` | Any monster that targets another member within 12 (tank) or 10 (melee) cells is taken off it. |
| Crowd to priest | tanklead.go `tankFallBack` | A tank with 3+ monsters on it walks to the priest when it is more than 5 cells away. |
| Help a mobbed mate | together.go `tankGoHelp` | A mate with 3+ monsters on it, 13-40 cells away: the tank goes to it. |
| Healer stick / escape | partystick.go `stickHealer`, `stickEscape`, `stickMate` | In danger a member runs to the healer / tank / leader, calls for help; Butterfly Wing only when about to die (PT31). |
| Healer reach | healreach.go `healApproach`, `healSpot` | The healer walks to a heal target within spell range. |
| Regroup (leash) | party.go ~755 with together.go `regroupNeeded` | A member beyond together_cells (20) of the leader regroups first, fighting only what hits it (PT04). |
| Leader collect | together.go `needsCollect`, `collectLagDue`; ai.go | The leader fetches a straggler beyond 20 cells; AY22/AY23 (lag wait, priest pace) for Ayla's party. |
| Fight in place | pt32.go `fightInPlace` | A member picks new targets only within 8 cells of itself or a party mate. |
| Rest hold | together.go `restGuard`, `partyRester` | A resting member holds the party still around it. |
| Support death | heal.go `rejoinAfterDeath`, `rejoining` | After a death the member walks back to the party. |

## Gaps against AY26

1. **Frontline does not stand between.** Nothing positions a tank or melee member between a support and an approaching
   monster. Protection starts only once a monster targets the support (peel within 4 s of TargetAt), so the support is
   reached first. -> Gap closed by `guardSpot` (2 cells in front of the support, on the monster's side), flag-gated.
2. **Ranged do not keep out of harm.** Ranged members choose targets by distance and walk where the fight goes; nothing
   keeps them behind the support. -> Gap closed by `backSpot` (3 cells behind the support, away from the monster), only
   when the threat is within 6 cells of the ranged member, flag-gated.
3. **No one leaves the support behind (support-specific).** The leash is the party's 20-cell regroup (together_cells),
   the same for every member. Only Ayla's priest has the 15-cell AY22 pace. -> Gap closed by `supportLeashed`: any
   non-support member on the support's map more than 15 cells away walks to the support first (not while being hit),
   flag-gated.
4. **Whole party falls back to the support.** Only tanks and melee protect a support in need; ranged and other members
   do not move toward it. -> Gap closed by `protectOwnSupport`, which runs for every non-support member in the member
   flow (before the tank's own pulls).
5. **Support is never a lone target.** Not handled: the support's own movement (it keeps its own healing/escape logic).
   Left open; the leash and the frontline guard reduce how often it is alone, nothing forbids it.
6. **Bot leader is outside the member flow.** The bot party leader is handled by the hunt code (ai.go), not by
   `partyThink`, so `protectOwnSupport` does not run for it. Its protection of the support is still the collect and
   leader rules. Left open.
7. **Support death: party stops hunting.** Existing: `rejoinAfterDeath` and the regroup. Not changed; needs a live check.

## Implemented (branch `fix/protect-support`)

- `legion/protectsupport.go` (new): `partyProtectOn`, `supportOf`, `supportLeashed`, `guardSpot`, `backSpot`,
  `protectSpot` (pure), `supportThreat`, `protectOwnSupport`. Constants: leash 15, threat 8, guard gap 2, back gap 3,
  ranged act 6.
- `legion/party.go` `partyThink` (member flow): one call after the rest hold, before the carry logic:
  `if !support && b.protectOwnSupport(p, members, gm, moving, now) { return true }`. Flag off: returns before touching anything.
- `legion/tuning.go`: `PartyProtectSupportOn` (`party_protect_support_on`), default false.
- `legion/protectsupport_test.go` (new): flag off is inert (target unchanged), engine party on with the flag off, the
  leash boundary, guard and back geometry, role rules, support lookup, leash acts with the flag on.

## Needs live evidence (cannot be proven by unit tests)

- Support damage taken per hour, before and after (the frontline guard should cut it).
- Support deaths per hour, before and after.
- Party kills per minute, before and after (the ranged step-back and the leash may cost kills).
- Ping-pong: a ranged member stepping back and the normal fight walking it forward again. The guard and back gaps and
  the act distance (6) are guesses to tune from the logs.

## Revision after review (docs/reports/reviews/01acf19ec5d0d7f930f117fcfe74866a4e9adec8.md)

The numbers above are the first version; this section supersedes them where they differ.

- Hook placement: after the tank's and melee's own jobs (tankFallBack, protectSupport, tankTakeAggro, tankGoHelp,
  meleePeel) and the fight a member is already in; before the assist and ranged fight. It no longer zeroes a target
  that is in melee or whose target is hitting the support (engagedNow).
- Real threat only (supportInDanger): a monster within 3 cells of the support that targets it, or while the support is
  being hit, called for help, or below 70% HP.
- Ranged step-back only while a monster is chasing it (targets it, within 6 cells).
- Hysteresis: one repositioning per 3 s per member (buffAt protspot). The test runs 12 s of ticks with the threat in place
  and asserts 1 to 5 repositionings.
- Leash: 18 cells (not tighter than the 16-cell tank pull range). The existing regroup starts beyond 20 cells (party.go
  and together.go regroupNeeded), so the leash acts between 18 and 20; beyond 20 the regroup owns the walk.
- Walkable check on every spot (a wall spot is refused).
- Nearest support on the map, not the first in the list.
- Off switch: party_protect_support_off (default false) wins over the on flag and over the engine default. Ayla's party
  can be reverted by tuning.
- Bot party leader (engine-era Ayla): the rule protects the SUPPORT, and Ayla is not the support (her members are: the
  priest is the support). Her own hunt is in ai.go, outside the member flow, and AY26 does not ask for her protection.
  So the leader's hunt tick is deliberately not hooked; if the owner wants the leader protected, that is a new rule.
- Tests drive partyThink with real mob entities (protectsupport_test.go). Mutation: disabling the hook in party.go fails
  5 of them (frontline guard, ranged behind, leash, no-ping-pong, lock).
