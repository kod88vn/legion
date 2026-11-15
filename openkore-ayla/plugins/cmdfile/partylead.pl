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
my $hooks = Plugins::addHooks(['AI_pre', \&tick], ['packet_partyMsg', \&heard], ['ai_processRandomWalk', \&short_walk], ['checkMonsterAutoAttack', \&area_target], ['npc_talk', \&npc_said]);
my $cmds = Commands::register(['partylead', 'Ayla party lead control: rally cancel | rally <town> | goto <map> | status', \&cmd_partylead]);

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
		if (!is_townmap($m) && $rgT ne '' && $KAFRA{$rgT} && !$allNear && ($config{saveMap} || '') eq $st) {
			# in town with the party scattered: regroup at this town's Kafra, then everyone walks out together (the plan's
			# "meet at the kafra" - QA 20:22: she left Payon via pay_fild07, the friends via pay_fild01, all lost)
			$rallyNext = 0;
			start_rally($hereT, undef, undef, "regroup at the $hereT kafra, then $m together", 1);
			if ($rally) { $rally->{thenGoto} = $m; $rally->{mind} = 1; $rally->{dest} = $m; message "[partylead] goto $m: regroup at the $hereT Kafra first\n", 'system' }
		} elsif (!is_townmap($m) && $KAFRA{$st} && ($config{saveMap} || '') ne $st) {
			$rallyNext = 0;
			start_rally($st, undef, undef, "save at the $st kafra before $m", 1);
			if ($rally) { $rally->{thenGoto} = $m; message "[partylead] goto $m: first a save stop at the $st Kafra (save point is " . ($config{saveMap} || '?') . ")\n", 'system' }
		}
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
		$apiPos{$m->{name}} = {map => $m->{map}, x => $m->{x}, y => $m->{y}, state => ($m->{state} || ''), lv => ($m->{level} || 0), saved => ($m->{saved_town} || ''), cls => ($m->{class} || ''), hpf => (($m->{hp} || '') =~ m{^(\d+)/(\d+)} && $2 ? $1 / $2 : undef), at => time};
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
	if ($how && $field && $field->baseName eq $town) {
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
	if ($all && $mineOk && (!@unsaved || time - $r->{allAt} > 120)) {
		my $next = ($saved && ($saved->{lockMap} || '') ne '') ? "ok going in, $saved->{lockMap}" : "good, wait here for the next stop";
		psay(@unsaved ? "everyone saved at $r->{town}? @unsaved save at the kafra pls. $next" : "everyone saved at $r->{town}? $next", 1, 0);
		end_rally("everyone is at the Kafra" . (@unsaved ? " (no 'saved' from @unsaved)" : ', all saved'), 3);
		return 0;
	}
	if ($all && $mineOk && @unsaved && time - ($r->{askSave} || 0) > 40) { $r->{askSave} = time; psay("@unsaved save at the kafra here pls, say when saved", 1, 0) }
	Commands::run('sit') if !$char->{sitting} && !AI::inQueue('sitting', 'route', 'move');
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
	return unless $field && $huntMap ne '' && $field->baseName eq $huntMap;
	my $m = $args->{monster} or return;
	return unless $m->{pos_to} && defined $m->{pos_to}{x};
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
	return if trivial_transit(); # crossing a map far below the party: the friends handle its monsters
	my $leaving = $rally || $gotoMap ne '' || $depart;
	my $cur = '';
	my $ai = AI::findAction('attack');
	if (defined $ai && $ai ne '') { my $a = AI::args($ai); $cur = $a->{ID} if $a }
	my ($best, $bd);
	foreach my $m (@{$monstersList->getItems() || []}) {
		next unless $m->{pos_to} && defined $m->{pos_to}{x} && !$m->{dead};
		next unless $m->{dmgToParty} || $m->{missedToParty} || $m->{castOnToParty};
		next if $leaving && !(($m->{dmgToYou} || $m->{missedYou}) && blockDistance($char->{pos_to}, $m->{pos_to}) <= 3);
		next if near_portal($field->baseName, $m->{pos_to}{x}, $m->{pos_to}{y});
		my $d = blockDistance($char->{pos_to}, $m->{pos_to});
		next if $d > 15;
		next if %travelSaved && $d > 5; # in transit only what is right at her / the friends next to her (no detours)
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
	# 58-99 with no healer online: passive / open fields (Juno, Mt. Mjolnir) a party clears without heals
	[80, 99, 'yuno_fild04', 0], [76, 92, 'yuno_fild07', 0], [72, 86, 'yuno_fild11', 0], [70, 84, 'yuno_fild08', 0],
	[62, 75, 'yuno_fild01', 0], [62, 75, 'yuno_fild12', 0], [58, 69, 'mjolnir_12', 0],
	# under 60 (pay_dun03: Sohee lv64 but mean monster level 44 - only to 65)
	[52, 65, 'pay_dun03', 0], [50, 61, 'prt_fild09', 0], [50, 61, 'pay_fild10', 0], [45, 59, 'pay_dun02', 1],
	[40, 51, 'moc_fild03', 0], [40, 49, 'gef_fild03', 1], [32, 47, 'pay_dun01', 1], [25, 39, 'pay_fild09', 0],
	[18, 31, 'pay_dun00', 1], [12, 24, 'prt_fild01', 0], [1, 11, 'prt_fild08', 0],
);
# highest level a map is worth (the transit check: a map 15+ levels under her is crossed, never farmed)
my %MAP_TOP = (pay_dun00 => 32, pay_dun01 => 48, pay_dun02 => 60, pay_dun03 => 70, pay_arche => 30, payon => 1,
	moc_fild01 => 34, moc_fild02 => 34, moc_fild03 => 55, mjolnir_01 => 35, mjolnir_02 => 42, mjolnir_06 => 36,
	mjolnir_07 => 68, mjolnir_08 => 66, mjolnir_09 => 22, mjolnir_10 => 58, mjo_dun01 => 36, mjo_dun02 => 50, mjo_dun03 => 56,
	orcsdun01 => 60, orcsdun02 => 66, gef_dun00 => 56, gef_dun01 => 72, glast_01 => 70);
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
	foreach my $f (online_friends()) { my $a = $apiPos{$f->[0]}; return 0 if $a && $a->{lv} && $a->{lv} < $top + 8 } # a low friend still needs her here
	my $dest = $gotoMap ne '' ? $gotoMap : ($config{lockMap} || '');
	$dest = $homeMap if $dest eq '' && !$rally;
	return ($dest ne '' && $dest ne $here) || $rally || $depart ? 1 : 0;
}
sub route_aor { return trivial_transit() ? 0 : 1 } # attackOnRoute for a map-to-map route

my %crowdedAt; # map -> when it was too crowded to farm (skipped for an hour)
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
# the party's farming level: her level, but at most 6 over the weakest online friend; a weak weapon costs 6 levels
sub party_fit_lv {
	my $lv = $char->{lv} || 1;
	my $min;
	foreach my $f (online_friends()) { my $a = $apiPos{$f->[0]}; next unless $a && $a->{lv}; $min = $a->{lv} if !defined $min || $a->{lv} < $min }
	my $plv = defined $min && $min + 6 < $lv ? $min + 6 : $lv;
	my $atk = ($char->{attack} || 0) + ($char->{attack_bonus} || 0);
	$plv -= 6 if $atk > 0 && $lv >= 30 && $atk < $lv * 2;
	return $plv < 1 ? 1 : $plv;
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
sub fit_tick {
	return if time - $fitAt < FIT_EVERY;
	$fitAt = time;
	return if mind_fresh();
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

# ---- zero-downtime watchdog (owner rule AY18, 2026-11-14: Ayla is never idle for any length of time) -------------------
# Audit 2026-11-14 (console log, 27.9 h): 38% of the time was in idle stretches of 60 s or more (waiting for the party
# 29% of those, char-select / login timeouts 27%, RODEX polling 19%, town / Kafra regroup 16%, routing 8%). Each of the
# plugin's own waits has its own timeout, but nothing ever asked "has she done anything useful lately?". This does:
# no kill, loot, errand, fight, exp gain or real route progress (a route to another map) for WD_IDLE seconds ->
# she takes the default productive action: go to the mind's map (homeMap / lockMap), else the map most of her party is
# on, else a level-fitting field, and farm there (the sweep: walk to monsters, fight). One map, one step; no mass action.
use constant WD_IDLE => 60;
my ($wdProdAt, $wdExp, $wdPos, $wdAct, $wdFired) = (time, 0, '', 0, 0);
sub wd_target {
	return $homeMap if mind_fresh();
	my ($tm) = party_turn();
	return $homeMap if $homeMap ne '' && $tm eq '' && map_fits($homeMap, party_fit_lv()) && !(time - ($crowdedAt{$homeMap} || 0) < 3600);
	my ($m) = choose_map();
	return $m if defined $m && $m ne '';
	return $homeMap ne '' ? $homeMap : 'prt_fild08';
}
sub watchdog_tick {
	my $now = time;
	my $exp = ($char->{exp} || 0) + ($char->{exp_job} || 0);
	my $pos = "$char->{pos_to}{x},$char->{pos_to}{y}";
	my $here = $field->baseName;
	my $busy = AI::inQueue('attack', 'take', 'items_take', 'items_gather', 'NPC', 'sellAuto', 'buyAuto', 'storageAuto', 'teleport', 'skill_use', 'deal')
		|| $char->{sitting} || own_rest() || $char->{dead} || $rally || (defined &aylagear::busy && aylagear::busy());
	# a real route (to another map, or a far cell) counts as progress while she is actually moving along it
	if (!$busy && AI::inQueue('route', 'move') && $pos ne $wdPos) {
		my $ri = AI::findAction('route');
		my $ra = (defined $ri && $ri ne '') ? AI::args($ri) : undef;
		my $rm = ($ra && ref $ra && $ra->{dest} && $ra->{dest}{map}) ? $ra->{dest}{map} : '';
		$rm = eval { $rm->baseName } || '' if ref $rm;
		$busy = 1 if $rm ne '' && $rm ne $here;
	}
	$busy = 1 if $exp != $wdExp && $wdExp; # she is gaining exp: she kills
	$wdExp = $exp; $wdPos = $pos;
	if ($busy) { $wdProdAt = $now; return }
	return if $now - $wdProdAt < WD_IDLE;
	return if $now - $wdAct < WD_IDLE; # one step per minute at most
	$wdAct = $now; $wdFired++;
	my $tgt = wd_target();
	message "[partylead] watchdog: no productive action for " . int($now - $wdProdAt) . " s (map $here, holding=$holding): default action = farm $tgt (#$wdFired)\n", 'system';
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
	# escape rules (main 2026-11-14): with the party's healer on her map she wings out only at HP < 45% with 4+ monsters
	# on her, or under 30% HP; alone at 6+ monsters or 35% HP; an MVP/boss in sight always (mon_control teleport flag)
	{
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
