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
use Globals qw($char $field %config $net %timeout $playersList %portals_lut $portalsList $monstersList %ai_v);
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
my $hooks = Plugins::addHooks(['AI_pre', \&tick_guarded], ['packet_partyMsg', \&heard], ['ai_processRandomWalk', \&short_walk], ['checkMonsterAutoAttack', \&area_target], ['npc_talk', \&npc_said], ['target_died', \&gm_kill], ['self_died', \&gm_died]);
my $cmds = Commands::register(['partylead', 'Ayla party lead control: rally cancel | rally <town> | goto <map> | status', \&cmd_guarded]);

my ($holding, $saved, $last, $lastMsg, $lastFetch) = (0, undef, 0, 0, 0);
my ($lastSay, $nextIdle, $lastLv, $errand, @pending) = (0, time + 60, 0, 0);
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
my $townWaitAt = 0; # passing through town: waiting for a friend behind since
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
	} elsif ($args =~ /^\s*goto\s+(\S+)/i) {
		my $m = $1;
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
		my $rgT = is_townmap($hereT) ? $hereT : (!is_dungeon($hereT) && $KAFRA{$st} ? $st : '');
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
	return $a->{map} if $a && $a->{map} && time - $a->{at} < 10;
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
sub online_friends {
	my @f;
	foreach my $id (keys %{$char->{party}{users}}) {
		my $u = $char->{party}{users}{$id};
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name});
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
		push @late, $f->[0] if !$onMap || !party_strong() && (!$pos || blockDistance($char->{pos_to}, $pos) > 15);
		# owner rule (enforced 2026-11-14): nobody goes in before saving at this Kafra - wait for the friend's save
		# (Legion reports each friend's save town; the friend saves on its own and says "saved at <town>")
		push @late, $f->[0] if $a && $a->{saved} ne '' && $a->{saved} ne $depart->{town} && $KAFRA{$depart->{town}} && !grep { $_ eq $f->[0] } @late;
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
		main::ai_route($lm, undef, undef, attackOnRoute => route_aor(), maxRouteTime => 6);
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
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name});
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
	return ($pl->{pos_to}, $pl) if $pl && $pl->{pos_to} && defined $pl->{pos_to}{x};
	my $a = $apiPos{$u->{name}};
	return ({x => $a->{x}, y => $a->{y}}, undef) if $a && time - $a->{at} < 10 && $a->{map} eq $here && $a->{x};
	return ($u->{pos}, undef) if $u->{pos} && defined $u->{pos}{x};
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
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name});
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
			main::ai_route($bm, undef, undef, attackOnRoute => 1);
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

use constant RALLY_WAIT => 180; # a rally waits at most 3 min for members after her save (owner 2026-11-14)
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
sub end_rally {
	my ($why, $how) = @_;
	return unless $rally;
	message "[partylead] rally in $rally->{town} over: $why\n", 'system';
	my $town = $rally->{town};
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
	my $on = $dest ne '' && $here ne $dest && !is_townmap($here) ? 1 : 0;
	$on = 0 if $on && $gotoMap eq '' && friend_in_danger($here) && !trivial_transit(); # a friend in danger here: she fights for it (no travel mode) - not on a map far below the party (QA: Zombies on pay_dun00 for a level 81)
	# crossing a map far below her and the party (trivial_transit): no fights at all there, not even OpenKore's own
	# attackAuto - a level 81 stopped for every pay_dun00 Zombie (12 exp). Saved in a package var: survives a reload.
	my $triv = trivial_transit() ? 1 : 0;
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
		message "[partylead] travelling to $dest: no new fights on the way (defend only)
", 'system';
	} elsif (!$on && %travelSaved) {
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
			main::ai_route($r->{town}, $r->{x}, $r->{y}, distFromGoal => 3, attackOnRoute => route_aor());
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
	my ($all, @unsaved) = (1);
	foreach my $f (online_friends()) {
		my $pl = $playersList ? $playersList->getByName($f->[0]) : undef;
		$all = 0 if $f->[1] ne $r->{town} || !$pl || !$pl->{pos_to} || !defined $pl->{pos_to}{x}
			|| blockDistance({x => $r->{x}, y => $r->{y}}, $pl->{pos_to}) > 12;
		my $sv = $friendSaved{$f->[0]};
		push @unsaved, $f->[0] unless $r->{friendsDone} || ($sv && $sv->[0] eq $r->{town} && $sv->[1] >= $r->{at} - 60);
	}
	$r->{allAt} = $all ? ($r->{allAt} || time) : 0;
	my $mineOk = $r->{saved} || ($r->{saveTries} >= 3 && time - $r->{saveAt} > 15);
	$r->{mineAt} ||= time if $mineOk;
	my $goMap = ($saved && ($saved->{lockMap} || '') ne '') ? $saved->{lockMap} : ($r->{thenGoto} || $r->{dest} || '');
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
	if ($all && $mineOk && @unsaved && time - ($r->{askSave} || 0) > 40) { $r->{askSave} = time; psay("@unsaved save at the kafra here pls, say when saved", 1, 0) }
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
				main::ai_route($here, $x, $y, maxRouteTime => 30, noMapRoute => 1, attackOnRoute => 2);
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
				main::ai_route($here, $x, $y, maxRouteTime => 8, noMapRoute => 1, attackOnRoute => 2);
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
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name});
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
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name});
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
		main::ai_route($here, $waitSpot->{x}, $waitSpot->{y}, distFromGoal => 2, maxRouteTime => 10, noMapRoute => 1);
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
	return if time - $lastFightAt < 60 || time - $crowdSaidAt < 300;
	my $n = 0;
	foreach my $p (@{$playersList ? $playersList->getItems() : []}) { $n++ unless $char->{party}{users}{$p->{ID}} }
	$crowdSaidAt = time;
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
			main::ai_route($m, $tx, $ty, maxRouteTime => 10, attackOnRoute => 2, noMapRoute => 1, isRandomWalk => 1, distFromGoal => 2);
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
	main::ai_route($m, $tx, $ty, maxRouteTime => 10, attackOnRoute => 2, noMapRoute => 1, isRandomWalk => 1, distFromGoal => 2);
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
	return $d eq $here ? '' : $d;
}
sub status_town_run {
	return 1 if $errand || AI::inQueue('sellAuto', 'buyAuto', 'storageAuto');
	return ($field && is_townmap($field->baseName) && time - $errandEnd < 600) ? 1 : 0;
}

sub flush_say {
	return unless @pending && time >= $pending[0][0];
	my $p = shift @pending;
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
	if ($tgt eq '' && is_townmap($field->baseName) && time - $errandEnd < 600) { return } # the promised Kafra regroup after a town run comes first
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
	main::ai_route($tgt, undef, undef, attackOnRoute => route_aor());
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
	return 0 unless ($char->{lv} || 0) >= $top + 15;
	return 0 unless transit_fit_lv() >= $top + 15; # the party's gear-rated level, not her base level (58 vs 81, live 2026-11-14)
	foreach my $f (online_friends()) { my $a = $apiPos{$f->[0]}; return 0 if $a && $a->{lv} && $a->{lv} < $top + 8 } # a low friend still needs her here
	my $dest = $gotoMap ne '' ? $gotoMap : ($config{lockMap} || '');
	$dest = $homeMap if $dest eq '' && !$rally;
	return ($dest ne '' && $dest ne $here) || $rally || $depart ? 1 : 0;
}
sub route_aor { return trivial_transit() ? 0 : 1 } # attackOnRoute for a map-to-map route

my %crowdedAt; # map -> when it was too crowded to farm (skipped for an hour)
my $gmRecheck = 0; # gear maps: the measured step changed - fit_tick re-checks at once
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
sub gm_verdict {
	my ($n, $min, $k, $d) = @_;
	return 0 if $n <= 0 || $min <= 0;
	my $bm = $n * $min;
	my ($kpm, $dph) = ($k / $bm, $d / ($bm / 60));
	return -3 if $dph >= 1.5 || $d > 0 && $kpm < 0.25;
	return 0 if $d > 0;
	return 2 if $kpm >= 2;
	return 0;
}
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
my $gmOff = 0;
sub gm_kill { $gmKills++ }
sub gm_died { $gmDeaths++ }
sub gm_step_offset {
	return $gmOff unless $char && $field;
	my $here = $field->baseName;
	return $gmOff if is_townmap($here);
	my ($n, $k, $d) = (1, $gmKills, $gmDeaths);
	foreach my $f (online_friends()) {
		my $a = $apiPos{$f->[0]};
		next unless $a && defined $a->{kills} && $f->[1] eq $here;
		$n++; $k += $a->{kills}; $d += $a->{deaths} || 0;
	}
	if ($gmWin{map} ne $here || $gmWin{n} != $n || $k < $gmWin{k} || $d < $gmWin{d}) {
		%gmWin = (map => $here, at => time, k => $k, d => $d, n => $n);
		return $gmOff;
	}
	my $el = (time - $gmWin{at}) / 60;
	return $gmOff if $el < 8;
	my $v = gm_verdict($n, $el, $k - $gmWin{k}, $d - $gmWin{d});
	if ($v) {
		my $old = $gmOff;
		$gmOff += $v; $gmOff = -15 if $gmOff < -15; $gmOff = 10 if $gmOff > 10;
		message "[partylead] gear maps: party on $here " . sprintf('%.1f', ($k - $gmWin{k}) / ($n * $el)) . " kills/min each, " . ($d - $gmWin{d}) . " deaths in " . int($el) . " min -> step $old -> $gmOff\n", 'system';
		$gmRecheck = 1; # fit_tick re-checks the map choice now
	}
	%gmWin = (map => $here, at => time, k => $k, d => $d, n => $n);
	return $gmOff;
}
# a map order far above the party: every table row for it starts 4+ levels over the party's gear-rated level (with the
# measured kills / deaths step, party_fit_lv). '' = fine (or a map not in the table). conf partylead_mindCheck 0 = off.
sub mind_map_far {
	my $m = shift || '';
	return '' if defined $config{partylead_mindCheck} && $config{partylead_mindCheck} eq '0';
	my $lo;
	foreach my $r (@FIT_MAPS) { $lo = $r->[0] if $r->[2] eq $m && (!defined $lo || $r->[0] < $lo) }
	return '' unless defined $lo;
	my $plv = party_fit_lv();
	return '' if $plv + 3 >= $lo;
	return "$m starts at level $lo, the party is rated $plv (gear, kills and deaths)";
}
sub map_fits {
	my ($m, $plv) = @_;
	foreach my $r (@FIT_MAPS) { return 1 if $r->[2] eq $m && $plv >= $r->[0] - 2 && $plv <= $r->[1] + 3 }
	return 0;
}
# the level / gear choice: the first row (table order = preference) that fits, isn't crowded, matches the healer state
sub level_map {
	my $plv = shift;
	my $heal = party_has_healer();
	my $ok = sub { my $r = shift; return $plv >= $r->[0] && $plv <= $r->[1] && !(time - ($crowdedAt{$r->[2]} || 0) < 3600) };
	foreach my $r (@FIT_MAPS) { return $r->[2] if $ok->($r) && $r->[3] == $heal }
	foreach my $r (@FIT_MAPS) { return $r->[2] if $ok->($r) && !$r->[3] } # no healer row: a field
	foreach my $r (@FIT_MAPS) { return $r->[2] if $ok->($r) }
	my ($best, $bd);
	foreach my $r (@FIT_MAPS) { my $d = abs(($r->[0] + $r->[1]) / 2 - $plv); ($best, $bd) = ($r->[2], $d) if !defined $bd || $d < $bd }
	return $best;
}
sub mind_fresh { return $homeBy eq 'mind' && $homeMap ne '' && time - $homeAt < HOME_STALE }
# where the party should farm now, and why
sub choose_map {
	my ($tm, $who) = party_turn();
	return ($tm, "$who\'s gear turn") if $tm ne '' && !(time - ($crowdedAt{$tm} || 0) < 3600);
	my $plv = party_fit_lv();
	return (level_map($plv), "fits the party (level $plv" . (party_has_healer() ? ', healer' : ', no healer') . ")");
}
sub auto_goto {
	my ($m, $why) = @_;
	message "[partylead] own map choice: $m - $why (was " . ($homeMap || '-') . ($homeBy eq 'mind' && $homeMap ne '' ? ", mind's order " . int((time - $homeAt) / 60) . " min old" : '') . ")\n", 'system';
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
	return if $homeMap ne '' && $tm eq '' && map_fits($homeMap, party_fit_lv()) && !(time - ($crowdedAt{$homeMap} || 0) < 3600) && ($config{lockMap} || $homeMap) eq $homeMap;
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
sub lockmap_guard {
	return unless $net && $net->getState() == 5 && $char && $field && $char->{party} && $char->{party}{users};
	my $lm = $config{lockMap} || '';
	return if defined $lmSet && $lm eq $lmSet;
	if ($lm ne '' && !is_townmap($lm)) {
		# the party's levels come from Legion's API: no verdict before they are in (5 min at most, as fit_tick)
		my @fr = online_friends();
		return if @fr && !grep({ $apiPos{$_->[0]} && $apiPos{$_->[0]}{lv} } @fr) && time - $fitLoadAt < 300;
		my $why = map_dropped($lm) || mind_map_far($lm);
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
sub tick_guarded {
	lockmap_guard();
	tick(@_);
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
	my $e = {id => sprintf("w%d-ayla", int(time * 1000)), t => sprintf("%04d-%02d-%02dT%02d:%02d:%02d", $l[5] + 1900, $l[4] + 1, @l[3, 2, 1, 0]),
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
	if (($rally && $here ne $rally->{town} || AI::inQueue('route', 'move')) && !AI::inQueue('attack', 'NPC', 'sellAuto', 'buyAuto', 'storageAuto', 'deal')
		&& !$char->{sitting} && !own_rest() && !$char->{dead} && $now - $wdStuckSince > 180) {
		my $dest = $rally ? ($rally->{thenGoto} || $rally->{dest} || $rally->{town}) : ($gotoMap || $config{lockMap} || '');
		wd_report('STUCK', $here, "$here $pos for " . int(($now - $wdStuckSince) / 60) . " min, route to " . ($dest || '?') . " fails" . ($rally ? " (rally $rally->{town})" : ''),
			($rally ? 'rally cancelled, ' : '') . 'respawn to ' . ($config{saveMap} || 'the save point'));
		# a respawn that lands in the town she is stuck in changes nothing (live 2026-11-14: STUCK geffen 118,39, route to
		# gl_church fails -> respawn to geffen -> the same route again, in a loop): the destination is dropped for 2 h at
		# once, whoever set it, and she falls back to her own gear-rated choice
		my $sameTown = is_townmap($here) && ($config{saveMap} || '') eq $here;
		if ($dest ne '' && !is_townmap($dest) && ($sameTown || ++$wdStuckN{$dest} >= 2 || $droppedUntil{$dest})) { drop_map($dest, $sameTown ? "route from $here fails and a respawn lands in $here again" : "route fails again"); $wdStuckN{$dest} = 0 }
		elsif ($dest ne '' && ++$wdStuckN{$dest} >= 2 && !mind_fresh()) { $crowdedAt{$dest} = $now + 3600; $wdStuckN{$dest} = 0 }
		if ($rally) { end_rally("can't find a way there", 0); $rally = undef }
		$depart = undef; $gotoMap = ''; $holding = 0;
		AI::clear('route', 'move');
		if ($sameTown) { ($wdStuckSince, $wdProdAt, $wdAct) = ($now, $now, $now); $wdExp = $exp; $wdPos = $pos; $fitAt = 0; return } # no respawn: fit_tick picks her own map now
		Commands::run('respawn');
		($wdStuckSince, $wdProdAt, $wdAct) = ($now, $now, $now);
		$wdExp = $exp; $wdPos = $pos;
		return;
	}
	# a map far below her level for 5+ min (not a short crossing): report it - fit_tick moves her unless a fresh order holds her
	my $top = map_top($here);
	if ($top && !is_townmap($here) && ($char->{lv} || 0) >= $top + 15) {
		($lowSince, $lowMap) = ($now, $here) if $lowMap ne $here;
		if ($now - $lowSince > 300) {
			wd_report('WRONG_MAP', $here, "on $here (worth to level $top) for " . int(($now - $lowSince) / 60) . " min at base $char->{lv}; party level " . party_fit_lv() . ", target " . ($homeMap || '-') . (mind_fresh() ? " (mind's order)" : ''),
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
		main::ai_route($tgt, undef, undef, attackOnRoute => route_aor());
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
	my $ok;
	if ($cross eq 'pre') { $ok = $pr ? ($prNear && @near >= $two) : @near >= $two }
	elsif ($cross eq 'on') { $ok = $pr ? $prNear : @near >= 1 }
	elsif ($s->{leaving}) { $ok = $pr ? $prNear : @near >= $two }
	elsif ($s->{farm} && $pr && $pr->[2] eq $here) { $ok = $prNear }
	else { $ok = @near >= 1 }
	my ($nearest) = sort { $sameD->($a) <=> $sameD->($b) } @f;
	my $an = $pr || $nearest;
	return ('ok', $an) if $ok;
	return ('back', $an) if $s->{farm} && $an->[2] eq $here && defined $an->[3];
	return ('goto', $an) if ($s->{waited} || 0) >= 180;
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
	my ($me, $t, $pr, $others, $onMe, $tOnMe) = @_;
	return 1 if $tOnMe;
	return 0 if $onMe >= 2;
	my $d = sub { my ($a, $b) = @_; my ($dx, $dy) = (abs($a->{x} - $b->{x}), abs($a->{y} - $b->{y})); return $dx > $dy ? $dx : $dy };
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
# ---- pack end
our %WAIT_FIELDS = (geffen => ['gef_fild13', 'gef_fild07'], prontera => ['prt_fild08', 'prt_fild05', 'prt_fild06', 'prt_fild01']);
my %pk = (waitAt => 0, sayAt => 0, logAt => 0, goName => '', routeAt => 0, crossSaid => '', wf => undef);
my @crossWings; # [time, map]
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
sub cross_wing {
	my ($m, $dest) = @_;
	@crossWings = grep { time - $_->[0] < 1800 && $_->[1] eq $m } @crossWings;
	push @crossWings, [time, $m];
	return unless @crossWings >= 2 && $dest ne '' && $dest ne $m && !is_townmap($dest);
	@crossWings = ();
	drop_map($dest, "crossing $m failed twice (2 wings within 30 min); the next fitting map that does not need it");
}
sub pack_tick {
	return 0 if defined $config{partylead_pack} && $config{partylead_pack} eq '0';
	return 0 if $rally || $depart || $holding || $collect || $fetchMap ne '' || $char->{dead} || !$char->{pos_to};
	return 0 if AI::inQueue('NPC', 'sellAuto', 'buyAuto', 'storageAuto', 'deal', 'teleport') || (defined &aylagear::busy && aylagear::busy());
	my $here = $field->baseName;
	my @fr;
	foreach my $u (values %{$char->{party}{users} || {}}) {
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name});
		my $m = friend_map($u); $m =~ s/\.gat$//;
		my ($pos) = $m eq $here ? friend_pos($u, $here) : ();
		my $a = $apiPos{$u->{name}};
		push @fr, [$u->{name}, ($a && $a->{cls}) || '', $m, ($pos && defined $pos->{x}) ? blockDistance($char->{pos_to}, $pos) : undef, $pos];
	}
	if (!@fr) { ($pk{waitAt}, $pk{goName}, $pk{wf}) = (0, '', undef); return 0 } # everyone offline: she may farm solo
	my ($dm, $nm) = route_maps();
	my $farm = $huntMap ne '' && $here eq $huntMap ? 1 : 0;
	my $town = is_townmap($here);
	my $leaving = $town && $dm ne '' && !is_townmap($dm) ? 1 : 0;
	return 0 if $town && !$leaving && !$pk{wf};       # town errands / shopping / the Kafra regroup
	return 0 if !$farm && $dm ne '' && is_townmap($dm) && !$pk{wf}; # a town run back (the party meets at the Kafra)
	my $cross = ($dm ne '' && $dm ne $here && cross_danger($here)) ? 'on' : ($nm ne '' && $nm ne $dm && cross_danger($nm)) ? 'pre' : '';
	# fighting while she waits: released only when the priest + one friend (priest offline: 2 friends) are within 20
	my ($act, $an) = pack_decide({here => $here, friends => \@fr, farm => $farm, leaving => ($leaving || $pk{wf} ? 1 : 0), cross => $cross,
		waited => $pk{waitAt} ? time - $pk{waitAt} : 0});
	if (($act eq 'ok' || $act eq 'solo') && $pk{wf}) {
		$pk{wf} = undef; ($pk{waitAt}, $pk{goName}) = (0, '');
		my $go = $homeMap ne '' ? $homeMap : ($config{lockMap} || (($saved && $saved->{lockMap}) || ''));
		psay("ok everyone here, lets go" . ($go ne '' ? " to $go" : ''), 1, 0);
		message "[partylead] PT04: the party is with her - setting off together" . ($go ne '' ? " for $go" : '') . "\n", 'system';
		if ($go ne '' && $go ne $here) { AI::clear('attack', 'route', 'move'); main::ai_route($go, undef, undef, attackOnRoute => route_aor()) }
		return 1;
	}
	if ($act eq 'ok' || $act eq 'solo') {
		if ($cross eq 'pre' && $pk{crossSaid} ne $nm) { $pk{crossSaid} = $nm; psay("crossing $nm together, stay close", 0, 0); message "[partylead] PT04: crossing $nm with the party\n", 'system' }
		$pk{crossSaid} = '' if $cross eq '' && !cross_danger($here);
		($pk{waitAt}, $pk{goName}) = (0, '');
		return 0;
	}
	return 0 if AI::inQueue('attack') && scalar(AI::ai_getAggressives()); # she answers what is on her
	$pk{waitAt} ||= time;
	my ($cx, $cy) = ($char->{pos_to}{x}, $char->{pos_to}{y});
	if (time - $pk{logAt} > 20) { $pk{logAt} = time; message "[partylead] PT04: $act - " . ($cross ? "crossing ($cross) needs the priest within 10, " : '') . "anchor $an->[0] on $an->[2]" . (defined $an->[3] ? " $an->[3] cells" : '') . "\n", 'system' }
	if ($act ne 'back' && !$farm) {
		if (!$pk{wf}) {
			my (%tops, %danger);
			foreach my $m ($here, @{$WAIT_FIELDS{$here} || []}) { $tops{$m} = map_top($m); $danger{$m} = cross_danger($m) }
			my $wm = pack_wait_map($here, $town, $WAIT_FIELDS{$here}, transit_fit_lv(), \%tops, \%danger);
			if ($wm ne '') {
				$pk{wf} = {map => $wm, meet => $here, spot => ($wm eq $here ? {x => $cx, y => $cy} : undef), at => time};
				message "[partylead] PT04: waiting for the party - fighting on $wm meanwhile (meeting point $here)\n", 'system';
			}
		}
		if ($pk{wf}) { return 1 if wait_fight(); }
	}
	if ($act eq 'back') {
		if (time - $pk{routeAt} > 4 && $an->[4]) {
			$pk{routeAt} = time;
			AI::clear('attack', 'route', 'move', 'items_take', 'take', 'items_gather');
			main::ai_route($here, $an->[4]{x}, $an->[4]{y}, distFromGoal => 4, maxRouteTime => 8, noMapRoute => 1, attackOnRoute => 1);
		}
		return 1;
	}
	if ($act eq 'goto') {
		my $a = $apiPos{$an->[0]};
		my ($am, $ax, $ay) = ($an->[2], $an->[4] ? ($an->[4]{x}, $an->[4]{y}) : ($a && time - $a->{at} < 10 && $a->{map} eq $an->[2] ? ($a->{x}, $a->{y}) : (undef, undef)));
		if ($am ne '') {
			if ($pk{goName} ne $an->[0]) { $pk{goName} = $an->[0]; psay("coming to u, $an->[0]", 1, 0) }
			if (time - $pk{routeAt} > 10 || !AI::inQueue('route', 'move')) {
				$pk{routeAt} = time;
				Commands::run('stand') if $char->{sitting};
				AI::clear('route', 'move');
				main::ai_route($am, $ax, $ay, distFromGoal => 4, attackOnRoute => route_aor(), ($am eq $here ? (noMapRoute => 1, maxRouteTime => 10) : ()));
			}
			return 1;
		}
	}
	# wait: stand where she is (OpenKore's sitAuto still sits her under its HP / SP marks)
	AI::clear('route', 'move') if AI::inQueue('route', 'move');
	if (time - $pk{sayAt} >= 60) { $pk{sayAt} = time; psay("waiting for u at $here $cx,$cy", 1, 0) }
	return 1;
}
# PT32 runtime: the monster against pt32_ok with her live surroundings (conf partylead_pt32 0 turns it off)
sub pt32_target {
	my $m = shift;
	return 1 if defined $config{partylead_pt32} && $config{partylead_pt32} eq '0';
	return 1 unless $m && $m->{pos_to} && $char && $char->{pos_to} && $field && !is_townmap($field->baseName);
	my %ag = map { $_ => 1 } AI::ai_getAggressives();
	my $tOnMe = ($ag{$m->{ID}} || $m->{dmgToYou} || $m->{missedYou}) ? 1 : 0;
	my (@others, $onMe);
	$onMe = 0;
	foreach my $mo (@{$monstersList ? $monstersList->getItems() : []}) {
		next unless $mo->{pos_to} && defined $mo->{pos_to}{x} && !$mo->{dead};
		my $hits = $mo->{dmgToYou} || $mo->{missedYou} ? 1 : 0;
		$onMe++ if $hits && blockDistance($char->{pos_to}, $mo->{pos_to}) <= 3;
		next if $mo->{ID} eq $m->{ID};
		push @others, $mo->{pos_to} if $ag{$mo->{ID}} || $hits;
	}
	my $pr;
	foreach my $f (stick_mates()) { if ($f->[2] =~ /priest|acolyte/i) { $pr = $f->[1]; last } }
	return pt32_ok($char->{pos_to}, $m->{pos_to}, $pr, \@others, $onMe, $tOnMe);
}
# fights near the meeting point while the party comes to her: at most one map from it, within 14 cells of where she
# arrived (PT04: friends reach her quickly, no charging ahead). False = the wait-farm ended (she is elsewhere).
sub wait_fight {
	my $w = $pk{wf};
	my $here = $field->baseName;
	my ($cx, $cy) = ($char->{pos_to}{x}, $char->{pos_to}{y});
	if ($here ne $w->{map}) {
		if ($here ne $w->{meet}) { $pk{wf} = undef; return 0 } # winged / respawned elsewhere: start over
		if (!AI::inQueue('route', 'move') || time - $pk{routeAt} > 15) {
			$pk{routeAt} = time;
			Commands::run('stand') if $char->{sitting};
			AI::clear('route', 'move');
			main::ai_route($w->{map}, undef, undef, attackOnRoute => 1);
		}
		return 1;
	}
	$w->{spot} ||= {x => $cx, y => $cy}; # the cell she arrived on: next to the way back to the meeting point
	if (time - $pk{sayAt} >= 90) { $pk{sayAt} = time; psay("farming at $w->{map} near the kafra, come here", 1, 0) }
	return 1 if AI::inQueue('attack') || $char->{sitting};
	my ($best, $bd);
	foreach my $mo (@{$monstersList ? $monstersList->getItems() : []}) {
		next unless $mo->{pos_to} && defined $mo->{pos_to}{x} && !$mo->{dead};
		next if blockDistance($w->{spot}, $mo->{pos_to}) > 14 || !pt32_target($mo);
		my $d = blockDistance($char->{pos_to}, $mo->{pos_to});
		($best, $bd) = ($mo, $d) if !defined $bd || $d < $bd;
	}
	if ($best) { AI::clear('route', 'move'); main::attack($best->{ID}); return 1 }
	if (AI::inQueue('route', 'move') && time - $pk{routeAt} < 8) { return 1 }
	$pk{routeAt} = time;
	my ($tx, $ty) = blockDistance($char->{pos_to}, $w->{spot}) > 10 ? ($w->{spot}{x}, $w->{spot}{y}) : free_cell($w->{spot}{x}, $w->{spot}{y}, 3, 8);
	($tx, $ty) = ($w->{spot}{x}, $w->{spot}{y}) unless defined $tx;
	AI::clear('route', 'move');
	main::ai_route($here, $tx, $ty, distFromGoal => 2, maxRouteTime => 8, noMapRoute => 1, attackOnRoute => 2);
	return 1;
}
sub stick_on { return !defined $config{partylead_stick} || $config{partylead_stick} ne '0' }
# online friends (not on an errand) on her map with a known cell: [name, pos, class]
sub stick_mates {
	my $here = $field->baseName;
	my @m;
	foreach my $u (values %{$char->{party}{users} || {}}) {
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name});
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
	# on the way to another map (travel mode): no new pulls at all - only what hits her or a friend (live 2026-11-14: 6
	# orcs pulled on gef_fild10 on the way to orcsdun02)
	return 0 if %travelSaved;
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
	my @mates = stick_mates();
	my $an = stick_anchor(@mates);
	my $ad = $an ? blockDistance($char->{pos_to}, $an->[1]) : -1;
	my ($cx, $cy) = ($char->{pos_to}{x}, $char->{pos_to}{y});
	# about to die and still being hit: Butterfly Wing to the save Kafra (not a random cell), and say where and what next
	if ($hpp < STICK_DEADLY && $stickLowAt && time - $stickLowAt >= 0.4 && $aggr >= 1 && time - $stickHitAt < 3 && time - $stickWingAt > 10 && $char->inventory->getByName('Butterfly Wing')) {
		$stickWingAt = time;
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
				main::ai_route($here, $tx, $ty, distFromGoal => 2, maxRouteTime => 6, noMapRoute => 1, attackOnRoute => 0);
				message "[partylead] PT31: $boss->{name} $bd cells away - the party pulls back together\n", 'system';
				return 1;
			}
		}
	}
	# PT32: a second monster on her with the priest / tank more than 5 cells away: step back toward them (no help call)
	if ($aggr == 2 && $hpp >= 0.45 && $an && $ad > 5 && time - $stickRunAt > 3 && !(defined $config{partylead_pt32} && $config{partylead_pt32} eq '0')) {
		$stickRunAt = time;
		AI::clear('route', 'move');
		main::ai_route($here, $an->[1]{x}, $an->[1]{y}, distFromGoal => 3, maxRouteTime => 4, noMapRoute => 1, attackOnRoute => 1);
		message "[partylead] PT32: 2 monsters on her - stepping back toward $an->[0] ($ad cells)\n", 'system';
		return 1;
	}
	# in danger: fall back to the priest / tank and ask for help (no wing, no teleport)
	if ($aggr >= 3 || $hpp < 0.45 && $aggr >= 1) {
		if (time - $stickHelpAt > 45) {
			$stickHelpAt = time;
			psay(pick("help, $aggr on me", "help! $aggr on me ($here $cx,$cy)", "$aggr mobs on me, need help"), 0, 0);
		}
		if ($an && $ad > 3 && time - $stickRunAt > 2) {
			$stickRunAt = time;
			Commands::run('stand') if $char->{sitting};
			AI::clear('attack', 'route', 'move', 'items_take', 'take', 'items_gather');
			main::ai_route($here, $an->[1]{x}, $an->[1]{y}, distFromGoal => 2, maxRouteTime => 6, noMapRoute => 1, attackOnRoute => 0);
			message sprintf("[partylead] PT31: falling back to %s (%d cells; HP %d%%, %d monsters on her)\n", $an->[0], $ad, $hpp * 100, $aggr), 'system';
			return 1;
		}
		# swarmed with no friend on this map (she got ahead of them on a crossing): back to the map the friends are on
		if (!$an && $aggr >= 4 && time - $stickRunAt > 5) {
			my %on; $on{$_->[1]}++ foreach grep { $_->[1] ne '' && $_->[1] ne $here } @friends;
			my ($fm) = sort { $on{$b} <=> $on{$a} } keys %on;
			if ($fm) {
				$stickRunAt = time;
				AI::clear('attack', 'route', 'move', 'items_take', 'take', 'items_gather');
				main::ai_route($fm, undef, undef, attackOnRoute => 0);
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
	if ($pk ne $lastPos) { ($lastPos, $stillSince) = ($pk, time); }
	elsif (time - $stillSince > 20 && !$holding && !$rally && !$depart && !party_resting() && !AI::inQueue('attack', 'NPC', 'sitting', 'sellAuto', 'buyAuto', 'storageAuto') && !$char->{sitting}) {
		$stillSince = time;
		my ($x, $y) = free_cell($char->{pos_to}{x}, $char->{pos_to}{y}, 2, 5);
		($x, $y) = ($char->{pos_to}{x}, $char->{pos_to}{y}) unless defined $x;
		message "[partylead] standing still too long, stepping to $x,$y
", 'system';
		AI::clear('route', 'move');
		main::ai_route($field->baseName, $x, $y, distFromGoal => 1, maxRouteTime => 8);
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
			$errandStart = time;
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
		# town runs are done together (owner rule): on the way out she walks at the party's pace - she stops while a friend
		# on her map is more than 20 cells behind, or still on a field/dungeon she has left (live positions: sight, then
		# Legion's API), for at most 3 min. QA 2026-11-14 13:33: she went pay_dun00 151,43 -> exit 21,173 in 40 s and
		# Zen.fox (82,26) walked out alone, reaching Payon 2.5 min after the rest.
		my $here0 = $field->baseName;
		if (!is_townmap($here0) && time - $errandStart >= 15 && time - $errandStart < 180) {
			my @behind;
			foreach my $id (keys %{$char->{party}{users}}) {
				my $u = $char->{party}{users}{$id};
				next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name});
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
				main::ai_route($t, undef, undef);
				psay(pick("done shopping, meet me outside", "coming out, wait for me outside"), 1, 60);
				return;
			}
		}
		# after shopping everyone meets at ONE point, the town's Kafra (owner 2026-11-14: Butterfly to town, shop, meet at
		# the Kafra, return together behind her): she walks there once and says so once
		if (time - $errandEnd < 600 && $KAFRA{$here} && time - $meetRouteAt > 10 && !AI::inQueue('route', 'move', 'NPC', 'attack')) {
			my ($k) = sort { blockDistance($char->{pos_to}, {x => $a->[0], y => $a->[1]}) <=> blockDistance($char->{pos_to}, {x => $b->[0], y => $b->[1]}) } @{$KAFRA{$here}};
			if (blockDistance($char->{pos_to}, {x => $k->[0], y => $k->[1]}) > 5) {
				$meetRouteAt = time;
				Commands::run('stand') if $char->{sitting};
				main::ai_route($here, $k->[0], $k->[1], distFromGoal => 3, maxRouteTime => 20);
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
		if (time - $errandEnd > 600) {
			# passing through town: no shopping wait, but she doesn't run ahead either - with a friend more than 20 cells
			# behind (or still on the previous map) she stops until it's close again (3 min max per stop). QA: she
			# crossed Payon 100 cells ahead of the party.
			my $far = '';
			foreach my $id (keys %{$char->{party}{users}}) {
				my $u = $char->{party}{users}{$id};
				next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name});
				my $m = friend_map($u); $m =~ s/\.gat$//;
				if ($m ne $here) { $far = $u->{name}; last }
				next if party_strong(); # a strong party: same town map is enough, they follow her out
				my ($pos) = friend_pos($u, $here);
				if ($pos && blockDistance($char->{pos_to}, $pos) > ($townWaitAt ? RESUME : TOGETHER)) { $far = $u->{name}; last }
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
			next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name});
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
		next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name});
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
				main::ai_route($here, $tpos->{x}, $tpos->{y}, distFromGoal => 2, attackOnRoute => 1);
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
				next if !$u || !$u->{online} || $u->{name} eq $char->{name} || on_errand($u->{name});
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
					main::ai_route($fetchMap, undef, undef, attackOnRoute => 1);
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

sub unload {
	if (defined $trivSaved) { $config{attackAuto} = $trivSaved; $trivSaved = undef }
	Plugins::delHooks($hooks);
	$config{route_randomWalk} = $restRW if defined $restRW && !$holding; # don't leave a rest's random-walk switch-off behind
	$config{itemsMaxWeight_sellOrStore} = $sellTrigSaved if $sellTrigUntil; # don't leave the raised trigger behind
	if (%travelSaved) { $config{$_} = $travelSaved{$_} foreach keys %travelSaved; %travelSaved = () } # travel mode's switches back
	Commands::unregister($cmds) if $cmds;
	$rally = undef;
	$depart = undef;
	gear_run_end();
	restore();
}

1;
