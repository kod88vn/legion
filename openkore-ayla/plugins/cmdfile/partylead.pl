# partylead - Ayla leads her party instead of running ahead (owner: "I need Ayla bot to be a true party leader,
# protect and guide its party instead of just running ahead"; "she needs to lead, so make her talk").
#
# Every AI tick:
#   - a partymate who is online but on another map, or more than LEASH cells away on this map, makes Ayla HOLD:
#     no random walking, no lockMap routing, current walk cancelled - she waits (and still fights whatever attacks
#     her or a partymate: attackAuto_party 1 in config.txt).
#   - a partymate far away on the same map (> FETCH cells): she walks back toward them.
#   - when everyone online is close again, normal hunting resumes.
#   - her own errands (auto-sell, auto-buy) are never interrupted.
# Voice: she talks in party chat when she waits for someone, when the party is together again, when she levels up,
# before a town errand, in reply to partymates, and now and then while grinding. Short, casual and rate-limited;
# her Haiku mind adds the real conversation on top.
# Friends who are offline are ignored. Settings are changed in memory only (config.txt is untouched).
package partylead;

use strict;
use Plugins;
use Globals qw($char $field %config $net %timeout $playersList %portals_lut $portalsList $monstersList %ai_v %pickupitems);
use AI;
use Commands;
use Log qw(message);
use Utils qw(timeOut blockDistance);
use Time::HiRes qw(time);
use IO::Socket::INET;
use JSON::PP;

use constant LEASH => 9;
use constant FETCH => 16;
use constant TOGETHER => 20; # owner 2026-11-14: "the party is considered together within 20 steps"
use constant RESUME => 12;   # hysteresis (= collect_stop_cells+4): once waiting (a friend > 20 away), carry on only
                             # when everyone is within 12 - stopping and going at 20/21 flip-flopped (QA 2026-11-14)

Plugins::register('partylead', 'Ayla waits for, guards and talks to her party', \&unload);
# every hook and the command run inside eval: a plugin error is logged ("[watchdog] ERROR ...", bug queue) and never
# kills OpenKore (owner 2026-11-14 "Must keep openkore running" - 04:29:58 an ai_route with undef coordinates did)
my $plErrAt = 0;
sub plugin_error {
	my ($where, $err) = @_;
	$err =~ s/\s+$//;
	Log::warning("[watchdog] ERROR in partylead $where: $err
");
	return if time - $plErrAt < 60;
	$plErrAt = time;
	eval { wd_report('ERROR', ($field ? $field->baseName : '-'), "plugin error in $where: $err", 'caught, OpenKore keeps running') };
}
sub guarded { my ($name, $f) = @_; return sub { my @r = eval { $f->(@_) }; plugin_error($name, $@) if $@; return wantarray ? @r : $r[0] } }
# ai_route that never dies: no coordinates -> a plain map route (no noMapRoute); nothing to route to -> skip
sub safe_route {
	my ($m, $x, $y, %a) = @_;
	return unless defined $m && $m ne '';
	if (!defined $x || !defined $y || $x !~ /^\d+$/ || $y !~ /^\d+$/) {
		($x, $y) = (undef, undef);
		return if $a{noMapRoute} || ($field && $m eq $field->baseName); # a cell on this map without a cell: nothing to do
		delete $a{noMapRoute};
	}
	eval { main::ai_route($m, $x, $y, %a) };
	plugin_error("ai_route($m)", $@) if $@;
}
# "Can't loot item...wait..." (someone else's drop under the party's item rules): no retry loop - her pickups pause 15 s
# (queue b1794725893jju: 31 times in 10 min on pay_dun01)
our $lootBlockUntil = 0;
my $logHook = eval { Log::addHook(sub { my (undef, undef, undef, undef, $msg) = @_; $lootBlockUntil = time + 15 if defined $msg && $msg =~ /Can't loot item/ }) };
my $hooks = Plugins::addHooks(['AI_pre', guarded('tick', \&tick_guarded)], ['packet_partyMsg', guarded('heard', \&heard)], ['ai_processRandomWalk', guarded('short_walk', \&short_walk)], ['checkMonsterAutoAttack', guarded('area_target', \&area_target)], ['npc_talk', guarded('npc_said', \&npc_said)], ['target_died', guarded('gm_kill', \&gm_kill)], ['self_died', guarded('gm_died', \&gm_died)]);
my $cmds = Commands::register(['partylead', 'Ayla party lead control: rally cancel | rally <town> | goto <map> | status', guarded('command', \&cmd_guarded)]);
# 'respawn' from outside (cmdfile: her helper) goes through respawn_guard (live 06:38: a Butterfly Wing in payon at full
# HP with the party around); the plugin's own STUCK respawn calls Commands::cmdRespawn directly
# 'p' (party chat) from outside (cmdfile: her helper) goes through party_line_guard: a meeting point / map change that
# contradicts her committed PT16 plan is refused (live: "Meet at the prontera kafra, we go to mjolnir 12 instead");
# her own lines (psay -> flush_say) and other plugins' talk pass unchanged
my $pCmd = Commands::register(['p', 'Party chat (guarded by partylead: no meeting points against the plan)', guarded('p', \&party_line_guard)]);
my $respCmd = Commands::register(['respawn', 'Respawn (guarded by partylead: refused in town / with friends near / when not stuck)', guarded('respawn', \&respawn_guard)]);

my ($holding, $saved, $last, $lastMsg, $lastFetch) = (0, undef, 0, 0, 0);
my ($lastSay, $nextIdle, $lastLv, $errand, @pending) = (0, time + 60, 0, 0);
my $townWingAt = 0; # PT22: when she uses the Butterfly Wing of an announced party town run
my $lastTownRun = 0;
my ($collect, $collectAt) = (undef, 0);
my (%awaySince, %helpFrom, %memPos, %sitSince, %farSince, %dangerFrom, %friendSaved);
my ($waitSpot, $waitMap, $waitFarmAt, $waitBackAt) = (undef, '', 0, 0); # wait_farm: where she waits
my $healerSaidAt = 0;
my $helpCheckAt = 0;
my (%townNeed, $townReason) = ((), ''); # friends asking for a town run (5 min window); the real reason of a party town run
my ($collectPos, $collectWhy) = (undef, ''); # the fetch target's last known cell and why she goes
my $diedAt = 0;
my $escapeAt = 0; # the last plugin-driven escape teleport # the last death (the town run after it says so)
my $holdReleasedAt = 0;
my ($travelWant, $travelWantAt) = (0, 0); # travel-mode hysteresis
# owner rule (RULES 3, 2026-11-14: one hunting map for ~2 hours, no shifting directions): the map the mind chose last
# (goto, 'rally <town> <map>', its own 'conf lockMap'). Every return after a hold / rally / town run goes back to it -
# never to a stale earlier map. AY07/AY19 (2026-11-14): with no fresh order from the mind (none for HOME_STALE) she picks
# the map herself (fit_tick: the party's gear turn, else the level / gear table) - a mind's order still wins while fresh.
# $homeBy: who set homeMap ('mind' = an order or a lockMap set from outside, 'auto' = her own choice). $homeAt: when the
# mind last gave (or repeated) it; the map loaded from config has an unknown age (0 = stale at once).
our ($homeMap, $homeAt) = ((defined $config{lockMap} && $config{lockMap} =~ /\S/ && $config{lockMap} !~ /^(prontera|payon|geffen|morocc|alberta|izlude|aldebaran|yuno|comodo|umbala)$/) ? $config{lockMap} : '', 0);
our $homeBy = 'mind';
our $plan; # PT16 map commitment {map, at, expAt}: logs/ayla_plan.json (survives reloads / restarts)
our @planDeaths; # [time, map]
use constant PLAN_SECS => 7200;
sub plan_save { eval { open(my $f, '>', 'logs/ayla_plan.json') or die; print $f JSON::PP->new->canonical->encode($plan || {}); close $f } }
sub plan_load { eval { open(my $f, '<', 'logs/ayla_plan.json') or die; local $/; my $d = JSON::PP->new->decode(<$f>); close $f; $plan = ($d && ref $d eq 'HASH' && $d->{map}) ? $d : undef } }
plan_load();
# the committed map while the 2 h last ('' = none)
sub plan_map { return ($plan && $plan->{map} && time - $plan->{at} < PLAN_SECS) ? $plan->{map} : '' }
our $autoPick = 0; # true while her own choice runs the goto (set_home then marks the map 'auto')
sub set_home { my $m = shift || ''; return if $m eq '' || is_townmap($m); ($homeMap, $homeAt, $homeBy) = ($m, ($autoPick ? $homeAt : time), ($autoPick ? 'auto' : 'mind')) if $m ne $homeMap }
sub refresh_home { my $m = shift || ''; return if $m eq '' || is_townmap($m); ($homeMap, $homeAt, $homeBy) = ($m, time, 'mind') } # a mind's order, even for the same map
my %mobSeen;                                  # sweep: monster spots seen lately ("x,y" -> time)
my ($lastFightAt, $crowdSaidAt) = (time, 0);  # last fight; last "map too crowded" note
my ($saveOkTown, $saveOkAt) = ('', 0); # the Kafra's "saved" line: town and time (npc_said)
# a friend on her map in danger right now: a fight-help call in the last 90 s (party chat), or HP under 50%
sub friend_in_danger {
	my $here = shift;
	return '' unless $char && $char->{party} && $char->{party}{users};
	foreach my $u (values %{$char->{party}{users}}) {
		next if !$u || !$u->{online} || $u->{name} eq $char->{name};
		my $m = friend_map($u); $m =~ s/\.gat$//;
		next if $m ne $here;
		return $u->{name} if $dangerFrom{$u->{name}} && time - $dangerFrom{$u->{name}} < 90;
		return $u->{name} if $u->{hp_max} && $u->{hp} && $u->{hp} / $u->{hp_max} < 0.5;
	}
	return '';
}
my $reloadCmdfile = 0;
my ($lastPos, $stillSince) = ('', time);
my $lastExit = 0;
my ($sellTrigSaved, $sellTrigUntil) = (0, 0); # sell-run loop guard
my $errandStart = 0; # her sell/buy errand (party town run) started
my $townWaitAt = 0;
my $meetAnn = ''; # the destination last announced when setting off with the priest from a town wait
my $meetArrAt = 0; # when she reached the Kafra for the regroup after a town run
my $regroupDoneAt = 0; # when that regroup ended (no new town wait for 5 min: she sets out with the priest) # passing through town: waiting for a friend behind since
my ($meetRouteAt, $meetSaid) = (0, 0); # after a town run: walking to the town's Kafra, announced once
my $errandEnd = 0;  # when her last sell/buy errand finished

# ---- party rally at the Kafra nearest the destination (owner, 2026-11-13: "parties should prefer regrouping at the
# closest Kafra to their intended destination. Save location with Kafra before setting out.")
# The friends (Legion, rally.go) say "meet at the Kafra in <town> (x,y)"; Ayla answers by going to that Kafra, saving
# there (Kafra menu "Save"), waiting until everyone is within 12 cells, then restoring her lockMap and setting out.
# A cross-map split of more than 90 s makes her call the rally herself (it replaced chasing friends map to map:
# she bounced pay_gld <-> moc_fild02). Coordinates = kafraSpots in legion/kafra.go.
my %KAFRA = (prontera => [[152,326],[151,29],[29,207],[282,200],[146,89]], payon => [[181,104],[175,226]],
	geffen => [[120,62],[203,123]], morocc => [[156,97]], alberta => [[28,229],[113,60]], aldebaran => [[143,119]],
	yuno => [[152,187],[327,108],[277,221]], comodo => [[195,150]], umbala => [[87,160]]);
our $huntMap = '';  # the map she hunts on (lockMap, or the one saved while holding) - statusout.pl publishes it
my $gotoMap = '';   # a 'partylead goto' in progress: no holding until she is there
my $gotoRouteAt = 0; # goto_tick: when it last started the route to $gotoMap
my $rally;          # {town, x, y, at, why, saveTries, saveAt, said, routeAt}
my $rallyNext = 0;  # cooldown after a rally
my $departEnded = 0; # when the last departure wait ended (no new one for 5 min)
my (%apiPos, $apiAt);
my ($backWho, $backAt, $backRouteAt) = ('', 0, 0); # back_tick: a friend back in town we hold for
my $depart;         # after a rally: {town, at, routeAt} - she leaves town only with everyone close behind her

my ($fetchMap, $fetchAt) = ('', 0);
my %toldAt; # a strong party: when she last told a friend where she is (instead of fetching it) # going to get a friend who called for help on a nearby map

# the mind's orders through control/commands.txt win over the plugin's own decisions (one owner per decision):
#   partylead rally cancel   - drop the rally at once (and don't start one for 30 min)
#   partylead rally <town>   - meet at that town's Kafra
#   partylead goto <map>     - set lockMap and go hunt there (cancels a rally / hold)
#   partylead status         - print what the plugin is doing
sub cmd_partylead {
	my (undef, $args) = @_;
	$args = '' unless defined $args;
	if ($args =~ /^\s*rally\s+(cancel|off|stop)/i) {
		if ($rally) { end_rally("cancelled by the mind", 2) }
		$depart = undef;
		$rallyNext = time + 1800;
		message "[partylead] rally cancelled by order; no rally for 30 min\n", 'system';
	} elsif ($args =~ /^\s*rally\s+([a-z]+)(?:\s+(\S+))?/i) {
		my ($town, $dest) = (lc $1, defined $2 ? $2 : '');
		my $pc = plan_map();
		# PT16: a committed plan (no real reason to leave it) refuses a rally to any town but the plan map's Kafra town -
		# no rally, no party line (live: the helper's 'rally prontera mjolnir_12' pulled the party out of pay_dun02)
		if (!$autoPick && $pc ne '' && plan_reason() eq '' && $town ne town_of_map($pc)) {
			wd_report('PLAN_KEEP', $town, "rally $town refused: committed to $pc (PT16)", "stays on $pc");
			return;
		}
		if ($dest ne '' && $pc ne '' && $dest ne $pc && plan_reason() eq '') { # PT16
			wd_report('PLAN_KEEP', $dest, "rally $town $dest: destination kept as $pc (committed, PT16)", "rally, then $pc");
			$dest = $pc;
		}
		$rallyNext = 0;
		# the mind's order replaces a rally already under way (QA 2026-11-14: 'partylead rally prontera' was ignored while
		# a Geffen rally ran - start_rally returns early when one exists - and she kept walking to the Geffen Kafra)
		if ($rally && $rally->{town} ne $town) {
			message "[partylead] rally moved from $rally->{town} to $town by order\n", 'system';
			$rally = undef;
		}
		start_rally($town, undef, undef, 'the mind ordered it' . ($dest ? ", then $dest" : ''), 1);
		# a mind-ordered rally replaces the old destination (QA 2026-11-14: after the mind's Payon rally the plugin
		# restored the pre-rally lockMap in_sphinx3 and took the party by Kafra to Morroc): 'rally <town> <map>' sets where
		# the party goes after regrouping; without a map she stays at the rally town until a goto
		if ($rally) {
			$rally->{mind} = 1;
			$rally->{dest} = $dest;
			refresh_home($dest) if $dest ne '';
			$saved = {randomWalk => $config{route_randomWalk}} unless $saved;
			$saved->{lockMap} = $dest;
			$config{lockMap} = '';
			$gotoMap = '';
			message "[partylead] rally $town: after it " . ($dest ? "the party goes to $dest" : "she waits there for a goto") . "\n", 'system';
		}
	} elsif ($args =~ /^\s*goto\s+(\S+)(\s+force)?/i) {
		my ($m, $force) = ($1, $2 ? 1 : 0);
		my $c = plan_map();
		if (!$autoPick && $c ne '' && $m ne $c) { # PT16: one map for ~2 h
			my $why = $force ? 'owner order' : plan_reason();
			if ($why eq '') {
				my $left = int((PLAN_SECS - (time - $plan->{at})) / 60);
				wd_report('PLAN_KEEP', $m, "goto $m ignored: committed to $c for $left more min (PT16; 'partylead goto $m force' = owner order)", "stays on $c");
				return;
			}
			plan_change($m, $why);
		}
		if (my $why = map_dropped($m)) { # a destination whose route kept failing (STUCK) is off for 2 h, for everyone
			wd_report('MIND_MAP', $m, "rejected: goto $m - $why", "keeping " . ($homeMap || '-'));
			return;
		}
		if (!$autoPick && (my $why = mind_map_far($m))) { # the mind / helper sent her somewhere far above the party
			my ($own) = choose_map();
			wd_report('MIND_MAP', $m, "rejected: goto $m - $why", "keeping her own choice " . ($own || $homeMap || '-'));
			return;
		}
		$rally = undef; $depart = undef; $collect = undef; $fetchMap = ''; $gotoMap = $m;
		$backWho = ''; # a mind's order beats holding for a straggler (QA 12:10: 'map pay_dun00' ignored while holding)
		%helpFrom = (); %farSince = (); # old calls for help don't pull her back off the map she was sent to
		$rallyNext = time + 1800;
		$saved = {randomWalk => ($saved ? $saved->{randomWalk} : $config{route_randomWalk}), lockMap => $m};
		$config{lockMap} = $m;
		$holding = 0;
		$config{route_randomWalk} = $saved->{randomWalk};
		$gotoRouteAt = 0; # goto_tick stands her up and starts the route on the next tick
		$autoPick ? set_home($m) : refresh_home($m);
		message "[partylead] going to $m " . ($autoPick ? "(her own choice)" : "by order") . " (lockMap $m)\n", 'system';
		# owner rule (RULES 2, enforced 2026-11-14): nobody enters a hunting ground before saving at the Kafra nearest it.
		# If her save point is another town, the goto starts with a rally at that town's Kafra (save, everyone saves,
		# "everyone saved at X? ok going in"), then the departure walks to $m (lockMap kept in $saved).
		my $st = town_of_map($m);
		my $hereT = $field ? $field->baseName : '';
		my $allNear = 1;
		foreach my $f (online_friends()) { my ($fu) = grep { $_ && $_->{name} eq $f->[0] } values %{$char->{party}{users}}; my ($fp) = $fu ? friend_pos($fu, $hereT) : (); $allNear = 0 if $f->[1] ne $hereT || !$fp || blockDistance($char->{pos_to}, $fp) > 15 }
		# the regroup town: where she is if it's a town, else the destination's town when she is outside any dungeon
		# (QA 20:54: goto from pay_fild08 while the plan said "regroup at payon kafra" - she walked through Payon alone)
		# the meeting town is the Kafra town nearest the DESTINATION, never the town she happens to be in (live 623cf94:
		# a Payon rally for gef_dun01 while a member stood near Geffen) - from another town she travels there first
		my $rgT = is_townmap($hereT) && $hereT eq $st ? $hereT : (!is_dungeon($hereT) && $KAFRA{$st} ? $st : '');
		$hereT = $rgT if $rgT ne '';
		# saved at the destination's Kafra in the last 15 min: no new regroup / save stop there, she goes straight (live
		# 2026-11-14: a repeated 'goto gl_church' restarted the Geffen Kafra rally - save again, wait again)
		my $savedRecently = $saveOkTown eq $st && time - $saveOkAt < 900;
		if ($savedRecently) {
			message "[partylead] goto $m: saved at the $st Kafra " . int((time - $saveOkAt) / 60) . " min ago - no new rally, going straight\n", 'system';
		} elsif (!is_townmap($m) && $rgT ne '' && $KAFRA{$rgT} && !$allNear && ($config{saveMap} || '') eq $st) {
			# in town with the party scattered: regroup at this town's Kafra, then everyone walks out together (the plan's
			# "meet at the kafra" - QA 20:22: she left Payon via pay_fild07, the friends via pay_fild01, all lost)
			$rallyNext = 0;
			start_rally($hereT, undef, undef, "regroup at the $hereT kafra, then $m together", pt30_on() ? 0 : 1);
			if ($rally) { $rally->{thenGoto} = $m; $rally->{mind} = 1; $rally->{dest} = $m; message "[partylead] goto $m: regroup at the $hereT Kafra first\n", 'system'; say_dest($m, $hereT, $rally->{x}, $rally->{y}) }
		} elsif (!is_townmap($m) && $KAFRA{$st} && ($config{saveMap} || '') ne $st) {
			$rallyNext = 0;
			start_rally($st, undef, undef, "save at the $st kafra before $m", pt30_on() ? 0 : 1);
			if ($rally) { $rally->{thenGoto} = $m; message "[partylead] goto $m: first a save stop at the $st Kafra (save point is " . ($config{saveMap} || '?') . ")\n", 'system'; say_dest($m, $st, $rally->{x}, $rally->{y}) }
		}
		say_dest($m) unless $rally || is_townmap($m); # PT30: no regroup stop - go straight there, said once
	} elsif ($args =~ /^\s*reload-cmdfile/i) {
		$reloadCmdfile = 1;
	} else {
		message sprintf("[partylead] holding=%d rally=%s depart=%s collect=%s fetch=%s lockMap=%s next_rally_in=%ds\n",
			$holding, ($rally ? "$rally->{town} $rally->{x},$rally->{y}" : '-'), ($depart ? $depart->{town} : '-'),
			(defined $collect && $char->{party}{users}{$collect} ? $char->{party}{users}{$collect}{name} : '-'), ($fetchMap || '-'), ($config{lockMap} || ($saved ? "($saved->{lockMap})" : '')),
			($rallyNext > time ? $rallyNext - time : 0)), 'system';
	}
}

# cells to a partymate she can SEE right now (undef = not in sight). The party list's positions go stale (a rally
# ended "everyone is at the Kafra" while Echobunny stood 120 cells away at 176,226), so presence checks use sight.
sub seen_dist {
	my $name = shift;
	my $pl = $playersList ? $playersList->getByName($name) : undef;
	return undef unless $pl && $pl->{pos_to} && defined $pl->{pos_to}{x};
	return blockDistance($char->{pos_to}, $pl->{pos_to});
}

# a friend's map: Legion's API when fresh (< 10 s) - the party list's map goes stale (QA 14:3x: "waiting for Echobunny
# (on payon)" while it stood next to her on pay_dun01, so she held and sat for minutes)
sub friend_map {
	my $u = shift;
	return '' unless $u;
	my $a = $apiPos{$u->{name}};
	return $a->{map} if $a && $a->{map} && time - $a->{at} < 30;
	my $m = defined $u->{map} ? $u->{map} : ''; $m =~ s/\.gat$//;
	return $m;
}
# a friend on an announced errand (owner 2026-11-14, RULES 3: a member may leave briefly for a necessary build/class
# step - "brb, renting a peco", "brb getting my weapon" - and the party keeps farming): no hold, wait, fetch or help
# call for it until it says it's back (or 15 min pass)
my %errandFrom;
sub on_errand {
	my $n = shift;
	return 0 unless $errandFrom{$n};
	if (time - $errandFrom{$n} > 900) { delete $errandFrom{$n}; return 0 }
	return 1;
}
# a friend lying dead (Legion's API: 0 HP or a "dead" state, fresh) is not waited for - it respawns and comes back by
# itself (queue b1794719424081: she held 5+ min in geffen "waiting for Zen.fox" after it died)
# her own cell now (interpolated mid-walk; pos_to is where the walk ends)
sub my_pos { my $p = eval { $char->position }; return (ref $p && defined $p->{x}) ? $p : $char->{pos_to} }
# cells to the party's priest on her map (on screen, else Legion's API <= 30 s); 999 = not on her map / unknown
# the party's priest is on another map than $m (by the engine API / party list): its map, else ''
sub priest_elsewhere {
	my $m = shift || '';
	foreach my $u (values %{$char->{party}{users} || {}}) {
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || friend_cls($u->{name}) !~ /priest|acolyte/i;
		my $pm = friend_map($u);
		return $pm if $pm ne '' && $pm ne $m;
	}
	return '';
}
sub party_has_priest_online {
	foreach my $u (values %{$char->{party}{users} || {}}) {
		return 1 if $u && $u->{online} && $u->{name} ne $char->{name} && friend_cls($u->{name}) =~ /priest|acolyte/i;
	}
	return 0;
}
sub priest_dist {
	my $here = $field ? $field->baseName : '';
	my $best = 999;
	foreach my $u (values %{$char->{party}{users} || {}}) {
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || friend_cls($u->{name}) !~ /priest|acolyte/i;
		my ($pos) = friend_pos($u, $here);
		next unless $pos && defined $pos->{x} && friend_map($u) eq $here;
		my $d = blockDistance(my_pos(), $pos);
		$best = $d if $d < $best;
	}
	return $best;
}
sub friend_dead {
	my $a = $apiPos{$_[0] || ''};
	return 0 unless $a && time - ($a->{at} || 0) < 15;
	return 1 if defined $a->{hpf} && $a->{hpf} <= 0;
	return ($a->{state} || '') =~ /\bdead\b|respawn/i ? 1 : 0;
}
my %askedWhere;
sub ask_where { my $n = shift; return if time - ($askedWhere{$n} || 0) < 120; $askedWhere{$n} = time; psay("$n where r u?", 1, 0) }
sub online_friends {
	my @f;
	foreach my $id (keys %{$char->{party}{users}}) {
		my $u = $char->{party}{users}{$id};
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name}) || friend_dead($u->{name});
		my $m = friend_map($u); $m =~ s/\.gat$//;
		push @f, [$u->{name}, $m];
	}
	return @f;
}

# setting out after a rally: walk toward the hunting map only while every online friend is within 15 cells of her
# (on the town map); otherwise stop and wait. Ends when she leaves the town (or after 3 min).
sub depart_tick {
	return 0 unless $depart;
	# hard timeout 2 min: then she goes and the follow logic brings them (QA 2026-11-14 13:39-13:44: the 3 min timeout
	# ended, the town branch at once started a new departure wait, and she waited 8+ min for a priest 2 cells away)
	if ($field->baseName ne $depart->{town} || time - $depart->{at} > 120) {
		message "[partylead] set out from $depart->{town}" . ($field->baseName eq $depart->{town} ? " (stopped waiting after 2 min)" : '') . "\n", 'system';
		$departEnded = time;
		$depart = undef;
		restore();
		return 0;
	}
	return 1 if AI::inQueue('attack');
	my $lm = $homeMap ne '' ? $homeMap : (($saved && $saved->{lockMap}) ? $saved->{lockMap} : ''); # the mind's map
	my @late;
	foreach my $f (online_friends()) {
		# live position: sight, else Legion's API (seen_dist alone missed a priest standing 2 cells away)
		my ($fu) = grep { $_ && $_->{name} eq $f->[0] } values %{$char->{party}{users}};
		my ($pos) = $fu ? friend_pos($fu, $depart->{town}) : ();
		my $a = $apiPos{$f->[0]};
		my $onMap = $f->[1] eq $depart->{town} || ($a && time - $a->{at} < 10 && $a->{map} eq $depart->{town});
		# a strong party (party_strong): on the town map is enough - they follow her out (QA 12:47: 10 min idle in Payon)
		if ($onMap && !$pos) { ask_where($f->[0]); next } # no fresh position: ask, don't wait on a guess
		push @late, $f->[0] if !$onMap || !party_strong() && blockDistance($char->{pos_to}, $pos) > 15;
		# owner rule (enforced 2026-11-14): nobody goes in before saving at this Kafra - wait for the friend's save
		# (Legion reports each friend's save town; the friend saves on its own and says "saved at <town>")
		push @late, $f->[0] if $a && $a->{saved} ne '' && $a->{saved} ne $depart->{town} && $KAFRA{$depart->{town}} && !grep { $_ eq $f->[0] } @late;
	}
	if (@late && time - $depart->{at} > 30 && wait_fields_for($depart->{town})) {
		my $town = $depart->{town};
		$departEnded = time; $depart = undef; restore();
		$config{lockMap} = '' if ($config{lockMap} || '') ne '' && $homeMap ne '';
		return 0 if wait_fight_start($town, "waiting for @late to set out");
		$depart = {town => $town, at => time - 31, routeAt => 0};
	}
	if (@late) {
		if (AI::inQueue('route', 'move')) { AI::clear('route', 'move') }
		if (time - $lastMsg > 20) { $lastMsg = time; message "[partylead] leaving town: waiting for @late to catch up\n", 'system' }
		psay(pick("wait up @late", "come on @late, stay with me", "@late follow me"), 1, 45);
		return 1;
	}
	if ($lm ne '' && (time - $depart->{routeAt} > 10 || !AI::inQueue('route', 'move'))) {
		$depart->{routeAt} = time;
		Commands::run('stand') if $char->{sitting};
		AI::clear('route', 'move');
		safe_route($lm, undef, undef, attackOnRoute => route_aor(), maxRouteTime => 6);
	}
	return 1;
}

# the friends' real positions from Legion's core API (in this container's network namespace): the party list's positions
# go stale or are missing for friends out of sight, and a missing position used to be skipped - she ran ahead while
# they were 20-40 cells behind (QA 2026-11-14, "together" dropped to 1 with holding=0)
$apiAt = 0; # (%apiPos, $apiAt declared at the top: depart_tick uses them)
my (%friendGear, $gearAt) = ((), 0); # friend name -> gear_next from /api/bots/<name> (item, shop, status)
my $gearWhy = '';                     # why the current town run goes to Prontera ('' = her own gear plan)
sub api_get {
	my $path = shift;
	my $sock = IO::Socket::INET->new(PeerAddr => '127.0.0.1', PeerPort => 8091, Proto => 'tcp', Timeout => 0.3) or return undef;
	print $sock "GET $path HTTP/1.0\r\nHost: 127.0.0.1\r\n\r\n";
	my $raw = '';
	my $deadline = time + 0.6;
	while (time < $deadline) {
		my $n = sysread($sock, my $buf, 65536);
		last unless $n;
		$raw .= $buf;
	}
	close $sock;
	my ($body) = $raw =~ /\r\n\r\n(.*)$/s;
	return undef unless $body;
	return eval { JSON::PP::decode_json($body) };
}
sub gear_refresh {
	return if time - $gearAt < 60;
	$gearAt = time;
	foreach my $id (keys %{$char->{party}{users}}) {
		my $u = $char->{party}{users}{$id};
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name}) || friend_dead($u->{name});
		my $n = $u->{name}; $n =~ s/([^A-Za-z0-9._-])/sprintf("%%%02X", ord($1))/ge;
		my $d = api_get("/api/bots/$n") or next;
		$friendGear{$u->{name}} = $d->{gear_next} if ref $d->{gear_next};
	}
}
sub api_refresh {
	return if time - $apiAt < 4;
	$apiAt = time;
	my $sock = IO::Socket::INET->new(PeerAddr => '127.0.0.1', PeerPort => 8091, Proto => 'tcp', Timeout => 0.3) or return;
	$sock->timeout(0.5);
	print $sock "GET /api/human-party?human=$char->{name} HTTP/1.0\r\nHost: 127.0.0.1\r\n\r\n";
	my $raw = '';
	my $deadline = time + 0.6;
	while (time < $deadline) {
		my $n = sysread($sock, my $buf, 65536);
		last unless $n;
		$raw .= $buf;
	}
	close $sock;
	my ($body) = $raw =~ /\r\n\r\n(.*)$/s;
	return unless $body;
	my $d = eval { JSON::PP::decode_json($body) } or return;
	for my $m (@{$d->{members} || []}) {
		next unless $m->{name} && $m->{online};
		$apiPos{$m->{name}} = {map => $m->{map}, x => $m->{x}, y => $m->{y}, state => ($m->{state} || ''), lv => ($m->{level} || 0), saved => ($m->{saved_town} || ''), cls => ($m->{class} || ''), power => ($m->{power} || 0), kills => $m->{kills}, deaths => $m->{deaths}, hpf => (($m->{hp} || '') =~ m{^(\d+)/(\d+)} && $2 ? $1 / $2 : undef), at => time};
	}
}

# the party's healer (Priest / Acolyte line, class from Legion's API) on her map within 15 cells: its name, or ''
sub healer_near {
	my $here = shift;
	foreach my $u (values %{$char->{party}{users} || {}}) {
		next if !$u || !$u->{online} || $u->{name} eq $char->{name};
		my $a = $apiPos{$u->{name}};
		next unless $a && ($a->{cls} || '') =~ /priest|acolyte/i;
		my ($pos) = friend_pos($u, $here);
		return $u->{name} if $pos && friend_map($u) eq $here && blockDistance($char->{pos_to}, $pos) <= 15;
	}
	return '';
}

# a friend's HP fraction: the party list, else Legion's API ("hp":"3588/4075"); undef = unknown
sub friend_hp {
	my $u = shift;
	return $u->{hp} / $u->{hp_max} if $u->{hp_max} && defined $u->{hp} && $u->{hp} > 0;
	my $a = $apiPos{$u->{name}};
	return $a->{hpf} if $a && defined $a->{hpf};
	return undef;
}

# owner 2026-11-14: "with higher level party, the mechanism changes a bit as bots can survive better. Member should run
# straight to party leaders instead of leader has to collect them one by one". A strong party = she and every online
# friend at base level STRONG_LV+ (levels from Legion's API): she doesn't turn back or fetch for a friend that is merely
# far / slow / asking where she is - it comes to her (Legion sends it); she still goes for one that is hurt (< 50% HP).
use constant STRONG_LV => 50;
sub party_strong {
	return 0 unless $char && ($char->{lv} || 0) >= STRONG_LV;
	my $n = 0;
	foreach my $f (online_friends()) {
		my $a = $apiPos{$f->[0]};
		return 0 unless $a && ($a->{lv} || 0) >= STRONG_LV;
		$n++;
	}
	return $n > 0;
}

# best known position of a friend on her map: what she sees, then the API (fresh < 10 s), then the party list
sub friend_pos {
	my ($u, $here) = @_;
	my $pl = $playersList ? $playersList->getByName($u->{name}) : undef;
	if ($pl && $pl->{pos_to} && defined $pl->{pos_to}{x}) { # on screen: exact - where it is now, not where its walk ends
		my $pp = eval { $pl->position };
		return ((ref $pp && defined $pp->{x}) ? $pp : $pl->{pos_to}, $pl);
	}
	my $a = $apiPos{$u->{name}};
	# Legion's API up to 30 s old; OpenKore's party-list position goes stale for minutes (live: "waiting for Zen.fox
	# (214 cells away)" with Zen.fox 2 cells from her) - never used: no fresh position = unknown
	return ({x => $a->{x}, y => $a->{y}}, undef) if $a && time - $a->{at} < 30 && $a->{map} eq $here && $a->{x};
	return (undef, undef);
}

# a friend who died (or went to town alone) never walks back through a dungeon alone: Ayla takes the party out to
# the first map outside the dungeon on the way to it and waits there until it is back with them (QA: Echobunny and
# Zen.fox respawned in Payon and walked back alone through pay_dun00/01's skeletons)
# ($backWho, $backAt, $backRouteAt) are declared near the top (cmd_partylead's goto clears $backWho)
my %lastMapOf;
sub is_dungeon { my $m = shift || ''; return $m =~ /dun\d|_dun|^anthell|^orcsdun|^gl_|^prt_sewb|^mjo_dun|^moc_pryd|^in_sphinx|^treasure/ }
sub is_townmap { my $m = shift || ''; return $m =~ /^(prontera|payon|geffen|morocc|alberta|izlude|aldebaran|yuno|comodo|umbala)$|_in\d*$/ }

sub back_tick {
	my $here = shift;
	# detect: a friend that was out in the field/dungeon shows up in a town (respawn, or a solo trip) while we hunt
	foreach my $id (keys %{$char->{party}{users}}) {
		my $u = $char->{party}{users}{$id};
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name}) || friend_dead($u->{name});
		# only Legion's fresh map counts (QA 20:36: stale party-list maps flagged friends standing ON pay_fild10 as "back in
		# payon" and she sent the priest + 2 more to town while 3 died)
		my $a = $apiPos{$u->{name}};
		next unless $a && time - $a->{at} < 10 && $a->{map};
		my $m = $a->{map}; $m =~ s/\.gat$//;
		my $old = $lastMapOf{$id};
		$lastMapOf{$id} = $m;
		next unless defined $old && $old ne $m && !is_townmap($old) && is_townmap($m);
		next if is_townmap($here) || AI::inQueue('sellAuto', 'buyAuto', 'storageAuto') || $rally || $backWho || $gotoMap ne '';
		# a friend on her map in danger comes first: no rally / hold for the one in town yet (looked at again next tick)
		if (my $d = friend_in_danger($here)) {
			$lastMapOf{$id} = $old;
			next;
		}
		# owner rule (strong party, all lv50+): the member comes back by itself (Kafra + route; Legion sends it) - she
		# neither rallies nor leaves the hunting map, she only tells it where the party is (QA 2026-11-14 18:57: the priest
		# died in the Sphinx, respawned in Prontera, and she walked the whole party out to a Prontera Kafra rally)
		if (party_strong() || healer_near($here)) { # strong party, or the healer is with her: it comes by itself
			my $t = town_of_map($here);
			my $hm = ($homeMap ne '' ? $homeMap : $here); $t = town_of_map($hm);
			psay(pick("$u->{name} come back to $hm, kafra to $t", "$u->{name} we stay on $hm, kafra to $t and come to us"), 1, 0);
			message "[partylead] $u->{name} is back in $m: strong party, it comes to $here by itself (no rally)\n", 'system';
			next;
		}
		# (no rally to the town any more - owner rule "one hunting map, no shifting directions": she waits on her map,
		# farming around her spot, and tells it the way back)
		my $hm2 = ($homeMap ne '' ? $homeMap : $here);
		psay(pick("$u->{name} come back to $hm2, kafra to " . town_of_map($hm2) . ", we wait here", "$u->{name} we hold on $hm2 for u, kafra to " . town_of_map($hm2)), 1, 0);
		($backWho, $backAt, $backRouteAt) = ($u->{name}, time, 0);
		if (!$holding) {
			$saved = {randomWalk => $config{route_randomWalk}, lockMap => $config{lockMap}};
			$holding = 1;
		}
		$config{lockMap} = ''; $config{route_randomWalk} = 0;
		psay(pick("$u->{name} we wait for u, come back to us", "$u->{name} we hold here till ur back", "waiting for $u->{name}, everyone stay"), 1, 0);
		message "[partylead] $u->{name} is back in $m: holding the party until it rejoins\n", 'system';
	}
	return 0 unless $backWho;
	my ($bu) = grep { $_ && $_->{name} eq $backWho } values %{$char->{party}{users}};
	my $bm = $bu ? friend_map($bu) : ''; $bm =~ s/\.gat$//;
	if (!$bu || !$bu->{online} || time - $backAt > 420) {
		message "[partylead] stopped waiting for $backWho\n", 'system';
		$backWho = ''; restore(); return 0;
	}
	if ($bm eq $here) {
		my ($pos) = friend_pos($bu, $here);
		if ($pos && blockDistance($char->{pos_to}, $pos) <= TOGETHER) {
			psay(pick("wb $backWho, lets go", "ok $backWho is back, lets go", "all together again, lets go"), 1, 0);
			message "[partylead] $backWho rejoined, carrying on\n", 'system';
			$backWho = ''; restore(); return 0;
		}
	}
	return 1 if AI::inQueue('attack');
	if (0 && is_dungeon($here) && $bm ne $here) { # (disabled 2026-11-14: no leaving the hunting map for a straggler)
		# leave the dungeon toward it; stop on the first map outside the dungeon - but only with the rest of the party
		# close behind (QA 2026-11-14 13:23: she led out pay_dun01 > pay_dun00 > pay_arche while the priest sat for SP
		# and Echobunny lagged; the priest was left 90+ cells behind asking "where r u"). Waits up to 2 min for them.
		my @late;
		foreach my $f (online_friends()) {
			next if $f->[0] eq $backWho;
			my ($fu) = grep { $_ && $_->{name} eq $f->[0] } values %{$char->{party}{users}};
			my ($pos) = $fu ? friend_pos($fu, $here) : ();
			push @late, $f->[0] if $f->[1] ne $here || !$pos || blockDistance($char->{pos_to}, $pos) > TOGETHER;
		}
		if (@late && time - $backAt < 120) {
			AI::clear('route', 'move') if !wait_farm() && !AI::inQueue('attack') && (AI::is('route') || AI::is('move')) && (!$waitSpot || blockDistance($char->{pos_to}, $waitSpot) <= 4);
			if (time - $lastMsg > 30) { $lastMsg = time; message "[partylead] leaving for $backWho: waiting for @late first\n", 'system' }
			return 1;
		}
		if (time - $backRouteAt > 15 || !AI::inQueue('route', 'move')) {
			$backRouteAt = time;
			Commands::run('stand') if $char->{sitting};
			AI::clear('route', 'move');
			safe_route($bm, undef, undef, attackOnRoute => 1);
		}
	} elsif (!wait_farm() && AI::inQueue('route', 'move') && (!$waitSpot || blockDistance($char->{pos_to}, $waitSpot) <= 4)) {
		AI::clear('route', 'move'); # outside the dungeon (or the friend is on this map): wait here, farming around the spot
	}
	if (time - $lastMsg > 30) { $lastMsg = time; message "[partylead] waiting for $backWho (on $bm) at $here\n", 'system' }
	return 1;
}

# the town a hunting map belongs to (for the rally she calls herself)
sub town_of_map {
	my $m = shift || '';
	return $m if $KAFRA{$m};
	return 'payon' if $m =~ /^pay_/;
	return 'prontera' if $m =~ /^prt_/;
	return 'morocc' if $m =~ /^moc_|^anthell/;
	return 'geffen' if $m =~ /^gef_|^glast|^gl_|^orcsdun/; # gl_church etc. = Glast Heim (QA 2026-11-14: "town run to payon")
	return 'morocc' if $m =~ /^in_sphinx|^moc_pryd/;
	return 'alberta' if $m =~ /^alb/;
	return 'aldebaran' if $m =~ /^alde|^mjolnir|^c_tower/;
	return 'yuno' if $m =~ /^yuno|^ein/;
	return 'comodo' if $m =~ /^cmd_|^beach_dun/;
	return 'umbala' if $m =~ /^um_|^yggdrasil/;
	return 'payon';
}

use constant RALLY_WAIT => 60; # a rally waits at most 60 s for members after her save, then AY22 (owner 2026-11-14)
sub start_rally {
	my ($town, $x, $y, $why, $announce) = @_;
	return unless $KAFRA{$town};
	if ($rally) {
		# the friends named the exact Kafra: use theirs (everyone must stand at the same one)
		($rally->{x}, $rally->{y}) = ($x, $y) if $rally->{town} eq $town && defined $x;
		return;
	}
	return if time < $rallyNext;
	if (!defined $x) {
		my $k = $KAFRA{$town}[0];
		($x, $y) = @$k;
	}
	if (!$holding) {
		$saved = {randomWalk => $config{route_randomWalk}, lockMap => $config{lockMap}};
		$holding = 1;
	}
	$config{lockMap} = ''; $config{route_randomWalk} = 0;
	$collect = undef;
	$depart = undef;
	$rally = {town => $town, x => $x, y => $y, at => time, why => $why, saveTries => 0, saveAt => 0, said => 0, routeAt => 0};
	message "[partylead] rally at the Kafra in $town ($x,$y): $why\n", 'system';
	psay(pick("lets meet at the kafra in $town ($x,$y)", "regroup at the kafra in $town, $x,$y", "meet me at the kafra in $town ($x,$y)"), 1, 0) if $announce;
}

# $how: 1 = everyone met (she says so), 0 = gave up, 2 = the friends said everyone met (no extra line)
our %rallyEndedAt; # town -> when a rally there ended (the meeting-point rule stays off there for 10 min)
sub end_rally {
	my ($why, $how) = @_;
	return unless $rally;
	message "[partylead] rally in $rally->{town} over: $why\n", 'system';
	my $town = $rally->{town};
	$rallyEndedAt{$town} = time;
	my $stay = $rally->{mind} && ($rally->{dest} || '') eq '' && !($saved && ($saved->{lockMap} || '') ne '');
	$rally = undef;
	if ($stay && $how && $field && $field->baseName eq $town) {
		# a mind-ordered rally with no destination: everyone stays at the Kafra until the mind's goto (no departure,
		# no old lockMap)
		$rallyNext = time + 1200;
		$holding = 1; $config{lockMap} = ''; $config{route_randomWalk} = 0;
		psay(pick("all here and saved. wait here, next stop coming", "ok everyone's here, stay at the kafra a sec"), 1, 0) if $how == 1 || $how == 3;
		return;
	}
	$rallyNext = time + ($how ? 1200 : 600); # rallies are rare: once when setting out (owner: 90% farming)
	Commands::run('stand') if $char->{sitting};
	if ($how && $how != 4 && $field && $field->baseName eq $town) { # 4: gave up waiting after the save - no departure wait either
		$depart = {town => $town, at => time, routeAt => 0};
	} else {
		restore();
	}
	psay(pick("all here, let's go", "ok everyone's here, lets go", "all here, lets head out"), 1, 0) if $how == 1;
	# $how 3 = everyone met and saved, the "everyone saved at X? ok going in" line was said
	psay("ok lets just go", 1, 0) if $how == 0;
}

# ---- travel mode: on the way to another map (a mind's goto, a lockMap elsewhere, a rally) she starts no fights - she
# only answers what attacks her (route attackOnRoute 1) - and the party doesn't drag her into its fights. QA 2026-11-14
# 13:03: 'goto gl_church' from pay_dun00 - attackAuto_routeToLock 1 + attackAuto_party 1 made her stop every few steps
# for Zombies the friends pulled. The two switches are changed in memory only and put back on arrival.
my %travelSaved;
our $trivSaved; # attackAuto saved while crossing a trivial map (no assignment: a plugin reload keeps it)
sub travel_mode_tick {
	return unless $char && $field;
	my $here = $field->baseName;
	my $dest = $gotoMap ne '' ? $gotoMap : ($rally ? $rally->{town} : ($config{lockMap} || ''));
	$dest = $homeMap if $dest eq '' && !$rally; # a lockMap cleared for a moment (a wait, a release) is no "travel over"
	# travel over only on her farm map (live 67546b4: "travel over (gef_fild10): fighting as usual" mid-crossing)
	my $on = $dest ne '' && $here ne $dest && !is_townmap($here) ? 1 : 0;
	$on = 0 if $on && $gotoMap eq '' && friend_in_danger($here) && !trivial_transit() && !cross_danger($here); # a friend in danger here: she fights for it (no travel mode) - not on a map far below the party (QA: Zombies on pay_dun00 for a level 81), never on a dangerous crossing
	# crossing a map far below her and the party (trivial_transit): no fights at all there, not even OpenKore's own
	# attackAuto - a level 81 stopped for every pay_dun00 Zombie (12 exp). Saved in a package var: survives a reload.
	my $triv = trivial_transit() && !scalar(AI::ai_getAggressives()) ? 1 : 0; # never attackAuto 0 with monsters on her
	if ($triv && !defined $trivSaved) { $trivSaved = $config{attackAuto}; $config{attackAuto} = 0; message "[partylead] crossing $here (far below the party): no fights here
", 'system' }
	elsif (!$triv && defined $trivSaved) { $config{attackAuto} = $trivSaved; $trivSaved = undef; message "[partylead] off the transit map ($here): attackAuto back
", 'system' }
	# hysteresis: the state must hold 5 s before switching (QA 20:05: travelling / travel over flipped ~10 times in 80 s)
	if ($on != $travelWant) { ($travelWant, $travelWantAt) = ($on, time) }
	return if time - $travelWantAt < 5 && !(!$on && friend_in_danger($here));
	if ($on && !%travelSaved) {
		%travelSaved = (attackAuto_routeToLock => $config{attackAuto_routeToLock}, attackAuto_party => $config{attackAuto_party},
			itemsTakeAuto => $config{itemsTakeAuto}, itemsGatherAuto => $config{itemsGatherAuto});
		$config{attackAuto_routeToLock} = 0;
		$config{attackAuto_party} = 0;
		$config{itemsTakeAuto} = 0; $config{itemsGatherAuto} = 0; # no loot detours in transit (QA 20:5x: the wipe crossing pay_dun01/02)
		message "[partylead] travelling to $dest: no farming on the way, only what comes within 6 cells
", 'system';
	}
	if (!$on && %travelSaved) {
		$config{$_} = $travelSaved{$_} foreach keys %travelSaved;
		%travelSaved = ();
		message "[partylead] travel over ($here): fighting as usual
", 'system';
	}
}

# drives the rally; true while it owns her movement
sub rally_tick {
	return 0 unless $rally;
	my $r = $rally;
	if (time - $r->{at} > 720) { end_rally("timed out after 12 min", 0); return 0 }
	# priority (main 2026-11-14): a friend on her map in danger > her own rally. A self-started rally pauses (the hold /
	# collect logic below the rally goes to that friend); a rally the mind or the friends called keeps going.
	if ($r->{self} && $field->baseName ne $r->{town} && (my $dz = friend_in_danger($field->baseName))) {
		if (time - ($r->{yieldMsg} || 0) > 30) { $r->{yieldMsg} = time; message "[partylead] rally paused: $dz is in danger on " . $field->baseName . "\n", 'system' }
		$r->{at} = time if time - $r->{at} > 600; # don't time the rally out while she helps
		return 0;
	}
	if (AI::inQueue('attack')) {
		# fight back only against a monster that is on HER and close; any other fight (a party member's, attackAuto_party)
		# is dropped - the rally walks on, attackOnRoute => 1 still answers whatever attacks her on the way (QA 12:10: the
		# rally never left pay_dun01 while the tank kept pulling)
		my $ai = AI::findAction('attack');
		my $a = (defined $ai && $ai ne '') ? AI::args($ai) : undef;
		my $t = ($a && $monstersList) ? $monstersList->getByID($a->{ID}) : undef;
		return 1 if $t && ($t->{dmgToYou} || $t->{missedYou}) && $t->{pos_to} && blockDistance($char->{pos_to}, $t->{pos_to}) <= 3;
		AI::clear('attack');
	}
	# the rally owns her movement: a lockMap set meanwhile (the mind's 'conf lockMap', a restore) is kept for afterwards,
	# not followed now (QA 2026-11-14: lockMap gl_church during a Geffen rally - OpenKore routed her toward the church and
	# the rally routed her back to the Kafra every 15 s, back and forth across Geffen for 10+ min)
	if (($config{lockMap} || '') ne '') {
		$saved = {randomWalk => $config{route_randomWalk}} unless $saved;
		$saved->{lockMap} = $config{lockMap};
		set_home($config{lockMap}); # a lockMap set during the rally is the mind's new destination
		$holding = 1;
		$config{lockMap} = '';
		AI::clear('route', 'move') if AI::is('route');
	}
	my $here = $field->baseName;
	my $d = blockDistance($char->{pos_to}, {x => $r->{x}, y => $r->{y}});
	if ($here ne $r->{town} || $d > 4) {
		if (time - $r->{routeAt} > 15 || !AI::inQueue('route', 'move')) {
			$r->{routeAt} = time;
			Commands::run('stand') if $char->{sitting};
			AI::clear('route', 'move');
			safe_route($r->{town}, $r->{x}, $r->{y}, distFromGoal => 3, attackOnRoute => route_aor());
		}
		if (time - $lastMsg > 30) { $lastMsg = time; message "[partylead] rally: going to the Kafra in $r->{town} ($r->{x},$r->{y})\n", 'system' }
		return 1;
	}
	# at the Kafra: save the respawn point here ("Save" is the first Kafra menu entry; matched by name). Owner rule
	# (RULES 2, 2026-11-14 "If they go to pyramid why are their spawn location somewhere other than moroc?"): verified -
	# the Kafra's "...has been saved here" / "Saved." line (npc_said) must come from this town after the try; up to 3 tries
	if (!$r->{saved} && $saveOkTown eq $r->{town} && $saveOkAt >= $r->{saveAt} && $r->{saveTries}) {
		$r->{saved} = 1;
		# file + memory (configModify skips the file write when memory already holds the value - the old rally set it)
		$config{saveMap} = '' if ($config{saveMap} || '') eq $r->{town};
		eval { Misc::configModify('saveMap', $r->{town}, 1) }; $config{saveMap} = $r->{town};
		message "[partylead] rally: respawn point saved at the $r->{town} Kafra (verified)
", 'system';
		psay("saved at $r->{town}", 1, 0);
	}
	if (!$r->{saved} && $r->{saveTries} < 3 && time - $r->{saveAt} > 15 && !AI::inQueue('NPC')) {
		$r->{saveTries}++; $r->{saveAt} = time;
		AI::clear('route', 'move');
		Commands::run('stand') if $char->{sitting};
		$aylagear::quietNpcUntil = time + 15; # the Kafra dialog never pops up in the watch client (owner rule RULES 5)
		Commands::run("talknpc $r->{x} $r->{y} c r~/Save/ n");
		message "[partylead] rally: saving at the Kafra in $r->{town} (try $r->{saveTries})
", 'system';
		return 1;
	}
	return 1 if AI::inQueue('NPC');
	if (!$r->{saved} && $r->{saveTries} >= 3 && time - $r->{saveAt} > 15 && !$r->{saveFailSaid}) {
		$r->{saveFailSaid} = 1;
		message "[partylead] rally: could NOT verify the save at the $r->{town} Kafra after 3 tries
", 'system';
	}
	if (!$r->{said}) { $r->{said} = 1; psay(pick("at the kafra, save here and wait", "at the kafra in $r->{town}, everyone save here pls"), 1, 0) }
	# everyone online within 12 cells of the Kafra, and everyone saved here (friends say "saved at <town>"; 2 min after
	# all arrived without that line she goes anyway - the Legion side may not say it), she too -> confirm and go
	# every rally wait ends at most 60 s after she reached the Kafra, saved or not (live: "IDLE rally (payon) wait 4 min"
	# - the regroup rally skipped the save and the unsaved-members wait ran on); AY22 / pack rules take over
	$r->{arrAt} ||= time;
	if (time - $r->{arrAt} > 60) {
		my $goMap0 = ($saved && ($saved->{lockMap} || '') ne '') ? $saved->{lockMap} : ($r->{thenGoto} || $r->{dest} || '');
		$gotoMap = $goMap0 if $goMap0 ne '' && !is_townmap($goMap0);
		psay($goMap0 ne '' ? "ok we go, $goMap0" : "ok, lets go", 1, 0);
		end_rally("60 s at the Kafra", 4);
		return 0;
	}
	my ($all, @unsaved) = (1);
	foreach my $f (online_friends()) {
		my $pl = $playersList ? $playersList->getByName($f->[0]) : undef;
		$all = 0 if $f->[1] ne $r->{town} || !$pl || !$pl->{pos_to} || !defined $pl->{pos_to}{x}
			|| blockDistance({x => $r->{x}, y => $r->{y}}, $pl->{pos_to}) > 12;
		my $sv = $friendSaved{$f->[0]};
		push @unsaved, $f->[0] unless $r->{friendsDone} || ($sv && $sv->[0] eq $r->{town} && $sv->[1] >= ($r->{at} - 60 < time - 900 ? $r->{at} - 60 : time - 900));
	}
	$r->{allAt} = $all ? ($r->{allAt} || time) : 0;
	my $mineOk = $r->{saved} || ($r->{saveTries} >= 3 && time - $r->{saveAt} > 15);
	$r->{mineAt} ||= time if $mineOk;
	my $goMap = ($saved && ($saved->{lockMap} || '') ne '') ? $saved->{lockMap} : ($r->{thenGoto} || $r->{dest} || '');
	if ($mineOk && !$all && $goMap ne '' && time - $r->{mineAt} > 60 && wait_fields_for($r->{town})) {
		my $town = $r->{town};
		end_rally("saved; waiting for the party by fighting next to $town", 4);
		$config{lockMap} = '' if ($config{lockMap} || '') ne '';
		set_home($goMap) if !is_townmap($goMap);
		return 0 if wait_fight_start($town, "rally wait");
	}
	if ($mineOk && !$all && $goMap eq '' && time - $r->{mineAt} > RALLY_WAIT) { # no destination: the wait ends too
		end_rally("waited " . int(time - $r->{mineAt}) . " s after saving, no destination", 4);
		return 0;
	}
	if ($mineOk && !$all && $goMap ne '' && time - $r->{mineAt} > RALLY_WAIT) {
		# owner 2026-11-14: she sat at the Geffen Kafra most of the time - after 3 min she goes with whoever is there; the
		# others travel to the destination by their own route (Legion PT30 reads her dest)
		my @miss;
		foreach my $f (online_friends()) {
			my $pl = $playersList ? $playersList->getByName($f->[0]) : undef;
			push @miss, $f->[0] if $f->[1] ne $r->{town} || !$pl || !$pl->{pos_to} || !defined $pl->{pos_to}{x}
				|| blockDistance({x => $r->{x}, y => $r->{y}}, $pl->{pos_to}) > 12;
		}
		psay((@miss ? join(', ', @miss) . " meet us on $goMap, we go" : "ok we go, $goMap"), 1, 0);
		$gotoMap = $goMap if !is_townmap($goMap);
		end_rally("waited " . int((time - $r->{mineAt}) / 60) . " min after saving; missing: " . (join(', ', @miss) || '-'), 4);
		return 0;
	}
	if ($all && $mineOk && (!@unsaved || time - $r->{allAt} > 120)) {
		my $next = ($saved && ($saved->{lockMap} || '') ne '') ? "ok going in, $saved->{lockMap}" : "good, wait here for the next stop";
		psay(@unsaved ? "everyone saved at $r->{town}? @unsaved save at the kafra pls. $next" : "everyone saved at $r->{town}? $next", 1, 0);
		end_rally("everyone is at the Kafra" . (@unsaved ? " (no 'saved' from @unsaved)" : ', all saved'), 3);
		return 0;
	}
	if ($all && $mineOk && @unsaved) { # each member asked at most once per 2 min
		my @ask = grep { time - ($r->{asked}{$_} || 0) > 120 } @unsaved;
		if (@ask) { $r->{asked}{$_} = time foreach @ask; psay("@ask save at the kafra here pls, say when saved", 1, 0) }
	}
	my $low = ($char->{hp_max} && $char->{hp} / $char->{hp_max} < 0.5) || ($char->{sp_max} && $char->{sp} / $char->{sp_max} < 0.5);
	if ($low) { Commands::run('sit') if !$char->{sitting} && !AI::inQueue('sitting', 'route', 'move') }
	elsif ($char->{sitting} && !AI::inQueue('sitAuto')) { Commands::run('stand') } # no sitting around at the Kafra (owner 2026-11-14)
	if (time - $lastMsg > 30) { $lastMsg = time; message "[partylead] rally: waiting at the Kafra in $r->{town}\n", 'system' }
	return 1;
}

sub pick { $_[int rand @_] }

# the Kafra confirms a save: "Your Respawn Point ... has been saved here" / "Saved." (rathena functions_kafras.txt)
sub npc_said {
	my (undef, $args) = @_;
	my $m = defined $args->{msg} ? $args->{msg} : '';
	return unless $m =~ /has been saved|^\s*Saved\.?\s*$/i;
	($saveOkTown, $saveOkAt) = ($field ? $field->baseName : '', time);
	message "[partylead] Kafra: respawn point saved in $saveOkTown
", 'system';
}

# short random steps (QA 2026-11-14: OpenKore's random walk picks any cell of the map, so a single "random step" dragged
# her 40-60 cells away from the party): pick a walkable cell within 12 cells of where she stands; the party follows.
use constant WALK_RADIUS => 12;
use constant PORTAL_KEEP => 9; # (was 5; warp areas span up to +-7: pay_dun00 east stairs 184,33 cover x 182-186, y 26-40) her own walk targets stay this far from any portal (QA: a short random step onto the
                               # pay_dun00 -> pay_dun01 stairs looped her between the floors for 12 min)

# portal cells on a map (portals.txt via %portals_lut, plus portals in sight), cached per map
my %portalCache;
sub portals_on {
	my $m = shift;
	unless ($portalCache{$m}) {
		my @p;
		foreach my $k (keys %portals_lut) {
			my $src = $portals_lut{$k}{source} or next;
			push @p, [$src->{x}, $src->{y}] if ($src->{map} || '') eq $m;
		}
		$portalCache{$m} = \@p;
	}
	my @all = @{$portalCache{$m}};
	if ($portalsList && $field && $field->baseName eq $m) {
		foreach my $pt (@{$portalsList->getItems() || []}) {
			push @all, [$pt->{pos}{x}, $pt->{pos}{y}] if $pt->{pos} && defined $pt->{pos}{x};
		}
	}
	return @all;
}
sub near_portal {
	my ($m, $x, $y, $r) = @_;
	$r = PORTAL_KEEP unless defined $r;
	foreach my $p (portals_on($m)) {
		return 1 if abs($p->[0] - $x) <= $r && abs($p->[1] - $y) <= $r;
	}
	return 0;
}
# a walkable cell d1..d2 cells from (cx,cy) that keeps clear of portals
sub free_cell {
	my ($cx, $cy, $d1, $d2) = @_;
	my $m = $field->baseName;
	for (1 .. 60) {
		my ($x, $y) = ($cx + int(rand(2 * $d2 + 1)) - $d2, $cy + int(rand(2 * $d2 + 1)) - $d2);
		next if abs($x - $cx) + abs($y - $cy) < $d1;
		next unless eval { $field->isWalkable($x, $y) };
		next if near_portal($m, $x, $y);
		return ($x, $y);
	}
	return ();
}

# map-change guard: (2) right after arriving next to a portal, step 4-6 cells away from it first;
# (3) 4+ map changes in 2 min between the same two maps -> stand still 20 s, then walk to the middle of the lockMap
my ($lastMapSeen, $freezeUntil, $freezeCenter, $arriveStep) = ('', 0, 0, 0);
my @mapChanges;
sub portal_guard {
	my $here = $field->baseName;
	if ($here ne $lastMapSeen) {
		push @mapChanges, [time, $lastMapSeen, $here] if $lastMapSeen ne '';
		$lastMapSeen = $here;
		@mapChanges = grep { time - $_->[0] < 120 } @mapChanges;
		$arriveStep = 1;
		my %pair;
		$pair{join '|', sort ($_->[1], $_->[2])}++ for @mapChanges;
		my ($top) = sort { $b <=> $a } values %pair;
		if (($top || 0) >= 4 && !$freezeUntil) {
			$freezeUntil = time + 20; $freezeCenter = 1; @mapChanges = ();
			message "[partylead] warp loop ($top map changes in 2 min): standing still 20 s, then to the middle of the map\n", 'system';
		}
	}
	if ($freezeUntil) {
		if (time < $freezeUntil) {
			AI::clear('route', 'move') if !AI::inQueue('attack') && (AI::is('route') || AI::is('move'));
			return 1;
		}
		$freezeUntil = 0;
		if ($freezeCenter) {
			$freezeCenter = 0;
			my ($x, $y) = free_cell(int(($field->width || 200) / 2), int(($field->height || 200) / 2), 0, 25);
			if (defined $x) {
				AI::clear('route', 'move');
				safe_route($here, $x, $y, maxRouteTime => 30, noMapRoute => 1, attackOnRoute => 2);
				message "[partylead] after the warp loop: walking to the middle of $here ($x,$y)\n", 'system';
				$arriveStep = 0;
				return 1;
			}
		}
	}
	if ($arriveStep) {
		$arriveStep = 0;
		my ($cx, $cy) = ($char->{pos_to}{x}, $char->{pos_to}{y});
		if (near_portal($here, $cx, $cy, 4)) {
			my ($x, $y) = free_cell($cx, $cy, 6, 12); # must clear PORTAL_KEEP (9) from a cell next to the portal
			if (defined $x) {
				AI::clear('route', 'move');
				safe_route($here, $x, $y, maxRouteTime => 8, noMapRoute => 1, attackOnRoute => 2);
				message "[partylead] arrived next to a portal: stepping off to $x,$y\n", 'system';
			}
		}
	}
	return 0;
}
# ---- resting together (owner 2026-11-14, RULES 3 "Rest together; no fidgeting"): while a friend sits/rests for SP or
# HP, or she rests herself, the party stays put: she sits too (unless something is fighting), attacks only what comes
# near, and does no collect/step-off/random walk. A friend in danger or asking for help still gets her.
my ($restSat, $restSitAt, $restRW) = (0, 0, undef);
my (%restAnn, $restSaid);
# her own need to rest (owner 2026-11-14: "ayla is just sitting around wayyy too much"; 10:08 the party rested 3x in
# 6 min at her 25% SP line): start below 50% HP / 10% SP, keep going until 80% HP / 40% SP, nothing else (OpenKore
# sitAuto uses the same numbers; Sonic Blow stops at 40% SP, so she rarely gets that low). Her rest is a breather,
# not a party stop: she says "keep killing near me" and the core friends farm within 15 cells (together.go restFarmFor).
sub own_rest {
	return 0 unless $char;
	return 1 if AI::inQueue('sitAuto');
	my $hpp = $char->{hp_max} ? $char->{hp} / $char->{hp_max} : 1;
	my $spp = $char->{sp_max} ? $char->{sp} / $char->{sp_max} : 1;
	return 1 if $hpp < 0.5 || $spp < 0.10;
	return 1 if $char->{sitting} && ($hpp < 0.8 || $spp < 0.4);
	return 0;
}
sub party_resting {
	return 1 if own_rest();
	return 0 unless $char && $char->{party} && $char->{party}{users} && $field;
	foreach my $n (keys %restAnn) { delete $restAnn{$n} if time - $restAnn{$n} > 180 }
	return 1 if %restAnn; # a friend announced a rest and hasn't said "ready" yet
	my $here = $field->baseName;
	foreach my $id (keys %{$char->{party}{users}}) {
		my $u = $char->{party}{users}{$id};
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name}) || friend_dead($u->{name});
		my $a = $apiPos{$u->{name}};
		my $st = ($a && time - $a->{at} < 10) ? ($a->{state} || '') : undef;
		# a friend sitting BECAUSE she rests ("sitting with the party (Ayla resting)", "guarding Ayla") is not a rest of
		# its own - counting it kept both sides sitting forever (her sit -> status.json sitting -> they sit -> she sits)
		next if defined $st && $st =~ /ayla/i;
		return 1 if defined $st && ($a->{map} || '') eq $here && $st =~ /\b(rest|resting|sit|sitting)\b/i;
		my $pl = $playersList ? $playersList->getByName($u->{name}) : undef;
		return 1 if $pl && $pl->{sitting} && !defined $st;
	}
	return 0;
}
# her own rest is short and the friends keep fighting near her (owner: the party must not sit around). The line must
# still read as a rest to Legion (together.go humanRestLineRe: "sitting"), never as a "ready"/"moving" line.
sub breather_line {
	return pick("sitting a sec, keep killing near me", "quick sit for sp, keep killing around me",
		"sitting a sec for sp, you guys keep farming close", "need a sec, sitting. keep killing near me");
}
sub rest_sit {
	my $resting = shift;
	if ($resting) { # no random walk during a rest (put back when it ends)
		$restRW = ($holding && $saved) ? $saved->{randomWalk} : $config{route_randomWalk} unless defined $restRW;
		$config{route_randomWalk} = 0;
	} elsif (defined $restRW) {
		if ($holding && $saved) { $saved->{randomWalk} = $restRW } else { $config{route_randomWalk} = $restRW }
		$restRW = undef;
	}
	if ($resting && own_rest() && !$char->{sitting} && !AI::inQueue('attack', 'NPC', 'route', 'move', 'sellAuto', 'buyAuto', 'storageAuto', 'items_take', 'take')
		&& time - $restSitAt > 10) {
		$restSitAt = time;
		$restSat = 1;
		Commands::run('sit');
		delete $ai_v{sitAuto_forcedBySitCommand}; # a 'sit' command would also stop auto-attack: she must still protect them
		message "[partylead] the party rests: sitting with them\n", 'system';
		if (!$restSaid) { $restSaid = 1; psay(breather_line(), 1, 0) }
	} elsif ((!$resting || !own_rest()) && ($restSat || ($char->{sitting} && !AI::inQueue('sitAuto')))) {
		$restSat = 0;
		Commands::run('stand') if $char->{sitting};
		if ($restSaid) { $restSaid = 0; psay(pick("ok ready, lets go", "all good, moving on", "ready, lets continue", "ok lets go guys"), 1, 0) }
		message "[partylead] rest over: standing up\n", 'system';
	} elsif ($resting && $char->{sitting} && !$restSaid) { # she sat for her own regen (sitAuto): say so too
		$restSaid = 1; psay(breather_line(), 1, 0);
	} elsif (!$resting && $restSaid && !$char->{sitting}) { # her own rest ended by itself
		$restSaid = 0; psay(pick("ok ready, lets go", "all good, moving on", "ready, lets continue"), 1, 0);
	}
}

# ---- sweep the map (owner 2026-11-14, RULES 3 "Sweep the map": "why is ayla walking around the same corner. Does it
# not know to sweep the map?"). The 25-cell grinding anchor (12:43) overcorrected: she circled one corner. Now she
# walks a patrol LOOP through the map's open areas: waypoints on a 20-cell grid (open cells only, 9+ cells clear of
# portals), ordered nearest-neighbour from where she starts and closed back to the start - so she never reverses,
# she just goes round. She fights what she meets (OpenKore attackAuto), skips ahead to a waypoint with 3+ monsters
# near it, and a waypoint she can't reach in 40 s is dropped. The sweep only moves when OpenKore would random-walk:
# a hold (friend > 20 cells), a rest (route_randomWalk 0) or a fight pauses it - the group pace comes for free.
use constant SWEEP_GRID => 20;
my (@sweep, $sweepMap, $sweepIdx, $sweepAt, $sweepBest, %sweepBad) = ((), '', 0, 0, 0, ());
sub sweep_build {
	my ($cx, $cy) = @_;
	my $m = $field->baseName;
	my ($w, $h) = ($field->width || 0, $field->height || 0);
	my @pts;
	for (my $y = SWEEP_GRID / 2; $y < $h; $y += SWEEP_GRID) {
		for (my $x = SWEEP_GRID / 2; $x < $w; $x += SWEEP_GRID) {
			# the most open walkable cell in this grid square (sampled), clear of portals
			my ($bx, $by, $bn);
			for my $dy (-6, -3, 0, 3, 6) { for my $dx (-6, -3, 0, 3, 6) {
				my ($px, $py) = ($x + $dx, $y + $dy);
				next unless eval { $field->isWalkable($px, $py) };
				next if near_portal($m, $px, $py);
				my $n = 0;
				for my $oy (-2 .. 2) { for my $ox (-2 .. 2) { $n++ if eval { $field->isWalkable($px + $ox, $py + $oy) } } }
				($bx, $by, $bn) = ($px, $py, $n) if !defined $bn || $n > $bn;
			} }
			push @pts, [$bx, $by] if defined $bn && $bn >= 18; # open ground, not a 1-cell corridor edge
		}
	}
	# nearest-neighbour loop from her position
	my @loop; my ($px, $py) = ($cx, $cy);
	while (@pts) {
		my ($bi, $bd) = (0, 1e9);
		for my $i (0 .. $#pts) { my $d = abs($pts[$i][0] - $px) + abs($pts[$i][1] - $py); ($bi, $bd) = ($i, $d) if $d < $bd }
		my $p = splice(@pts, $bi, 1);
		push @loop, $p; ($px, $py) = @$p;
	}
	@sweep = @loop; $sweepMap = $m; $sweepIdx = 0; $sweepAt = time; $sweepBest = 1e9; %sweepBad = ();
	message sprintf("[partylead] sweep route on %s: %d waypoints\n", $m, scalar @sweep), 'system';
}
sub sweep_next {
	return unless @sweep;
	my $n = 0;
	do { $sweepIdx = ($sweepIdx + 1) % @sweep; $n++ } while ($sweepBad{$sweepIdx} && $n < @sweep);
	($sweepAt, $sweepBest) = (time, 1e9);
}
# targets: everything she meets, except monsters standing near a portal (no chase onto the stairs)
sub area_target {
	my (undef, $args) = @_;
	return unless $field && $char && $char->{pos_to};
	my $m = $args->{monster} or return;
	return unless $m->{pos_to} && defined $m->{pos_to}{x};
	if (!stick_target($m)) { $args->{return} = 0; return } # PT04/PT06: not beyond 20 cells of the friends near her
	if (!pt32_target($m)) { $args->{return} = 0; return }  # PT32: in place, one or a few at a time
	return unless $huntMap ne '' && $field->baseName eq $huntMap;
	return if $m->{dmgToYou} || $m->{missedYou}; # what hits her is always fought back
	$args->{return} = 0 if near_portal($field->baseName, $m->{pos_to}{x}, $m->{pos_to}{y});
}
sub area_tick { } # (the grinding anchor is gone: the sweep replaced it)

# ---- protect the party first (owner 2026-11-14: "now she forgets to protect the party"): a monster that hit, missed or
# cast on a party member within 15 cells is her target before anything else (OpenKore puts monsters on HER first)
my ($protectAt, $protectID) = (0, '');
sub protect_tick {
	return if time - $protectAt < 1;
	$protectAt = time;
	return unless $char && $field && $monstersList && $char->{pos_to} && !is_townmap($field->baseName);
	# leaving (a rally, a mind's goto, departing): no fights for the party - only a monster on HER, close (QA 12:10: she
	# kept "protecting the party" against Skeletons the tank pulled and never set off for the Payon rally)
	# finish what she is fighting, and don't add monsters when hard pressed (QA 20:14: switching Munak / Archer Skeleton
	# / Bongun every second pulled a crowd and killed her)
	return if AI::inQueue('attack');
	{ my $hpp = $char->{hp_max} ? $char->{hp} / $char->{hp_max} : 1; return if $hpp < 0.5 || scalar(AI::ai_getAggressives()) >= 3 }
	# crossing a map far below the party (trivial_transit): no farming there, but PT07 - a friend being hit right now
	# (a monster that damaged / cast on a party member, within 10 cells) is still helped (2026-11-14: she walked past)
	my $transit = trivial_transit();
	my $leaving = $rally || $gotoMap ne '' || $depart;
	my $cur = '';
	my $ai = AI::findAction('attack');
	if (defined $ai && $ai ne '') { my $a = AI::args($ai); $cur = $a->{ID} if $a }
	my ($best, $bd);
	foreach my $m (@{$monstersList->getItems() || []}) {
		next unless $m->{pos_to} && defined $m->{pos_to}{x} && !$m->{dead};
		next unless $m->{dmgToParty} || $m->{missedToParty} || $m->{castOnToParty};
		my $onFriend = $transit && ($m->{dmgToParty} || $m->{castOnToParty}); # transit: a friend under attack is helped even when leaving
		next if $leaving && !$onFriend && !(($m->{dmgToYou} || $m->{missedYou}) && blockDistance($char->{pos_to}, $m->{pos_to}) <= 3);
		next if near_portal($field->baseName, $m->{pos_to}{x}, $m->{pos_to}{y});
		my $d = blockDistance($char->{pos_to}, $m->{pos_to});
		next if $d > 15;
		next if $transit && !($onFriend && $d <= 10); # transit: only a friend under attack, close
		next if %travelSaved && !$transit && $d > 5; # in transit only what is right at her / the friends next to her (no detours)
		return if $cur ne '' && $m->{ID} eq $cur; # already on one of them
		($best, $bd) = ($m, $d) if !defined $bd || $d < $bd;
	}
	return unless $best;
	Commands::run('stand') if $char->{sitting};
	AI::clear('attack', 'route', 'move');
	main::attack($best->{ID});
	if ($protectID ne $best->{ID}) {
		$protectID = $best->{ID};
		message "[partylead] protecting the party: attacking $best->{name} on a friend ($bd cells)\n", 'system';
	}
}

# the group's pace: no sweep step while a friend on this map is more than 12 cells away (it catches up first)
sub pace_ok {
	# with the healer close the party farms (owner rule): pace = the healer only, the rest catch up by themselves
	return 1 if healer_near($field->baseName);
	foreach my $id (keys %{$char->{party}{users}}) {
		my $u = $char->{party}{users}{$id};
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name}) || friend_dead($u->{name});
		next if friend_map($u) ne $field->baseName;
		my ($pos) = friend_pos($u, $field->baseName);
		return 0 if $pos && blockDistance($char->{pos_to}, $pos) > 12;
	}
	return 1;
}
my $sweepTickAt = 0;
# owner rule (RULES 3, 2026-11-14: "instead of sitting and wait, Ayla could also farm while waiting"): while she waits on
# a field / dungeon (holding for a straggler, waiting at a dungeon exit, the party resting while she doesn't need to)
# she fights monsters within WAIT_FARM cells of her wait spot and walks back to it after each kill - no standing
# around. She sits only for her own rest (own_rest / sitAuto). In town there is nothing to farm: she just waits.
use constant WAIT_FARM => 12;
sub wait_farm {
	my $here = $field->baseName;
	return if is_townmap($here) || $rally || $depart || $gotoMap ne '';
	if (!$waitSpot || $waitMap ne $here || time - $waitFarmAt > 30) { # a new wait: here is the spot
		$waitSpot = {x => $char->{pos_to}{x}, y => $char->{pos_to}{y}}; $waitMap = $here;
	}
	$waitFarmAt = time;
	return if AI::inQueue('attack', 'NPC', 'sellAuto', 'buyAuto', 'storageAuto', 'take', 'items_take', 'skill_use');
	return if own_rest() || AI::inQueue('sitAuto');
	my ($best, $bd);
	foreach my $m (@{$monstersList ? $monstersList->getItems() : []}) {
		next unless $m->{pos_to} && defined $m->{pos_to}{x} && !$m->{dead};
		next if blockDistance($waitSpot, $m->{pos_to}) > WAIT_FARM;
		next if near_portal($here, $m->{pos_to}{x}, $m->{pos_to}{y});
		# not a monster another (non-party) player is fighting (no kill stealing)
		next if $m->{dmgFromPlayer} && grep { my $p = $playersList ? $playersList->getByID($_) : undef; !$p || !$char->{party}{users}{$_} } keys %{$m->{dmgFromPlayer}};
		my $d = blockDistance($char->{pos_to}, $m->{pos_to});
		($best, $bd) = ($m, $d) if !defined $bd || $d < $bd;
	}
	if ($best) {
		Commands::run('stand') if $char->{sitting};
		AI::clear('route', 'move');
		main::attack($best->{ID});
		message "[partylead] farming while waiting: $best->{name} ($bd cells)\n", 'system' if time - $lastMsg > 20;
		return 1;
	}
	# nothing to fight: back to the wait spot if she wandered off chasing a kill
	if (blockDistance($char->{pos_to}, $waitSpot) > 4 && !AI::inQueue('route', 'move') && time - $waitBackAt > 5) {
		$waitBackAt = time;
		safe_route($here, $waitSpot->{x}, $waitSpot->{y}, distFromGoal => 2, maxRouteTime => 10, noMapRoute => 1);
	}
	return 0;
}

sub sweep_tick { # an active farmer never stands idle: no target, nobody resting, not holding -> walk the loop now
	return if time - $sweepTickAt < 2;
	# the party rests and she doesn't need to: she keeps killing what comes near the resting members (owner rules)
	if ($char && $field && !$holding && party_resting() && !own_rest() && !$char->{sitting} && time - $sweepTickAt >= 2) {
		$sweepTickAt = time;
		wait_farm();
		return;
	}
	return unless $char && $field && $huntMap ne '' && $field->baseName eq $huntMap && !$holding && !$char->{sitting};
	return unless AI::isIdle();
	return if party_resting();
	$sweepTickAt = time;
	my %a = (return => 0);
	short_walk(undef, \%a);
}
# the nearest monster spot worth walking to: [x, y] or undef. Skips monsters with a non-party player within 6 cells
# (someone else's fight; a crowded map) and portal cells; remembers spots for 2 min so the sweep heads back there.
sub mob_target {
	my ($m, $cx, $cy) = @_;
	my @others;
	foreach my $p (@{$playersList ? $playersList->getItems() : []}) {
		next unless $p->{pos_to} && defined $p->{pos_to}{x};
		next if $char->{party} && $char->{party}{users} && $char->{party}{users}{$p->{ID}};
		push @others, $p->{pos_to};
	}
	my $crowded = sub { my $q = shift; foreach my $o (@others) { return 1 if blockDistance($o, $q) <= 6 } return 0 };
	my ($best, $bd);
	foreach my $mo (@{$monstersList ? $monstersList->getItems() : []}) {
		next unless $mo->{pos_to} && defined $mo->{pos_to}{x} && !$mo->{dead};
		next if near_portal($m, $mo->{pos_to}{x}, $mo->{pos_to}{y}) || $crowded->($mo->{pos_to});
		$mobSeen{"$mo->{pos_to}{x},$mo->{pos_to}{y}"} = time;
		my $d = blockDistance({x => $cx, y => $cy}, $mo->{pos_to});
		($best, $bd) = ([$mo->{pos_to}{x}, $mo->{pos_to}{y}], $d) if $d <= 30 && (!defined $bd || $d < $bd);
	}
	return $best if $best;
	foreach my $k (keys %mobSeen) {
		if (time - $mobSeen{$k} > 120) { delete $mobSeen{$k}; next }
		my ($x, $y) = split /,/, $k;
		my $d = blockDistance({x => $cx, y => $cy}, {x => $x, y => $y});
		next if $d <= 5 || $crowded->({x => $x, y => $y});
		($best, $bd) = ([$x, $y], $d) if !defined $bd || $d < $bd;
	}
	return $best;
}

# nothing to fight for 60 s on her hunting map with other players around: tell the mind the map is too crowded (it
# picks the map; she doesn't circle forever) - console + logs/alerts.log, every 5 min at most
sub crowd_check {
	my $here = shift;
	$lastFightAt = time if AI::inQueue('attack') || $char->{sitting} || is_townmap($here) || $here ne ($huntMap || '');
	# crowded = on that map, nothing to fight for 5+ min with 6+ other players around her (live 06:03: "pay_dun02 is
	# crowded" sent her across the world while she stood at its entrance on pay_dun01)
	return if time - $lastFightAt < 300 || time - $crowdSaidAt < 300;
	my $n = 0;
	foreach my $p (@{$playersList ? $playersList->getItems() : []}) { $n++ unless $char->{party}{users}{$p->{ID}} }
	$crowdSaidAt = time;
	return if $n < 6; # nothing to fight but no crowd: the sweep / own map choice handles a thin map
	my $line = sprintf("map too crowded: nothing to fight for %ds on %s, %d other players in sight - pick another map", time - $lastFightAt, $here, $n);
	message "[partylead] $line
", 'system';
	if (open(my $f, '>>', 'logs/alerts.log')) { my @l = localtime; printf $f "%02d:%02d:%02d %s
", @l[2, 1, 0], $line; close $f }
	crowd_move($here); # AY19: with no fresh order she moves on herself
}

sub short_walk {
	my (undef, $args) = @_;
	return unless $char && $field && $char->{pos_to} && defined $char->{pos_to}{x};
	my ($cx, $cy) = ($char->{pos_to}{x}, $char->{pos_to}{y});
	my $m = $field->baseName;
	sweep_build($cx, $cy) if $sweepMap ne $m || !@sweep;
	$args->{return} = 1; # never OpenKore's own any-cell random walk
	return unless @sweep;
	return unless !$char->{party} || pace_ok();
	# go where the monsters are (owner 2026-11-14: "ayla just walking around in circle"): a monster in sight that no
	# other player is next to, else a spot where monsters were seen in the last 2 min - before the fixed sweep loop
	if (my $mt = mob_target($m, $cx, $cy)) {
		my ($tx, $ty) = @$mt;
		my $dd = blockDistance({x => $cx, y => $cy}, {x => $tx, y => $ty});
		if ($dd > 12) { ($tx, $ty) = (int($cx + ($tx - $cx) * 12 / $dd), int($cy + ($ty - $cy) * 12 / $dd)) }
		if (eval { $field->isWalkable($tx, $ty) } && !near_portal($m, $tx, $ty)) {
			safe_route($m, $tx, $ty, maxRouteTime => 10, attackOnRoute => 2, noMapRoute => 1, isRandomWalk => 1, distFromGoal => 2);
			return;
		}
	}
	my $wp = $sweep[$sweepIdx];
	my $d = blockDistance({x => $cx, y => $cy}, {x => $wp->[0], y => $wp->[1]});
	if ($d <= 5) { sweep_next(); $wp = $sweep[$sweepIdx]; $d = blockDistance({x => $cx, y => $cy}, {x => $wp->[0], y => $wp->[1]}) }
	# progress check: no step closer for 40 s -> unreachable from here, drop it for this map
	if ($d < $sweepBest) { ($sweepBest, $sweepAt) = ($d, time) }
	elsif (time - $sweepAt > 40) {
		$sweepBad{$sweepIdx} = 1;
		message "[partylead] sweep: waypoint $wp->[0],$wp->[1] unreachable, next\n", 'system';
		sweep_next(); $wp = $sweep[$sweepIdx];
	}
	# skip ahead (never back) to one of the next 3 waypoints that has 3+ monsters around it
	if ($monstersList) {
		for my $k (1 .. 3) {
			my $j = ($sweepIdx + $k) % @sweep;
			next if $sweepBad{$j};
			my $c = 0;
			foreach my $mo (@{$monstersList->getItems() || []}) {
				$c++ if $mo->{pos_to} && defined $mo->{pos_to}{x} && blockDistance($mo->{pos_to}, {x => $sweep[$j][0], y => $sweep[$j][1]}) <= 10;
			}
			if ($c >= 3) { $sweepIdx = $j; ($sweepAt, $sweepBest) = (time, 1e9); $wp = $sweep[$j]; last }
		}
	}
	# never more than ~12 cells per step (the leader never runs ahead; the pace check runs before every step - QA 15:01:
	# one 100-cell leg to a far waypoint left Zen.fox 21 cells behind)
	my ($tx, $ty) = ($wp->[0], $wp->[1]);
	my $dd = blockDistance({x => $cx, y => $cy}, {x => $tx, y => $ty});
	if ($dd > 12) {
		my ($ix, $iy) = (int($cx + ($tx - $cx) * 12 / $dd), int($cy + ($ty - $cy) * 12 / $dd));
		my $found = 0;
		OUTER: for my $r (0 .. 3) { for my $oy (-$r .. $r) { for my $ox (-$r .. $r) {
			if (eval { $field->isWalkable($ix + $ox, $iy + $oy) } && !near_portal($m, $ix + $ox, $iy + $oy)) { ($tx, $ty, $found) = ($ix + $ox, $iy + $oy, 1); last OUTER }
		} } }
	}
	safe_route($m, $tx, $ty, maxRouteTime => 10, attackOnRoute => 2, noMapRoute => 1, isRandomWalk => 1, distFromGoal => 2);
}

# queue a party line; $gap = minimum seconds since her last line
sub psay {
	my ($text, $delay, $gap) = @_;
	return if time - $lastSay < (defined $gap ? $gap : 12);
	$lastSay = time;
	push @pending, [time + (defined $delay ? $delay : 1), $text];
}

# ---- PT30 (2026-11-14): the leader tells the party where to be, once per new destination; members travel there by
# their own route (Legion partydest.go reads "dest" from status.json). conf partylead_dest 0 turns it off.
my ($destSaid, $destSaidAt) = ('', 0);
my $holdInfo; # the current hold: {who, why, since} (tick sets it, status_intent reads it)
my (%farHold, %toldGo); # a friend on another map: since when she holds for it / when she told it where to meet
sub pt30_on { return !defined $config{partylead_dest} || $config{partylead_dest} ne '0' }
sub say_dest {
	my ($m, $meet, $x, $y) = @_;
	return unless pt30_on() && defined $m && $m ne '';
	return if $m eq $destSaid && time - $destSaidAt < 600;
	($destSaid, $destSaidAt) = ($m, time);
	my $line;
	if ($meet && $meet ne $m) {
		my $at = defined $x ? " ($x,$y)" : '';
		$line = pick("heading to $m, meet at the kafra in $meet$at", "next spot $m, meet at the kafra in $meet$at", "new spot: $m. meet at the kafra in $meet$at first");
	} else {
		$line = pick("new spot: $m, see u there", "heading to $m, meet me there", "next: $m, come straight there");
	}
	psay($line, 1, 0);
}
# every ~5 min while a friend is on another map (not the spot): say again where to go and where to meet, with a hint
# for a friend stuck in a dungeon of another area (live 2026-11-15 02:57Z: the priest walked pay_dun01 while she
# waited in Geffen, nobody said a word)
sub dest_remind_tick {
	return unless pt30_on() && $char && $field;
	return if time - $destSaidAt < 300;
	my $here = $field->baseName;
	my $dest = $homeMap ne '' ? $homeMap : (($config{lockMap} || '') ne '' ? $config{lockMap} : ($saved ? ($saved->{lockMap} || '') : ''));
	return if $dest eq '' || is_townmap($dest);
	my @far = grep { $_->[1] ne '' && $_->[1] ne $here && $_->[1] ne $dest } online_friends();
	return unless @far;
	my $kt = town_of_map($dest);
	my $line;
	if ($rally) {
		$line = "going $dest - meet at the kafra in $rally->{town} ($rally->{x},$rally->{y})";
	} elsif ($here eq $dest) {
		$line = pick("im on $dest, come here", "we're on $dest, come straight here");
	} else {
		$line = "going $dest - meet me there" . ($KAFRA{$kt} ? ", kafra to $kt is fastest" : '');
	}
	my ($stuck) = grep { is_dungeon($_->[1]) && town_of_map($_->[1]) ne $kt } @far;
	$line .= ". $stuck->[0], wing out and kafra to $kt" if $stuck && $KAFRA{$kt};
	($destSaid, $destSaidAt) = ($dest, time);
	psay($line, 1, 0);
}
# statusout.pl: where she is heading ("" = there / nothing) and whether she is on a town run
sub status_dest {
	return '' unless $field;
	my $here = $field->baseName;
	my $d = $rally ? $rally->{town} : ($gotoMap ne '' ? $gotoMap : ($config{lockMap} || ($saved ? ($saved->{lockMap} || '') : '')));
	$d = pk_lmhold() || $homeMap || plan_map() if ($d || '') eq ''; # queue b1794712675pfj: lockMap held / cleared for a moment
	return $d eq $here ? '' : $d;
}
# rallysync (Legion rallysync.go): her rally right now - (town, x, y) or () - and whether she is setting out with the
# party; the members wait at her Kafra while she rallies and follow her when it ends (one rally clock for both sides)
sub status_rally { return $rally ? ($rally->{town}, $rally->{x} || 0, $rally->{y} || 0) : () }
sub status_departing { return $depart ? 1 : 0 }
sub status_town_run {
	return 1 if $errand || AI::inQueue('sellAuto', 'buyAuto', 'storageAuto');
	return ($field && is_townmap($field->baseName) && time - $errandEnd < 180) ? 1 : 0;
}

our $ownSay = 0;
# a party line from outside that names a meeting point / another map while a plan is committed: refused
sub party_line_guard {
	my ($sw, $args) = @_;
	$args = '' unless defined $args;
	my $pc = plan_map();
	if (!$ownSay && $pc ne '' && plan_reason() eq '' && plan_line_conflict($args, $pc, town_of_map($pc))) {
		wd_report('PLAN_KEEP', $pc, "party line refused: \"" . substr($args, 0, 120) . "\" (committed to $pc, PT16)", 'not said');
		return;
	}
	return Commands::cmdChat($sw, $args);
}
# pure: a line proposing a meeting point or a map change that names another town or map than the plan map / its town
sub plan_line_conflict {
	my ($line, $pc, $pt) = @_;
	my $low = lc($line || '');
	return 0 unless $low =~ /\b(meet|regroup|gather)\b[^.!?]*\bkafra\b|\bnew spot\b|\bwe go\b|\bgoing to\b|\bkafra in [a-z]+|\binstead\b|\bchange of plan\b/;
	my @towns = $low =~ /\b(prontera|geffen|morocc|alberta|aldebaran|izlude|yuno|comodo|umbala|payon)\b/g;
	return 1 if grep { $_ ne ($pt || '') } @towns;
	(my $pn = lc($pc || '')) =~ s/[ _]//g;
	foreach my $m ($low =~ /\b([a-z]{3,}(?:[ _][a-z]{2,})?[ _]?\d{1,2})\b/g) {
		(my $n = $m) =~ s/[ _]//g;
		next if $n =~ /^(lv|level|base|job)\d/;
		return 1 if $n ne $pn;
	}
	return 0;
}
# her bag at a town run's start / end: weight and the 12 biggest stacks (owner 2026-11-15: town runs every ~28 min,
# she stayed at 46% weight after a run - what stays heavy?) - "[partylead] bag <when>: w N/M (P%): item xA, ..."
sub bag_log {
	my $when = shift;
	return unless $char && $char->inventory;
	my @it = sort { ($b->{amount} || 0) <=> ($a->{amount} || 0) } grep { $_ && !$_->{equipped} } @{$char->inventory->getItems() || []};
	my $pct = $char->{weight_max} ? int(100 * ($char->{weight} || 0) / $char->{weight_max}) : 0;
	message sprintf("[partylead] bag %s: w %d/%d (%d%%): %s\n", $when, $char->{weight} || 0, $char->{weight_max} || 0, $pct,
		join(', ', map { "$_->{name} x$_->{amount}" } @it[0 .. ($#it < 11 ? $#it : 11)])), 'system';
}
sub flush_say {
	return unless @pending && time >= $pending[0][0];
	my $p = shift @pending;
	local $ownSay = 1;
	Commands::run("p $p->[1]");
}

# a partymate said something: answer like a leader would
sub heard {
	my (undef, $args) = @_;
	my $who = defined $args->{MsgUser} ? $args->{MsgUser} : '';
	my $msg = lc(defined $args->{Msg} ? $args->{Msg} : '');
	return if !$char || $who eq '' || $who eq $char->{name};
	# announced rests (owner 2026-11-14, RULES 3 "Announce rests and their end"): "need sp, resting" starts one,
	# "ok ready" ends it; the party rests until every friend that announced a rest has said it's ready (3 min cap)
	if ($msg =~ /\bbrb\b|renting|errand|getting my|going to (buy|get|rent)/ && $msg !~ /town run/) { $errandFrom{$who} = time }
	elsif ($errandFrom{$who} && $msg =~ /\bback\b|omw|coming|done|\bready\b/) { delete $errandFrom{$who} }
	if ($msg =~ /\b(ready|done resting|good to go|lets go|let's go)\b/ && $msg !~ /not ready/) { delete $restAnn{$who} }
	elsif ($msg =~ /need sp|need hp|resting|rest a bit|sp low|low sp|need a sec|give me a moment|sitting/) { $restAnn{$who} = time }
	if ($msg =~ /(?:meet|regroup|gather)\b[^.!?]*?kafra in ([a-z]+)(?:\D+(\d+)\D+(\d+))?/) {
		start_rally($1, $2, $3, "$who called it", 0) unless $field && ($field->baseName eq ($homeMap || $huntMap || '') && $field->baseName ne '');
		return;
	}
	if ($rally && $msg =~ /all here|everyone'?s here|lets just go|let's just go/) {
		# the friends checked everyone (Legion's rally waits for the saves) - she still saves first (rally_tick goes on)
		if ($rally->{saved}) { end_rally("$who says everyone met", 2) } else { $rally->{friendsDone} = 1 }
		return;
	}
	# a friend's save at a Kafra (owner rule: everyone saves at the Kafra nearest the hunting ground before going in)
	if ($msg =~ /\bsaved\b(?:[^.!?]*?\b(?:at|in)\s+(?:the\s+)?([a-z]+))?/ && $msg !~ /\b(not|unsaved|didn'?t|havent|haven't)\b/) {
		my $t = $1 && $KAFRA{$1} ? $1 : ($rally ? $rally->{town} : ($field ? $field->baseName : ''));
		$friendSaved{$who} = [$t, time] if $t;
	}
	return if $rally && $msg =~ /at the kafra/;
	# a friend asks for a town run: the whole party goes (she starts her own sell/restock run; they follow her)
	# only a real REQUEST with a need starts a town run - not friends' replies ("ok town run, omw"), which made her
	# walk to town twice with a bag 22% full
	if ($msg =~ /overweight|bag (is )?full|out of pots|out of potions|no pots left/
		&& $msg !~ /^(ok|k|coming|omw|yes|sure)\b/ && time - $lastTownRun > 600 && !AI::inQueue('sellAuto', 'buyAuto')
		&& $field && $field->baseName !~ /^(payon|prontera|geffen|morocc|alberta)$|_in\d*$/) {
		# one friend's full bag is its own errand (RULES 2: ~90% farming; QA 20:28: 3 party town runs in 40 min): it
		# butterflies to town alone and comes back; the whole party goes only when 2+ friends need it within 5 min
		$townNeed{$who} = time;
		foreach my $n (keys %townNeed) { delete $townNeed{$n} if time - $townNeed{$n} > 300 }
		if (keys(%townNeed) < 2) {
			$errandFrom{$who} = time;
			psay(pick("$who butterfly to town and sell, we keep farming here, come back after", "$who go sell solo real quick (butterfly wing), we hold the spot"), 1, 30);
			return;
		}
		$lastTownRun = time;
		$townReason = join(' + ', sort keys %townNeed) . " need to sell/restock";
		%townNeed = ();
		psay(pick("ok town run, everyone sell and restock", "k, party town run. stay with me"), 1, 0);
		Commands::run('autosell');
		return;
	}
	if ($msg =~ /prontera/ && $msg =~ /town run|next run|can our|can we/) {
		psay(pick("ok $who, next time we shop in prontera", "sure, prontera next time then"), 2, 30); # never the words "town run": the core starts a party run on them (QA 14:39)
		return;
	}
	if ($msg =~ /where r u|where are u|\bhelp\b|\blost\b|cant find|can't find|come get me|\bon me\b/ && $msg !~ /^(omw|ok|k|coming)\b/) { # ("omw, im at 200,156" is a position, not a call - QA 20:05)
		$helpFrom{$who} = time;
		# a call for help in a fight ("help! 6 mobs on me", "dying") is danger: it outranks every self-started rally /
		# fetch and makes her turn back even in a strong party (QA 2026-11-14 18:57: vex/Silent "help! 6 mobs on me" in
		# in_sphinx1, she said "omw" and then walked off to her own Prontera rally)
		# ("21 on me at 148,31" from the priest was not caught by "mobs on me" - QA 19:54; "cant find u" counts too)
		$dangerFrom{$who} = time if $msg =~ /\bon me\b|dying|gonna die|low hp|hp low|heal pls|save me|\bhelp\b|aggro|cant find|can't find|lost u|where r u|where are u/;
		# no "omw" here (QA 19:46-19:50: she said "omw" 4 times to the priest and never went): the hold logic says it
		# when it really sets off ("omw to X"); here only "where?" if she can't place the caller
		$helpCheckAt = 0;
		# she can't come right now (town run / not on its map): say so honestly instead of a promise (QA 14:50-14:56)
		if (AI::inQueue('sellAuto', 'buyAuto', 'storageAuto') || ($field && is_townmap($field->baseName))) {
			psay(pick("$who im in town, 2 min, hold on - fly wing out if its bad", "$who in town rn, back asap. fly wing if u have to"), 1, 60);
		}
		return;
	}
	return if rand() > 0.6;
	my $r;
	if ($msg =~ /brb|sell|pots|shop|buy|overweight|town/) { $r = pick("k $who, we wait", "ok go, we stay close", "sure, hurry back") }
	elsif ($msg =~ /\bback\b|omw|coming/) { $r = pick("wb", "wb $who", "ok lets go") }
	elsif ($msg =~ /heal|hp low|low hp|help/) { $r = pick("coming", "stay behind me", "pot up, im on it") }
	elsif ($msg =~ /where next|following|we stay|wait up/) { $r = pick("stick with me", "follow me, stay close", "right here, dont run off") }
	elsif ($msg =~ /sp low|sitting|need a sec/) { $r = pick("take ur time", "k, sit, we cover u", "np, rest up") }
	elsif ($msg =~ /nice|gg|good|lol|ty|grats|gz/) { $r = pick("hehe", "nice one", ":)", "we doing good") }
	else { return if rand() > 0.5; $r = pick("ok", "yep", "lol", "k") }
	psay($r, 2 + rand(3), 6);
}

# ---- gear plan (2026-11-14; owner: "the party is one mind for gear ... one step at a time", "town runs only for a
# real need", "no back-and-forth trips"). Her next gear step lives in config.txt: gearPlan_item (e.g. Jur [2]),
# gearPlan_zeny, and a buyAuto slot with "gearRun 1" (disabled 1) for it. When a REAL-NEED town run starts (sellAuto/
# buyAuto queued by OpenKore: bag at itemsMaxWeight_sellOrStore, potions low, or a friend's overweight call), she
# doesn't own the item yet and has gearPlan_zeny, this one run goes to the Prontera south-gate market instead of
# Payon: sellAuto_npc and every buyAuto npc are swapped (in memory) to their "pronteraNpc" twin and the gear slot is
# enabled. Everything is put back when the errand ends. Never a trip for gear alone; at most one try per 30 min.
my $gearRun;          # {saved => {key => old value}, at}
my $gearNext = 0;     # no new gear run before this time
sub gear_count {
	my $name = shift;
	return 0 unless $name && $char->inventory;
	return $char->inventory->sumByName($name) || 0; # equipped items are in the inventory too
}
sub gear_owned { return gear_count(shift) > 0 ? 1 : 0 }
sub gear_run_start {
	my $item = $config{gearPlan_item};
	return 0 if $gearRun || time < $gearNext;
	return 0 unless $config{sellAuto_pronteraNpc};
	$gearWhy = '';
	my $own = $item && !gear_owned($item) && ($char->{zeny} || 0) >= ($config{gearPlan_zeny} || 999999999);
	if (!$own) {
		# a friend's funded gear turn item that Payon doesn't sell (QA: Zen.fox / Zendancer84 "next buy: Wand", only
		# at the Prontera market): this real-need town run goes to Prontera so they can buy it (at most every 30 min)
		foreach my $n (sort keys %friendGear) {
			my $g = $friendGear{$n};
			next unless $g && ($g->{shop} || '') =~ /^prontera/ && ($g->{status} || '') =~ /can pay/i;
			$gearWhy = "$n needs a $g->{item}";
			last;
		}
		return 0 unless $gearWhy;
	}
	my %old;
	$old{sellAuto_npc} = $config{sellAuto_npc};
	$config{sellAuto_npc} = $config{sellAuto_pronteraNpc};
	for (my $i = 0; exists $config{"buyAuto_$i"}; $i++) {
		next unless $config{"buyAuto_$i"};
		if ($config{"buyAuto_${i}_pronteraNpc"}) {
			$old{"buyAuto_${i}_npc"} = $config{"buyAuto_${i}_npc"};
			$config{"buyAuto_${i}_npc"} = $config{"buyAuto_${i}_pronteraNpc"};
		}
		if ($config{"buyAuto_${i}_gearRun"} && !gear_owned($config{"buyAuto_$i"})) { # never for an item she owns
			$old{"buyAuto_${i}_disabled"} = $config{"buyAuto_${i}_disabled"};
			$config{"buyAuto_${i}_disabled"} = 0;
		}
	}
	$gearRun = {saved => \%old, at => time, had => ($item ? gear_count($item) : 0)};
	$gearNext = time + 1800;
	message "[partylead] gear plan: this town run goes to the Prontera market for " . ($gearWhy || "$item (zeny $char->{zeny})") . "\n", 'system';
	return 1;
}
sub gear_run_end {
	return unless $gearRun;
	my $old = $gearRun->{saved};
	$config{$_} = $old->{$_} for keys %$old;
	my $item = $config{gearPlan_item} || '';
	my $got = $item && gear_count($item) > ($gearRun->{had} || 0); # really bought on this run (not just owned)
	message "[partylead] gear plan: Prontera run over - " . ($got ? "bought $item" : ($item && gear_owned($item) ? "$item already owned, nothing bought" : "$item not bought (zeny $char->{zeny})")) . "\n", 'system';
	psay(pick("got my new $item :D", "finally got my $item, ty for waiting"), 2, 5) if $got && !$gearWhy;
	# owned now: advance the plan to the next Prontera-only step (gearPlan_next "Item=zeny;Item=zeny")
	if ($item && gear_owned($item)) {
		my @next = grep { /\S/ } split /\s*;\s*/, ($config{gearPlan_next} || '');
		my ($ni, $nz) = @next ? split(/\s*=\s*/, shift @next) : ('', '');
		$config{gearPlan_item} = $ni || '';
		$config{gearPlan_zeny} = $nz || 999999999;
		$config{gearPlan_next} = join(';', @next);
		message "[partylead] gear plan: $item owned, next step " . ($ni || 'none (set gearPlan_item)') . "\n", 'system';
	}
	$gearRun = undef;
}

# a mind's goto drives the trip itself: stand up and route to the map, like the console's 'move <map>' (QA 2026-11-14:
# after 'partylead goto in_sphinx3' at the Morroc Kafra she sat in town with an empty ai_seq for 2+ min - lockMap set,
# but OpenKore never picked it up while she was sitting). Re-started every 10 s while nothing else moves her.
my $idleSince = 0;
sub help_pending {
	my $here = shift;
	foreach my $u (values %{($char->{party} && $char->{party}{users}) || {}}) {
		next if !$u || !$u->{online} || $u->{name} eq $char->{name};
		next unless ($helpFrom{$u->{name}} && time - $helpFrom{$u->{name}} < 120) || ($dangerFrom{$u->{name}} && time - $dangerFrom{$u->{name}} < 90);
		my $m = friend_map($u); $m =~ s/\.gat$//;
		return $u->{name} if $m eq $here;
	}
	return '';
}
sub goto_tick {
	return if $rally || $depart;
	# never leave for the next floor / map while a friend here called for help (main 2026-11-14 19:46: "goto: route to
	# pay_dun03" while half the party was dead or lost on pay_dun01) - the hold logic fetches it first
	if (my $hp = help_pending($field->baseName)) {
		my $ri = AI::findAction('route');
		my $ra = (defined $ri && $ri ne '') ? AI::args($ri) : undef;
		my $rm = $ra && ref $ra eq 'HASH' || ref $ra ? ($ra->{dest} && $ra->{dest}{map} ? $ra->{dest}{map} : '') : '';
		$rm = eval { $rm->baseName } || '' if ref $rm; # Task::Route keeps the Field object, Task::MapRoute the map name
		AI::clear('route', 'move') if $rm ne '' && $rm ne $field->baseName; # a route off this map waits; the fetch stays
		return;
	}
	# the target: the mind's goto at once; or her lockMap when she has done nothing at all (empty ai_seq) for 15 s
	# while not holding for the party (the same stall after a rally / town run)
	my $tgt = $gotoMap;
	if ($tgt eq '' && is_townmap($field->baseName) && time - $errandEnd < 180) { return } # the promised Kafra regroup after a town run comes first
	if ($tgt eq '' && !$holding && ($config{lockMap} || '') ne '' && !party_resting() && !own_rest()) {
		if (AI::action() ne '' && AI::action() ne 'sitting') { $idleSince = 0; return }
		$idleSince ||= time;
		return if time - $idleSince < 15;
		$tgt = $config{lockMap};
	}
	return if $tgt eq '' || $field->baseName eq $tgt;
	return if AI::inQueue('route', 'move', 'attack', 'NPC', 'sellAuto', 'buyAuto', 'storageAuto', 'take', 'items_take');
	return if time - $gotoRouteAt < 10;
	$gotoRouteAt = time;
	$idleSince = 0;
	if ($char->{sitting}) { Commands::run('stand'); AI::clear('sitting') }
	safe_route($tgt, undef, undef, attackOnRoute => route_aor());
	message "[partylead] " . ($tgt eq $gotoMap ? 'goto' : 'idle, not on lockMap') . ": route to $tgt started\n", 'system';
}

# ---- her own map choice (AY07 + AY19, 2026-11-14) ---------------------------------------------------------------------
# With no AI around she still farms where a normal party of her level and gear would: the map fits the PARTY (its
# weakest online member, from Legion's party list, may be at most 6 levels under the map's band), its make-up (a healer
# online = the undead / aggro party dungeons, else the passive open fields) and her gear (a weak weapon for her level
# counts 6 levels lower). During a member's gear turn (Legion's /api/party-turn, the engine picks a drop map safe for the
# party's bots) the party goes to that member's map. A crowded map -> the next fitting one. A mind's order wins while it
# is fresh (refreshed within HOME_STALE); after that her own choice wins (QA 2026-11-14: a stale 'pay_dun03' from the
# mind kept a level 81 Assassin on Zombies for 12 exp each). Rows: [min lv, max lv, map, 1 = needs a healer]. Sources:
# legion/strategy.go guideSpots (the engine's level -> map table) and legionctl mapinfo (mean monster level, aggro).
use constant HOME_STALE => 1800;
use constant FIT_EVERY => 60;
my @FIT_MAPS = ( # preference order: the highest band she qualifies for first (a party farms the newest map it can)
	# 60-99 with a healer: party dungeons (gl_church: Evil Druid / Wraith lv77-80, aggro undead - holy + fire party;
	# c_tower: Clock / Punk lv81-82; alde_dun02: High Orc lv81 + Arclouze lv107; gl_prison: Zombie Prisoner / Injustice
	# lv89-95; gl_chyard: Dark Priest lv98 + Dark Lord MVP)
	[92, 99, 'gl_chyard', 1], [86, 99, 'gl_prison', 1], [84, 95, 'alde_dun02', 1], [72, 89, 'gl_church', 1],
	[76, 92, 'c_tower2', 1], [72, 86, 'c_tower1', 1], [64, 75, 'mjolnir_11', 1], [60, 71, 'gef_dun01', 1], [58, 68, 'orcsdun02', 1],
	# 58-99 with no healer online: open fields near Geffen / Al De Baran (no Juno: OpenKore routed Payon -> Juno through
	# the Izlude airship and stuck on izlude 24,98, an unwalkable cell in its field data - live 2026-11-15)
	[80, 99, 'gef_fild06', 0], [72, 90, 'gef_fild08', 0], [66, 82, 'mjolnir_05', 0], [64, 80, 'mjolnir_03', 0],
	[58, 74, 'mjolnir_12', 0],
	# under 60 (pay_dun03: Sohee lv64 but mean monster level 44 - only to 65)
	[52, 65, 'pay_dun03', 0], [50, 61, 'prt_fild09', 0], [50, 61, 'pay_fild10', 0], [45, 59, 'pay_dun02', 1],
	[40, 51, 'moc_fild03', 0], [40, 49, 'gef_fild03', 1], [32, 47, 'pay_dun01', 1], [25, 39, 'pay_fild09', 0],
	[18, 31, 'pay_dun00', 1], [12, 24, 'prt_fild01', 0], [1, 11, 'prt_fild08', 0],
);
# highest level a map is worth (the transit check: a map 15+ levels under her is crossed, never farmed)
my %MAP_TOP = (pay_dun00 => 32, pay_dun01 => 48, pay_dun02 => 60, pay_dun03 => 70, pay_arche => 30, payon => 1,
	moc_fild01 => 34, moc_fild02 => 34, moc_fild03 => 55, mjolnir_01 => 35, mjolnir_02 => 42, mjolnir_06 => 36,
	mjolnir_07 => 68, mjolnir_08 => 66, mjolnir_09 => 22, mjolnir_10 => 58, mjo_dun01 => 36, mjo_dun02 => 50, mjo_dun03 => 56,
	orcsdun01 => 60, orcsdun02 => 66, gef_dun00 => 56, gef_dun01 => 72, glast_01 => 70,
	gef_fild10 => 60); # aggressive orc packs (live 2026-11-14: 12 orcs, 100% -> 19% HP in 8 s while "crossing" it)
foreach my $r (@FIT_MAPS) { $MAP_TOP{$r->[2]} = $r->[1] if !$MAP_TOP{$r->[2]} || $MAP_TOP{$r->[2]} < $r->[1] }
sub map_top {
	my $m = shift || '';
	return $MAP_TOP{$m} if $MAP_TOP{$m};
	return 24 if $m =~ /^(prt|pay|gef|moc)_fild0[0-8]$/;
	return 44 if $m =~ /^(prt|pay|gef|moc)_fild/;
	return 0; # unknown: never treated as trivial
}
# she is crossing a map far below her level on the way somewhere else: no fights there but what blocks her
sub trivial_transit {
	return 0 unless $char && $field;
	my $here = $field->baseName;
	my $top = map_top($here);
	return 0 if !$top || is_townmap($here);
	return 0 unless ($char->{lv} || 0) >= $top + 20;
	return 0 unless transit_fit_lv() >= $top + 20; # the party's gear-rated level, not her base level (58 vs 81, live 2026-11-14)
	foreach my $f (online_friends()) { my $a = $apiPos{$f->[0]}; return 0 if $a && $a->{lv} && $a->{lv} < $top + 8 } # a low friend still needs her here
	my $dest = $gotoMap ne '' ? $gotoMap : ($config{lockMap} || '');
	$dest = $homeMap if $dest eq '' && !$rally;
	return ($dest ne '' && $dest ne $here) || $rally || $depart ? 1 : 0;
}
# attackOnRoute for a map-to-map route: 2 = she fights what comes into reach on the way (stick_target: within 6 cells,
# pt32_target: one or a few at a time) instead of walking past it into the pack; 0 on a map far below the party
sub route_aor { return trivial_transit() ? 0 : 2 }

my %crowdedAt; # map -> when it was too crowded to farm (skipped for an hour)
my $gmRecheck = 0; # gear maps: the measured step changed - fit_tick re-checks at once
my %routeFailAt; # map -> until when its route counts as failing (STUCK twice): skipped like a crowded map
our %farmedAt;   # map -> when a plan left it (drained / crowded / ...): not picked again for 30 min
# kept in logs/ayla_farmed.json: a reload doesn't send her back to a drained map
# with her kills on the plan map (times, last 15 min) and when she arrived on it: the drained-rate window survives reloads
our (@planKillTimes, %planArr); # %planArr: {map, at}
our $killTrackSince = time; # kill counting runs since this load: no drained verdict before 10 min of it
our $pk_fsAt = 0;
our $pk_drainLog = 0;
our $planKillAt = 0; # her last kill on the plan map
sub farmed_save {
	@planKillTimes = grep { time - $_ < 900 } @planKillTimes;
	eval { open(my $f, '>', 'logs/ayla_farmed.json') or die; print $f JSON::PP->new->canonical->encode({
		farmed => {map { $_ => int($farmedAt{$_}) } grep { time - $farmedAt{$_} < 1800 } keys %farmedAt},
		kills => [map { int($_) } @planKillTimes], arr => {%planArr}}); close $f };
}
eval { if (open(my $f, '<', 'logs/ayla_farmed.json')) { local $/; my $d = JSON::PP->new->decode(<$f>); close $f;
	if (ref $d eq 'HASH' && ref $d->{farmed} eq 'HASH') { %farmedAt = (%farmedAt, %{$d->{farmed}}); @planKillTimes = @{$d->{kills} || []}; %planArr = %{$d->{arr} || {}} }
	elsif (ref $d eq 'HASH') { %farmedAt = (%farmedAt, %$d) } } };
my ($turnAt, $turnMap, $turnWho) = (0, '', '');
sub party_turn {
	return ($turnMap, $turnWho) if time - $turnAt < 60;
	$turnAt = time;
	my $n = $char->{name}; $n =~ s/([^A-Za-z0-9._-])/sprintf("%%%02X", ord($1))/ge;
	my $d = api_get("/api/party-turn?human=$n");
	return ($turnMap, $turnWho) unless $d && ref $d eq 'HASH'; # unreachable: keep the last answer
	($turnMap, $turnWho) = ('', '');
	foreach my $p (@{$d->{parties} || []}) {
		next unless ref $p eq 'HASH' && ($p->{human} || '') eq $char->{name};
		($turnMap, $turnWho) = ($p->{map} || '', $p->{turn} || '') if ($p->{turn} || '') ne '' && ($p->{map} || '') ne '' && !is_townmap($p->{map});
	}
	return ($turnMap, $turnWho);
}
sub party_has_healer {
	return 1 if ($char->{jobID} || 0) =~ /^(4|8|15|4009|4016)$/; # she is the healer
	foreach my $f (online_friends()) { my $a = $apiPos{$f->[0]}; return 1 if $a && ($a->{cls} || '') =~ /priest|acolyte|monk|champion/i }
	return 0;
}
# the party's farming level. AY07 sharpened (2026-11-14): gear counts as much as level - with gear maps on (default;
# config partylead_gearMaps 0 = the old rule) it is the weakest member's GEAR-RATED level (hers from her own kit, the
# friends' from Legion's API "power", their base level when the API is down) plus the healer / tank bonus, capped at
# the weakest base level, moved by the measured step (gm_step_offset). Old rule: her level, at most 6 over the weakest
# online friend; a weak weapon costs 6 levels.
sub party_fit_lv {
	my $lv = $char->{lv} || 1;
	my $gm = defined $config{partylead_gearMaps} ? $config{partylead_gearMaps} : 1;
	if ($gm) {
		my @m = ([gm_self_lv(), $lv, gm_fragile_job($char->{jobID} || 0)]);
		my ($heal, $tank) = (party_has_healer(), gm_tank_job($char->{jobID} || 0));
		foreach my $f (online_friends()) {
			my $a = $apiPos{$f->[0]};
			next unless $a && $a->{lv};
			my $cls = $a->{cls} || '';
			push @m, [($a->{power} || $a->{lv}), $a->{lv}, ($cls =~ /mage|wizard|sage|scholar|professor|alchemist|creator|genetic/i ? 1 : 0)];
			$tank ||= $cls =~ /knight|crusader|paladin/i ? 1 : 0;
		}
		my $plv = gm_party_lv(\@m, $heal, $tank) + gm_step_offset();
		my ($minBase) = sort { $a <=> $b } map { $_->[1] } @m;
		$plv = $minBase if defined $minBase && $plv > $minBase; # the measured step never rates the party over its weakest level
		return $plv < 1 ? 1 : $plv;
	}
	my $min;
	foreach my $f (online_friends()) { my $a = $apiPos{$f->[0]}; next unless $a && $a->{lv}; $min = $a->{lv} if !defined $min || $a->{lv} < $min }
	my $plv = defined $min && $min + 6 < $lv ? $min + 6 : $lv;
	my $atk = ($char->{attack} || 0) + ($char->{attack_bonus} || 0);
	$plv -= 6 if $atk > 0 && $lv >= 30 && $atk < $lv * 2;
	return $plv < 1 ? 1 : $plv;
}
# ---- gearmaps begin (pure helpers, no globals: tested offline by tests/ayla/gearmaps_test.pl) ----
# job families by OpenKore jobID (1st, 2nd, transcendent)
sub gm_family {
	my $j = shift || 0;
	return 'mage' if $j =~ /^(2|9|16|4003|4010|4017)$/;
	return 'archer' if $j =~ /^(3|11|19|20|4004|4012|4020|4021)$/;
	return 'acolyte' if $j =~ /^(4|8|15|4005|4009|4016)$/;
	return 'melee';
}
sub gm_fragile_job { my $j = shift || 0; return $j =~ /^(2|9|16|18|4003|4010|4017|4019)$/ ? 1 : 0 } # mage line + Alchemist line
sub gm_tank_job { my $j = shift || 0; return $j =~ /^(7|14|4008|4015)$/ ? 1 : 0 }                    # Knight / Crusader lines
# the weapon attack a character of family $f normally holds at level $lv (legion/gearpower.go expectedWeaponPower)
sub gm_expected_atk { my ($f, $lv) = @_; return $f eq 'archer' ? 10 + $lv : $f eq 'mage' ? 20 + 1.2 * $lv : 15 + 1.5 * $lv }
# legion/gearscore.go gearEffectiveLv: W / A = weapon / armor ratios (1 = at par); never under half the level, at most
# +15% (max +8) for very good gear, beginners (< 12) not rated
sub gm_eff_lv {
	my ($f, $lv, $w, $a) = @_;
	return $lv if $lv < 12;
	my $k = $f eq 'archer' ? 0.35 : ($f eq 'mage' || $f eq 'acolyte') ? 0.25 : 0.4;
	my $pen = (1 - $w) * $lv * $k + (1 - $a) * $lv * 0.18;
	my $bonus = 0.15 * $lv < 8 ? 0.15 * $lv : 8;
	$pen = -$bonus if $pen < -$bonus;
	my $out = $lv - ($pen >= 0 ? int($pen + 0.5) : -int(-$pen + 0.5));
	my $half = int(($lv + 1) / 2);
	return $out < $half ? $half : $out;
}
# her kit: equipment ATK incl. refine (status attack_bonus) against the expected weapon, equipment DEF + half MDEF
# against a full level-fitting set (legion/gearscore.go expectedArmorDef), +10% per weapon card, +4% per armor card,
# -5% per empty core slot (body, garment, shoes, head). Casters: MATK is not split out in the status packet - neutral.
sub gm_rate {
	my ($f, $lv, $atkEq, $def, $mdef, $wCards, $aCards, $missing) = @_;
	my $w = $atkEq / gm_expected_atk($f eq 'mage' ? 'melee' : $f, $lv);
	$w = 0.7 if ($f eq 'mage' || $f eq 'acolyte') && $w < 0.7;
	$w = $w * (1 + 0.10 * $wCards); $w = 1.5 if $w > 1.5; $w = 0 if $w < 0;
	my $a = ($def + 0.5 * $mdef) / (18 + 1.1 * $lv) + 0.04 * $aCards - 0.05 * $missing;
	$a = 1.4 if $a > 1.4; $a = 0 if $a < 0;
	return ($w, $a);
}
# the party level from [effective lv, base lv, fragile] rows: the weakest (fragile -6) + healer 4 + tank 3 + 4 online 1,
# capped at the weakest base level (legion/lfg.go partyStrength)
sub gm_party_lv {
	my ($rows, $heal, $tank) = @_;
	my ($min, $minBase);
	foreach my $r (@$rows) {
		my $e = $r->[0] - ($r->[2] ? 6 : 0); $e = 1 if $e < 1;
		$min = $e if !defined $min || $e < $min;
		$minBase = $r->[1] if !defined $minBase || $r->[1] < $minBase;
	}
	return 1 unless defined $min;
	my $b = 0;
	if (@$rows >= 2) { $b += 4 if $heal; $b += 3 if $tank; $b += 1 if @$rows >= 4 }
	my $p = $min + $b;
	my $cap = $minBase > $min ? $minBase : $min;
	return $p > $cap ? $cap : $p;
}
# one measured window (legion/gearscore.go perfVerdict; no fair-kill data here): n members, minutes, kills, deaths.
# -3: deaths >= 1.5 per member-hour, or under 0.25 kills per member-minute with a death; +2: no deaths and >= 2 kills
# per member-minute (the party farms it easily)
# owner 2026-11-15 "this party is not strong enough, choose the right map": a death steps down at once (-5); a step up
# (+3) only after 20+ min on the map with 0 deaths and good kills (1.5+ per member per minute)
sub gm_verdict {
	my ($n, $min, $k, $d) = @_;
	return 0 if $n <= 0 || $min <= 0;
	return -5 if $d > 0;
	return 0 if $min < 20;
	return 3 if $k / ($n * $min) >= 0.8; # kills are shared in a party: 0.8 per member-minute is good farming
	return 0;
}
# a farm map whose route crosses a map with aggressive monsters at or above the party's level - 5 is out (owner
# 2026-11-14: she ran across gef_fild10's orc packs to orcsdun02 with a party rated 58). $via: map -> transit maps on
# the way from its town; $danger: map -> top of its aggressive monster band. Returns the unsafe transit map or ''.
sub gm_route_unsafe {
	my ($m, $plv, $via, $danger) = @_;
	foreach my $v (@{$via->{$m} || []}) { return $v if $danger->{$v} && $danger->{$v} >= $plv - 5 }
	return '';
}
# measured exp per map (owner 2026-11-15 "she barely gains exp": the gear rating kept a level 78-85 party on lv23-33
# monsters at ~33k exp/h). A map record {e => base exp gained there, s => seconds spent there as the hunting map,
# d => deaths there}; a death costs $dcost exp (pre-re: 1% of the level). undef = not measured yet (under 15 min).
use constant GM_MEASURE_SECS => 900;
sub gm_exp_rate {
	my ($r, $dcost) = @_;
	return undef unless $r && ref $r eq 'HASH' && ($r->{s} || 0) >= GM_MEASURE_SECS;
	my $e = ($r->{e} || 0) - ($r->{d} || 0) * ($dcost || 0);
	return ($e < 0 ? 0 : $e) * 3600 / $r->{s};
}
# the map choice on measured exp: $base = the gear / level pick (the starting point), $cands = [map, lowest band] of the
# other maps the party may farm now, $rate = map -> exp/h (undef = unmeasured). A measured map beats another measured one
# by its exp/h; an unmeasured base pick in a band at or above the best measured map is tried (the party stepped up with
# no deaths); otherwise the best measured map wins. Returns the map.
sub gm_pick_measured {
	my ($base, $baseBand, $cands, $rate) = @_;
	my ($best, $bestR, $bestBand);
	foreach my $c ($base ne '' ? [$base, $baseBand] : (), @$cands) {
		my $r = $rate->{$c->[0]};
		next unless defined $r;
		($best, $bestR, $bestBand) = ($c->[0], $r, $c->[1]) if !defined $bestR || $r > $bestR;
	}
	return $base if !defined $best;
	return $base if $base ne '' && !defined $rate->{$base} && $baseBand >= $bestBand;
	return $best;
}
# is $m worth leaving $c for: both measured and $m 30%+ better, or $m unmeasured in a higher band while $c is measured
sub gm_better {
	my ($m, $mBand, $c, $cBand, $rate) = @_;
	return 0 if $m eq '' || $m eq $c;
	my ($rm, $rc) = ($rate->{$m}, $rate->{$c});
	return 0 unless defined $rc;
	return $mBand > $cBand ? 1 : 0 unless defined $rm;
	return $rm > 1.3 * $rc ? 1 : 0;
}
# step up with no deaths: every 20 min of hunting-map time without a death or a low-HP step (counted across town runs;
# the old 20-min window restarted on every map change, so with a town run every ~30 min it rose twice in 8 h)
use constant GM_SAFE_STEP_SECS => 1200;
sub gm_safe_step { my $s = shift || 0; return $s >= GM_SAFE_STEP_SECS ? 3 : 0 }
# ---- gearmaps end ----
sub gm_self_lv {
	my $lv = $char->{lv} || 1;
	my $f = gm_family($char->{jobID} || 0);
	my ($wc, $ac, %have) = (0, 0);
	foreach my $slot (keys %{$char->{equipment} || {}}) {
		my $it = $char->{equipment}{$slot};
		next unless $it && ref $it;
		$have{$slot} = 1;
		my $raw = $it->{cards} || '';
		my $len = length($raw) >= 16 ? 4 : 2;
		my $n = 0;
		for my $i (0 .. 3) {
			last if length($raw) < ($i + 1) * $len;
			my $c = unpack($len == 4 ? 'V' : 'v', substr($raw, $i * $len, $len));
			$n++ if $c >= 4001 && $c < 5000 || $c >= 27000 && $c < 32000;
		}
		if ($slot eq 'rightHand') { $wc += $n } elsif ($slot ne 'arrow') { $ac += $n }
	}
	my $missing = 0;
	$missing++ for grep { !$have{$_} } qw(armor robe shoes);
	$missing++ unless $have{topHead} || $have{midHead} || $have{lowHead};
	my ($w, $a) = gm_rate($f, $lv, $char->{attack_bonus} || 0, $char->{def} || 0, $char->{def_magic} || 0, $wc, $ac, $missing);
	return gm_eff_lv($f, $lv, $w, $a);
}
# measured step: the party's kills / deaths on one map over 8+ min (hers from OpenKore's hooks, the friends' from
# Legion's API counters - without the API only hers count); a new map or member count starts a new window, the step
# (-15..+10) is kept
my ($gmKills, $gmDeaths) = (0, 0);
my %gmWin = (map => '', at => 0, k => 0, d => 0, n => 0);
# the step starts conservative: monsters ~10 levels under the gear-rated party level (owner 2026-11-15), kept in
# logs/ayla_step.json across reloads; the first load of this version re-picks the map at once (PLAN_CHANGE)
my $gmOff = -10;
our $forceRepick = 0;
my $gmDiedStepAt = 0;
sub step_save { eval { open(my $f, '>', 'logs/ayla_step.json') or die; print $f JSON::PP->new->encode({off => $gmOff, v => 5, repick => $forceRepick ? 1 : 0}); close $f } }
# no step file yet: create it now (-10) with the owner's re-pick pending; a pending re-pick survives further reloads
eval {
	if (open(my $f, '<', 'logs/ayla_step.json')) {
		local $/; my $d = JSON::PP->new->decode(<$f>); close $f;
		$gmOff = $d->{off} if $d && defined $d->{off} && ($d->{v} || 0) >= 2;
		$forceRepick = 1 if $d && ($d->{repick} || ($d->{v} || 0) < 5); # v4: a false STUCK dropped pay_dun02 - re-pick nearest (PLAN_RESTORE)
	} else { $forceRepick = 1; step_save() }
};
sub gm_kill {
	# kills are counted from her exp gains (plan_track): every party kill shares exp with her, the target_died hook
	# only saw some of hers (the step never rose with 40-60 party kills / 5 min)
}
# measured exp per map (gm_exp_rate): logs/ayla_mapexp.json {maps => {map => {e, s, d, at}}, died => {map => time},
# safe => hunting-map seconds since the last death / low-HP step}; survives reloads
our (%mapExp, %mapDiedAt);
our $gmSafeSecs = 0;
my ($meMap, $meT, $meExp, $meSaveAt) = ('', 0, -1, 0);
sub mapexp_save {
	eval { open(my $f, '>', 'logs/ayla_mapexp.json') or die; print $f JSON::PP->new->canonical->encode({maps => \%mapExp,
		died => {map { $_ => int($mapDiedAt{$_}) } grep { time - $mapDiedAt{$_} < 7200 } keys %mapDiedAt}, safe => int($gmSafeSecs)}); close $f };
}
eval { if (open(my $f, '<', 'logs/ayla_mapexp.json')) { local $/; my $d = JSON::PP->new->decode(<$f>); close $f;
	if (ref $d eq 'HASH') { %mapExp = %{$d->{maps} || {}} if ref $d->{maps} eq 'HASH'; %mapDiedAt = %{$d->{died} || {}} if ref $d->{died} eq 'HASH'; $gmSafeSecs = $d->{safe} || 0 } } };
# what a death costs in exp (pre-re: 1% of the level's exp)
sub death_cost { return $char && $char->{exp_max} ? 0.01 * $char->{exp_max} : 0 }
sub map_rate { my $m = shift || ''; return gm_exp_rate($mapExp{$m}, death_cost()) }
sub map_band { my $m = shift || ''; my $lo; foreach my $r (@FIT_MAPS) { $lo = $r->[0] if $r->[2] eq $m && (!defined $lo || $r->[0] < $lo) } return defined $lo ? $lo : 0 }
sub map_rate_txt { my $m = shift; my $r = map_rate($m); return defined $r ? sprintf('%dk exp/h', int($r / 1000 + 0.5)) : 'not measured' }
# every tick: time and base exp on her hunting map (only while it is the map she hunts: not crossing it, not in town)
sub mapexp_tick {
	return unless $char && $field;
	my $here = $field->baseName;
	my $exp = $char->{exp} || 0;
	my $hunt = plan_map() || $huntMap || '';
	if ($here ne $meMap || is_townmap($here) || $here ne $hunt || $char->{dead}) { ($meMap, $meT, $meExp) = ($here, time, $exp); return }
	my $dt = time - $meT; $dt = 0 if $dt < 0 || $dt > 10;
	my $de = $exp - $meExp; $de = 0 if $de < 0 || $meExp < 0; # a level-up resets the exp bar: that gain is skipped
	my $r = ($mapExp{$here} ||= {e => 0, s => 0, d => 0});
	$r->{s} += $dt; $r->{e} += $de; $r->{at} = int(time);
	if ($r->{s} > 14400) { $r->{$_} /= 2 for qw(e s d) } # the last ~4 h count
	$gmSafeSecs += $dt;
	($meT, $meExp) = (time, $exp);
	if (time - $meSaveAt > 60) { $meSaveAt = time; mapexp_save() }
}
sub gm_died {
	$gmDeaths++; push @planDeaths, [time, $field ? $field->baseName : '']; @planDeaths = grep { time - $_->[0] < 1800 } @planDeaths;
	# one death steps down at once (owner 2026-11-15); the hook fired twice for one death (-10 -> -15 -> -20): once per 30 s
	return if time - ($gmDiedStepAt || 0) < 30;
	$gmDiedStepAt = time;
	my $dm = $field ? $field->baseName : '';
	if ($dm ne '' && !is_townmap($dm)) { ($mapExp{$dm} ||= {e => 0, s => 0, d => 0})->{d}++; $mapDiedAt{$dm} = time } # the map is off for 2 h
	$gmSafeSecs = 0; mapexp_save();
	my $old = $gmOff; $gmOff -= 5; $gmOff = -20 if $gmOff < -20;
	message "[partylead] gear maps: died on " . ($field ? $field->baseName : '?') . " -> step $old -> $gmOff (one map easier)\n", 'system';
	step_save(); $gmRecheck = 1; %gmWin = (map => '', at => 0, k => 0, d => 0, n => 0);
}
# HP regularly under 50% on the farm map (30%+ of 5 min of samples) steps down too
my @hpSamples; my $hpSampleAt = 0;
sub hp_sample {
	return unless $char && $field && $char->{hp_max} && !is_townmap($field->baseName) && time - $hpSampleAt >= 2;
	$hpSampleAt = time;
	push @hpSamples, [time, $char->{hp} / $char->{hp_max} < 0.5 ? 1 : 0];
	@hpSamples = grep { time - $_->[0] < 300 } @hpSamples;
	return if @hpSamples < 100;
	my $low = grep { $_->[1] } @hpSamples;
	return if $low / @hpSamples < 0.3;
	my $old = $gmOff; $gmOff -= 3; $gmOff = -20 if $gmOff < -20;
	message "[partylead] gear maps: HP under 50% in " . int(100 * $low / @hpSamples) . "% of the last 5 min -> step $old -> $gmOff\n", 'system';
	@hpSamples = (); $gmSafeSecs = 0; step_save(); $gmRecheck = 1;
}
# the step rises +3 per 20 min of hunting-map time with no death and no low-HP step (gm_safe_step), counted across
# town runs and map changes; a death steps down at once (gm_died), so the party climbs to the hardest map it survives
sub gm_step_offset {
	my $v = gm_safe_step($gmSafeSecs);
	return $gmOff unless $v && $gmOff < 10;
	my $old = $gmOff;
	$gmOff += $v; $gmOff = 10 if $gmOff > 10;
	message "[partylead] gear maps: " . int($gmSafeSecs / 60) . " min of hunting with no deaths -> step $old -> $gmOff (" . ($field ? $field->baseName . ' ' . map_rate_txt($field->baseName) : '-') . ")\n", 'system';
	$gmSafeSecs = 0; step_save(); mapexp_save();
	$gmRecheck = 1; # fit_tick re-checks the map choice now
	return $gmOff;
}
# a map order far above the party: every table row for it starts 4+ levels over the party's gear-rated level (with the
# measured kills / deaths step, party_fit_lv). '' = fine (or a map not in the table). conf partylead_mindCheck 0 = off.
sub mind_map_far {
	my $m = shift || '';
	return '' if defined $config{partylead_mindCheck} && $config{partylead_mindCheck} eq '0';
	my $lo;
	foreach my $r (@FIT_MAPS) { $lo = $r->[0] if $r->[2] eq $m && (!defined $lo || $r->[0] < $lo) }
	my $plv = party_fit_lv();
	if ((my $v = route_unsafe($m, $plv)) ne '') { return "the way to $m crosses $v (aggressive packs up to level $partylead::CROSS_DANGER{$v}), the party is rated $plv" }
	return '' unless defined $lo;
	return '' if $plv + 3 >= $lo;
	return "$m starts at level $lo, the party is rated $plv (gear, kills and deaths)";
}
sub map_fits {
	my ($m, $plv) = @_;
	return 0 if route_unsafe($m, $plv) ne '';
	foreach my $r (@FIT_MAPS) { return 1 if $r->[2] eq $m && $plv >= $r->[0] - 2 && $plv <= $r->[1] + 3 }
	return 0;
}
# the level / gear choice: the first row (table order = preference) that fits, isn't crowded, matches the healer state
sub level_map {
	my $plv = shift;
	my $heal = party_has_healer();
	my $free = sub { my $m = shift; return !(time - ($crowdedAt{$m} || 0) < 3600) && !(time < ($routeFailAt{$m} || 0)) && !(time - ($farmedAt{$m} || 0) < 1800) && !(time - ($mapDiedAt{$m} || 0) < 7200) && route_unsafe($m, $plv) eq '' };
	my $ok = sub { my $r = shift; return $plv >= $r->[0] && $plv <= $r->[1] && $free->($r->[2]) };
	# (2) nearest first: a fitting map of the area she is in / heading to (the same Kafra town) before a far one
	my $area = town_of_map(plan_map() || $homeMap || ($field ? $field->baseName : ''));
	my $base;
	foreach my $r (@FIT_MAPS) { if ($ok->($r) && $r->[3] == $heal && town_of_map($r->[2]) eq $area) { $base = $r->[2]; last } }
	if (!defined $base) { foreach my $r (@FIT_MAPS) { if ($ok->($r) && $r->[3] == $heal) { $base = $r->[2]; last } } }
	if (!defined $base) { foreach my $r (@FIT_MAPS) { if ($ok->($r) && !$r->[3]) { $base = $r->[2]; last } } } # no healer row: a field
	if (!defined $base) { foreach my $r (@FIT_MAPS) { if ($ok->($r)) { $base = $r->[2]; last } } }
	if (defined $base) {
		# measured exp (owner 2026-11-15): the gear / level pick is only the starting point - among the maps the party may
		# farm now (its band reached, healer state, not crowded / died on / just farmed) the best measured exp/h wins; an
		# unmeasured pick in a higher band than the best measured map is tried first (the step rose with no deaths)
		my %seen;
		my @c = map { [$_->[2], map_band($_->[2])] } grep { !$seen{$_->[2]}++ && $_->[2] ne $base && $plv >= $_->[0] && ($_->[3] == $heal || !$_->[3]) && $free->($_->[2]) } @FIT_MAPS;
		my %rate = map { $_->[0] => map_rate($_->[0]) } @c, [$base];
		my $m = gm_pick_measured($base, map_band($base), \@c, \%rate);
		return $m;
	}
	my ($best, $bd);
	foreach my $r (@FIT_MAPS) { next if route_unsafe($r->[2], $plv) ne ''; my $d = abs(($r->[0] + $r->[1]) / 2 - $plv); ($best, $bd) = ($r->[2], $d) if !defined $bd || $d < $bd }
	return $best;
}
sub mind_fresh { return $homeBy eq 'mind' && $homeMap ne '' && time - $homeAt < HOME_STALE }
# where the party should farm now, and why
sub choose_map {
	my ($tm, $who) = party_turn();
	# a gear-turn map far above the conservative party level is no reason to go there (live 05:14: the owner's re-pick
	# chose the gear turn's mjolnir_12 for a party rated 48)
	return ($tm, "$who\'s gear turn") if $tm ne '' && !(time - ($crowdedAt{$tm} || 0) < 3600) && route_unsafe($tm) eq '' && mind_map_far($tm) eq '';
	my $plv = party_fit_lv();
	return (level_map($plv), "fits the party (level $plv" . (party_has_healer() ? ', healer' : ', no healer') . ")");
}
sub auto_goto {
	my ($m, $why) = @_;
	message "[partylead] own map choice: $m - $why (was " . ($homeMap || '-') . ($homeBy eq 'mind' && $homeMap ne '' && $homeAt ? ", mind's order " . int((time - $homeAt) / 60) . " min old" : '') . ")\n", 'system';
	my $c = plan_map();
	if ($c ne '' && $m ne $c) { # PT16: her own choice leaves a committed map only for a real reason
		my ($tm) = party_turn();
		my $r = plan_reason() || ($tm ne '' && $tm eq $m ? "gear turn ($m)" : '');
		return if $r eq '';
		plan_change($m, $r);
	}
	local $autoPick = 1;
	cmd_partylead(undef, "goto $m");
}
my $fitAt = 0;
my $fitLoadAt = time;
sub fit_tick {
	return if time - $fitAt < FIT_EVERY && !$gmRecheck;
	$gmRecheck = 0;
	$fitAt = time;
	return if mind_fresh();
	if ((my $pc = plan_map()) ne '') { # PT16: one map for ~2 h
		my ($tm) = party_turn();
		return if plan_reason() eq '' && ($tm eq '' || $tm eq $pc);
	}
	# the party's levels and classes come from Legion's API: no choice before they are in (live 2026-11-15: the first
	# pick right after a reload saw no friends - "level 81, no healer" - with a Priest in the party); 5 min at most
	my @fr = online_friends();
	return if @fr && !grep({ $apiPos{$_->[0]} && $apiPos{$_->[0]}{lv} } @fr) && time - $fitLoadAt < 300;
	return if $rally || $depart || $holding || $collect || $fetchMap ne '' || $char->{dead};
	return if (defined &aylagear::busy && aylagear::busy()) || AI::inQueue('NPC', 'sellAuto', 'buyAuto', 'storageAuto', 'deal');
	my ($m, $why) = choose_map();
	return if !defined $m || $m eq '' || $m eq $homeMap && ($config{lockMap} || '') eq $m;
	my ($tm) = party_turn();
	# her current map still fits the party and no gear turn calls elsewhere: stay (one map for ~2 hours, RULES 3)
	return if $homeMap ne '' && $tm eq '' && map_fits($homeMap, party_fit_lv()) && !(time - ($crowdedAt{$homeMap} || 0) < 3600) && ($config{lockMap} || $homeMap) eq $homeMap && better_map($homeMap) eq '';
	auto_goto($m, $why);
}
# crowd_check found her map too crowded: with no fresh order she moves on to the next fitting map herself
sub crowd_move {
	my $here = shift;
	$crowdedAt{$here} = time;
	return if mind_fresh() || $rally || $depart;
	my ($m, $why) = choose_map();
	return if !defined $m || $m eq '' || $m eq $here;
	auto_goto($m, "$here is crowded; $why");
}

# ---- lockMap guard (AY05/AY07, 2026-11-14): a lockMap set from outside the plugin (a raw 'conf lockMap X' from the
# helper / mind / ayla.sh, or one left in config.txt) gets the same gear-rated check as 'partylead goto' - live: the
# helper's 'conf lockMap gl_church' (party rated 58, gl_church from 72) followed every rejected goto. $lmSet is what the
# plugin itself last left in lockMap (updated after every tick and every partylead command); anything else is outside.
our $lmSet; # undef until the first check (then a lockMap from config.txt is checked too)
our %droppedUntil; # map -> until when it is off (STUCK: its route keeps failing)
# 07:29 a STUCK while she held at the Payon Kafra dropped pay_dun02 (her good farm map): undone on this load
if ($droppedUntil{pay_dun02}) {
	delete $droppedUntil{pay_dun02};
	eval { message "[watchdog] PLAN_RESTORE pay_dun02 (dropped by a false STUCK while holding for the party)\n", 'system' };
}
sub map_dropped {
	my $m = shift || '';
	return '' unless $droppedUntil{$m};
	if (time >= $droppedUntil{$m}) { delete $droppedUntil{$m}; return '' }
	return "its route kept failing, off for " . int(($droppedUntil{$m} - time) / 60) . " more min";
}
sub drop_map {
	my ($m, $why) = @_;
	$droppedUntil{$m} = time + 7200;
	$crowdedAt{$m} = time + 3600; # level_map / map_fits skip it for 2 h as well
	$gotoMap = '' if $gotoMap eq $m;
	($homeMap, $homeAt, $homeBy) = ('', 0, 'auto') if $homeMap eq $m; # a stale mind order on it no longer holds her
	$saved->{lockMap} = '' if $saved && ($saved->{lockMap} || '') eq $m;
	$config{lockMap} = '' if ($config{lockMap} || '') eq $m;
	$fitAt = 0; $lmSet = $config{lockMap} || '';
	wd_report('DROP_MAP', $m, "$m dropped for 2 h: $why", "own gear-rated choice");
}
# a real reason to leave the committed map now ('' = none): 2+ deaths there in 30 min, no exp there for 20 min,
# dropped (route failing) / crowded / its way unsafe
sub plan_reason {
	my $c = plan_map();
	return '' if $c eq '';
	my $d = grep { $_->[1] eq $c && time - $_->[0] < 1800 } @planDeaths;
	return "$d deaths on $c in 30 min" if $d >= 2;
	return "no kills on $c for 20 min" if $field && $field->baseName eq $c && $plan->{expAt} && time - $plan->{expAt} > 1200;
	if (my $d = map_drained($c)) { return $d }
	return "$c can't be reached (dropped)" if $droppedUntil{$c} && time < $droppedUntil{$c};
	return "$c is crowded" if $crowdedAt{$c} && time - $crowdedAt{$c} < 3600 && $crowdedAt{$c} <= time;
	return "the route to $c keeps failing" if time < ($routeFailAt{$c} || 0);
	my $v = route_unsafe($c);
	return "the way to $c crosses $v" if $v ne '';
	if ((my $b = better_map($c)) ne '') { return $b }
	return '';
}
# a better map than the plan's $c ('' = none): after 20+ min on the plan, the map choice (level_map) names another map
# that measured 30% more exp/h, or an unmeasured one in a higher band (the party stepped up with no deaths), or $c no
# longer fits the party. A map in another town only right after a town run (the party is in town anyway: no extra trip).
my ($bmAt, $bmFor, $bmWhy) = (0, '', '');
sub better_map {
	my $c = shift || '';
	return '' if $c eq '' || !$plan || time - ($plan->{at} || time) < 1200;
	return $bmWhy if $bmFor eq $c && time - $bmAt < 30;
	($bmAt, $bmFor, $bmWhy) = (time, $c, '');
	my $plv = party_fit_lv();
	my $m = level_map($plv);
	return '' if !defined $m || $m eq '' || $m eq $c;
	my %rate = ($m => map_rate($m), $c => map_rate($c));
	my $why = gm_better($m, map_band($m), $c, map_band($c), \%rate) ? "$m looks better ($m " . map_rate_txt($m) . ", $c " . map_rate_txt($c) . ")"
		: !map_fits($c, $plv) && (defined $rate{$m} ? $rate{$m} >= ($rate{$c} || 0) : 1) ? "the party outgrew $c (rated $plv; $c " . map_rate_txt($c) . ")" : '';
	return '' if $why eq '';
	if (town_of_map($m) ne town_of_map($c)) {
		my $here = $field ? $field->baseName : '';
		return '' unless is_townmap($here) && time - ($errandEnd || 0) < 600;
	}
	$bmWhy = $why;
	return $why;
}
# leave the commitment for $new, logged ("[watchdog] PLAN_CHANGE old -> new: reason")
sub plan_change {
	my ($new, $why) = @_;
	my $c = plan_map();
	return if $c eq '' || $c eq ($new || '');
	wd_report('PLAN_CHANGE', $c, "$c -> " . ($new || '-') . ": $why", "new plan on arrival");
	$farmedAt{$c} = time; farmed_save(); # level_map skips it for 30 min (also after a reload)
	$plan = undef; plan_save();
	announce_plan($new);
}
# "new spot: <map>, meet at the kafra in <town>" - Legion's rally.go rallyLineRe reads it as the party's newest meeting
# point and drops an older rally (live: after the false gef_fild03 plan, Zendancer84 / LilLilyblade waited at the
# Geffen Kafra while she was back on pay_dun01)
our ($meetTown, $meetTownAt) = ('', 0); # the meeting Kafra town she announced last
sub announce_plan {
	my $new = shift || '';
	return if $new eq '' || is_townmap($new);
	my $t = town_of_map($new);
	return unless $KAFRA{$t};
	psay("new spot: $new, meet at the kafra in $t", 1, 0);
	($meetTown, $meetTownAt) = ($t, time);
}
sub lockmap_guard {
	return unless $net && $net->getState() == 5 && $char && $field && $char->{party} && $char->{party}{users};
	my $lm = $config{lockMap} || '';
	return if defined $lmSet && $lm eq $lmSet;
	if ($lm ne '' && !is_townmap($lm)) {
		# the party's levels come from Legion's API: no verdict before they are in (5 min at most, as fit_tick)
		my @fr = online_friends();
		return if @fr && !grep({ $apiPos{$_->[0]} && $apiPos{$_->[0]}{lv} } @fr) && time - $fitLoadAt < 300;
		my $why = map_dropped($lm) || mind_map_far($lm);
		my $pc = plan_map();
		$why = "committed to $pc (PT16)" if !$why && $pc ne '' && $lm ne $pc && plan_reason() eq '';
		if ($why) {
			my $back = defined $lmSet && !map_dropped($lmSet) && !mind_map_far($lmSet) ? $lmSet : '';
			$config{lockMap} = $back;
			eval { Misc::configModify('lockMap', $back, 1) } if defined &Misc::configModify; # config.txt too (conf wrote it)
			$config{lockMap} = $back;
			$gotoMap = '' if $gotoMap eq $lm;
			($homeMap, $homeAt, $homeBy) = ('', 0, 'auto') if $homeMap eq $lm;
			$saved->{lockMap} = $back if $saved && ($saved->{lockMap} || '') eq $lm;
			AI::clear('route', 'move') if AI::is('route', 'move');
			$fitAt = 0 if $back eq ''; # nothing to go back to: her own choice now
			wd_report('MIND_MAP', $lm, "rejected (conf): lockMap $lm - $why", "lockMap back to " . ($back || "her own choice"));
		}
	}
	$lmSet = $config{lockMap} || '';
}
our (%leftTown, $prevMap);
our $regroupTo = ''; # AY22: walking back to the priest - the town she left is no barrier then
my $tbgLogAt = 0;
our $tbgHold; # [lockMap, until] held while the town she left blocks the way
sub town_back_guard {
	return unless $field && $char;
	my $here = $field->baseName;
	if ($tbgHold && (time > $tbgHold->[1] || $regroupTo ne '' || $rally)) { $config{lockMap} = $tbgHold->[0] if ($config{lockMap} || '') eq ''; $tbgHold = undef }
	$leftTown{$prevMap} = time if defined $prevMap && $prevMap ne $here && is_townmap($prevMap) && !is_townmap($here) && $prevMap !~ /_in\d*$/;
	$prevMap = $here;
	return if $rally || $regroupTo ne '' || pk_retreating() || is_townmap($here) || AI::inQueue('sellAuto', 'buyAuto', 'storageAuto', 'teleport');
	my ($dm, $nm) = route_maps();
	foreach my $t ($nm, $dm) {
		next unless $t ne '' && $leftTown{$t} && time - $leftTown{$t} < 300;
		# lockMap held until the window ends (clearing its route every tick = a route recalculation every 0.5 s)
		if (($config{lockMap} || '') ne '' && !$tbgHold) { $tbgHold = [$config{lockMap}, $leftTown{$t} + 300]; $config{lockMap} = '' }
		AI::clear('route', 'move');
		message "[partylead] PT04: not going back into $t (left it " . int(time - $leftTown{$t}) . " s ago) - holding here
", 'system' if time - $tbgLogAt > 20;
		$tbgLogAt = time;
		return;
	}
}
# ---- no Fly Wing / Teleport with friends online (owner 2026-11-14; live 5b2c5ee: a Fly Wing on gef_fild10 with no
# danger - OpenKore's own mon_control "teleport if on screen" flag for Orc Lord / Orc Hero, or a teleportAuto_* left on
# by a solo moment). While any friend is online every teleportAuto_* and route_teleport is 0, mon_control's teleport
# flags are off, and any queued teleport but the PT31 dying Butterfly Wing is cancelled and logged with its context.
# Solo again: the saved values come back.
our ($wingOkUntil, %tpSaved, %tpMonSaved) = (0);
my @TP_KEYS = map { "teleportAuto_$_" } qw(hp sp idle portal search minAggressives minAggressivesInLock maxDmg maxDmgInLock deadly
	unstuck lostTarget dropTarget dropTargetEngaged dropTargetKS dropTargetHidden attackedWhenSitting totalDmg totalDmgInLock atkCount atkMiss allPlayers);
my $tpLogAt = 0;
sub tp_guard {
	return unless $net && $net->getState() == 5 && $char && $field && $char->{party} && $char->{party}{users};
	my @fr = stick_on() ? online_friends() : ();
	if (@fr) {
		# saveMap_warpToBuyOrSell: OpenKore's auto-buy / sell / storage wings her to the save map (live 04:38:49) - with
		# friends online town trips are party town runs on foot / by Kafra
		foreach my $k (@TP_KEYS, 'route_teleport', 'saveMap_warpToBuyOrSell') {
			next unless $config{$k};
			$tpSaved{$k} = $config{$k} unless exists $tpSaved{$k};
			$config{$k} = 0;
		}
		foreach my $n (keys %Globals::mon_control) {
			my $mc = $Globals::mon_control{$n};
			next unless ref $mc eq 'HASH' && $mc->{teleport_auto};
			$tpMonSaved{$n} = $mc->{teleport_auto} unless exists $tpMonSaved{$n};
			$mc->{teleport_auto} = 0;
		}
		if (AI::inQueue('teleport') && time > ($wingOkUntil || 0)) {
			my @mons = map { $_->{name} } grep { $_ && $_->{pos_to} && defined $_->{pos_to}{x} } @{$monstersList ? $monstersList->getItems() : []};
			my $ctx = sprintf("HP %d/%d, %d monsters on her, in sight: %s", $char->{hp} || 0, $char->{hp_max} || 0, scalar(AI::ai_getAggressives()), join(', ', @mons[0 .. ($#mons < 5 ? $#mons : 5)]) || '-');
			AI::clear('teleport');
			wd_report('TELEPORT', $field->baseName, "teleport cancelled (friends online: " . join(', ', map { $_->[0] } @fr) . "); $ctx", "stays with the party") if time - $tpLogAt > 10;
			$tpLogAt = time;
		}
	} else {
		$config{$_} = delete $tpSaved{$_} foreach keys %tpSaved;
		foreach my $n (keys %tpMonSaved) { $Globals::mon_control{$n}{teleport_auto} = delete $tpMonSaved{$n} if ref $Globals::mon_control{$n} eq 'HASH' }
		%tpMonSaved = ();
	}
}
# a farm map whose way crosses dangerous packs is never kept as a destination (live 5b2c5ee: she still went into
# orcsdun02 - homeMap from config.txt / a restore set it, not an outside lockMap, so the guard never saw it)
my $ruAt = 0;
sub route_guard {
	return if time - $ruAt < 5;
	$ruAt = time;
	return unless $net && $net->getState() == 5 && $char && $field && $char->{party} && $char->{party}{users};
	my @fr = online_friends();
	return if @fr && !grep({ $apiPos{$_->[0]} && $apiPos{$_->[0]}{lv} } @fr) && time - $fitLoadAt < 300;
	foreach my $m (grep { $_ ne '' } ($homeMap, $gotoMap, $config{lockMap} || '', ($saved ? $saved->{lockMap} || '' : ''))) {
		my $v = route_unsafe($m);
		next if $v eq '';
		$gotoMap = '' if $gotoMap eq $m;
		($homeMap, $homeAt, $homeBy) = ('', 0, 'auto') if $homeMap eq $m;
		$saved->{lockMap} = '' if $saved && ($saved->{lockMap} || '') eq $m;
		if (($config{lockMap} || '') eq $m) { $config{lockMap} = ''; eval { Misc::configModify('lockMap', '', 1) } if defined &Misc::configModify; $config{lockMap} = '' }
		$lmSet = '' if defined $lmSet && $lmSet eq $m;
		my ($rdm) = route_maps();
		AI::clear('route', 'move') if $rdm eq $m;
		$fitAt = 0;
		wd_report('ROUTE_UNSAFE', $m, "$m dropped as destination: its way crosses $v (party rated " . transit_fit_lv() . ")", "own choice");
	}
}
# PT16: farming (exp gained) on her hunting map commits it for 2 h; exp there keeps expAt fresh
my $ptExp = -1;
# map drained (owner 2026-11-15: a 7-member party cleared pay_dun02 faster than it respawns - 40 -> 3 kills / 5 min):
# her kills on the plan map sampled every minute; after 20+ min there, the last 15 min under 40% of the first 20 min's
# rate, or under 1 kill a minute -> the plan breaks ("map drained")
my @killSamples; # [time, gmKills, map]
sub kill_sample {
	my $here = $field ? $field->baseName : '';
	return if @killSamples && time - $killSamples[-1][0] < 60;
	@killSamples = grep { $_->[2] eq $here } @killSamples;
	push @killSamples, [time, $gmKills, $here];
	shift @killSamples while @killSamples > 120;
}
sub map_drained {
	my $c = shift;
	# a kill rate: 10+ min on the plan map and under 15 kills by her in the last 10 min, while she sweeps there with the
	# priest (not waiting / holding) -> drained (live: ~2 kills per 5 min on pay_dun02 kept resetting an 8-min timer)
	if ($field && $field->baseName eq $c) {
		# back on the map after a town run / regroup: the arrival starts again (live 12:19: "0 kills in the last 10 min"
		# 40 s after coming back from Payon - the window still held the arrival from before the town run)
		if (($planArr{map} || '') ne $c || time - ($planArr{seen} || 0) > 180) { %planArr = (map => $c, at => time, seen => time); farmed_save() }
		$planArr{seen} = time;
		my $n = grep { time - $_ < 600 } @planKillTimes;
		if (time - ($pk_drainLog || 0) >= 300) { $pk_drainLog = time; message "[partylead] drained check: $n kills in 10 min on $c\n", 'system' }
		my $since = $planArr{at} > $killTrackSince ? $planArr{at} : $killTrackSince;
		if ((!$holding || priest_dist() <= 15) && !$rally && !pk_waiting() && priest_dist() <= 20 && time - $since >= 600) { # holding while the sweep runs with the priest still counts (live 11:15-11:55: 40 min at ~1 kill/min, holding=1)
			return sprintf("map drained: %d kills in the last 10 min (under 15)", $n) if $n < 15;
		}
	}
	my @s = grep { $_->[2] eq $c } @killSamples;
	return '' unless @s >= 36; # 35+ min of samples: the first 20 and the last 15
	my ($t0, $k0) = @{$s[0]}[0, 1];
	my ($k20) = map { $_->[1] } grep { $_->[0] - $t0 >= 1200 } @s;
	my ($tn, $kn) = @{$s[-1]}[0, 1];
	my ($k15) = map { $_->[1] } reverse grep { $tn - $_->[0] >= 900 } @s;
	return '' unless defined $k20 && defined $k15;
	my ($first, $last) = (($k20 - $k0) / 20, ($kn - $k15) / 15);
	return '' unless $last < 1 || ($first > 0 && $last < 0.4 * $first);
	return sprintf("map drained: %.1f kills/min in the last 15 min (%.1f in the first 20)", $last, $first);
}
# every 30 s on her hunting / plan map: the arrival time is recorded (also right after a reload - the window starts
# then), and a drained map breaks the plan at once (live 06:48: no farmed file, no PLAN_CHANGE after 11 min, 0 kills)
my $dtAt = 0;
sub drained_tick {
	return if time - $dtAt < 30 || !$field || !$char;
	$dtAt = time;
	my $here = $field->baseName;
	my $hm = plan_map() || $huntMap || '';
	return if $hm eq '' || $here ne $hm || is_townmap($here);
	if (($planArr{map} || '') ne $here) { %planArr = (map => $here, at => time); farmed_save() }
	my $d = map_drained($here);
	return if $d eq '';
	$farmedAt{$here} = time; farmed_save();
	wd_report('PLAN_CHANGE', $here, "$here -> next fitting map: $d", 'map choice re-run');
	$plan = undef; plan_save();
	%planArr = ();
	$forceRepick = 1; # force_repick: the nearest fitting map not farmed in the last 30 min, announced to the party
}
sub plan_track {
	kill_sample() if $field && ($huntMap || '') eq $field->baseName;
	return unless $char && $field;
	my $here = $field->baseName;
	my $exp = ($char->{exp} || 0) + ($char->{exp_job} || 0);
	my $gain = $ptExp >= 0 && $exp != $ptExp;
	$gmKills++ if $gain && $exp > $ptExp && !is_townmap($here); # the measured step counts party kills by her exp gains
	if ($gain && $exp > $ptExp && ($here eq (plan_map() || $huntMap || '-'))) { # a kill she or the party made (shared exp)
		push @planKillTimes, time; $planKillAt = time;
		farmed_save() if time - ($pk_fsAt || 0) > 60 && ($pk_fsAt = time);
	}
	$ptExp = $exp;
	return if !$gain || is_townmap($here);
	my $hunt = ($config{lockMap} || '') ne '' ? $config{lockMap} : $homeMap;
	if ($plan && $plan->{map} eq $here) {
		$plan->{expAt} = time;
		$plan->{at} = time if time - $plan->{at} >= PLAN_SECS; # still farming it after 2 h: a new commitment (checks keep running)
		plan_save() if time - ($plan->{savedAt} || 0) > 60 && ($plan->{savedAt} = time);
		return;
	}
	return if $here ne $hunt || plan_map() ne '';
	$plan = {map => $here, at => time, expAt => time};
	plan_save();
	message "[partylead] PT16: farming $here - this is the plan for the next 2 h
", 'system';
}
# no crossing straight back through a portal: after a map change, 30 s without going back to the map she came from
# (unless dying) - live: aldebaran <-> mjolnir_12 every few seconds
my ($pgPrev, $pgFrom, $pgAt, $pgLog) = ('', '', 0, 0);
our ($arrInfo, %pgCross, $portalHoldUntil, $portalHoldMap); # arrival cell of the current map; portal-loop hold
sub portal_back_guard {
	return unless $char && $field;
	my $here = $field->baseName;
	if ($here ne $pgPrev) {
		($pgFrom, $pgAt, $pgPrev) = ($pgPrev, time, $here);
		$arrInfo = {map => $here, x => $char->{pos_to}{x}, y => $char->{pos_to}{y}, at => time, from => $pgFrom};
		# more than 2 crossings of the same portal in 3 min = a loop: hold on the town side for 90 s
		# only field / dungeon portals count: not town <-> building, not during a town run or a shop / storage task
		if ($pgFrom ne '' && !(is_townmap($pgFrom) && is_townmap($here)) && !$errand && !AI::inQueue('sellAuto', 'buyAuto', 'storageAuto', 'NPC') && !status_town_run()) {
			my $k = join('|', sort ($pgFrom, $here));
			$pgCross{$k} = [grep { time - $_ < 180 } @{$pgCross{$k} || []}, time];
			if (@{$pgCross{$k}} > 2) {
				my $safe = (is_dungeon($here) || cross_danger($here)) ? $pgFrom : $here;
				($portalHoldUntil, $portalHoldMap) = (time + 90, $safe);
				$pgCross{$k} = [];
				wd_report('PORTAL_LOOP', $here, "crossed $k 3 times in 3 min - holding on $safe for 90 s", "hold on $safe");
			}
		}
	}
	return if $pgFrom eq '' || time - $pgAt >= 30 || AI::inQueue('teleport') || pk_retreating();
	return if $char->{hp_max} && $char->{hp} / $char->{hp_max} < 0.2;
	my ($dm, $nm) = route_maps();
	return unless $nm eq $pgFrom || ($dm eq $pgFrom && $nm eq '');
	AI::clear('route', 'move');
	if (time - $pgLog > 10) { $pgLog = time; message "[partylead] no crossing back to $pgFrom within 30 s of coming from it - holding on $here\n", 'system' }
}
# the owner's re-pick (2026-11-15 "this party is not strong enough"): runs from the guarded tick wrapper, so no wait /
# collect / rally can skip it; overrides the PT16 commitment with a PLAN_CHANGE line
sub force_repick {
	return unless $forceRepick && $net && $net->getState() == 5 && $char && $field && $char->{party} && $char->{party}{users};
	my @fr0 = online_friends();
	return if @fr0 && !grep({ $apiPos{$_->[0]} && $apiPos{$_->[0]}{lv} } @fr0) && time - $fitLoadAt < 300;
	$forceRepick = 0;
	my $old = plan_map() || $homeMap || ($config{lockMap} || '-');
	my @keep = ($plan, $homeMap, $homeAt, $homeBy);
	($plan, $homeMap, $homeAt, $homeBy) = (undef, '', 0, 'auto'); # the area preference starts from where she is
	my ($m, $why) = choose_map();
	($plan, $homeMap, $homeAt, $homeBy) = @keep;
	if (defined $m && $m ne '') {
		wd_report('PLAN_CHANGE', $old, "$old -> $m: re-pick, the nearest fitting map ($why)", 'new plan on arrival');
		announce_plan($m);
		$plan = undef; plan_save();
		($homeMap, $homeAt, $homeBy) = ('', 0, 'auto');
		auto_goto($m, "re-pick, the nearest fitting map; $why") if $old ne $m;
	} else { $forceRepick = 1 } # nothing chosen yet: try again next tick
	step_save();
}
our $loadAnnounced = 0; # once per plugin load: the plan's meeting point said again, stale routes dropped
sub load_announce {
	return if $loadAnnounced || !($net && $net->getState() == 5 && $char && $field && $char->{party} && $char->{party}{users});
	my $pc = plan_map() || $homeMap;
	return if ($pc || '') eq '';
	$loadAnnounced = 1;
	my ($rdm) = route_maps();
	AI::clear('route', 'move') if $rdm ne '' && $rdm ne $pc && is_townmap($rdm) && town_of_map($pc) ne $rdm; # e.g. the old Prontera rally route
	announce_plan($pc);
	message "[partylead] load: plan $pc re-announced to the party\n", 'system';
}
# loot a lv80 party leaves on the floor (owner 2026-11-15: she barely gains exp; EC27/EC28 town runs at 50% weight):
# Fly Wings past the 10 she keeps (EC26; live: Fly Wing x217 = half the bag at the 10:57 town run, 11 picked up in 47 s
# on pay_dun01, so the 50% trigger came every ~25 min and twice before she even reached pay_dun02) and Jellopy (3z).
# While partied only; pickupitems.txt's own values come back when solo and on unload. Herbs / Skel-Bones stay (EC31).
my %pickSaved;
sub loot_trim_tick {
	return unless $char && $char->inventory;
	my $party = online_friends() ? 1 : 0;
	my $fw = $char->inventory->getByName('Fly Wing');
	my %off = ('fly wing' => ($party && $fw && ($fw->{amount} || 0) >= 10) ? 1 : 0, 'jellopy' => $party);
	foreach my $k (keys %off) {
		if ($off{$k}) {
			$pickSaved{$k} = exists $pickupitems{$k} ? $pickupitems{$k} : undef unless exists $pickSaved{$k};
			$pickupitems{$k} = 0;
		} elsif (exists $pickSaved{$k}) {
			my $v = delete $pickSaved{$k};
			if (defined $v) { $pickupitems{$k} = $v } else { delete $pickupitems{$k} }
		}
	}
}
sub loot_trim_restore {
	foreach my $k (keys %pickSaved) { my $v = delete $pickSaved{$k}; if (defined $v) { $pickupitems{$k} = $v } else { delete $pickupitems{$k} } }
}
sub tick_guarded {
	load_announce();
	force_repick();
	tp_guard();
	route_guard();
	lockmap_guard();
	tick(@_);
	town_back_guard() if $net && $net->getState() == 5;
	wait_idle_check() if $net && $net->getState() == 5;
	plan_track() if $net && $net->getState() == 5;
	mapexp_tick() if $net && $net->getState() == 5;
	loot_trim_tick() if $net && $net->getState() == 5;
	drained_tick() if $net && $net->getState() == 5;
	AI::clear('take', 'items_take', 'items_gather') if time < $lootBlockUntil && AI::inQueue('take', 'items_take', 'items_gather');
	hp_sample() if $net && $net->getState() == 5;
	portal_back_guard() if $net && $net->getState() == 5;
	if ($portalHoldUntil && time < $portalHoldUntil && $field && $field->baseName eq ($portalHoldMap || '') && AI::inQueue('route', 'move') && !AI::inQueue('teleport')
		&& !priest_elsewhere($portalHoldMap)) { # the hold never splits her from the priest: with the priest across the portal she goes to it
		AI::clear('route', 'move'); # PORTAL_LOOP: hold on the town side
	}
	$lmSet = $config{lockMap} || '' if defined $lmSet;
}
sub cmd_guarded {
	cmd_partylead(@_);
	$lmSet = $config{lockMap} || '' if defined $lmSet;
}

# ---- PT29 (2026-11-14): like every party member she asks the party priest for buffs she lacks ("bless pls", "agi?");
# the engine priest (legion/priestbuff.go buffAskHeard) reads such party lines and moves the asker to the front.
# Only with the priest on her map within 9 cells, never while idle in town, once per 90-150 s (varied).
my ($buffAskAt, $buffAskGap) = (0, 120);
sub has_status {
	my $re = shift;
	foreach my $k (keys %{$char->{statuses} || {}}) { return 1 if $k =~ $re || (($Globals::statusName{$k} || '') =~ $re) }
	return 0;
}
sub buff_ask_tick {
	return if time - $buffAskAt < $buffAskGap;
	return if !$char || $char->{dead} || !$field || !$char->{pos_to};
	my $here = $field->baseName;
	return if is_townmap($here) && !$rally && !$depart; # idle in town: no asking
	my $noBless = !has_status(qr/bless/i);
	my $noAgi = !has_status(qr/inc_?agi|increase ?ag/i);
	return unless $noBless || $noAgi;
	my $pr = '';
	foreach my $u (values %{$char->{party}{users} || {}}) {
		next if !$u || !$u->{online} || $u->{name} eq $char->{name};
		my $a = $apiPos{$u->{name}};
		next unless $a && ($a->{cls} || '') =~ /priest|acolyte/i;
		my ($pos) = friend_pos($u, $here);
		if ($pos && friend_map($u) eq $here && blockDistance($char->{pos_to}, $pos) <= 9) { $pr = $u->{name}; last }
	}
	return if $pr eq '';
	($buffAskAt, $buffAskGap) = (time, 90 + int(rand(61)));
	my $t = $noBless && $noAgi ? pick("buff pls", "buffs?", "bless agi pls", "can i get buffs?", "bless + agi pls ty")
		: $noBless ? pick("bless pls", "bless?", "blessing pls") : pick("agi pls", "agi?", "inc agi pls", "agi pls ty");
	psay($t, 1 + rand(2), 0);
}

# ---- zero-downtime watchdog (owner rule AY18, 2026-11-14: Ayla is never idle for any length of time) -------------------
# Audit 2026-11-14 (console log, 27.9 h): 38% of the time was in idle stretches of 60 s or more (waiting for the party
# 29% of those, char-select / login timeouts 27%, RODEX polling 19%, town / Kafra regroup 16%, routing 8%). Each of the
# plugin's own waits has its own timeout, but nothing ever asked "has she done anything useful lately?". This does:
# no kill, loot, errand, fight, exp gain or real route progress (a route to another map) for WD_IDLE seconds ->
# she takes the default productive action: go to the mind's map (homeMap / lockMap), else the map most of her party is
# on, else a level-fitting field, and farm there (the sweep: walk to monsters, fight). One map, one step; no mass action.
use constant WD_IDLE => 60;
my ($wdProdAt, $wdExp, $wdPos, $wdAct, $wdFired) = (time, 0, '', 0, 0);
my ($wdStuckSince, $wdStuckMap, %wdStuckN) = (time, '');
sub wd_target {
	my $pc = plan_map();
	return $pc if $pc ne '' && plan_reason() eq ''; # PT16: the committed map
	return $homeMap if mind_fresh();
	my ($tm) = party_turn();
	return $homeMap if $homeMap ne '' && $tm eq '' && map_fits($homeMap, party_fit_lv()) && !(time - ($crowdedAt{$homeMap} || 0) < 3600);
	my ($m) = choose_map();
	return $m if defined $m && $m ne '';
	return $homeMap ne '' ? $homeMap : 'prt_fild08';
}
# ---- reports (owner 2026-11-15: "why did her watchdog report nothing while she was stuck?"): every action or finding
# of the watchdog goes to her log as a "[watchdog] ..." line AND into the bug queue - logs/bugqueue.jsonl (this
# container's logs mount = openkore-ayla/logs on the host), which tools/system/bugq.py reads next to
# legion/data/bugqueue.jsonl (same record format). Deduped: one queue item per cause and map per hour.
my %wdReported; # "cause|map" -> last queue write
sub wd_report {
	my ($cause, $map, $text, $action) = @_;
	my $line = "[watchdog] $cause $text" . ($action ? " -> $action" : '');
	message "$line\n", 'system';
	my $k = "$cause|$map";
	return if time - ($wdReported{$k} || 0) < 3600;
	$wdReported{$k} = time;
	my @l = localtime;
	my $e = {id => sprintf("w%d-ayla", int(time * 1000)), t => int(time), # epoch seconds (queue b17947116852uo: a zone-less local time read 5 h off)
		from => 'ayla-watchdog', priority => ($cause eq 'STUCK' || $cause eq 'IDLE' ? 'high' : 'normal'), status => 'open',
		key => lc("ayla-watchdog-$cause-$map"), metric => ($cause eq 'WRONG_MAP' ? 'zph' : 'idle'), bots => 1,
		text => "Ayla watchdog $cause on $map: $text" . ($action ? " (action: $action)" : ''),
		evidence => sprintf("char %s B%d job %s; map %s cell %d,%d; homeMap %s (%s, %d min old); lockMap %s; rally %s; goto %s; party level %d",
			$char->{name}, $char->{lv} || 0, $char->{jobID} || '?', $map, $char->{pos_to}{x} || 0, $char->{pos_to}{y} || 0,
			$homeMap || '-', $homeBy, $homeAt ? int((time - $homeAt) / 60) : -1, $config{lockMap} || '-', ($rally ? $rally->{town} : '-'), $gotoMap || '-', party_fit_lv())};
	if (open(my $f, '>>', 'logs/bugqueue.jsonl')) { print $f JSON::PP->new->canonical->encode($e), "\n"; close $f }
}
my ($lowSince, $lowMap) = (0, '');
sub watchdog_tick {
	my $now = time;
	my $exp = ($char->{exp} || 0) + ($char->{exp_job} || 0);
	my $pos = "$char->{pos_to}{x},$char->{pos_to}{y}";
	my $here = $field->baseName;
	# same cell since when (a failing route / rally recalculates forever without a step: NOT progress)
	if ($pos ne $wdPos || $here ne ($wdStuckMap || '')) { ($wdStuckSince, $wdStuckMap) = ($now, $here) }
	my $moving = $now - $wdStuckSince < 20;
	# a rally counts as busy only while she walks toward it or waits at its Kafra (live 2026-11-15: a rally whose route
	# failed on izlude 24,98 kept the watchdog quiet for 12 min)
	my $rallyOk = $rally && ($here eq $rally->{town} || $moving);
	my $busy = AI::inQueue('attack', 'take', 'items_take', 'items_gather', 'NPC', 'sellAuto', 'buyAuto', 'storageAuto', 'teleport', 'skill_use', 'deal')
		|| $char->{sitting} || own_rest() || $char->{dead} || $rallyOk || (defined &aylagear::busy && aylagear::busy());
	# a real route (to another map, or a far cell) counts as progress while she is actually moving along it
	if (!$busy && AI::inQueue('route', 'move') && $moving) {
		my $ri = AI::findAction('route');
		my $ra = (defined $ri && $ri ne '') ? AI::args($ri) : undef;
		my $rm = ($ra && ref $ra && $ra->{dest} && $ra->{dest}{map}) ? $ra->{dest}{map} : '';
		$rm = eval { $rm->baseName } || '' if ref $rm;
		$busy = 1 if $rm ne '' && $rm ne $here;
	}
	$busy = 1 if $exp != $wdExp && $wdExp; # she is gaining exp: she kills
	# stuck on one cell while a rally / route to another map keeps failing ("Cannot calculate a route from izlude
	# (24,98)", live 2026-11-15): after 3 min the plugin cancels the rally itself, drops the goto and flies to her save
	# point (Butterfly Wing); the same destination failing twice is skipped for 2 h (unless the mind ordered it)
	# (live 07:29: "STUCK payon 179,107 ... -> DROP_MAP pay_dun02" while she held at the Kafra for the party)
	my $waitingNow = $holding || $rally || $depart || pk_waiting() || (is_townmap($here) && time - ($errandEnd || 0) < 300)
		|| ($townWaitAt && time - $townWaitAt < 300); # the town wait ("in <town>: waiting for X") is a wait too (live 03:37)
	if (!$waitingNow && AI::inQueue('route', 'move') && !AI::inQueue('attack', 'NPC', 'sellAuto', 'buyAuto', 'storageAuto', 'deal')
		&& !$char->{sitting} && !own_rest() && !$char->{dead} && !pk_waiting() && $now - $wdStuckSince > 180) { # holding for the party is no STUCK
		my $dest = $rally ? ($rally->{thenGoto} || $rally->{dest} || $rally->{town}) : ($gotoMap || $config{lockMap} || '');
		wd_report('STUCK', $here, "$here $pos for " . int(($now - $wdStuckSince) / 60) . " min, route to " . ($dest || '?') . " fails" . ($rally ? " (rally $rally->{town})" : ''),
			($rally ? 'rally cancelled, ' : '') . 'respawn to ' . ($config{saveMap} || 'the save point'));
		# a respawn that lands in the town she is stuck in changes nothing (live 2026-11-14: STUCK geffen 118,39, route to
		# gl_church fails -> respawn to geffen -> the same route again, in a loop): the destination is dropped for 2 h at
		# once, whoever set it, and she falls back to her own gear-rated choice
		my $sameTown = is_townmap($here) && ($config{saveMap} || '') eq $here;
		if ($dest ne '' && !is_townmap($dest) && ($sameTown || ++$wdStuckN{$dest} >= 2 || $droppedUntil{$dest})) { drop_map($dest, $sameTown ? "route from $here fails and a respawn lands in $here again" : "route fails again"); $wdStuckN{$dest} = 0 }
		elsif ($dest ne '' && ++$wdStuckN{$dest} >= 2 && !mind_fresh()) { $routeFailAt{$dest} = $now + 3600; $wdStuckN{$dest} = 0 }
		if ($rally) { end_rally("can't find a way there", 0); $rally = undef }
		$depart = undef; $gotoMap = ''; $holding = 0;
		AI::clear('route', 'move');
		if ($sameTown) { ($wdStuckSince, $wdProdAt, $wdAct) = ($now, $now, $now); $wdExp = $exp; $wdPos = $pos; $fitAt = 0; return } # no respawn: fit_tick picks her own map now
		Commands::cmdRespawn('respawn'); # the watchdog's own respawn (not through the outside-command guard)
		($wdStuckSince, $wdProdAt, $wdAct) = ($now, $now, $now);
		$wdExp = $exp; $wdPos = $pos;
		return;
	}
	# a map far below her level for 5+ min (not a short crossing): report it - fit_tick moves her unless a fresh order holds her
	my $top = map_top($here);
	# the same effective level as the map choice (gear-rated + the conservative step), and never on the PT16 plan map
	# she farms with success (exp in the last 5 min, no death there in 30 min) - live: "WRONG_MAP on pay_dun02 ... at
	# base 82" on her own conservative pick
	my $okPlan = plan_map() eq $here && $plan && $plan->{expAt} && time - $plan->{expAt} < 300 && !grep({ $_->[1] eq $here && time - $_->[0] < 1800 } @planDeaths);
	if ($top && !is_townmap($here) && !$okPlan && transit_fit_lv() >= $top + 15) {
		($lowSince, $lowMap) = ($now, $here) if $lowMap ne $here;
		if ($now - $lowSince > 300) {
			wd_report('WRONG_MAP', $here, "on $here (worth to level $top) for " . int(($now - $lowSince) / 60) . " min, party rated " . party_fit_lv() . " (base $char->{lv}), target " . ($homeMap || '-') . (mind_fresh() ? " (mind's order)" : ''),
				mind_fresh() ? "kept: the mind's order is fresh" : "own map choice re-checked");
			$lowSince = $now; # next log line in 5 min (the queue item once an hour)
			$fitAt = 0 unless mind_fresh(); # re-check the map choice now
		}
	} else { $lowMap = '' }
	$wdExp = $exp; $wdPos = $pos;
	if ($busy) { $wdProdAt = $now; return }
	return if $now - $wdProdAt < WD_IDLE;
	return if $now - $wdAct < WD_IDLE; # one step per minute at most
	$wdAct = $now; $wdFired++;
	my $tgt = wd_target();
	wd_report('IDLE', $here, "no productive action for " . int($now - $wdProdAt) . " s on $here $pos (holding=$holding" . ($rally ? ", rally $rally->{town}" : '') . ", #$wdFired)", "farm $tgt");
	Commands::run('stand') if $char->{sitting};
	if ($holding) { restore() }       # a stalled hold never outlives a minute of nothing
	$depart = undef if $depart;       # nor a stalled departure wait
	if ($tgt ne $homeMap && !mind_fresh()) { auto_goto($tgt, 'the watchdog'); return } # her own choice: goto (Kafra save rule, lockMap)
	set_home($tgt);
	$config{lockMap} = $tgt if ($config{lockMap} || '') eq '' || is_townmap($config{lockMap});
	AI::clear('route', 'move');
	if ($here eq $tgt) {
		my %a = (return => 0);
		short_walk(undef, \%a);
	} else {
		safe_route($tgt, undef, undef, attackOnRoute => route_aor());
	}
}

sub restore {
	if ($holding && $saved) {
		$config{route_randomWalk} = $saved->{randomWalk};
		# back to the mind's hunting map (homeMap) - not a stale lockMap saved by an older hold/rally
		$config{lockMap} = ($homeMap ne '' ? $homeMap : $saved->{lockMap}) if $config{lockMap} eq ''; # keep a map her mind set meanwhile
	}
	$holding = 0;
}

# ---- PT31 / PT04 / PT06 (owner 2026-11-14: "staying close to the party is rule number one"): while friends are online
# she never escapes alone with a random Teleport (live 2026-11-14: "You are casting Teleport on yourself" on gef_fild08,
# then 51% HP with monsters on her, the friends in geffen / gef_fild07). In danger she falls back to the priest / tank
# (else the nearest friend) on her map, drinks (useSelf_item), says "help, N on me" in party chat; only about to die
# (< 20% HP and still being hit) a Butterfly Wing to her save Kafra, announced ("winged to <town> kafra ... back to
# <map>"). A boss in sight: the party pulls back together (announced), not her alone. No friend online: the old solo
# escape rules (teleportAuto 35% / 6 monsters). Her targets stay within 20 cells of the friends near her
# (stick_target, from area_target). conf partylead_stick 0 turns it off.
use constant STICK_DEADLY => 0.20;
my $stickLowAt = 0; # since when HP reads under STICK_DEADLY
my ($stickHelpAt, $stickRunAt, $stickWingAt, $stickBossAt, $stickHp, $stickHitAt, $stickDeadly) = (0, 0, 0, 0, 1, 0, undef);
our $stickWinged; # {town, back, at}: her last announced wing (the town-run line says the same)
# live 03:5x: "falling back to the friends on geffen" repeated for a minute at 4-14 monsters but she never left - the
# route was re-made every 5 s and attackAuto / the rest of the tick put a fight on top of it. Now the flight is a
# commitment: attackAuto off, attacks cleared every tick, the route kept, until she is off that map (90 s at most).
our ($stickFlee, $fleeAA);
my ($mvPos, $mvAt) = ('', 0);
# her cell changed within the last 2 s (a fall-back only runs while she really moves)
sub moved_recently {
	my $k = "$char->{pos_to}{x},$char->{pos_to}{y}";
	if ($k ne $mvPos) { ($mvPos, $mvAt) = ($k, time) }
	return time - $mvAt < 2 ? 1 : 0;
}
# an outside 'respawn' (Butterfly Wing to the save point) only when she is really stuck: not in a town, no friend within
# 20 cells, and on one cell for 3+ min; dead = the normal respawn. Refusals: "[watchdog] RESPAWN refused: <reason>"
sub respawn_guard {
	my ($sw, $args) = @_;
	return Commands::cmdRespawn($sw) if !$char || $char->{dead};
	my $here = $field ? $field->baseName : '';
	my $why = '';
	if (is_townmap($here)) { $why = "she is in $here (a town)" }
	else {
		foreach my $f (stick_mates()) { if (blockDistance($char->{pos_to}, $f->[1]) <= 20) { $why = "$f->[0] is " . blockDistance($char->{pos_to}, $f->[1]) . " cells away"; last } }
		moved_recently();
		$why = 'she moved ' . int(time - $mvAt) . ' s ago (not stuck 3+ min)' if $why eq '' && $mvAt && time - $mvAt < 180;
	}
	if ($why ne '') { wd_report('RESPAWN', $here, "refused: $why", 'no wing'); return }
	message "[partylead] respawn allowed: stuck on $here for " . int(time - $mvAt) . " s with no friend near\n", 'system';
	return Commands::cmdRespawn($sw);
}
# ---- party sweep on the farm map (owner 2026-11-15: on pay_dun02 she one-shot a Bongun, then stood still and only
# side-stepped): no monster within 8 cells for 8 s -> a short leg (10-15 cells) toward the nearest seen monster (live or
# remembered, mob_target) or an unexplored cell, staying within 20 cells of the priest and pausing while the priest is
# more than 10 cells away; never toward a group (another monster within 5 cells of the spot); not the same spot twice
my ($fsIdleSince, $fsAt, @fsLast) = (0, 0);
my ($fsWhyAt, $fsWhy) = (0, '');
my $fsFightAt = 0;
my $fsLeg; # {idx, d0, at}: the waypoint leg under way (unreachable check) # her last fight on the farm map (the sweep then goes on from the nearest waypoint)
# why no leg (debug, every 30 s when it repeats): "[partylead] sweep waits: <reason>"
sub fs_why { my ($r, $w) = @_; if ($w ne $fsWhy || time - $fsWhyAt > 30) { ($fsWhy, $fsWhyAt) = ($w, time); message "[partylead] sweep waits: $w\n", 'system' if $w !~ /^idle under/ } return $r }
# a monster she attacks (mon_control attack flag not 0: plants / mushrooms are 0)
sub fightable {
	my $m = shift;
	my $mc = eval { Misc::mon_control($m->{name}, $m->{nameID}) };
	return 0 if ref $mc && defined $mc->{attack_auto} && $mc->{attack_auto} =~ /^0$/;
	return 0 if ($m->{name} || '') =~ /\b(Plant|Mushroom|Shining Plant)\b/;
	return 1;
}
sub farm_sweep {
	return 0 unless $char && $field && $char->{pos_to};
	my $here = $field->baseName;
	return 0 if ($huntMap || '') eq '' || $here ne $huntMap || $char->{sitting} || $char->{dead} || $holding || $rally;
	if (AI::inQueue('attack', 'take', 'items_take', 'items_gather', 'NPC', 'sitting', 'sitAuto', 'sellAuto', 'buyAuto', 'storageAuto', 'teleport') || party_resting() || own_rest()) {
		$fsFightAt = time if AI::inQueue('attack');
		return fs_why(0, 'busy: ' . join(',', grep { AI::inQueue($_) } qw(attack take items_take items_gather NPC sitting sitAuto sellAuto buyAuto storageAuto teleport)) . (party_resting() ? ' party resting' : '') . (own_rest() ? ' own rest' : ''));
	}
	my $me = my_pos();
	# only monsters she would attack count (live 76bbce6: a Red Plant 2 cells away - mon_control attack 0 - kept her
	# frozen for 4 min: "something to fight here" that she never fights)
	my @mons = grep { $_->{pos_to} && defined $_->{pos_to}{x} && !$_->{dead} && fightable($_) } @{$monstersList ? $monstersList->getItems() : []};
	if (grep { blockDistance($me, $_->{pos_to}) <= 8 } @mons) { $fsIdleSince = 0; $fsFightAt = time; return fs_why(0, 'a monster within 8 cells') }
	$fsIdleSince ||= time;
	return fs_why(0, 'idle under 8 s') if time - $fsIdleSince < 8;
	# a waypoint leg that brought her no closer within 12 s: that waypoint is unreachable, skipped from now on (queue
	# b1794726149fx4: "Cannot reach target" on pay_dun02 waypoints)
	if ($fsLeg && time - $fsLeg->{at} > 12) {
		my $w = $sweep[$fsLeg->{idx}];
		if ($w && blockDistance($me, {x => $w->[0], y => $w->[1]}) > $fsLeg->{d0} - 3 && !$sweepBad{$fsLeg->{idx}}) {
			$sweepBad{$fsLeg->{idx}} = 1;
			message sprintf("[partylead] sweep: waypoint %d (%d,%d) unreachable - skipped\n", $fsLeg->{idx} + 1, @$w), 'system';
			AI::clear('route', 'move');
		}
		$fsLeg = undef;
	}
	return 1 if AI::inQueue('route', 'move') && time - $fsAt < 12; # a leg under way
	my $an = stick_anchor(stick_mates());
	my $ap = $an ? $an->[1] : undef;
	return fs_why(0, "the priest $an->[0] is " . blockDistance($me, $ap) . " cells away") if $ap && blockDistance($me, $ap) > 10; # let the priest close up first (PT04)
	my $prd = priest_dist();
	return fs_why(0, $prd >= 999 ? 'the priest is not near (position unknown / another map)' : "the priest is $prd cells away") if $prd > 10 && party_has_priest_online();
	my $ok = sub {
		my ($x, $y) = @_;
		return 0 unless eval { $field->isWalkable($x, $y) };
		return 0 if $ap && blockDistance({x => $x, y => $y}, $ap) > 20;
		my $b = int($x / 6) . ',' . int($y / 6);
		return 0 if grep { $_ eq $b } @fsLast;
		my $grp = grep { blockDistance({x => $x, y => $y}, $_->{pos_to}) <= 5 } @mons;
		return 0 if $grp >= 2; # a group there: not toward it
		return 1;
	};
	# a fightable monster on screen within 15 (not in a group): a leg toward it; otherwise the map's sweep loop in order
	# (owner 2026-11-15: random legs kept the party in one corner of pay_dun02, 2 kills in 5 min)
	my ($tx, $ty, $what);
	my ($near) = sort { blockDistance($me, $a->{pos_to}) <=> blockDistance($me, $b->{pos_to}) } grep { blockDistance($me, $_->{pos_to}) <= 15 } @mons;
	if ($near) {
		my $t = [$near->{pos_to}{x}, $near->{pos_to}{y}];
		my ($dx, $dy) = ($t->[0] - $me->{x}, $t->[1] - $me->{y});
		my $n = (abs($dx) > abs($dy) ? abs($dx) : abs($dy)) || 1;
		my $k = $n > 10 ? 10 / $n : 1;
		($tx, $ty) = (int($me->{x} + $dx * $k), int($me->{y} + $dy * $k));
		if ($ok->($tx, $ty)) { $what = "toward $near->{name}" } else { ($tx, $ty) = () }
	}
	if (!defined $tx) {
		sweep_build($me->{x}, $me->{y}) if !@sweep || ($sweepMap || '') ne $here;
		if (@sweep) {
			if ($fsFightAt > $fsAt) { # after a fight: on from the nearest waypoint
				my ($bi, $bd) = (0, 1e9);
				for my $q (0 .. $#sweep) { my $d = blockDistance($me, {x => $sweep[$q][0], y => $sweep[$q][1]}); ($bi, $bd) = ($q, $d) if $d < $bd }
				$sweepIdx = $bi;
			}
			for (1 .. scalar @sweep) { # the next waypoint: not reached yet, no group at it
				my $w = $sweep[$sweepIdx];
				my $grp = grep { blockDistance({x => $w->[0], y => $w->[1]}, $_->{pos_to}) <= 5 } @mons;
				last unless blockDistance($me, {x => $w->[0], y => $w->[1]}) <= 3 || $grp >= 2 || $sweepBad{$sweepIdx};
				sweep_next();
			}
			my $w = $sweep[$sweepIdx];
			my ($dx, $dy) = ($w->[0] - $me->{x}, $w->[1] - $me->{y});
			my $n = (abs($dx) > abs($dy) ? abs($dx) : abs($dy)) || 1;
			my $k = $n > 10 ? 10 / $n : 1; # legs of at most 10 cells: the priest stays within 20
			($tx, $ty) = (int($me->{x} + $dx * $k), int($me->{y} + $dy * $k));
			($tx, $ty) = ($w->[0], $w->[1]) unless eval { $field->isWalkable($tx, $ty) };
			$what = sprintf("waypoint %d/%d", $sweepIdx + 1, scalar @sweep);
			$fsLeg = {idx => $sweepIdx, d0 => blockDistance($me, {x => $w->[0], y => $w->[1]}), at => time};
		}
	}
	return fs_why(0, 'no walkable leg cell found') unless defined $tx;
	unshift @fsLast, int($tx / 6) . ',' . int($ty / 6); splice(@fsLast, 3) if @fsLast > 3;
	$fsAt = time; $fsIdleSince = time;
	AI::clear('route', 'move');
	safe_route($here, $tx, $ty, distFromGoal => 2, maxRouteTime => 10, noMapRoute => 1, attackOnRoute => 2);
	message "[partylead] sweep: " . ($what || 'leg') . " -> $tx,$ty" . ($an ? " ($an->[0] " . blockDistance($me, $ap) . " cells)" : '') . "\n", 'system';
	return 1;
}
# the nearest monster hitting her, attacked now (OpenKore's attack task takes it from there)
sub fight_back {
	return if AI::inQueue('attack');
	my ($best, $bd);
	foreach my $mo (@{$monstersList ? $monstersList->getItems() : []}) {
		next unless $mo->{pos_to} && defined $mo->{pos_to}{x} && !$mo->{dead} && ($mo->{dmgToYou} || $mo->{missedYou});
		my $d = blockDistance($char->{pos_to}, $mo->{pos_to});
		($best, $bd) = ($mo, $d) if !defined $bd || $d < $bd;
	}
	return unless $best;
	AI::clear('route', 'move');
	main::attack($best->{ID});
	message "[partylead] PT31: not moving with monsters on her - fighting back ($best->{name})
", 'system';
}
# a fall-back step is allowed only while she moves; stuck 2 s -> she fights the nearest attacker instead
sub moved_recently_or_fight {
	return 1 if moved_recently() || $mvAt == 0;
	fight_back();
	return 0;
}

# ---- PT04 as a hard invariant (owner 2026-11-14: "Ayla is still wandering around without her party"): with friends
# online she never travels or farms with no friend within 20 cells. Leaving town for a hunt: only with the priest
# (priest offline: 2 friends) beside her. Travelling: the nearest friend more than 20 cells behind -> she stops and waits
# ("waiting for u at <map> <x,y>", once a minute; no sitting but OpenKore's own HP/SP sitAuto). On the farm map she stays
# within 20 cells of the priest (else of a friend) and walks back to them. After 3 min of waiting she goes TO the priest
# ("coming to u, <name>") instead of going on alone. Crossing a map with aggressive packs near / above the party's
# gear-rated level (%CROSS_DANGER; live 2026-11-14: alone on gef_fild10, 7-13 orcs, winged out at 4%) waits at the
# last safe map until the priest and one more friend are within 10 cells, says "crossing <map> together, stay close"
# and on that map keeps the priest within 10 cells. 2 wings on it within 30 min -> the destination behind it is
# dropped for 2 h and her own choice picks the next fitting map. No friend online: solo (fit_tick picks a field).
# conf partylead_pack 0 turns it off.
our %CROSS_DANGER = (gef_fild10 => 60); # map -> top of its monster band (orc packs, aggressive)
# farm maps -> the dangerous transit maps on the way from their town (orcsdun01/02: entrance on gef_fild10)
our %ROUTE_VIA = (orcsdun01 => ['gef_fild10'], orcsdun02 => ['gef_fild10']);
sub route_unsafe { my ($m, $plv) = @_; return gm_route_unsafe($m, defined $plv ? $plv : transit_fit_lv(), \%ROUTE_VIA, \%CROSS_DANGER) }
# ---- pack begin (pure: tested offline by tests/ayla/pack_test.pl)
# $s: {here, friends => [[name, class, map, dist or undef]], farm, leaving, cross ('' | 'pre' | 'on'), waited (s)}
# returns (action, anchor row): solo | ok | wait | back (walk to the anchor on this map) | goto (go to the anchor)
sub pack_decide {
	my $s = shift;
	my @f = @{$s->{friends} || []};
	return ('solo', undef) unless @f;
	my $here = $s->{here};
	my $cross = $s->{cross} || '';
	my $R = $cross ? 10 : 20;
	my @near = grep { $_->[2] eq $here && defined $_->[3] && $_->[3] <= $R } @f;
	my $isPr = sub { ($_[0][1] || '') =~ /priest|acolyte/i };
	my $sameD = sub { my $r = shift; return $r->[2] eq $here && defined $r->[3] ? $r->[3] : 9999 };
	my ($pr) = sort { $sameD->($a) <=> $sameD->($b) } grep { $isPr->($_) } @f;
	my $prNear = $pr && grep({ $_->[0] eq $pr->[0] } @near) ? 1 : 0;
	my $two = @f >= 2 ? 2 : 1;
	# AY22 (owner 2026-11-14): with a priest online the priest is the anchor - within 10 cells on her map she goes on;
	# behind (> 10 or another map) she stops and waits 10 s, then walks back to the priest (goto, until within 5)
	# Pace (owner 2026-11-15): she travels in short legs at the priest's pace - on only with the priest within 6 cells;
	# 7-15 cells: she waits for it. A collect starts only beyond 15 cells / another map, and never while the priest is
	# coming toward her (distance shrinking): then she stands still. Collecting ends when the priest has been within
	# 6 cells for 3 s ($s->{nearFor}); she walks on toward it only while the distance hasn't shrunk for 10 s.
	if ($pr) {
		my $prD = $pr->[2] eq $here && defined $pr->[3] ? $pr->[3] : undef;
		my $near6 = defined $prD && $prD <= 6 ? 1 : 0;
		return ('wait', $pr) if $s->{farm} && !defined $prD && $pr->[2] eq $here; # on her map, cell unknown: no farming ahead
		if ($s->{farm} && defined $prD) { # PT04 on the farm map: farm freely within 20 cells, go toward it beyond
			return ('ok', $pr) if $prD <= 20 && !$s->{collecting};
			return ('ok', $pr) if $s->{collecting} && $prD <= 6;
			return ('goto', $pr);
		}
		if ($s->{collecting}) {
			return ('ok', $pr) if $near6 && ($s->{nearFor} || 0) >= 3;
			return ('wait', $pr) if $near6 || $s->{shrinking}; # the priest is here / coming: never both walk
			return ('goto', $pr);
		}
		my $ok = $near6;
		$ok = 0 if $ok && $cross eq 'pre' && @near < $two; # crossing a dangerous map: the priest + one more within 10
		return ('ok', $pr) if $ok;
		return ('wait', $pr) if defined $prD && $prD <= 15; # pace: wait for it to close up
		return ('wait', $pr) if $s->{shrinking};            # coming toward her: stand still
		return ('goto', $pr) if ($s->{waited} || 0) >= (defined $prD ? 10 : 60); # another map: 60 s for it to come first
		return ('wait', $pr);
	}
	my $ok;
	if ($cross eq 'pre') { $ok = $pr && $prNear && @near >= $two ? 1 : 0 } # no known priest: no crossing (live 03:5x)
	elsif ($cross eq 'on') { $ok = $pr && $prNear ? 1 : 0 }
	elsif ($s->{leaving}) { $ok = $pr ? $prNear : @near >= $two }
	elsif ($s->{farm} && $pr && $pr->[2] eq $here) { $ok = $prNear }
	else { $ok = @near >= 1 }
	my ($nearest) = sort { $sameD->($a) <=> $sameD->($b) } @f;
	my $an = $pr || $nearest;
	return ('ok', $an) if $ok;
	# no priest online: she holds where she is until a friend is close (the party comes to her)
	return ('wait', $an);
}
# waiting for the party she fights instead of standing (owner 2026-11-14 "fighting is her main priority"): on the map
# she waits on when it is safe, else (in town) the best safe field next to the town. Safe = not a dangerous crossing,
# monsters not above the party's gear-rated level. '' = nothing fits (she waits as before).
# $cands: the fields next to this town; $tops: map -> top of its monster band; $danger: map -> 1
sub pack_wait_map {
	my ($here, $town, $cands, $plv, $tops, $danger) = @_;
	my $safe = sub { my $m = shift; return !$danger->{$m} && ($tops->{$m} || 0) > 0 && $tops->{$m} <= $plv + 5 };
	return $safe->($here) ? $here : '' unless $town;
	my ($best) = sort { $tops->{$b} <=> $tops->{$a} } grep { $safe->($_) } @{$cands || []};
	return $best || '';
}
# PT32 (owner 2026-11-14): fight in place near the party, one or a few monsters at a time where the priest covers.
# A new target only: within 8 cells of her or of the priest; no other aggressive monster within 2 cells of the way to it;
# not while 2+ monsters are already on her (then only those). What is hitting her is always fair game.
# $me / $t / $pr: {x, y} ($pr undef = no priest on the map); $others: positions of the other aggressive monsters;
# $onMe: monsters on her now; $tOnMe: this target is one of them
sub pt32_ok {
	my ($me, $t, $pr, $others, $onMe, $tOnMe, $all) = @_;
	return 1 if $tOnMe;
	return 0 if $onMe >= 2;
	my $d = sub { my ($a, $b) = @_; my ($dx, $dy) = (abs($a->{x} - $b->{x}), abs($a->{y} - $b->{y})); return $dx > $dy ? $dx : $dy };
	# never into a group (live 00:26: 11 Zombies on her in pay_dun01): no other monster within 5 cells of the target
	foreach my $o (@{$all || []}) { return 0 if $d->($o, $t) <= 5 && !($o->{x} == $t->{x} && $o->{y} == $t->{y}) }
	return 0 unless $d->($me, $t) <= 8 || ($pr && $d->($pr, $t) <= 8);
	my $len = $d->($me, $t);
	foreach my $o (@{$others || []}) {
		next if $d->($me, $o) <= 1; # already beside her: on her, not on the way
		for my $i (1 .. $len) {
			my $p = {x => $me->{x} + ($t->{x} - $me->{x}) * $i / $len, y => $me->{y} + ($t->{y} - $me->{y}) * $i / $len};
			return 0 if $d->($p, $o) <= 2 && $d->($o, $t) > 0;
		}
	}
	return 1;
}
# in a dungeon with the priest away (another map / 20+ cells): wait at the cave entrance - for 60 s after arriving, and
# after that as long as no monster is on her (only then the town Kafra)
sub entrance_wait {
	my ($prD, $atEntrance, $hit) = @_;
	return 0 if defined $prD && $prD <= 20;
	return ($atEntrance || !$hit) ? 1 : 0;
}
# ---- pack end
our %WAIT_FIELDS = (geffen => ['gef_fild13', 'gef_fild07'], prontera => ['prt_fild08', 'prt_fild05', 'prt_fild06', 'prt_fild01']);
sub wait_fields_for { return 0 } # owner 2026-11-14: rally / departure waits hold at the meeting point (no field trips)
my %pk = (waitAt => 0, sayAt => 0, logAt => 0, goName => '', routeAt => 0, crossSaid => '', wf => undef);
my @crossWings; # [time, map]
sub pk_waiting { return $pk{waitAt} ? 1 : 0 }
sub pk_prnear_recent { return time - ($pk{prLastNear} || 0) < 20 ? 1 : 0 } # the priest was within 15 cells lately
sub pk_retreating { return ($pk{retreatSaid} || '') ne '' ? 1 : 0 } # back to the town's Kafra while the priest is away
sub pk_lmhold { return $pk{lmHold} || '' }
sub cross_danger { my $m = shift || ''; return $CROSS_DANGER{$m} && transit_fit_lv() < $CROSS_DANGER{$m} + 10 ? 1 : 0 }
# her current map route: (destination map, the next map on the way)
sub route_maps {
	my $ri = AI::findAction('route');
	return ('', '') unless defined $ri && $ri ne '';
	my $ra = AI::args($ri);
	return ('', '') unless ref $ra;
	my $dm = (ref $ra->{dest} eq 'HASH' && $ra->{dest}{map}) ? $ra->{dest}{map} : '';
	$dm = eval { $dm->baseName } || '' if ref $dm;
	my $nm = (ref $ra->{mapSolution} eq 'ARRAY' && @{$ra->{mapSolution}} >= 2) ? ($ra->{mapSolution}[1]{map} || '') : '';
	$nm = $dm if $nm eq '' && $dm ne '' && $field && $dm ne $field->baseName;
	return ($dm, $nm);
}
# the portal cell her route takes off this map ({x, y}) or undef (OpenKore's map solution: "map x y=...")
sub route_portal {
	my $here = shift;
	my $ri = AI::findAction('route');
	return undef unless defined $ri && $ri ne '';
	my $ra = AI::args($ri);
	return undef unless ref $ra && ref $ra->{mapSolution} eq 'ARRAY' && @{$ra->{mapSolution}};
	my $st = $ra->{mapSolution}[0];
	return undef unless ref $st && ($st->{portal} || '') =~ /^(\S+)\s+(\d+)\s+(\d+)/ && $1 eq $here;
	return {x => $2, y => $3};
}
sub cross_wing {
	my ($m, $dest) = @_;
	@crossWings = grep { time - $_->[0] < 1800 && $_->[1] eq $m } @crossWings;
	push @crossWings, [time, $m];
	return unless @crossWings >= 2 && $dest ne '' && $dest ne $m && !is_townmap($dest);
	@crossWings = ();
	drop_map($dest, "crossing $m failed twice (2 wings within 30 min); the next fitting map that does not need it");
}
sub friend_cls {
	my $n = shift;
	my $a = $apiPos{$n};
	return $a->{cls} if $a && ($a->{cls} || '') ne '';
	my $pl = $playersList ? $playersList->getByName($n) : undef;
	my $j = $pl ? ($pl->{jobID} // '') : '';
	return 'Priest' if $j =~ /^(4|8|15|4005|4009|4016)$/;
	return '';
}
# one party line per decision: at most one of "coming to u" / "waiting for u" / "im at ..." every 30 s
# the Kafra of town $t where most of the party (the priest among them, if online) stands: [x, y] or undef
sub gathered_kafra {
	my ($t, $fr) = @_;
	my @on = grep { !friend_dead($_->[0]) } @$fr;
	return undef unless @on;
	my $need = @on >= 2 ? int((@on + 1) / 2) : 1;
	my ($prName) = map { $_->[0] } grep { ($_->[1] || '') =~ /priest|acolyte/i } @on;
	foreach my $k (@{$KAFRA{$t} || []}) {
		my ($n, $pr) = (0, 0);
		foreach my $f (@on) {
			next unless $f->[2] eq $t && $f->[4] && defined $f->[4]{x};
			next if blockDistance({x => $k->[0], y => $k->[1]}, $f->[4]) > 12;
			$n++; $pr = 1 if defined $prName && $f->[0] eq $prName;
		}
		return [$k->[0], $k->[1]] if $n >= $need && (!defined $prName || $pr);
	}
	return undef;
}
sub pk_say { my $t = shift; return if time - ($pk{lineAt} || 0) < 30; $pk{lineAt} = time; psay($t, 1, 0) }
sub pack_tick {
	# a lockMap held during the last tick's wait comes back first (the wait below takes it again if still waiting)
	if (($pk{lmHold} || '') ne '') { $config{lockMap} = $pk{lmHold} if ($config{lockMap} || '') eq ''; $pk{lmHold} = '' }
	return 0 if defined $config{partylead_pack} && $config{partylead_pack} eq '0';
	# a rally in ANOTHER town is travel too (live: the owner's re-pick to pay_dun02 started a Payon save-stop rally and
	# she took the Kafra from aldebaran alone): the departure rule holds until she is in the rally town
	my $rallyTravel = $rally && $field && $field->baseName ne $rally->{town} ? 1 : 0;
	return 0 if ($rally && !$rallyTravel) || $depart || $collect || $fetchMap ne '' || $char->{dead} || !$char->{pos_to}; # a legacy hold no longer bypasses the priest rules (it let goto walk her into pay_dun00 alone)
	return 0 if AI::inQueue('NPC', 'sellAuto', 'buyAuto', 'storageAuto', 'deal', 'teleport') || (defined &aylagear::busy && aylagear::busy());
	my $here = $field->baseName;
	my @fr;
	foreach my $u (values %{$char->{party}{users} || {}}) {
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name}) || friend_dead($u->{name});
		my $m = friend_map($u); $m =~ s/\.gat$//;
		my ($pos) = $m eq $here ? friend_pos($u, $here) : ();
		my $a = $apiPos{$u->{name}};
		push @fr, [$u->{name}, friend_cls($u->{name}), $m, ($pos && defined $pos->{x}) ? blockDistance(my_pos(), $pos) : undef, $pos];
	}
	if (!@fr) { ($pk{waitAt}, $pk{goName}, $pk{wf}, $regroupTo) = (0, '', undef, ''); return 0 } # everyone offline: she may farm solo
	my ($dm, $nm) = route_maps();
	my $farm = $huntMap ne '' && $here eq $huntMap ? 1 : 0;
	my $town = is_townmap($here);
	my $leaving = $town && (($dm ne '' && !is_townmap($dm)) || $rallyTravel) ? 1 : 0;
	# AY22 in towns too (live 03:47: 6 min "waiting for the party" in prontera, the priest 42 cells away in the same
	# town): with the priest beyond 15 cells (or on another map) and no errand running, she goes to it like on a field
	# her own meeting point in this town (announced in the last 15 min), or the party gathered at a Kafra here: she goes
	# to that Kafra and the rally (60 s cap, then AY22) takes over - the collect never holds her away from it (live:
	# "waiting for Zendancer84 (payon 22 cells)" while the priest waited at the Payon Kafra she had named)
	my $atKafraWithPriest = $KAFRA{$here} && priest_dist() <= 6 && grep({ blockDistance(my_pos(), {x => $_->[0], y => $_->[1]}) <= 6 } @{$KAFRA{$here} || []});
	if ($town && !$errand && !$rally && $KAFRA{$here} && time - ($pk{meetRallyAt} || 0) > 300 && time - ($rallyEndedAt{$here} || 0) > 600 && !$atKafraWithPriest && (($meetTown eq $here && time - $meetTownAt < 900) || gathered_kafra($here, \@fr))) {
		my $k = gathered_kafra($here, \@fr) || $KAFRA{$here}[0];
		$rallyNext = 0;
		start_rally($here, $k->[0], $k->[1], "her meeting point", 0);
		$pk{meetRallyAt} = time; # one meeting-point rally per 5 min (it ends after 60 s at the Kafra)
		if ($rally) {
			$rally->{saved} = 1; $rally->{dest} = plan_map() || $homeMap || ''; $rally->{mind} = 1;
			$rally->{said} = 1; $rally->{friendsDone} = 1; # a meeting, not a save stop: no "save here pls"
			($meetTown, $meetTownAt) = ('', 0);
			message "[partylead] going to the meeting Kafra in $here ($k->[0],$k->[1])\n", 'system';
			return 0;
		}
	}
	my $townCollect = $town && !$leaving && !$errand && grep({ ($_->[1] || '') =~ /priest|acolyte/i } @fr) && priest_dist() > 15 ? 1 : 0;
	$leaving = 1 if $townCollect;
	return 0 if $town && !$leaving && !$pk{wf};       # town errands / shopping / the Kafra regroup
	return 0 if !$farm && $dm ne '' && is_townmap($dm) && !$pk{wf} && !$rallyTravel; # a town run back (the party meets at the Kafra)
	# the party (with the priest) already gathered at a Kafra of THIS town: she never walks away from it - the rally
	# moves here, they regroup and set out together for the new destination (live 00:36: Echobunny / vex_x18 / the
	# priest at the Payon Kafra while she announced geffen and walked out of payon alone)
	if ($rallyTravel && $town && $KAFRA{$here} && (my $k = gathered_kafra($here, \@fr))) {
		message "[partylead] the party is at the $here Kafra ($k->[0],$k->[1]): regrouping there, then $rally->{town} together\n", 'system';
		$rally->{town} = $here; ($rally->{x}, $rally->{y}) = @$k; $rally->{saved} = 1; $rally->{said} = 0; $rally->{routeAt} = 0;
		$rally->{dest} ||= $rally->{thenGoto} || '';
		pk_say("everyone at the kafra here, coming. then we go" . ($rally->{dest} ne '' ? " to $rally->{dest}" : '') . " together");
		return 0; # rally_tick takes her to that Kafra
	}
	if ($rallyTravel && $town && ($pk{rallySaid} || '') ne $rally->{town}) { # one line: where the party meets
		$pk{rallySaid} = $rally->{town};
		my $dst = $rally->{dest} || $rally->{thenGoto} || '';
		pk_say(($dst ne '' ? "new spot: $dst, " : '') . "meet at the kafra in $rally->{town}");
	}
	my $cross = ($dm ne '' && $dm ne $here && cross_danger($here)) ? 'on' : ($nm ne '' && $nm ne $dm && cross_danger($nm)) ? 'pre' : '';
	# fighting while she waits: released only when the priest + one friend (priest offline: 2 friends) are within 20
	# the priest's distance over time: shrinking = it got closer within the last 10 s (or changed map within 20 s);
	# nearFor = how long it has been within 6 cells
	my ($prRow) = sort { (($a->[2] eq $here && defined $a->[3]) ? $a->[3] : 9999) <=> (($b->[2] eq $here && defined $b->[3]) ? $b->[3] : 9999) } grep { ($_->[1] || '') =~ /priest|acolyte/i } @fr;
	my $prD = $prRow && $prRow->[2] eq $here && defined $prRow->[3] ? $prRow->[3] : undef;
	if ($prRow && ($pk{prMap} || '') ne $prRow->[2]) { ($pk{prMap}, $pk{prMapAt}) = ($prRow->[2], time) }
	if (defined $prD && $prRow->[4]) {
		my $pc = "$prRow->[4]{x},$prRow->[4]{y}";
		if (($pk{prCell} || '') ne $pc) { # the priest moved: closer to her than before?
			$pk{prBestAt} = time if defined $pk{prBest} && $prD < $pk{prBest};
			($pk{prCell}, $pk{prBest}) = ($pc, $prD);
		}
	} else { ($pk{prBest}, $pk{prCell}) = (undef, '') }
	my $shrinking = defined $prD ? (time - ($pk{prBestAt} || 0) < 10 ? 1 : 0) : ($prRow && time - ($pk{prMapAt} || 0) < 20 ? 1 : 0);
	if (defined $prD && $prD <= 6) { $pk{nearSince} ||= time } else { $pk{nearSince} = 0 }
	# (b) 3+ members off her map (town, dead, respawned; live 12:38: four winged to payon) - she stops farming, holds
	# 60 s with the priest, then rallies the party at the town where they are and returns to the plan map after
	if ($farm) {
		my @off = grep { $_->[2] ne $here } @fr;
		if (@off >= 3) {
			$pk{offSince} ||= time;
			if (time - $pk{offSince} < 60) {
				if (!$pk{offSaid}) { $pk{offSaid} = 1; pk_say("waiting for u guys here") }
				AI::clear('route', 'move') if AI::inQueue('route', 'move');
				@pk{qw(act an seenAt)} = ('wait', $off[0], time);
				return 1;
			}
			my %tc; $tc{town_of_map($_->[2])}++ foreach grep { $_->[2] ne '' } @off;
			my ($t) = sort { $tc{$b} <=> $tc{$a} } grep { $KAFRA{$_} } keys %tc;
			if ($t && !$rally) {
				$rallyNext = 0;
				start_rally($t, undef, undef, scalar(@off) . " members are off the map - regroup", 0);
				if ($rally) {
					$rally->{dest} = plan_map() || $homeMap || $here; $rally->{mind} = 1;
					pk_say("lets regroup at the $t kafra, then back to " . $rally->{dest});
					($pk{offSince}, $pk{offSaid}) = (0, 0);
					return 0;
				}
			}
		} else { ($pk{offSince}, $pk{offSaid}) = (0, 0) }
	}
	my ($act, $an) = pack_decide({here => $here, friends => \@fr, farm => $farm, leaving => ($leaving || $pk{wf} ? 1 : 0), cross => $cross,
		waited => $pk{waitAt} ? time - $pk{waitAt} : 0, shrinking => $shrinking, collecting => ($pk{goName} ne '' ? 1 : 0),
		nearFor => $pk{nearSince} ? time - $pk{nearSince} : 0});
	# a dungeon map with a member more than 20 cells behind on it: she waits for it before advancing (live 00:26
	# pay_dun01: she pushed on with the priest, the others 95-140 cells back)
	# (live 00:51: "waiting for Zendancer84 ... 3 cells" with the priest and three friends next to her - the wait was for
	# xXRinfangXx 124 cells back): with the priest within 6 and at least half the online party within 20 she goes on;
	# otherwise only for a member on this map that is coming toward her (distance shrinking), 30 s at most, named
	# the priest "coming" (live 68c1088 loop: she crossed first, for a moment the priest was on another map and she
	# turned back): within 15 cells on her map in the last 20 s = coming, not away - no collect, no retreat, she waits
	$pk{prLastNear} = time if defined $prD && $prD <= 15;
	my $prComing = !defined $prD && time - ($pk{prLastNear} || 0) < 20;
	($act) = ('wait') if $prComing && ($act eq 'goto' || $act eq 'ok');
	# the priest went AHEAD onto the next map of her route (or her destination): she crosses to join it at once, with
	# the party (live 01:26: the whole party waited on pay_arche "for Zendancer84" who was already in pay_dun00)
	my $prAhead = $prRow && !defined $prD && $prRow->[2] ne '' && ($prRow->[2] eq $nm || $prRow->[2] eq $dm || $prRow->[2] eq (status_dest() || '-') || ($pk{lastNm} || '-') eq $prRow->[2]) ? 1 : 0;
	$pk{lastNm} = $nm if $nm ne ''; # the next map of her route, kept while a wait cleared the route
	if ($prAhead) {
		$act = 'ok';
		if (($pk{aheadSaid} || '') ne $prRow->[2]) { $pk{aheadSaid} = $prRow->[2]; pk_say("$prRow->[0] is in $prRow->[2] already, going in, follow me"); message "[partylead] AY22: $prRow->[0] is ahead on $prRow->[2] - joining it\n", 'system' }
	}
	# before a map change (her route's next map is another map) she collects a member still on this map far back
	# (bug b1794746024920-544: Silversage13 fighting 61 cells back when she took the party pay_dun02 -> pay_dun03):
	# 10 s for it to come, then she walks back to it (within 8 cells, 90 s at most) - the portal guards stay as they are
	my $nmS = $nm ne '' ? $nm : ($pk{strag} ? ($pk{lastNm} || '') : ''); # a wait clears the route: the last next map
	if (($act eq 'ok' || $pk{strag}) && $nmS ne '' && $nmS ne $here) {
		my $s0 = $pk{strag};
		my ($st) = $s0 ? grep({ $_->[0] eq $s0->{name} } @fr) : ();
		if ($s0 && (!$st || $st->[2] ne $here || !defined $st->[3] || $st->[3] <= 8 || friend_dead($st->[0]) || time - $s0->{at} > 100)) {
			message "[partylead] collected $s0->{name} before leaving $here\n", 'system' if $st && defined $st->[3] && $st->[3] <= 8;
			$pk{strag} = undef; $s0 = undef; $st = undef;
		}
		if (!$s0) {
			my $pp = route_portal($here);
			my ($far) = sort { $b->[3] <=> $a->[3] } grep { $_->[2] eq $here && defined $_->[3] && $_->[3] > 20 && !friend_dead($_->[0]) } @fr;
			if ($far && (!$pp || blockDistance(my_pos(), $pp) <= 15)) {
				$pk{strag} = $s0 = {name => $far->[0], at => time};
				$st = $far;
				pk_say("$far->[0] we go to $nmS, come here first");
			}
		}
		if ($s0 && $st) {
			if (($config{lockMap} || '') ne '') { $pk{lmHold} = $config{lockMap}; $config{lockMap} = '' }
			if (time - $s0->{at} < 10) { # 10 s for it to come
				AI::clear('route', 'move') if AI::inQueue('route', 'move');
			} elsif ($st->[4] && time - ($s0->{routeAt} || 0) > 5) {
				$s0->{routeAt} = time;
				AI::clear('route', 'move');
				safe_route($here, $st->[4]{x}, $st->[4]{y}, distFromGoal => 6, maxRouteTime => 8, noMapRoute => 1, attackOnRoute => 1);
				message "[partylead] going back for $st->[0] ($st->[3] cells) before leaving $here\n", 'system';
			}
			@pk{qw(act an seenAt)} = ('goto', $st, time);
			return 1;
		}
	}
	# (1) before a portal into a dungeon / a dangerous map she stops on the near side until the priest is within 6
	if (!$prAhead && $act eq 'ok' && $nm ne '' && $nm ne $here && (is_dungeon($nm) || cross_danger($nm)) && !(defined $prD && $prD <= 6)) {
		my $pp = route_portal($here);
		if (!$pp || blockDistance($char->{pos_to}, $pp) <= 10) { $act = 'wait' }
	}
	# (3) after a retreat to the Kafra: back in only with the priest within 6, and not for 60 s
	if (!$prAhead && $act eq 'ok' && $nm ne '' && (is_dungeon($nm) || cross_danger($nm)) && time - ($pk{retreatAt} || 0) < 60 && !(defined $prD && $prD <= 6)) { $act = 'wait' }
	if ($act eq 'ok' && is_dungeon($here) && !$farm) {
		my @live = grep { !friend_dead($_->[0]) } @fr;
		my $near20 = grep { $_->[2] eq $here && defined $_->[3] && $_->[3] <= 20 } @live;
		my $prNear = defined $prD && $prD <= 6;
		foreach my $f (@live) { # distance trend per member on this map
			next unless $f->[2] eq $here && defined $f->[3];
			my $t = $pk{dtr}{$f->[0]} ||= {best => $f->[3], at => time};
			if ($f->[3] < $t->{best}) { ($t->{best}, $t->{at}) = ($f->[3], time) } elsif ($f->[3] > $t->{best} + 3) { $t->{best} = $f->[3] }
		}
		my ($slow) = grep { $_->[2] eq $here && defined $_->[3] && $_->[3] > 20 && time - ($pk{dtr}{$_->[0]}{at} || 0) < 10 } @live;
		if (defined $prD && !($prNear && $near20 * 2 >= @live) && $slow) { # the priest away: no waiting for the others
			$pk{dunWaitAt} ||= time;
			if (time - $pk{dunWaitAt} < 30) { ($act, $an) = ('wait', $slow) } # the intent names the member waited for
		} else { $pk{dunWaitAt} = 0 }
	}
	if ($act eq 'ok' && $pk{goName} ne '') {
		message "[partylead] AY22: with $pk{goName} again (within 6 cells for 3 s) - going on together\n", 'system';
		psay("lets go, follow me", 1, 0); # the engine's follow order: the members switch from 'stay put' to following her
		($pk{goName}, $regroupTo, $pk{goAt}) = ('', '', 0);
	}
	# gave up collecting (3 min, or the priest stuck far away): she holds and waits for 10 min before trying again
	($act) = ('wait') if $act eq 'goto' && $pk{goGaveUp} && time - $pk{goGaveUp} < 600;
	if (($act eq 'ok' || $act eq 'solo') && $pk{wf}) {
		$pk{wf} = undef; ($pk{waitAt}, $pk{goName}) = (0, '');
		my $go = $homeMap ne '' ? $homeMap : ($config{lockMap} || (($saved && $saved->{lockMap}) || ''));
		psay("ok everyone here, lets go" . ($go ne '' ? " to $go" : ''), 1, 0);
		message "[partylead] PT04: the party is with her - setting off together" . ($go ne '' ? " for $go" : '') . "\n", 'system';
		if ($go ne '' && $go ne $here) {
			$config{lockMap} = $go if ($config{lockMap} || '') eq '' && !is_townmap($go);
			AI::clear('route', 'move'); AI::clear('attack') unless scalar(AI::ai_getAggressives());
			safe_route($go, undef, undef, attackOnRoute => route_aor());
		}
		return 1;
	}
	# AY22: the priest within 6 cells is enough to set off (the others follow her through the engine) - the older
	# "everyone close" holds / town waits below stand aside (live: she waited in geffen with the priest 0 cells away)
	$pk{prOkAt} = ($act eq 'ok' && defined $prD && $prD <= 6) ? time : 0;
	$pk{prFarmOkAt} = ($act eq 'ok' && $farm && defined $prD && $prD <= 20) ? time : 0; # farm map: the priest is the only anchor
	$pk{farFriends} = [map { $_->[0] } grep { !friend_dead($_->[0]) && !(defined $_->[3] && $_->[3] <= 20) && ($_->[1] || '') !~ /priest|acolyte/i } @fr];
	if ($act eq 'ok' || $act eq 'solo') {
		if ($cross eq 'pre' && $pk{crossSaid} ne $nm) { $pk{crossSaid} = $nm; psay("crossing $nm together, stay close", 0, 0); message "[partylead] PT04: crossing $nm with the party\n", 'system' }
		$pk{crossSaid} = '' if $cross eq '' && !cross_danger($here);
		($pk{waitAt}, $pk{goName}, $regroupTo) = (0, '', '');
		return 0;
	}
	return 0 if AI::inQueue('attack') && scalar(AI::ai_getAggressives()); # she answers what is on her
	# the priest away (another map, or 20+ cells) while she stands in a dungeon / on a map with aggressive packs: she
	# doesn't wait there (live 06:11: "help! 9 on me" in pay_dun01, winged out nearly dead) - back to the town's Kafra,
	# wait there, and go in only with the priest within 6
	if (($act eq 'wait' || $act eq 'goto') && (is_dungeon($here) || cross_danger($here))) {
		# away = not on her map / 20+ cells for 20 s (a priest crossing behind her is not away)
		my $prAway = (!defined $prD || $prD > 20) && time - ($pk{prLastNear} || 0) >= 20;
		my $t = town_of_map($here);
		# first: wait at the cave entrance (the cell she arrived on) for 60 s - the town Kafra only after that and only
		# with monsters on her (live: pay_dun00 -> "back to the payon Kafra" at once, a payon <-> pay_dun00 loop)
		my $atEntrance = $arrInfo && $arrInfo->{map} eq $here && time - $arrInfo->{at} < 60;
		my $hit = scalar(AI::ai_getAggressives());
		if (entrance_wait($prD, $atEntrance, $hit)) {
			if (($config{lockMap} || '') ne '') { $pk{lmHold} = $config{lockMap}; $config{lockMap} = '' }
			my $pn = $an ? $an->[0] : 'guys';
			if (($pk{entSaid} || '') ne $here) { $pk{entSaid} = $here; pk_say("$pn waiting at the cave entrance") }
			if ($arrInfo && $arrInfo->{map} eq $here && blockDistance($char->{pos_to}, $arrInfo) > 3 && time - $pk{routeAt} > 4) {
				$pk{routeAt} = time;
				AI::clear('route', 'move');
				safe_route($here, $arrInfo->{x}, $arrInfo->{y}, distFromGoal => 2, maxRouteTime => 6, noMapRoute => 1, attackOnRoute => 1);
			} elsif (AI::inQueue('route', 'move')) { AI::clear('route', 'move') }
			return 1;
		}
		$prAway = 0 if !$hit; # no monsters on her: no reason to leave the entrance
		if ($prAway && $KAFRA{$t}) {
			if (($config{lockMap} || '') ne '') { $pk{lmHold} = $config{lockMap}; $config{lockMap} = '' }
			if (($pk{retreatSaid} || '') ne $t) { $pk{retreatSaid} = $t; pk_say("too dangerous to wait here, waiting at the $t kafra, come there") }
			my ($rdm) = route_maps();
			if (!AI::inQueue('route', 'move') || ($rdm ne '' && $rdm ne $t)) {
				my $k = $KAFRA{$t}[0];
				AI::clear('route', 'move');
				safe_route($t, $k->[0], $k->[1], distFromGoal => 3, attackOnRoute => 1);
				$pk{retreatAt} = time;
				message "[partylead] AY22: the priest is away - back to the $t Kafra to wait (not in $here)\n", 'system';
			}
			return 1;
		}
	}
	$pk{retreatSaid} = '' if !is_dungeon($here) && !cross_danger($here) && !is_townmap($here);
	$pk{waitAt} ||= time;
	@pk{qw(act an seenAt)} = ($act, $an, time); # status_intent (dashboard): what this wait is about
	my ($cx, $cy) = ($char->{pos_to}{x}, $char->{pos_to}{y});
	if (time - $pk{logAt} > 20) { $pk{logAt} = time; message "[partylead] PT04: $act - " . ($cross ? "crossing ($cross) needs the priest within 10, " : '') . "anchor $an->[0] on $an->[2]" . (defined $an->[3] ? " $an->[3] cells" : '') . "\n", 'system' }
	if ($act eq 'back') {
		if (time - $pk{routeAt} > 4 && $an->[4]) {
			$pk{routeAt} = time;
			AI::clear('attack', 'route', 'move', 'items_take', 'take', 'items_gather');
			safe_route($here, $an->[4]{x}, $an->[4]{y}, distFromGoal => 4, maxRouteTime => 8, noMapRoute => 1, attackOnRoute => 1);
		}
		return 1;
	}
	if ($act eq 'goto') {
		my $a = $apiPos{$an->[0]};
		my ($am, $ax, $ay) = ($an->[2], $an->[4] ? ($an->[4]{x}, $an->[4]{y}) : ($a && time - $a->{at} < 10 && $a->{map} eq $an->[2] ? ($a->{x}, $a->{y}) : (undef, undef)));
		# the priest stuck on one cell for 2+ min far away (e.g. waiting for a Kafra): go only when one Kafra hop
		# (she is in a Kafra town, the priest's map belongs to a Kafra town); otherwise hold and tell it where she is
		my $ak = join(',', $am, $ax // '', $ay // '');
		if (($pk{anKey} || '') ne $ak) { ($pk{anKey}, $pk{anSince}) = ($ak, time) }
		my $stuckFar = $am ne $here && time - $pk{anSince} > 120 && !$KAFRA{town_of_map($am)}; # a Kafra town's area: she gets there (walk + Kafra hop)
		if ($am ne '' && ($stuckFar || ($pk{goAt} && $pk{goName} eq $an->[0] && time - $pk{goAt} > 180))) {
			$pk{goGaveUp} = time; ($pk{goName}, $regroupTo, $pk{goAt}) = ('', '', 0);
			my $out = $here =~ /^(.+?)_in\d*$/ ? $1 : $here;
			pk_say("$an->[0] im at $out $cx,$cy, come to me");
			message "[partylead] AY22: stopped collecting $an->[0] (" . ($stuckFar ? "stuck on $am for " . int((time - $pk{anSince}) / 60) . " min" : '3 min without reaching it') . ") - holding at $here\n", 'system';
			$am = ''; # falls through to the wait below
		}
		if ($am ne '') {
			# one owner of movement: lockMap set aside while she collects (no lockMap route between the ticks)
			if (($config{lockMap} || '') ne '') { $pk{lmHold} = $config{lockMap}; $config{lockMap} = '' }
			if ($holding) { restore(); $config{lockMap} = '' if ($pk{lmHold} || '') ne '' } # a town / party hold doesn't block the collect
			if ($pk{goName} ne $an->[0]) { $pk{goName} = $an->[0]; $pk{goAt} = time; $regroupTo = $am; pk_say("coming to u, $an->[0]"); message "[partylead] AY22: $an->[0] fell behind ($am) - walking back to them\n", 'system' }
			my ($rdm) = route_maps();
			# the route is made once and kept (re-made every 10 s it flipped between the Kafra and the walk: aldebaran <->
			# mjolnir_12 ping-pong) - only when there is none, it points elsewhere, or she hasn't moved for 20 s
			# only HER collect route may run (live 01:37: "going back to collect Zendancer84" while the old route to pay_dun02
			# walked her away): any other route in the queue is cleared; the distance must shrink (checked every 5 s)
			my $ri = AI::findAction('route');
			my $cur = (defined $ri && $ri ne '') ? AI::args($ri) : undef;
			my $foreign = $cur && (!$pk{colArgs} || $cur != $pk{colArgs}) ? 1 : 0;
			my $wrong = 0;
			if (defined $an->[3] && $am eq $here) {
				if (!$pk{colChk} || time - $pk{colChk}[1] >= 5) {
					$wrong = 1 if $pk{colChk} && $pk{colChk}[2] eq $an->[0] && $an->[3] > $pk{colChk}[0] + 2;
					$pk{colChk} = [$an->[3], time, $an->[0]];
				}
			}
			if ($wrong) { wd_report('COLLECT_WRONG_WAY', $here, "collecting $an->[0]: distance grew to $an->[3] cells - re-routing", 're-route to its cell') }
			if ($am eq $here && !defined $ax) { # no fresh cell on this map: ask, wait (never a guess)
				AI::clear('route', 'move') if AI::inQueue('route', 'move');
				ask_where($an->[0]);
				return 1;
			}
			if (!$cur || $foreign || $wrong || ($rdm ne '' && $rdm ne $am) || (!moved_recently() && time - $pk{routeAt} > 20)) { # never onward: only to the friend
				$pk{routeAt} = time;
				Commands::run('stand') if $char->{sitting};
				AI::clear('route', 'move');
				safe_route($am, $ax, $ay, distFromGoal => 4, attackOnRoute => route_aor(), ($am eq $here ? (noMapRoute => 1, maxRouteTime => 10) : ()));
				my $ni = AI::findAction('route');
				$pk{colArgs} = (defined $ni && $ni ne '') ? AI::args($ni) : undef;
			}
			return 1;
		}
	}
	# the priest on another map: say once where she is, so it can come (AY22: she goes to it after 60 s)
	if ($an && ($an->[1] || '') =~ /priest|acolyte/i && $an->[2] ne $here && ($pk{toldPr} || '') ne "$an->[0]|$here") {
		$pk{toldPr} = "$an->[0]|$here";
		my $out = $here =~ /^(.+?)_in\d*$/ ? $1 : $here;
		pk_say("$an->[0] im at $out $cx,$cy, come here");
	}
	# wait: stand where she is (OpenKore's sitAuto still sits her under its HP / SP marks). lockMap is held for the
	# wait - clearing OpenKore's lockMap route every tick made it recalculate it every 0.5 s ("Calculating lockMap route")
	if (($config{lockMap} || '') ne '') { $pk{lmHold} = $config{lockMap}; $config{lockMap} = '' }
	AI::clear('route', 'move') if AI::inQueue('route', 'move');
	if (time - $pk{sayAt} >= 60 && $pk{waitAt} && time - $pk{waitAt} > 15) { # a short pace stop says nothing
		$pk{sayAt} = time;
		my $out = $here =~ /^(.+?)_in\d*$/ ? $1 : $here; # indoors (geffen_in): name the town, not the room
		pk_say($out ne $here ? "waiting for u in $out, at the kafra" : "waiting for u at $here $cx,$cy");
	}
	return 1;
}
# PT32 runtime: the monster against pt32_ok with her live surroundings (conf partylead_pt32 0 turns it off)
sub pt32_target {
	my $m = shift;
	return 1 if defined $config{partylead_pt32} && $config{partylead_pt32} eq '0';
	return 1 unless $m && $m->{pos_to} && $char && $char->{pos_to} && $field && !is_townmap($field->baseName);
	my %ag = map { $_ => 1 } AI::ai_getAggressives();
	my $tOnMe = ($ag{$m->{ID}} || $m->{dmgToYou} || $m->{missedYou}) ? 1 : 0;
	my (@others, @allMon, $onMe);
	$onMe = 0;
	foreach my $mo (@{$monstersList ? $monstersList->getItems() : []}) {
		next unless $mo->{pos_to} && defined $mo->{pos_to}{x} && !$mo->{dead};
		my $hits = $mo->{dmgToYou} || $mo->{missedYou} ? 1 : 0;
		$onMe++ if $hits && blockDistance($char->{pos_to}, $mo->{pos_to}) <= 3;
		next if $mo->{ID} eq $m->{ID};
		push @others, $mo->{pos_to} if $ag{$mo->{ID}} || $hits;
		push @allMon, $mo->{pos_to};
	}
	my $pr;
	foreach my $f (stick_mates()) { if ($f->[2] =~ /priest|acolyte/i) { $pr = $f->[1]; last } }
	return pt32_ok($char->{pos_to}, $m->{pos_to}, $pr, \@others, $onMe, $tOnMe, \@allMon);
}
# a rally / departure wait turns into fighting near the meeting point (owner 2026-11-14: "fighting is her main
# priority"): true when a safe field next to the town exists and the wait-farm started
# ---- any wait (rally, pack, departure, regroup walk) longer than 2 min with no exp is IDLE (owner 2026-11-14: she
# sat in a Payon rally for minutes and the watchdog said nothing - a rally counted as productive). Reported every
# 2 min ("[watchdog] IDLE <kind> wait N min ..."), which also wakes her helper.
my ($wiSince, $wiExp, $wiKind, $wiSaid) = (0, 0, '', 0);
sub wait_idle_check {
	return unless $char && $field;
	my $kind = $rally ? "rally ($rally->{town})" : $depart ? "departure ($depart->{town})" : $regroupTo ne '' ? "regroup to $regroupTo"
		: $pk{waitAt} ? 'party' : $holding ? 'hold' : '';
	my $exp = ($char->{exp} || 0) + ($char->{exp_job} || 0);
	if ($kind eq '' || $exp != $wiExp) { ($wiSince, $wiExp, $wiKind, $wiSaid) = ($kind eq '' ? 0 : time, $exp, $kind, 0); return }
	$wiSince ||= time;
	my $n = time - $wiSince;
	return if $n < 120 || time - $wiSaid < 120;
	$wiSaid = time;
	wd_report('IDLE', $field->baseName, "$kind wait " . int($n / 60) . " min with no exp at " . $field->baseName . " $char->{pos_to}{x},$char->{pos_to}{y}", "her helper should step in");
}
sub wait_fight_start {
	my ($town, $why) = @_;
	my (%tops, %danger);
	foreach my $m (@{$WAIT_FIELDS{$town} || []}) { $tops{$m} = map_top($m); $danger{$m} = cross_danger($m) }
	my $wm = pack_wait_map($town, 1, $WAIT_FIELDS{$town}, transit_fit_lv(), \%tops, \%danger);
	return 0 if $wm eq '';
	$pk{wf} = {map => $wm, meet => $town, spot => undef, at => time};
	($pk{waitAt}, $pk{goName}, $pk{sayAt}) = (time, '', 0);
	message "[partylead] PT04: $why - fighting on $wm until the party is here\n", 'system';
	return 1;
}
# fights near the meeting point while the party comes to her: at most one map from it, within 14 cells of where she
# arrived (PT04: friends reach her quickly, no charging ahead). False = the wait-farm ended (she is elsewhere).
sub wait_fight {
	my $w = $pk{wf};
	my $here = $field->baseName;
	my ($cx, $cy) = ($char->{pos_to}{x}, $char->{pos_to}{y});
	my ($rdm) = route_maps();
	if ($rdm ne '' && $rdm ne $here && $rdm ne $w->{map}) { AI::clear('route', 'move') } # no leaving for the hunt alone
	if ($here ne $w->{map}) { $pk{wf} = undef; return 0 } # she holds one map: elsewhere (a wing) -> start over
	$w->{spot} ||= {x => $cx, y => $cy};
	if (time - $pk{sayAt} >= 90) { $pk{sayAt} = time; psay("waiting for u at $here $w->{spot}{x},$w->{spot}{y}, come here", 1, 0) }
	return 1 if AI::inQueue('attack') || $char->{sitting};
	my ($best, $bd);
	foreach my $mo (@{$monstersList ? $monstersList->getItems() : []}) {
		next unless $mo->{pos_to} && defined $mo->{pos_to}{x} && !$mo->{dead};
		next if blockDistance($w->{spot}, $mo->{pos_to}) > 6 || !pt32_target($mo); # only what comes to her
		my $d = blockDistance($char->{pos_to}, $mo->{pos_to});
		($best, $bd) = ($mo, $d) if !defined $bd || $d < $bd;
	}
	if ($best) { AI::clear('route', 'move'); main::attack($best->{ID}); return 1 }
	if (AI::inQueue('route', 'move') && time - $pk{routeAt} < 8) { return 1 }
	if (blockDistance($char->{pos_to}, $w->{spot}) > 4) { # after a fight: back to her spot (same map, a few cells)
		$pk{routeAt} = time;
		AI::clear('route', 'move');
		safe_route($here, $w->{spot}{x}, $w->{spot}{y}, distFromGoal => 1, maxRouteTime => 6, noMapRoute => 1, attackOnRoute => 1);
	}
	return 1;
}
sub stick_on { return !defined $config{partylead_stick} || $config{partylead_stick} ne '0' }
# online friends (not on an errand) on her map with a known cell: [name, pos, class]
sub stick_mates {
	my $here = $field->baseName;
	my @m;
	foreach my $u (values %{$char->{party}{users} || {}}) {
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name}) || friend_dead($u->{name});
		next if friend_map($u) ne $here;
		my ($pos) = friend_pos($u, $here);
		next unless $pos && defined $pos->{x};
		my $a = $apiPos{$u->{name}};
		push @m, [$u->{name}, $pos, ($a && $a->{cls}) || ''];
	}
	return @m;
}
# the friend to fall back to: the priest / acolyte, else a tank, else the nearest
sub stick_anchor {
	my ($best, $bs);
	foreach my $f (@_) {
		my $d = blockDistance($char->{pos_to}, $f->[1]);
		my $s = $d + ($f->[2] =~ /priest|acolyte/i ? -1000 : $f->[2] =~ /knight|crusader|paladin|swordman|swordsman/i ? -500 : 0);
		($best, $bs) = ($f, $s) if !defined $bs || $s < $bs;
	}
	return $best;
}
# a boss / MVP in sight (OpenKore's monster flags, else the well-known boss / MVP names)
my $STICK_BOSS = qr/^(Eddga|Osiris|Baphomet|Doppelganger|Golden Thief Bug|Moonlight Flower|Phreeoni|Mistress|Maya|Orc Hero|Orc Lord|Drake|Pharaoh|Dark Lord|Turtle General|Stormy Knight|Hatii|Garm|Amon Ra|Lord of the Dead|Samurai Specter|Tao Gunka|Atroce|Ifrit|Detardeurus|Angeling|Deviling|Archangeling|Ghostring|Mysteltainn|Ogretooth|Executioner|Eclipse|Mastering|Dragon Fly|Vagabond Wolf|Toad|Vocal|King Dramoh|Chepet|Dark Priest)$/;
sub stick_boss {
	foreach my $m (@{$monstersList ? $monstersList->getItems() : []}) {
		next unless $m->{pos_to} && defined $m->{pos_to}{x} && !$m->{dead};
		return $m if $m->{boss} || $m->{mvp} || ($m->{name} || '') =~ $STICK_BOSS;
	}
	return undef;
}
# solo again: OpenKore's one-hit-deadly teleport back as config.txt had it
sub stick_solo_cfg {
	$config{teleportAuto_deadly} = $stickDeadly if defined $stickDeadly && ($config{teleportAuto_deadly} || 0) != $stickDeadly;
}
# her targets (area_target): a monster that would take her beyond TOGETHER cells of a friend near her is skipped -
# unless it is hitting her or a friend. 0 = skip.
sub stick_target {
	my $m = shift;
	return 1 unless stick_on() && $char->{party} && !is_townmap($field->baseName);
	return 1 if $m->{dmgToYou} || $m->{missedYou} || $m->{dmgToParty} || $m->{castOnToParty} || $m->{missedToParty};
	# on the way (PT32 clarified 2026-11-14 "how come she cannot just hit the first target she sees?"): no farming
	# detours, but the first monster that comes within 6 cells is fought right there, nearest first - never walked past
	# into the middle of the pack (pt32_target still blocks a target behind other aggressive monsters / with 2+ on her)
	return blockDistance($char->{pos_to}, $m->{pos_to}) <= 6 ? 1 : 0 if %travelSaved;
	foreach my $f (stick_mates()) {
		my $now = blockDistance($char->{pos_to}, $f->[1]);
		next if $now > TOGETHER; # a lost friend: the fetch logic, not the target choice
		my $d = blockDistance($m->{pos_to}, $f->[1]);
		return 0 if $d > TOGETHER && $d > $now;
	}
	return 1;
}
# every tick (from tick): 1 = it acted, the tick ends
sub stick_tick {
	return 0 unless stick_on() && $char->{hp_max};
	my @friends = online_friends();
	return 0 unless @friends; # nobody to stay with: solo rules (the escape block in tick)
	my $here = $field->baseName;
	# no random Teleport while friends are online (OpenKore's teleportAuto: HP, crowd, one-hit-deadly)
	$config{teleportAuto_hp} = 0 if $config{teleportAuto_hp};
	$config{teleportAuto_minAggressives} = 0 if $config{teleportAuto_minAggressives};
	$config{teleportAuto_minAggressivesInLock} = 0 if $config{teleportAuto_minAggressivesInLock};
	if ($config{teleportAuto_deadly}) { $stickDeadly = $config{teleportAuto_deadly}; $config{teleportAuto_deadly} = 0 }
	return 0 if is_townmap($here);
	# a real HP reading only (0 / over max = a stale or half-updated status right after a map change or reload)
	return 0 unless ($char->{hp} || 0) > 0 && $char->{hp} <= $char->{hp_max};
	my $hpp = $char->{hp} / $char->{hp_max};
	$stickHitAt = time if $hpp < $stickHp - 0.004;
	$stickHp = $hpp;
	# deadly must hold on readings at least 0.4 s apart (live 2026-11-14: a wing "nearly dead" at ~53% HP)
	if ($hpp < STICK_DEADLY) { $stickLowAt ||= time } else { $stickLowAt = 0 }
	my $aggr = scalar(AI::ai_getAggressives());
	moved_recently(); # keeps her last-move time current (fall-backs only while she moves)
	my @mates = stick_mates();
	my $an = stick_anchor(@mates);
	my $ad = $an ? blockDistance($char->{pos_to}, $an->[1]) : -1;
	my ($cx, $cy) = ($char->{pos_to}{x}, $char->{pos_to}{y});
	# about to die and still being hit: Butterfly Wing to the save Kafra (not a random cell), and say where and what next
	if ($hpp < STICK_DEADLY && $stickLowAt && time - $stickLowAt >= 0.4 && $aggr >= 1 && time - $stickHitAt < 3 && time - $stickWingAt > 10 && $char->inventory->getByName('Butterfly Wing')) {
		$stickWingAt = time;
		$wingOkUntil = time + 8; # tp_guard: this one teleport (the dying Butterfly Wing) is allowed
		my $town = $config{saveMap} || 'town';
		my $back = $homeMap ne '' ? $homeMap : $here;
		$stickWinged = {town => $town, back => $back, at => time};
		cross_wing($here, $back) if $CROSS_DANGER{$here};
		message sprintf("[partylead] PT31: about to die (HP %d/%d = %d%%, under 20%% for %.1f s, %d monsters on her): Butterfly Wing to %s\n", $char->{hp}, $char->{hp_max}, $hpp * 100, time - $stickLowAt, $aggr, $town), 'system';
		AI::clear('attack', 'route', 'move', 'items_take', 'take', 'items_gather', 'sitting', 'sitAuto');
		AI::ai_useTeleport(2); # 2 = the respawn point (Butterfly Wing), never 1 (a random cell)
		psay(pick("almost died, winged to $town kafra. meet me there, then we go back to $back",
			"had to wing to the $town kafra (nearly dead) - regroup there, then back to $back"), 0, 0);
		return 1;
	}
	if ($stickFlee) {
		if ($here ne $stickFlee->{from} || time - $stickFlee->{at} > 90) {
			message "[partylead] PT31: fall-back over (" . ($here ne $stickFlee->{from} ? "reached $here" : 'timed out') . ")\n", 'system';
			$config{attackAuto} = $fleeAA if defined $fleeAA;
			($stickFlee, $fleeAA) = (undef, undef);
		} else {
			# she ALWAYS fights back what hits her (live: 90 s of "Orc Warrior attacks you" with the old flight); the
			# flight only moves her while she is really moving - standing still for 2 s with monsters on her: fight
			$config{attackAuto} = $fleeAA if defined $fleeAA; $fleeAA = undef;
			AI::clear('items_take', 'take', 'items_gather') if AI::inQueue('items_take', 'take', 'items_gather');
			Commands::run('stand') if $char->{sitting};
			return 0 if AI::inQueue('attack');
			if ($aggr && !moved_recently()) { fight_back(); return 1 }
			my ($rdm) = route_maps();
			if (!AI::inQueue('route', 'move') || ($rdm ne '' && $rdm ne $stickFlee->{map})) {
				AI::clear('route', 'move');
				safe_route($stickFlee->{map}, undef, undef, attackOnRoute => 1);
			}
			return 1;
		}
	}
	# a boss in sight: the party pulls back together, announced - she walks away from it, toward a friend farther from it
	if (my $boss = stick_boss()) {
		my $bd = blockDistance($char->{pos_to}, $boss->{pos_to});
		if ($bd <= 14 && time - $stickBossAt > 3) {
			$stickBossAt = time;
			psay(pick("$boss->{name} here! pull back with me", "careful, $boss->{name}! everyone fall back this way", "$boss->{name} coming, back off together"), 0, 30);
			my ($dx, $dy) = ($cx - $boss->{pos_to}{x}, $cy - $boss->{pos_to}{y});
			my $n = sqrt($dx * $dx + $dy * $dy) || 1;
			my ($tx, $ty) = (int($cx + $dx * 8 / $n), int($cy + $dy * 8 / $n));
			($tx, $ty) = ($an->[1]{x}, $an->[1]{y}) if $an && blockDistance($an->[1], $boss->{pos_to}) > $bd;
			if (eval { $field->isWalkable($tx, $ty) }) {
				AI::clear('attack', 'route', 'move', 'items_take', 'take', 'items_gather');
				safe_route($here, $tx, $ty, distFromGoal => 2, maxRouteTime => 6, noMapRoute => 1, attackOnRoute => 0);
				message "[partylead] PT31: $boss->{name} $bd cells away - the party pulls back together\n", 'system';
				return 1;
			}
		}
	}
	# PT32: a second monster on her with the priest / tank more than 5 cells away: step back toward them (no help call)
	if (0 && $aggr == 2 && $hpp >= 0.45 && $an && $ad > 5 && time - $stickRunAt > 3 && !AI::inQueue('attack') && !(defined $config{partylead_pt32} && $config{partylead_pt32} eq '0')) {
		$stickRunAt = time;
		AI::clear('route', 'move');
		safe_route($here, $an->[1]{x}, $an->[1]{y}, distFromGoal => 3, maxRouteTime => 4, noMapRoute => 1, attackOnRoute => 1);
		message "[partylead] PT32: 2 monsters on her - stepping back toward $an->[0] ($ad cells)\n", 'system';
		return 1;
	}
	# in danger: fall back to the priest / tank and ask for help (no wing, no teleport)
	if ($aggr >= 3 || $hpp < 0.45 && $aggr >= 1) {
		if (time - $stickHelpAt > 45) {
			$stickHelpAt = time;
			psay(pick("help, $aggr on me", "help! $aggr on me ($here $cx,$cy)", "$aggr mobs on me, need help"), 0, 0);
		}
		if ($an && $ad > 3 && time - $stickRunAt > 3) { # 3+ on her: back toward the priest / tank (attack kept, attackOnRoute 1)
			$stickRunAt = time;
			Commands::run('stand') if $char->{sitting};
			AI::clear('route', 'move', 'items_take', 'take', 'items_gather'); # never the attack: she fights back what hits her
			safe_route($here, $an->[1]{x}, $an->[1]{y}, distFromGoal => 2, maxRouteTime => 6, noMapRoute => 1, attackOnRoute => 1);
			message sprintf("[partylead] PT31: falling back to %s (%d cells; HP %d%%, %d monsters on her)\n", $an->[0], $ad, $hpp * 100, $aggr), 'system';
			return 1;
		}
		# swarmed with no friend on this map (she got ahead of them on a crossing): back to the map the friends are on
		# not across a portal while the priest was within 15 cells in the last 20 s (it is right behind her at the portal:
		# live 09:14, a fall-back / rejoin loop pay_dun01 <-> pay_dun02) - she fights at the entrance and waits for it
		if (!$an && $aggr >= 4 && time - $stickRunAt > 5 && !AI::inQueue('attack') && !$stickFlee && !pk_prnear_recent()) {
			my %on; $on{$_->[1]}++ foreach grep { $_->[1] ne '' && $_->[1] ne $here } @friends;
			my ($fm) = sort { $on{$b} <=> $on{$a} } keys %on;
			if ($fm) {
				$stickRunAt = time;
				$stickFlee = {map => $fm, from => $here, at => time};
				AI::clear('route', 'move', 'items_take', 'take', 'items_gather'); # never the attack (she fights back)
				safe_route($fm, undef, undef, attackOnRoute => 1);
				psay(pick("too many here, falling back to u in $fm", "$aggr on me on $here, coming back to $fm"), 0, 20);
				message sprintf("[partylead] PT31: %d monsters on her on %s with no friend here (HP %d%%): falling back to the friends on %s\n", $aggr, $here, $hpp * 100, $fm), 'system';
				return 1;
			}
		}
	}
	return 0;
}
# party_fit_lv cached for 10 s (trivial_transit runs several times a tick)
my ($tflv, $tflvAt) = (0, 0);
sub transit_fit_lv {
	($tflv, $tflvAt) = (party_fit_lv(), time) if time - $tflvAt > 10;
	return $tflv;
}

sub tick {
	return if time - $last < 0.5;
	$last = time;
	return unless $net && $net->getState() == 5 && $char && $field && $char->{party} && $char->{party}{users};
	# dead: the plugin does nothing (QA 2026-11-14 20:14: it kept attacking / routing / fetching on her corpse, every
	# action displaced OpenKore's 'dead' task, processDead re-queued it ~40 times and the respawn waited 70 s)
	$diedAt = 0 if !$char->{dead} && $diedAt && time - $diedAt > 300; # queue b17947128005sb: next death is announced again
	if ($char->{dead}) {
		if (!$diedAt) { $diedAt = time; psay(pick("argh died, respawning in " . ($config{saveMap} || 'town') . ", regroup at the kafra", "dead, sorry guys - back at the " . ($config{saveMap} || 'town') . " kafra in a sec"), 1, 0) }
		return;
	}
	# an escape comes first: OpenKore's teleport task only runs while it is the CURRENT AI action - any attack / route /
	# loot queued on top of it (this plugin's protect / farm / fetch, OpenKore's attackAuto) kept it waiting 43 s
	# (QA 21:22:55 -> 21:23:38, and the Eddga death 20:49). Clear what sits above it and leave her alone until it's done.
	if (AI::inQueue('teleport')) {
		AI::clear('attack', 'route', 'move', 'items_take', 'take', 'items_gather', 'sitting', 'sitAuto') if AI::action() ne 'teleport';
		return;
	}
	# PT31: with friends online no random Teleport - fall back to the party, ask for help, Butterfly Wing only when dying
	return if stick_tick();
	# PT04 hard invariant: never travel or farm with no friend within 20 cells (crossing gef_fild10: priest within 10)
	return if pack_tick();
	# escape rules (main 2026-11-14), solo since PT31 (no friend online): at 6+ monsters or 35% HP; an MVP/boss in sight
	# always (mon_control teleport flag). The healer branch below only runs with partylead_stick 0.
	if (!stick_on() || !online_friends()) {
		stick_solo_cfg();
		my $withHealer = 0;
		foreach my $u (values %{$char->{party}{users} || {}}) {
			next if !$u || !$u->{online} || $u->{name} eq $char->{name};
			my $a = $apiPos{$u->{name}};
			$withHealer = 1 if $a && ($a->{cls} || '') =~ /priest|acolyte/i && friend_map($u) eq $field->baseName;
		}
		my ($hpT, $aggT) = $withHealer ? (30, 0) : (35, 6);
		$config{teleportAuto_hp} = $hpT if ($config{teleportAuto_hp} || 0) != $hpT;
		$config{teleportAuto_minAggressives} = $aggT if ($config{teleportAuto_minAggressives} || 0) != $aggT;
		$config{teleportAuto_minAggressivesInLock} = $aggT ? $aggT + 1 : 0 if ($config{teleportAuto_minAggressivesInLock} || 0) != ($aggT ? $aggT + 1 : 0);
		if ($withHealer && !is_townmap($field->baseName) && $char->{hp_max}) {
			my $hpp = $char->{hp} / $char->{hp_max};
			if ($hpp < 0.45 && scalar(AI::ai_getAggressives()) >= 4 && time - ($escapeAt || 0) > 5) {
				$escapeAt = time;
				message "[partylead] escaping: HP " . int($hpp * 100) . "% with " . scalar(AI::ai_getAggressives()) . " monsters on her\n", 'system';
				AI::clear('attack', 'route', 'move', 'items_take', 'take', 'items_gather');
				AI::ai_useTeleport(1);
				return;
			}
		}
	}
	# in town after a death the town run (storage / sell / buy) must not wait behind a sitAuto to 80% HP (QA 20:15-20:19:
	# she sat at the respawn point for 4 min and drank 22 Red Potions): town is safe, she shops and regenerates later
	if (is_townmap($field->baseName) && AI::inQueue('sitAuto') && AI::inQueue('storageAuto', 'sellAuto', 'buyAuto')) {
		AI::clear('sitAuto');
		Commands::run('stand') if $char->{sitting};
	}
	# no looting under pressure (QA 20:14: picking up loot at 49% HP with 8+ monsters on her): 3+ attackers or < 60% HP
	{
		my $aggr = scalar(AI::ai_getAggressives());
		my $hpp = $char->{hp_max} ? $char->{hp} / $char->{hp_max} : 1;
		if (($aggr >= 3 || $hpp < 0.6) && AI::inQueue('take', 'items_take', 'items_gather')) {
			AI::clear('take', 'items_take', 'items_gather');
			message "[partylead] no looting now: $aggr monsters on her, HP " . int($hpp * 100) . "%\n", 'system' if time - $lastMsg > 10;
		}
	}
	flush_say();
	# aylagear.pl is buying a piece from a player vendor in Prontera (a short walk + the shop window): leave her alone
	return if defined &aylagear::busy && aylagear::busy();
	# a call for help counts for 2 min only (QA 2026-11-14 12:27: Echobunny's "help, cant find u" from 12:19 made her
	# turn back from pay_dun00 to pay_arche 8 min later, right after the mind's 'goto pay_dun00' - back and forth)
	foreach my $n (keys %helpFrom) { delete $helpFrom{$n} if time - $helpFrom{$n} > 120 }
	if ($reloadCmdfile) { # 'partylead reload-cmdfile': reload cmdfile.pl from here, never from inside its own loop
		$reloadCmdfile = 0;
		message "[partylead] reloading the cmdfile plugin\n", 'system';
		eval { Plugins::reload('cmdfile') };
		message "[partylead] cmdfile reload failed: $@\n", 'system' if $@;
	}
	if ($sellTrigUntil && time > $sellTrigUntil) { $config{itemsMaxWeight_sellOrStore} = $sellTrigSaved; ($sellTrigSaved, $sellTrigUntil) = (0, 0) }
	$huntMap = $config{lockMap} ne '' ? $config{lockMap} : (($saved && $saved->{lockMap}) ? $saved->{lockMap} : '');
	set_home($config{lockMap}) if ($config{lockMap} || '') ne ''; # a non-empty lockMap is always the current destination (QA 20:54: stale homeMap pay_fild08)
	buff_ask_tick(); # PT29: ask the priest for missing buffs
	fit_tick(); # AY07/AY19: her own level / gear / turn map choice when the mind's order is stale
	watchdog_tick();
	# never stand still (owner: "Never stand still, walk around a bit to unstuck yourself"): no movement for 20 s
	# while not fighting, sitting or talking to an NPC -> step a few cells in a random direction
	my $pk = "$char->{pos_to}{x},$char->{pos_to}{y}";
	my $swept = farm_sweep(); # the party sweep on the farm map (it replaces the side-step there)
	if ($pk ne $lastPos) { ($lastPos, $stillSince) = ($pk, time); }
	elsif (!$swept && $field->baseName ne ($huntMap || '') && time - $stillSince > 20 && !$holding && !$rally && !$depart && !party_resting() && !AI::inQueue('attack', 'NPC', 'sitting', 'sellAuto', 'buyAuto', 'storageAuto') && !$char->{sitting}) {
		$stillSince = time;
		my ($x, $y) = free_cell($char->{pos_to}{x}, $char->{pos_to}{y}, 2, 5);
		($x, $y) = ($char->{pos_to}{x}, $char->{pos_to}{y}) unless defined $x;
		message "[partylead] standing still too long, stepping to $x,$y
", 'system';
		AI::clear('route', 'move');
		safe_route($field->baseName, $x, $y, distFromGoal => 1, maxRouteTime => 8);
	}

	if ($char->{lv} && $lastLv && $char->{lv} > $lastLv) {
		psay(pick("lv $char->{lv}!", "ding $char->{lv} :D", "level up, ty guys"), 1, 3);
	}
	$lastLv = $char->{lv} if $char->{lv};

	# her own errands (selling loot, buying potions/weapon) come first: don't cancel those routes
	if (AI::inQueue('sellAuto', 'buyAuto', 'storageAuto')) {
		# town runs are done together (owner rule): announce it with "town run ... to <town>" - Legion's core parses
		# this line (rally.go rallyHeard -> callTownRun) and the friends start their trips - then give them ~15 s to
		# get moving before she walks off (QA: she walked pay_dun00 -> payon alone, friends left in the dungeon)
		# a friend on her map in danger defers a town run that isn't an emergency (main 2026-11-14: she shopped in Payon for
		# 6 min while the priest fought 21 monsters alone in pay_dun00); out of potions (< 3) still goes
		if (!$errand && !is_townmap($field->baseName) && (my $dz = friend_in_danger($field->baseName))) {
			my $pots = 0; foreach my $it (@{$char->inventory->getItems()}) { $pots += $it->{amount} if $it->{name} =~ /Potion|Herb/ && $it->{type} <= 2 }
			if ($pots >= 3) {
				AI::clear('sellAuto', 'buyAuto', 'storageAuto');
				message "[partylead] town run deferred: $dz is in danger here\n", 'system' if time - $lastMsg > 15;
				$lastMsg = time;
				return;
			}
		}
		if (!$errand) {
			$errand = 1;
			bag_log('start');
			$errandStart = time;
			# PT22 (owner rule): on a party town run everyone uses a Butterfly Wing, shops and meets at the Kafra - she
			# wings 4 s after announcing it (the no-teleport guard lets exactly this wing through)
			$townWingAt = (!is_townmap($field->baseName) && $char->inventory->getByName('Butterfly Wing')) ? time + 4 : 0;
			my $hm = ($saved && $saved->{lockMap}) ? $saved->{lockMap} : ($config{lockMap} || $field->baseName);
			my $need = AI::inQueue('sellAuto', 'storageAuto') ? 'bag full' : 'low on pots';
			$need = 'i died and respawned here, regroup at the kafra' if $diedAt && time - $diedAt < 300; # not "bag full" (QA 20:15)
			$need = "winged out nearly dead, regroup at the kafra then back to $stickWinged->{back}" if $stickWinged && time - $stickWinged->{at} < 300 && $field->baseName eq $stickWinged->{town}; # PT31: the same story as her wing line
			if ($townReason ne '') { $need = $townReason; $townReason = '' } # a friend's call (not "bag full" at 26% - QA 20:28)
			elsif ($need eq 'bag full' && $char->{weight_max} && $char->{weight} / $char->{weight_max} < 0.45 && !($diedAt && time - $diedAt < 300)) { $need = AI::inQueue('storageAuto') ? 'storage run' : 'restock' }
			my $gr = defined &aylagear::run_start ? eval { aylagear::run_start($hm) } : undef;
			message "[partylead] aylagear run_start failed: $@\n", 'system' if $@;
			if ($gr && $gr->{town}) {
				# the town is the one the sell/buy NPCs now point at (aylagear.pl): the line names the town she really goes to
				# (QA 2026-11-14: "town run to payon" then she went to Geffen). Keep "town run ... to <town>": Legion's core
				# parses it (rally.go townRunLineRe) and sends the friends to that town.
				my $t = $gr->{town};
				my $extra = $gr->{line} ? ", $gr->{line}" : '';
				psay(pick("$need, town run to $t$extra. sell + restock there, meet at the kafra after",
					"town run to $t, $need$extra. butterfly wing, shop, then meet me at the $t kafra"), 0, 0);
			} elsif (gear_run_start()) {
				if ($gearWhy) {
					psay(pick("town run to the prontera market this time, $gearWhy. sell + restock there too",
						"town run to prontera south gate, $gearWhy. everyone can sell + shop there"), 0, 0);
				} else {
					# the run is for a real need (bag full / pots); the gear only rides along - say so (QA 2026-11-14:
					# "need my katar" read like a gear-only trip, and she already had the katar). Keep "town run to <town>":
					# Legion's core parses it (rally.go townRunLineRe).
					my $gi = lc($config{gearPlan_item} || 'new gear');
					my $need = AI::inQueue('sellAuto', 'storageAuto') ? 'bag full' : 'low on pots';
					psay(pick("$need. town run to the prontera market this time, i grab my $gi there. sell + restock there too",
						"$need. town run to prontera south gate, ill get my $gi on the way. everyone can sell + shop there"), 0, 0);
				}
			} else {
				my $t = town_of_map(($saved && $saved->{lockMap}) ? $saved->{lockMap} : ($config{lockMap} || $field->baseName));
				$t = $config{saveMap} if $config{saveMap} && $config{saveMap} =~ /^[a-z]+$/ && !$t;
				psay(pick("town run, bag full, follow me to $t", "town run to $t, everyone sell + restock, follow me",
					"bag full, town run to $t, come with me"), 0, 0);
			}
		}
		restore();
		if ($townWingAt && !is_townmap($field->baseName)) {
			if (time >= $townWingAt && !AI::inQueue('teleport')) {
				$townWingAt = 0;
				$wingOkUntil = time + 10;
				psay("town run: wing to " . ($config{saveMap} || 'town') . ", meet at the kafra", 0, 0);
				message "[watchdog] TELEPORT allowed: party town run (Butterfly Wing to " . ($config{saveMap} || '?') . ")\n", 'system';
				AI::clear('route', 'move');
				AI::ai_useTeleport(2); # 2 = Butterfly Wing to the save point
			}
			return; # no walking out while the wing is due
		}
		$townWingAt = 0 if is_townmap($field->baseName);
		# town runs are done together (owner rule): on the way out she walks at the party's pace - she stops while a friend
		# on her map is more than 20 cells behind, or still on a field/dungeon she has left (live positions: sight, then
		# Legion's API), for at most 3 min. QA 2026-11-14 13:33: she went pay_dun00 151,43 -> exit 21,173 in 40 s and
		# Zen.fox (82,26) walked out alone, reaching Payon 2.5 min after the rest.
		my $here0 = $field->baseName;
		if (!is_townmap($here0) && time - $errandStart >= 15 && time - $errandStart < 180) {
			my @behind;
			foreach my $id (keys %{$char->{party}{users}}) {
				my $u = $char->{party}{users}{$id};
				next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name}) || friend_dead($u->{name});
				my $m = friend_map($u); $m =~ s/\.gat$//;
				my $a = $apiPos{$u->{name}};
				$m = $a->{map} if $a && time - $a->{at} < 10 && $a->{map};
				next if $m eq '' || is_townmap($m); # already in town: ahead of her, no wait
				if ($m ne $here0) { push @behind, $u->{name}; next }
				my ($pos) = friend_pos($u, $here0);
				push @behind, $u->{name} if $pos && blockDistance($char->{pos_to}, $pos) > TOGETHER;
			}
			if (@behind) {
				AI::clear('route', 'move') if !AI::inQueue('attack') && (AI::is('route') || AI::is('move'));
				if (time - $lastMsg > 15) { $lastMsg = time; message "[partylead] town run: waiting for @behind to catch up\n", 'system' }
				psay(pick("wait for @behind, we go together", "@behind come, we go together"), 1, 45);
				return;
			}
		}
		if (time - $errandStart < 15 && !is_townmap($field->baseName)) {
			AI::clear('route', 'move') if AI::is('route') || AI::is('move');
			if (time - $lastMsg > 10) { $lastMsg = time; message "[partylead] town run: giving the party a moment to come along
", 'system' }
		}
		return;
	}
	gear_run_end() if $gearRun;
	if ($errand && defined &aylagear::run_end) { eval { aylagear::run_end() }; message "[partylead] aylagear run_end failed: $@\n", 'system' if $@ }
	if ($errand) {
		$errand = 0;
		$errandEnd = time;
		bag_log('end');
		$meetSaid = 0;
		# a run that freed little weight must not re-trigger a few kills later (QA: sell-run loop at 48%, only 4 items
		# sold each time): raise the trigger to itemsMaxWeight for 15 min, then back
		my $wpct = $char->{weight_max} ? 100 * $char->{weight} / $char->{weight_max} : 0;
		my $trig = $sellTrigSaved || $config{itemsMaxWeight_sellOrStore} || 70;
		# owner 2026-11-14: "a 50% weight should immediately trigger a town run" - so the guard only kicks in when the
		# run left her at/over the trigger (nothing more to store/sell), and then only 5 points above where she is
		if ($wpct >= $trig) {
			$sellTrigSaved = $trig; $sellTrigUntil = time + 900;
			$config{itemsMaxWeight_sellOrStore} = int($wpct) + 5;
			message sprintf("[partylead] town run freed little (weight %d%%): next sell run only at %d%% for 15 min\n", $wpct, $config{itemsMaxWeight_sellOrStore}), 'system';
		}
		psay(pick("back, omw to u", "done shopping, coming", "ok im back"), 1, 5);
	}

	goto_tick();
	dest_remind_tick(); # PT30: where to go / meet, again every ~5 min while friends are elsewhere
	return if portal_guard();
	travel_mode_tick();
	protect_tick();
	return if rally_tick();
	return if depart_tick();
	api_refresh();
	gear_refresh();
	rest_sit(party_resting()) unless is_townmap($field->baseName); # sit/stand for a rest before any hold logic returns
	return if back_tick($field->baseName);
	if ($pk{prOkAt} && time - $pk{prOkAt} < 3 && $field->baseName ne ($huntMap || '') && !$errand && !AI::inQueue('sellAuto', 'buyAuto', 'storageAuto', 'NPC')) {
		restore() if $holding;
		$depart = undef; $townWaitAt = 0;
		my $dest = $gotoMap ne '' ? $gotoMap : ($config{lockMap} || $homeMap || '');
		if ($dest ne '' && $dest ne $field->baseName && ($pk{annDest} || '') ne $dest) { $pk{annDest} = $dest; psay("going to $dest, follow me", 1, 0) }
		return;
	}
	my $here = $field->baseName;
	# in town (a town run): don't touch her movement - she walks to shops - and don't send her back to the hunting
	# map while the friends are still shopping. The old version restored lockMap here, so she left for the cave
	# before anyone had bought anything. Hunting resumes when everyone is with her outside town (or in town and close).
	if ($here =~ /^(prontera|payon|geffen|morocc|alberta|izlude|aldebaran|yuno|comodo|umbala)$|_in\d*$/) {
		# a mind's goto releases every town wait at once: she leaves for the ordered map, the friends follow (QA 12:47:
		# 'partylead goto pay_dun01' while she kept "in payon: waiting for X to catch up")
		if ($gotoMap ne '' && $here ne $gotoMap) {
			if ($holding) { $holding = 0; $config{lockMap} = $gotoMap; $config{route_randomWalk} = $saved ? $saved->{randomWalk} : 1 }
			$townWaitAt = 0;
			return;
		}
		# done shopping inside a building: walk out and wait in the town itself, where the friends gather (they never
		# follow into buildings - she sat inside payon_in01 while they waited outside)
		if ($here =~ /_in\d*$/ && !AI::inQueue('sellAuto', 'buyAuto', 'storageAuto', 'NPC', 'route') && time - $lastExit > 15) {
			my %town = (payon_in01 => 'payon', payon_in02 => 'payon', prt_in => 'prontera', geffen_in => 'geffen',
				morocc_in => 'morocc', alberta_in => 'alberta', izlude_in => 'izlude', aldeba_in => 'aldebaran',
				cmd_in01 => 'comodo', um_in => 'umbala', yuno_in01 => 'yuno');
			if (my $t = $town{$here}) {
				$lastExit = time;
				Commands::run('stand') if $char->{sitting};
				safe_route($t, undef, undef);
				psay(pick("done shopping, meet me outside", "coming out, wait for me outside"), 1, 60);
				return;
			}
		}
		# after shopping everyone meets at ONE point, the town's Kafra (owner 2026-11-14: Butterfly to town, shop, meet at
		# the Kafra, return together behind her): she walks there once and says so once
		if (time - $errandEnd < 180 && $KAFRA{$here} && time - $meetRouteAt > 10 && !AI::inQueue('route', 'move', 'NPC', 'attack')) {
			my ($k) = sort { blockDistance($char->{pos_to}, {x => $a->[0], y => $a->[1]}) <=> blockDistance($char->{pos_to}, {x => $b->[0], y => $b->[1]}) } @{$KAFRA{$here}};
			# the regroup after a town run ends 60 s after she reached the Kafra (owner: every regroup wait <= 60 s)
			if (blockDistance($char->{pos_to}, {x => $k->[0], y => $k->[1]}) <= 5) { $meetArrAt ||= time } else { $meetArrAt = 0 }
			if ($meetArrAt && time - $meetArrAt > 60) { $errandEnd = 0; $meetArrAt = 0; $regroupDoneAt = time; restore() if $holding; message "[partylead] town run regroup: 60 s at the Kafra - going on with the priest\n", 'system' }
			if (blockDistance($char->{pos_to}, {x => $k->[0], y => $k->[1]}) > 5) {
				$meetRouteAt = time;
				Commands::run('stand') if $char->{sitting};
				safe_route($here, $k->[0], $k->[1], distFromGoal => 3, maxRouteTime => 20);
				if (!$meetSaid) { $meetSaid = 1; psay(pick("done? meet me at the kafra, we go back together", "everyone to the kafra when ur done, then back to hunting"), 1, 0) }
			}
		}
		# only a real town run (she just sold/bought) waits in town for the friends' shopping; passing through town is
		# not a town run (owner: ~90% of online time must be farming) - the field logic keeps the party together
		# a departure that already timed out: no new wait in this town - she sets out, the friends follow
		if (time - $departEnded < 300) {
			if ($holding) { restore(); }
			return;
		}
		if (time - $errandEnd > 600 && time - $regroupDoneAt > 300) { # not right after the 60 s regroup at the Kafra
			# passing through town: no shopping wait, but she doesn't run ahead either - with a friend more than 20 cells
			# behind (or still on the previous map) she stops until it's close again (3 min max per stop). QA: she
			# crossed Payon 100 cells ahead of the party.
			my $far = '';
			foreach my $id (keys %{$char->{party}{users}}) {
				my $u = $char->{party}{users}{$id};
				next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name}) || friend_dead($u->{name});
				my $m = friend_map($u); $m =~ s/\.gat$//;
				if ($m ne $here) { $far = $u->{name}; last }
				next if party_strong(); # a strong party: same town map is enough, they follow her out
				my ($pos) = friend_pos($u, $here);
				if ($pos && blockDistance($char->{pos_to}, $pos) > ($townWaitAt ? RESUME : TOGETHER)) { $far = $u->{name}; last }
			}
			# AY22: the priest within 6 cells - no town wait for anyone else, she sets off ("going to <map>, follow me")
			my $pd = priest_dist();
			if ($far ne '' && ($pd <= 6 || $pd <= 15 && $townWaitAt && time - $townWaitAt > 30)) { # priest close (7-15: after 30 s)
				restore() if $holding; $townWaitAt = 0;
				my $dest = $gotoMap ne '' ? $gotoMap : ($config{lockMap} || $homeMap || '');
				if ($dest ne '' && $dest ne $here && ($meetAnn || '') ne $dest) { $meetAnn = $dest; psay("going to $dest, follow me", 1, 0) }
				$far = '';
			}
			if ($far ne '' && (!$townWaitAt || time - $townWaitAt < 180)) {
				$townWaitAt ||= time;
				if (!$holding) {
					$saved = {randomWalk => $config{route_randomWalk}, lockMap => $config{lockMap}};
					$config{lockMap} = ''; $config{route_randomWalk} = 0; $holding = 1;
				}
				AI::clear('route', 'move') if !AI::inQueue('attack', 'NPC') && (AI::is('route') || AI::is('move'));
				if (time - $lastMsg > 30) { $lastMsg = time; message "[partylead] in $here: waiting for $far to catch up
", 'system' }
				return;
			}
			$townWaitAt = 0 if $far eq '';
			if ($holding) { restore(); }
			return;
		}
		my $all = 1;
		foreach my $id (keys %{$char->{party}{users}}) {
			my $u = $char->{party}{users}{$id};
			next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name}) || friend_dead($u->{name});
			my $m = friend_map($u); $m =~ s/\.gat$//;
			my ($pos) = friend_pos($u, $here); # sight / core API (the party list's positions go stale)
			$all = 0 if $m ne $here || !party_strong() && (!$pos || blockDistance($char->{pos_to}, $pos) > TOGETHER);
		}
		if (!$holding) {
			$saved = {randomWalk => $config{route_randomWalk}, lockMap => $config{lockMap}};
			$config{lockMap} = ''; $config{route_randomWalk} = 0; $holding = 1;
			psay(pick("everyone sell + buy what u need, i wait here", "shop time, meet me here when done"), 1, 60) if !$all;
		}
		if ($all && $here !~ /_in\d*$/) {
			# everyone is with her: set out, but only at the party's pace - depart_tick leaves town only while every
			# friend is within 15 cells (QA: after a sell run she left Payon with the friends 100+ cells behind)
			$depart = {town => $here, at => time, routeAt => 0};
			psay(pick("all done? lets go back", "ok everyone here, back to hunting"), 1, 40);
		}
		return;
	}
	# farm map with the priest within 20 (PT04, d1c7320): the other members don't hold her - they come to her ("<name>
	# come here" once per 2 min each); she farms and sweeps (live: "PT04: wait - anchor Zen.fox 31 cells" / "farming
	# while waiting" paused the sweep, 9 kills in 5 min)
	if ($pk{prFarmOkAt} && time - $pk{prFarmOkAt} < 3 && $here eq ($huntMap || '')) {
		restore() if $holding;
		foreach my $n (@{$pk{farFriends} || []}) { next if time - ($pk{comeSaid}{$n} || 0) < 120; $pk{comeSaid}{$n} = time; psay("$n come here", 1, 0); last }
		farm_sweep();
		crowd_check($here);
		return;
	}
	# ---- hold / collect on the hunting map
	# owner 2026-11-14: "the party is considered together within 20 steps" (TOGETHER) and "If a party member does not
	# need help, keep going forward or wait, do not go back and forth". So:
	#   - a friend within TOGETHER cells is with the party: nothing to do;
	#   - a friend further away who is fine (following, not hurt, not asking) -> she WAITS where she is (no walking
	#     back, no random steps) until everyone is within RESUME cells, then carries on;
	#   - she walks back ONLY for a friend who needs help: asked in party chat, HP below 50%, or not moving for 30+ s
	#     while far (lost/stuck). Once she commits to one, she finishes (next to it, or 90 s) - no flip-flopping;
	#   - a friend on another map: rally at the Kafra (see start_rally).
	# a deliberate move ordered by the mind ('partylead goto'): never hold for members on the map she is leaving -
	# they follow her (QA: she was held on floor 2 waiting for members already heading to floor 1 - a deadlock)
	if ($gotoMap ne '') {
		if ($here eq $gotoMap) { $gotoMap = '' }
		elsif (!help_pending($here)) { # a friend here calling for help is fetched first (hold logic below), then the goto goes on
			if ($holding) { $holding = 0; $config{lockMap} = $gotoMap; $config{route_randomWalk} = $saved ? $saved->{randomWalk} : 1 }
			return;
		}
	}
	my ($why, $whoFar, $near, @needy);
	my $resting = party_resting();
	foreach my $id (keys %{$char->{party}{users}}) {
		my $u = $char->{party}{users}{$id}; next unless $u;
		my $m = friend_map($u); $m =~ s/\.gat$//;
		delete $awaySince{$id} if $m eq $here || !$u->{online};
		delete $farSince{$id} if $m ne $here || !$u->{online};
	}
	my $limit = $holding ? RESUME : TOGETHER;
	foreach my $id (keys %{$char->{party}{users}}) {
		my $u = $char->{party}{users}{$id};
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name}) || friend_dead($u->{name});
		my $m = friend_map($u);
		$m =~ s/\.gat$//;
		# a friend already on her destination (the hunting map she is heading to) is ahead: no hold for it - she goes there
		# (QA 20:32-20:37: 5 min by the pay_fild10 portal "waiting for Zen.fox (on pay_fild10)")
		my $destM = $homeMap ne '' ? $homeMap : (($config{lockMap} || '') ne '' ? $config{lockMap} : ($saved ? ($saved->{lockMap} || '') : ''));
		if ($m ne '' && $m ne $here && $m eq $destM) { $near++; next }
		# PT30: a friend on another map travels to the spot by its own route - in transit she doesn't wait for it at all,
		# on the spot at most 3 min; she says once where to meet (live 02:57Z: Ayla held on gef_fild07 for the priest
		# walking out of pay_dun01)
		if ($m ne '' && $m ne $here && pt30_on() && $destM ne '') {
			$farHold{$u->{name}} ||= time;
			if ($here ne $destM || time - $farHold{$u->{name}} > 180) {
				if (time - ($toldGo{$u->{name}} || 0) > 600) {
					$toldGo{$u->{name}} = time;
					psay(pick("$u->{name} we go on, meet us on $destM", "$u->{name} come to $destM, we start there", "going on to $destM, catch up $u->{name}"), 1, 0);
				}
				next;
			}
		}
		delete $farHold{$u->{name}} if $m eq $here;
		if ($m ne '' && $m ne $here) {
			$why = "$u->{name} is on $m";
			$whoFar = $u->{name};
			next;
		}
		# position: what she sees, then Legion's API, then the party list (friend_pos)
		my ($pos, $pl) = friend_pos($u, $here);
		next unless $pos;
		my $pk = "$pos->{x},$pos->{y}";
		if (!$memPos{$id} || $memPos{$id}[0] ne $pk) { $memPos{$id} = [$pk, time] }
		my $d = blockDistance($char->{pos_to}, $pos);
		# a friend resting (sitting) far away: wait at most 60 s, then carry on - it catches up when it stands
		# (QA: the party stood 5+ min with 0 kills while the priest sat, re-buffed and sat again)
		if ($pl && $pl->{sitting}) { $sitSince{$id} ||= time } else { delete $sitSince{$id} }
		if ($d > $limit && $sitSince{$id} && time - $sitSince{$id} > 60) {
			$near++;
			next;
		}
		my $dangerNow = ($dangerFrom{$u->{name}} && time - $dangerFrom{$u->{name}} < 90) || ($u->{hp_max} && $u->{hp} && $u->{hp} / $u->{hp_max} < 0.5);
		# a higher-level party (RULES 3): a friend within 30 cells that is not in danger walks to her by itself - no hold,
		# no turning back (QA 20:05: "hold up, Echobunny is behind" at 22-25 cells, 19 min for one floor)
		if ($d > $limit && $d <= 30 && party_strong() && !$dangerNow) { $near++; delete $farSince{$id}; next }
		if ($d > $limit) {
			$why = "$u->{name} is $d cells away" unless $why;
			$whoFar = $u->{name} unless defined $whoFar;
			# hurt = real HP under 60%; a "N on me" line counts only while its HP is under 80% (QA 20:23: "it is hurt" for
			# Zen.fox at 2735/2735 - the friends say "3 mobs on me, running to u" at full HP)
			my $hpf = friend_hp($u);
			my $hurt = (defined $hpf && $hpf < 0.6)
				|| ($dangerFrom{$u->{name}} && time - $dangerFrom{$u->{name}} < 90 && defined $hpf && $hpf < 0.6); # hurt = under 60% HP, a help line alone is 'asked' (QA 21:12: Zen.fox 79% 'hurt')
			my $asked = $helpFrom{$u->{name}} && time - $helpFrom{$u->{name}} < 120;
			# not moving for 30 s = lost/stuck - unless it sits (a priest resting for SP is fine: she just waits)
			# a friend standing still while busy (fighting, tanking, healing, resting, looting - its state from Legion's API)
			# is not lost: only 45 s without moving, 25+ cells away and not busy counts (QA: she went back for a priest
			# standing at 21-26 cells and the party's kill rate dropped)
			my $st = $apiPos{$u->{name}} ? ($apiPos{$u->{name}}{state} || '') : '';
			my $busy = $st =~ /fight|tank|taking|provok|defend|heal|buff|rest|sit|loot|pull|engag|assist|casting/i;
			my $lost = time - $memPos{$id}[1] > 45 && $d > 25 && !$busy && !($pl && $pl->{sitting});
			# waited 60 s for it beyond 20 cells and it hasn't come: the leader collects the straggler (owner rule).
			# QA 2026-11-14 12:25-12:27: Zen.fox stood 24 cells away "with Ayla" (its leader position was stale) while
			# she waited for it - 0 kills for minutes; 'lost' never fired below 25 cells.
			if ($d > TOGETHER) { $farSince{$id} ||= time } else { delete $farSince{$id} }
			my $stale = $farSince{$id} && time - $farSince{$id} > 60 && !($pl && $pl->{sitting});
			# while the party rests (owner 2026-11-14: "party need to regen sp so everybody should sit or attack mob nearby
			# to protect resting members"; QA 13:55: sit -> "not coming (24 cells)" -> stand, fetch -> sit, repeated) she
			# turns back only for a friend in danger or asking for help - never for 'not moving' / 'not coming'
			($lost, $stale) = (0, 0) if $resting;
			# a strong party: friends come to her by themselves - she turns back only for a hurt one; one asking where she
			# is gets her position instead of a walk back
			# a strong party: a friend that is merely far comes by itself; one that asked for help (or is hurt) she
			# goes to get (main 2026-11-14: the priest asked 4 times and stayed 194 cells away)
			($lost, $stale) = (0, 0) if party_strong();
			# owner 2026-11-14 ("ayla just walking around in circle"): never turn back for a straggler that is fine - only
			# for one that is hurt / in danger or asked for help (not moving / not coming no longer pull her back)
			$asked = 0 if $asked && $d <= 30 && !$dangerNow; # "come get me" from 25 cells: it comes itself
			push @needy, [$id, $d, $hurt ? 'is hurt' : 'asked for help'] if ($hurt || $asked) && $d > TOGETHER; # turning back only for a friend beyond 20 cells (RESUME only ends the wait)
		} else {
			$near++;
			delete $farSince{$id};
		}
	}
	# owner rule (RULES 3, 2026-11-14 "as soon as priest is there, they can already start farming"): with the party's
	# healer within 15 cells she doesn't wait for the rest - she farms, the others catch up on their own (a friend in
	# danger or asking for help still gets her)
	if ($why && !@needy && !$holding && time - $holdReleasedAt < 20) { $why = undef } # min. 20 s of walking between holds
	if ($why && !@needy && (my $hl = healer_near($here))) {
		if (time - ($healerSaidAt || 0) > 300) { $healerSaidAt = time; message "[partylead] $hl (healer) is with her: farming, the others catch up ($why)\n", 'system' }
		$why = undef;
	}

	if (!$why && $near && time > $nextIdle) {
		$nextIdle = time + 120 + rand(150);
		psay(pick("stay close to me", "exp is nice here", "i pull, u guys hit", "tell me if u need a break", "watch ur hp",
			"anyone need pots?", "good job team", "lets get some cards lol", "say if ur overweight, we go sell",
			"zen, bolt the one on me", "next lvl soon", "dont wander off pls"), 1, 30);
	}

	if ($why) {
		$holdInfo = {who => $whoFar, why => $why, since => ($holding && $holdInfo ? $holdInfo->{since} : time)}; # status_intent (dashboard)
		if (!$holding) {
			psay(pick("waiting for u $whoFar", "hold up, $whoFar is behind", "stay together guys, waiting for $whoFar"), 1, 40);
			$saved = {randomWalk => $config{route_randomWalk}, lockMap => $config{lockMap}};
			$config{route_randomWalk} = 0;
			$config{lockMap} = '';
			$holding = 1;
		}
		$collect = undef if $resting && !@needy; # a rest: no walking off for a friend that is fine
		# committed to someone who needs help: finish it (next to it, gone, or 90 s)
		my $tgt = $collect ? $char->{party}{users}{$collect} : undef;
		my $tpl = $tgt ? ($playersList ? $playersList->getByName($tgt->{name}) : undef) : undef;
		my ($tpos) = $tgt ? friend_pos($tgt, $here) : (undef); # sight, then Legion's API, then the (stale) party list
		$tpos ||= $collectPos if $tgt; # a moment without a position doesn't end the fetch (QA 20:23: 8 target changes in 2 min)
		$collectPos = $tpos if $tpos;
		my $tmap = $tgt ? friend_map($tgt) : ''; $tmap =~ s/\.gat$//; # live map (core API), not the stale party list
		# one target until reached (<= 4 cells), gone, on another map, or 2 min
		if (!$tgt || !$tgt->{online} || ($tmap ne '' && $tmap ne $here) || ($tpos && blockDistance($char->{pos_to}, $tpos) <= 4) || time - $collectAt > 120) {
			$collect = undef; $tgt = undef;
			if (@needy) {
				my $isHealer = sub { my $a = $apiPos{$char->{party}{users}{$_[0]}{name}}; return ($a && ($a->{cls} || '') =~ /priest|acolyte/i) ? 1 : 0 };
				my ($best) = sort { $isHealer->($b->[0]) <=> $isHealer->($a->[0]) || $a->[1] <=> $b->[1] } @needy; # the priest first
				$collect = $best->[0]; $collectAt = time; $lastFetch = 0;
				$tgt = $char->{party}{users}{$collect};
				my $tp = $playersList ? $playersList->getByName($tgt->{name}) : undef;
				($tpos) = friend_pos($tgt, $here);
				$collectPos = $tpos;
				$collectWhy = "$best->[2] ($best->[1] cells)";
				delete $helpFrom{$tgt->{name}};
				psay(pick("$tgt->{name} wait there, im coming", "omw to $tgt->{name}", "coming to get u $tgt->{name}"), 1, 15);
				message "[partylead] going back for $tgt->{name}: it $best->[2] ($best->[1] cells)\n", 'system';
			}
		}
		if ($tgt && $tpos) {
			if (!AI::inQueue('attack') && time - $lastFetch > 3) {
				$lastFetch = time;
				AI::clear('route', 'move');
				safe_route($here, $tpos->{x}, $tpos->{y}, distFromGoal => 2, attackOnRoute => 1);
			}
		} else {
			# nobody needs help: wait here and farm around the spot (owner rule "farm while waiting"), let them catch up
			if (!wait_farm()) {
				AI::clear('route', 'move') if !AI::inQueue('attack') && (AI::is('route') || AI::is('move')) && (!$waitSpot || blockDistance($char->{pos_to}, $waitSpot) <= 4);
			}
			# stragglers on another map: after 90 s (or when one asks for help) regroup at the Kafra nearest the hunting
			# ground instead of chasing friends map to map (she bounced pay_gld <-> moc_fild02)
			my ($farId, $farMap);
			foreach my $id (keys %{$char->{party}{users}}) {
				my $u = $char->{party}{users}{$id};
				next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name}) || friend_dead($u->{name});
				my $m = friend_map($u); $m =~ s/\.gat$//;
				next if $m eq '' || $m eq $here;
				$awaySince{$id} ||= time;
				# a friend in a town is on its own town trip and walks back to her by itself: no rally for that (she
				# turned back to payon ~10 times an hour for it). Rally only for a friend asking for help, or one stuck
				# on another field/dungeon for 4+ min (owner: ~90% of online time must be farming).
				next if $m =~ /^(prontera|payon|geffen|morocc|alberta|izlude|aldebaran|yuno|comodo|umbala)$|_in\d*$/ && !$helpFrom{$u->{name}};
				next unless time - $awaySince{$id} > 240 || $helpFrom{$u->{name}};
				($farId, $farMap) = ($id, $m);
				last;
			}
			# she no longer calls rallies herself for a split (owner 2026-11-14: a rally happens once, when the party sets
			# out; ~90% of the time must be farming - she dragged the party from pay_dun01 back to Payon because Zen.fox
			# was still fighting on pay_dun00). The friends travel to her map on their own; she waits. She still joins a
			# rally the friends call ("meet at the Kafra in ...").
			# a friend asking for help on a map of the same area (next floor, next field): go and get it - never a
			# rally for that (QA: Zen.fox on pay_dun00 said "help, cant find u" and she started a Payon Kafra rally).
			# One trip, committed for up to 3 min; once it is on her map the same-map logic brings it along.
			if ($farId && $helpFrom{$char->{party}{users}{$farId}{name}} && !$fetchMap && !AI::inQueue('attack')
				&& town_of_map($farMap) eq town_of_map($here)) {
				my $u = $char->{party}{users}{$farId};
				($fetchMap, $fetchAt) = ($farMap, time);
				delete $helpFrom{$u->{name}};
				psay(pick("coming to $farMap for u $u->{name}", "$u->{name} stay there, omw"), 1, 0);
				message "[partylead] going to $farMap to get $u->{name} (asked for help)\n", 'system';
			}
			if ($fetchMap) {
				if ($here eq $fetchMap || time - $fetchAt > 180) {
					$fetchMap = '';
				} elsif (!AI::inQueue('attack') && time - $lastFetch > 15) {
					$lastFetch = time;
					AI::clear('route', 'move');
					safe_route($fetchMap, undef, undef, attackOnRoute => 1);
				}
			}
		}
		if (time - $lastMsg > 30) {
			$lastMsg = time;
			message "[partylead] " . ($tgt ? "fetching $tgt->{name}: it $collectWhy" : "waiting for the party: $why") . "\n", 'system';
		}
	} elsif ($holding) {
		$collect = undef;
		restore();
		psay(pick("ok all here, lets go", "good, moving on", "k lets continue"), 1, 40);
		message "[partylead] party is together, carrying on\n", 'system';
		$holdReleasedAt = time;
	}
	crowd_check($here);
	sweep_tick();
}

# ---- dashboard (owner 2026-11-14 "since OpenKore crashed I have no way to know what Ayla is trying to do"): her current
# intent in plain words, derived from the plugin's own state. Read-only; statusout.pl calls it inside eval every ~3 s.
my ($intentKey, $intentSince) = ('', time);
sub intent_of {
	my $here = shift;
	my $now = time;
	return 'dead - respawning at ' . ($config{saveMap} || 'the save point') if $char->{dead};
	return 'teleporting away (escape)' if AI::inQueue('teleport');
	return "retreating to the party on $stickFlee->{map} (too many monsters on $stickFlee->{from})" if ref $stickFlee && $now - ($stickFlee->{at} || 0) < 90;
	if ($pk{waitAt} && $now - ($pk{seenAt} || 0) < 5 && ref $pk{an}) {
		my ($act, $an) = ($pk{act} || '', $pk{an});
		my $cells = defined $an->[3] ? " $an->[3] cells" : '';
		return "going back to collect $an->[0] ($an->[2]$cells)" if $act eq 'goto' || $act eq 'back';
		return "waiting for $an->[0] ($an->[2]$cells, " . int($now - $pk{waitAt}) . " s) before moving on";
	}
	return 'buying gear from a player shop' if defined &aylagear::busy && aylagear::busy();
	if ($errand || AI::inQueue('sellAuto', 'buyAuto', 'storageAuto')) {
		my $what = AI::inQueue('sellAuto') ? 'selling' : AI::inQueue('buyAuto') ? 'buying' : AI::inQueue('storageAuto') ? 'storage' : 'on the way';
		my $t = is_townmap($here) ? $here : (status_dest() || '');
		return 'town run' . ($t ne '' ? " to $t" : '') . " ($what)";
	}
	if ($rally) {
		return "rally at the $rally->{town} kafra" . ($rally->{why} ? " ($rally->{why})" : '') . ($rally->{thenGoto} ? ", then $rally->{thenGoto}" : '');
	}
	return "leaving $depart->{town} with the party (everyone close behind)" if ref $depart && $depart->{town};
	return "holding in town for $backWho" if $backWho ne '';
	if (defined $collect && $char->{party}{users}{$collect}) {
		return "going back for $char->{party}{users}{$collect}{name}" . ($collectWhy ne '' ? ": it $collectWhy" : '') . ' (' . int($now - $collectAt) . ' s)';
	}
	return "going to $fetchMap to get a friend who asked for help" if $fetchMap ne '';
	if ($holding) {
		my $h = $holdInfo || {};
		return 'waiting for ' . ($h->{who} || 'the party') . ($h->{why} ? " ($h->{why})" : '') . ', ' . int($now - ($h->{since} || $now)) . ' s';
	}
	return "going to $gotoMap" if $gotoMap ne '' && $gotoMap ne $here;
	my $aggr = scalar(AI::ai_getAggressives());
	return 'fighting ' . ($aggr > 1 ? "$aggr monsters" : 'a monster') . " on $here" if AI::inQueue('attack');
	if ($char->{sitting} || own_rest()) {
		return sprintf('resting (HP %d%%, SP %d%%)', $char->{hp_max} ? 100 * $char->{hp} / $char->{hp_max} : 0, $char->{sp_max} ? 100 * $char->{sp} / $char->{sp_max} : 0);
	}
	return "looting on $here" if AI::inQueue('take', 'items_take', 'items_gather');
	if (AI::inQueue('route', 'move')) {
		my $ri = AI::findAction('route');
		my $ra = (defined $ri && $ri ne '') ? AI::args($ri) : undef;
		my $rm = ($ra && ref $ra && $ra->{dest} && $ra->{dest}{map}) ? $ra->{dest}{map} : '';
		$rm = eval { $rm->baseName } || '' if ref $rm;
		return "walking to $rm" if $rm ne '' && $rm ne $here;
		return "farming $here (walking the loop)" if $huntMap ne '' && $here eq $huntMap;
		return "walking on $here";
	}
	return "farming $here" if $huntMap ne '' && $here eq $huntMap;
	return "in town ($here)" if is_townmap($here);
	return "standing on $here";
}
# status_intent: (intent text, since when that kind of intent runs). Never dies.
sub status_intent {
	my $s = eval { ($char && $field && $net && $net->getState() == 5) ? intent_of($field->baseName) : 'not in game' };
	$s = 'unknown (' . substr($@, 0, 80) . ')' if !defined $s;
	(my $k = $s) =~ s/\d+//g;
	($intentKey, $intentSince) = ($k, time) if $k ne $intentKey;
	return ($s, int($intentSince));
}
sub unload {
	if (defined $trivSaved) { $config{attackAuto} = $trivSaved; $trivSaved = undef }
	Plugins::delHooks($hooks);
	eval { Log::delHook($logHook) } if defined $logHook;
	Commands::unregister($respCmd) if $respCmd;
	Commands::unregister($pCmd) if $pCmd;
	Commands::register(['p', 'Chat in the party chat.', \&Commands::cmdChat]); # the built-in handler back
	Commands::register(['respawn', 'Respawn', \&Commands::cmdRespawn]); # the built-in handler back
	$config{route_randomWalk} = $restRW if defined $restRW && !$holding; # don't leave a rest's random-walk switch-off behind
	$config{itemsMaxWeight_sellOrStore} = $sellTrigSaved if $sellTrigUntil; # don't leave the raised trigger behind
	loot_trim_restore(); # pickupitems.txt values back
	mapexp_save();
	if (%travelSaved) { $config{$_} = $travelSaved{$_} foreach keys %travelSaved; %travelSaved = () } # travel mode's switches back
	Commands::unregister($cmds) if $cmds;
	$rally = undef;
	$depart = undef;
	gear_run_end();
	restore();
}

1;
