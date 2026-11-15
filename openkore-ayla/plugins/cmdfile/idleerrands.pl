# idleerrands - Ayla uses waiting time for errands (EC36, owner 2026-11-14: "if they have nothing to do at least go sell
# their stuff. It's almost full. Stop doing nothing useful.") and runs party town runs (PT22) on order or on her own (AY19).
#
# 1. Idle errand. She stands waiting (partylead's hold, a rally wait at the Kafra, waiting for a member: no fight, no
#    shop, no NPC, no loot, within 8 cells of one spot) for idleErrands_wait (45 s) in a town with a Tool Dealer, or on
#    a map with a portal straight into one, and her bag is at idleErrands_weight (40%) or idleErrands_slots (85 entries)
#    with something to sell / store, or a buyAuto stock (potions, wings) is under half: she runs OpenKore's own
#    sellAuto -> buyAuto -> storageAuto chain at THAT town's Tool Dealer and Kafra, walking (no Butterfly Wing for a
#    short errand). items_control.txt decides what goes (cards, gear, potions, wings, gear-plan items are kept: sell 0).
#    partylead.pl sees the chain like any town run (announces it, Legion's members shop too, everyone meets at the town's
#    Kafra afterwards - its normal after-run regroup). A job that started on a field next to town is called off while
#    she is still walking (no NPC yet) once every online member stands on that field with her: the party is ready.
# 2. 'idleerrands townrun [town]' (ayla.sh townrun [town]): a party town run on order (PT22). The town defaults to the
#    town she is in, else her save town. A Butterfly Wing takes her when the town is her save point and she is not in
#    it, otherwise she walks. partylead announces "town run to <town> ... meet at the <town> kafra" (Legion's core parses
#    it, rally.go townRunLineRe, and the members wing / sell / restock / meet there). After shopping she waits at the
#    Kafra (partylead); after townrun_meet (300 s) still in town she sets off for her hunting map (partylead goto), and
#    the members follow.
# 3. AY19: waiting idleErrands_partyWait (90 s) away from towns while at least half of the online party (her included,
#    Legion's /api/human-party weights) is at idleErrands_partyWeight (45%): she calls the town run herself (to her save
#    town, by wing; one per 20 min).
#
# How the town is forced: partylead calls aylagear::run_start(<hunting map>) on every town run, which points the sell /
# buy / storage NPCs at the hunting map's town. While a job of this plugin runs, a thin wrapper passes the job's town
# instead (and nothing else changes); outside a job it calls the original unchanged. Config changes are in memory only
# (saveMap_warpToBuyOrSell, restored at the end). Zeny is never sent anywhere (G12): selling earns, buyAuto keeps its own
# zeny lines. Nothing here touches partylead.pl's state.
package idleerrands;

use strict;
use Plugins;
use Globals qw($char $field %config $net %portals_lut);
use AI;
use Commands;
use Log qw(message);
use Utils qw(blockDistance);
use Time::HiRes qw(time);
use IO::Socket::INET;
use JSON::PP;

# towns with a Tool Dealer (aylagear.pl %TOOL) and the shop buildings that belong to them
our %TOOLTOWN = map { $_ => 1 } qw(prontera payon geffen morocc alberta aldebaran izlude yuno comodo umbala);
our %SHOPTOWN = (alberta_in => 'alberta', aldeba_in => 'aldebaran', prt_in => 'prontera', izlude_in => 'izlude',
	payon_in01 => 'payon', payon_in02 => 'payon', morocc_in => 'morocc', geffen_in => 'geffen', cmd_in01 => 'comodo',
	um_in => 'umbala', yuno_in01 => 'yuno');

Plugins::register('idleerrands', 'Ayla sells / stores / restocks while she waits; party town runs', \&unload);
my $hooks = Plugins::addHooks(['AI_pre', \&tick]);
my $cmds = Commands::register(['idleerrands', 'idle errands: status | townrun [town] | on | off', \&cmd]);

our $job;                  # {kind => idle|townrun|auto, town, at, set => {k => v}, saved => {k => old}, anchor => {map,x,y}, field, phase, doneAt}
our ($anchor, $idleSince) = (undef, 0); # where she stands still and since when
our $nextAt = time + 60;   # no idle errand before this (cooldown)
our $nextAuto = time + 300;# no self-called town run before this
my ($last, $lastLog, $apiAt, %apiW) = (0, 0, 0);
my $origRunStart;          # aylagear::run_start before the wrapper

sub unload {
	Plugins::delHooks($hooks);
	Commands::unregister($cmds);
	finish('plugin unloaded') if $job;
	no warnings 'redefine';
	*aylagear::run_start = $origRunStart if $origRunStart && defined &aylagear::run_start && \&aylagear::run_start == \&wrapped_run_start;
}

sub cv { my ($k, $d) = @_; my $v = $config{$k}; return (defined $v && $v ne '') ? $v : $d }
sub log_m { message "[idleerrands] $_[0]\n", 'system' }

# ---------------------------------------------------------------- pure logic (tests: tools/dev/idleerrands_test.pl)
# the town a map belongs to for shopping: the town itself, its shop building's town, else ''
sub shop_town_of { my $m = shift || ''; return $m if $TOOLTOWN{$m}; return $SHOPTOWN{$m} || '' }

# a town reachable through one portal from map $m (portals_lut-shaped hash), else ''
sub adjacent_town {
	my ($m, $lut) = @_;
	foreach my $k (sort keys %$lut) {
		my $p = $lut->{$k};
		next unless ref $p eq 'HASH' && ref $p->{source} eq 'HASH' && ($p->{source}{map} || '') eq $m;
		foreach my $d (values %{$p->{dest} || {}}) {
			my $t = $TOOLTOWN{$d->{map} || ''} ? $d->{map} : '';
			return $t if $t;
		}
	}
	return '';
}

# why an errand is worth it, or '': $s = {wpct, slots, sellable, storable, low => [names], zeny}, $c = thresholds
sub need_reason {
	my ($s, $c) = @_;
	my $stuff = $s->{sellable} || $s->{storable};
	return sprintf('bag %d%%', $s->{wpct}) if $stuff && $s->{wpct} >= $c->{weight};
	return "bag $s->{slots} slots" if $stuff && $s->{slots} >= $c->{slots};
	return 'low on ' . join(', ', @{$s->{low}}) if @{$s->{low} || []} && ($s->{zeny} || 0) > 3000;
	return '';
}

# every online friend on map $m within $r cells of $pos? $friends = [{map, x, y, online}]
sub party_ready {
	my ($friends, $m, $pos, $r) = @_;
	foreach my $f (@$friends) {
		next unless $f->{online};
		return 0 if ($f->{map} || '') ne $m;
		return 0 if !defined $f->{x} || blockDistance({x => $f->{x}, y => $f->{y}}, $pos) > $r;
	}
	return 1;
}

# AY19: at least half of the weights (fractions 0..1) at the line, with at least 2 counted
sub party_heavy {
	my ($ws, $line) = @_;
	my @w = grep { defined } @$ws;
	return 0 if @w < 2;
	my $heavy = grep { $_ >= $line } @w;
	return $heavy * 2 >= @w ? 1 : 0;
}

# ---------------------------------------------------------------- game state
sub low_stock {
	my @low;
	for (my $n = 0; exists $config{"buyAuto_$n"}; $n++) {
		my $name = $config{"buyAuto_$n"} or next;
		next if $config{"buyAuto_${n}_disabled"} || $config{"buyAuto_${n}_gearSlot"} || $config{"buyAuto_${n}_gearRun"};
		next if $name =~ /^\d+$/; # an id slot set by aylagear for a gear piece
		my $max = $config{"buyAuto_${n}_maxAmount"} || 0;
		next unless $max > 0;
		my $have = eval { $char->inventory->sumByName($name) } || 0;
		push @low, lc($name) . 's' if $have * 2 < $max;
	}
	return @low;
}

sub bag_state {
	my $wpct = $char->{weight_max} ? int(100 * $char->{weight} / $char->{weight_max}) : 0;
	return {wpct => $wpct, slots => (eval { $char->inventory->size() } || 0),
		sellable => ($config{sellAuto} && eval { AI::ai_sellAutoCheck() }) ? 1 : 0,
		storable => ($config{storageAuto} && eval { AI::ai_storageAutoCheck() }) ? 1 : 0,
		low => [low_stock()], zeny => ($char->{zeny} || 0)};
}

sub thresholds { return {weight => cv('idleErrands_weight', 40), slots => cv('idleErrands_slots', 85)} }

sub friends {
	my @f;
	foreach my $u (values %{($char->{party} && $char->{party}{users}) || {}}) {
		next if !$u || $u->{name} eq $char->{name};
		my $m = $u->{map} || ''; $m =~ s/\.gat$//;
		push @f, {name => $u->{name}, online => $u->{online} ? 1 : 0, map => $m, x => ($u->{pos} ? $u->{pos}{x} : undef), y => ($u->{pos} ? $u->{pos}{y} : undef)};
	}
	return @f;
}

sub busy {
	return 1 if AI::inQueue('attack', 'take', 'items_take', 'items_gather', 'NPC', 'sellAuto', 'buyAuto', 'storageAuto',
		'teleport', 'skill_use', 'deal', 'dead'); # route / move: partylead's few-cell unstick steps; the 8-cell spot covers walks
	return 1 if defined &aylagear::busy && aylagear::busy();
	return 1 if $aylagear::run || $aylagear::mailjob || $aylagear::dealjob;
	return 0;
}

# a town partylead / aylagear can shop in (partylead::town_of_map must give the town back, or run_start goes elsewhere)
sub town_ok {
	my $t = shift || '';
	return 0 unless $TOOLTOWN{$t};
	return 1 unless defined &partylead::town_of_map;
	return partylead::town_of_map($t) eq $t ? 1 : 0;
}

# ---------------------------------------------------------------- the aylagear wrapper
sub wrapped_run_start {
	my ($hunt, $dry) = @_;
	$hunt = $job->{town} if $job && !$dry;
	return $origRunStart ? $origRunStart->($hunt, $dry) : undef;
}

sub install_wrap {
	return 0 unless defined &aylagear::run_start;
	my $cur = \&aylagear::run_start;
	return 1 if $cur == \&wrapped_run_start;
	$origRunStart = $cur; # first install, or aylagear.pl was reloaded since
	no warnings 'redefine';
	*aylagear::run_start = \&wrapped_run_start;
	return 1;
}

# ---------------------------------------------------------------- jobs
sub set_tmp { my ($k, $v) = @_; $job->{saved}{$k} = $config{$k} unless exists $job->{saved}{$k}; $config{$k} = $v; $job->{set}{$k} = $v }

sub start_job {
	my ($kind, $town, $why) = @_;
	return 0 unless town_ok($town);
	return 0 unless install_wrap(); # without the wrapper the run would go to the hunting town: no errand
	my $here = $field->baseName;
	my $inTown = shop_town_of($here) eq $town;
	$job = {kind => $kind, town => $town, at => time, set => {}, saved => {}, phase => 'shop', why => $why,
		anchor => {map => $here, x => $char->{pos_to}{x}, y => $char->{pos_to}{y}}, field => ($inTown ? 0 : 1)};
	# a Butterfly Wing only for a real town run to her save town from outside it; an errand next door is a walk
	my $wing = $kind ne 'idle' && !$inTown && ($config{saveMap} || '') eq $town && ($config{saveMap_warpToBuyOrSell} || 0) ? 1 : 0;
	set_tmp('saveMap_warpToBuyOrSell', $wing);
	Commands::run('stand') if $char->{sitting};
	AI::clear('move', 'route', 'mapRoute', 'items_take', 'take', 'items_gather', 'sitAuto', 'sitting');
	my $bs = bag_state();
	my $first = $kind ne 'idle' || $bs->{sellable} ? 'sellAuto' : $bs->{storable} ? 'storageAuto' : 'buyAuto';
	AI::queue($first); # OpenKore chains sell -> buy -> storage itself
	log_m("$kind: $why -> $first at $town (" . ($wing ? 'butterfly wing' : 'walking') . ", from $here)");
	return 1;
}

sub finish {
	my $why = shift;
	return unless $job;
	foreach my $k (keys %{$job->{saved}}) {
		next if ($config{$k} // '') ne ($job->{set}{$k} // ''); # changed meanwhile by someone else: keep it
		$config{$k} = $job->{saved}{$k};
	}
	log_m("$job->{kind} in $job->{town} over: $why");
	$job = undef;
	($anchor, $idleSince) = (undef, 0);
	$nextAt = time + 60 * cv('idleErrands_cooldown', 10);
}

sub job_tick {
	install_wrap();
	my $shopping = AI::inQueue('sellAuto', 'buyAuto', 'storageAuto');
	if ($job->{phase} eq 'shop') {
		if (!$shopping && time - $job->{at} > 3) {
			if ($job->{kind} eq 'idle') { finish('done, back to the party'); return }
			$job->{phase} = 'meet'; $job->{doneAt} = time;
			# the NPC settings go back now; the job stays for the Kafra wait
			foreach my $k (keys %{$job->{saved}}) { $config{$k} = $job->{saved}{$k} if ($config{$k} // '') eq ($job->{set}{$k} // '') }
			$job->{saved} = {};
			log_m("townrun: shopping done, waiting at the $job->{town} kafra for the party");
			return;
		}
		# an errand from a field next to town: the party gathered there while she is still on her way -> call it off
		if ($job->{kind} eq 'idle' && $job->{field} && !AI::inQueue('NPC') && $field->baseName ne $job->{town}
			&& shop_town_of($field->baseName) eq '' && party_ready([friends()], $job->{anchor}{map}, $job->{anchor}, 20)) {
			AI::clear('sellAuto', 'buyAuto', 'storageAuto', 'route', 'move', 'mapRoute');
			finish('party ready, errand called off');
			return;
		}
		my $cap = $job->{kind} eq 'idle' ? 240 : 600;
		if (time - $job->{at} > $cap && !AI::inQueue('NPC')) {
			AI::clear('sellAuto', 'buyAuto', 'storageAuto', 'route', 'move', 'mapRoute');
			finish("took over ${cap}s, called off");
		}
		return;
	}
	# meet: partylead holds her at the Kafra until everyone is there and then leaves with them. Still in town after
	# townrun_meet seconds: she sets off for the hunting map, the members follow (a mind-style goto, her own decision).
	my $here = $field->baseName;
	if (shop_town_of($here) ne $job->{town}) { finish('left town with the party'); return }
	if (time - $job->{doneAt} > cv('idleErrands_meet', 300)) {
		my $hm = $partylead::homeMap || $partylead::huntMap || '';
		if ($hm ne '' && defined &partylead::cmd_partylead) {
			log_m("townrun: waited " . int(time - $job->{doneAt}) . " s at the kafra, heading back to $hm");
			Commands::run("partylead goto $hm");
		}
		finish('meet time over');
	}
}

# AY19: Legion's weights for the party (fractions), cached 20 s
sub api_weights {
	if (time - $apiAt > 20) {
		$apiAt = time;
		my $sock = IO::Socket::INET->new(PeerAddr => '127.0.0.1', PeerPort => 8091, Proto => 'tcp', Timeout => 0.3);
		if ($sock) {
			$sock->timeout(0.5);
			print $sock "GET /api/human-party?human=$char->{name} HTTP/1.0\r\nHost: 127.0.0.1\r\n\r\n";
			my ($raw, $deadline) = ('', time + 0.6);
			while (time < $deadline) { my $n = sysread($sock, my $buf, 65536); last unless $n; $raw .= $buf }
			close $sock;
			my ($body) = $raw =~ /\r\n\r\n(.*)$/s;
			my $d = $body ? eval { JSON::PP::decode_json($body) } : undef;
			if ($d) {
				%apiW = ();
				for my $m (@{$d->{members} || []}) {
					next unless $m->{name} && $m->{online};
					$apiW{$m->{name}} = $2 ? $1 / $2 : undef if ($m->{weight} || '') =~ m{^(\d+)/(\d+)};
				}
			}
		}
	}
	return values %apiW;
}

sub tick {
	return if time - $last < 1;
	$last = time;
	return unless $net && $net->getState() == 5 && $char && $field && !$char->{dead};
	if ($job) { job_tick(); return }
	return unless cv('idleErrands', 1);
	# standing still?
	my $here = $field->baseName;
	my $pos = $char->{pos_to};
	if (!$anchor || $anchor->{map} ne $here || blockDistance($anchor, $pos) > 8 || busy()) {
		$anchor = {map => $here, x => $pos->{x}, y => $pos->{y}};
		$idleSince = time;
		return;
	}
	my $idle = time - $idleSince;
	return if $idle < cv('idleErrands_wait', 45);
	my $town = shop_town_of($here);
	$town = adjacent_town($here, \%portals_lut) if $town eq '' && cv('idleErrands_nearTown', 1);
	if ($town ne '' && time >= $nextAt) {
		my $why = need_reason(bag_state(), thresholds());
		if ($why ne '') { start_job('idle', $town, "waited " . int($idle) . " s, $why") or ($nextAt = time + 120); return }
		return;
	}
	# AY19: the whole party waits far from town with heavy bags: her own town run (save town, by wing)
	if ($town eq '' && $char->{party} && $idle >= cv('idleErrands_partyWait', 90) && time >= $nextAuto) {
		my $st = $config{saveMap} || '';
		my $line = cv('idleErrands_partyWeight', 45) / 100;
		my @w = (api_weights(), ($char->{weight_max} ? $char->{weight} / $char->{weight_max} : undef));
		if (town_ok($st) && party_heavy(\@w, $line) && (eval { $char->inventory->sumByName('Butterfly Wing') } || 0) > 0) {
			$nextAuto = time + 1200;
			start_job('auto', $st, sprintf('party waiting %d s with heavy bags', $idle));
		}
	}
}

sub cmd {
	my (undef, $args) = @_;
	$args ||= '';
	if ($args =~ /^\s*townrun\b\s*([a-z]*)/i) {
		my $t = lc($1 || '');
		if ($t eq '') {
			$t = shop_town_of($field ? $field->baseName : '');
			$t = $config{saveMap} || '' if $t eq '';
		}
		if ($job) { log_m("busy: a $job->{kind} job in $job->{town} runs"); return }
		if (!town_ok($t)) { log_m("townrun: no shop town '$t'"); return }
		start_job('townrun', $t, 'ordered') or log_m('townrun: could not start (aylagear.pl not loaded?)');
	} elsif ($args =~ /^\s*off/i) { $config{idleErrands} = 0; log_m('idle errands off (in memory)') }
	elsif ($args =~ /^\s*on/i) { $config{idleErrands} = 1; log_m('idle errands on') }
	else {
		my $bs = $char ? bag_state() : {};
		log_m(sprintf('on=%d job=%s idle=%ds next_in=%ds bag=%s%% slots=%s sell=%s store=%s low=%s wrap=%s',
			cv('idleErrands', 1), ($job ? "$job->{kind}/$job->{phase}/$job->{town}" : '-'), ($idleSince ? time - $idleSince : 0),
			($nextAt > time ? $nextAt - time : 0), $bs->{wpct} // '?', $bs->{slots} // '?', $bs->{sellable} // '?', $bs->{storable} // '?',
			join(',', @{$bs->{low} || []}) || '-', (defined &aylagear::run_start && \&aylagear::run_start == \&wrapped_run_start) ? 'on' : 'off'));
	}
}

1;
