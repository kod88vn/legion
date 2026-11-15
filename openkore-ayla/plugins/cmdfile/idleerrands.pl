# idleerrands - Ayla uses the party's Kafra gathering for errands and runs party town runs.
# EC36 (rewritten 2026-11-14): "A solo bot, or a party gathering at a Kafra before setting out, uses idle time for
# errands (sell, store, restock). A party member does errands only on the party town run (PT22/PT24) or at 50% weight
# (EC28); otherwise it farms near the party (PT18)." So:
#
# 1. Kafra-gathering errand. She stands waiting IN a town (never a field: no walking away from the party) within
#    KAFRA_NEAR cells of one of that town's Kafras (the party's gathering point: a rally, the meet after a town run)
#    for idleErrands_wait (45 s) - no fight, no loot, no NPC, no shop, within 8 cells of one spot - and her bag is at
#    idleErrands_weight (40%) or idleErrands_slots (85 entries) with something to sell / store, or a buyAuto stock
#    (potions, wings) is under half: she runs OpenKore's own sellAuto -> buyAuto -> storageAuto chain at THAT town's
#    Tool Dealer and Kafra, on foot inside the town. items_control.txt decides what goes (cards, gear, potions, wings,
#    gear-plan items are kept: sell 0). partylead.pl sees the chain like any town run (it announces it - the members
#    shop too, Legion rally.go townRunLineRe - and everyone meets at the Kafra afterwards).
# 2. 'idleerrands townrun [town]' (ayla.sh townrun): a party town run on order (PT22). In a town: that town, on foot.
#    Outside a town: her save town - partylead.pl does the one allowed wing (its PT22 party town-run Butterfly Wing,
#    let through by its no-teleport guard); this plugin never wings and never touches the teleport settings. After
#    shopping she waits at the Kafra (partylead); after idleErrands_meet (300 s) still in town she sets off for her
#    hunting map (partylead goto) and the members follow.
# 3. AY19 / EC28: waiting idleErrands_partyWait (90 s) away from towns while at least half of the online party (her
#    included, Legion's /api/human-party weights) is at idleErrands_partyWeight (50%): she calls the party town run
#    herself (as 2; one per 20 min).
#
# How the town is forced: partylead calls aylagear::run_start(<hunting map>) on every town run, which points the sell /
# buy / storage NPCs at the hunting map's town. While a job of this plugin runs, a thin wrapper passes the job's town
# instead; outside a job it calls the original unchanged. Zeny is never sent anywhere (G12): selling earns, buyAuto
# keeps its own zeny lines. Nothing here touches partylead.pl's state or settings.
package idleerrands;

use strict;
use Plugins;
use Globals qw($char $field %config $net);
use AI;
use Commands;
use Log qw(message);
use Utils qw(blockDistance);
use Time::HiRes qw(time);
use IO::Socket::INET;
use JSON::PP;

use constant KAFRA_NEAR => 15;

# towns with a Tool Dealer (aylagear.pl %TOOL) and the shop buildings that belong to them
our %TOOLTOWN = map { $_ => 1 } qw(prontera payon geffen morocc alberta aldebaran izlude yuno comodo umbala);
our %SHOPTOWN = (alberta_in => 'alberta', aldeba_in => 'aldebaran', prt_in => 'prontera', izlude_in => 'izlude',
	payon_in01 => 'payon', payon_in02 => 'payon', morocc_in => 'morocc', geffen_in => 'geffen', cmd_in01 => 'comodo',
	um_in => 'umbala', yuno_in01 => 'yuno');
# Kafra cells (partylead.pl %KAFRA)
our %KAFRA = (prontera => [[152,326],[151,29],[29,207],[282,200],[146,89]], payon => [[181,104],[175,226]],
	geffen => [[120,62],[203,123]], morocc => [[156,97]], alberta => [[28,229],[113,60]], aldebaran => [[143,119]],
	yuno => [[152,187],[327,108],[277,221]], comodo => [[195,150]], umbala => [[87,160]], izlude => [[134,88]]);

Plugins::register('idleerrands', 'Ayla sells / stores / restocks at the Kafra gathering; party town runs', \&unload);
my $hooks = Plugins::addHooks(['AI_pre', \&tick]);
my $cmds = Commands::register(['idleerrands', 'idle errands: status | townrun [town] | on | off', \&cmd]);

our $job;                  # {kind => idle|townrun|auto, town, at, phase => shop|meet, doneAt, why}
our ($anchor, $idleSince) = (undef, 0); # where she stands still and since when
our $nextAt = time + 60;   # no Kafra errand before this (cooldown)
our $nextAuto = time + 300;# no self-called town run before this
my ($last, $apiAt, %apiW) = (0, 0);
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

# standing at a Kafra of town map $m (the party's gathering point)? only the town map itself counts
sub at_kafra {
	my ($m, $pos) = @_;
	foreach my $k (@{$KAFRA{$m} || []}) {
		return 1 if blockDistance({x => $k->[0], y => $k->[1]}, $pos) <= KAFRA_NEAR;
	}
	return 0;
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

# the town of a party town run: in a town that town (on foot); outside one only the save town (partylead's wing).
# returns (town, note)
sub townrun_town {
	my ($here, $want, $save) = @_;
	my $t = shop_town_of($here);
	my $base = $t ne '' ? $t : ($save || '');
	return ($base, '') if !$want || $want eq $base;
	return ($base, "$want is not this town / the save town: no walking off, the run goes to $base");
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

sub busy {
	return 1 if AI::inQueue('attack', 'take', 'items_take', 'items_gather', 'NPC', 'sellAuto', 'buyAuto', 'storageAuto',
		'teleport', 'skill_use', 'deal', 'dead'); # route / move: partylead's few-cell steps; the 8-cell spot covers walks
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
sub start_job {
	my ($kind, $town, $why) = @_;
	return 0 unless town_ok($town);
	return 0 unless install_wrap(); # without the wrapper the run would go to the hunting town: no errand
	my $here = $field->baseName;
	$job = {kind => $kind, town => $town, at => time, phase => 'shop', why => $why};
	Commands::run('stand') if $char->{sitting};
	AI::clear('move', 'route', 'mapRoute', 'items_take', 'take', 'items_gather', 'sitAuto', 'sitting');
	my $bs = bag_state();
	my $first = $kind ne 'idle' || $bs->{sellable} ? 'sellAuto' : $bs->{storable} ? 'storageAuto' : 'buyAuto';
	AI::queue($first); # OpenKore chains sell -> buy -> storage itself; partylead announces it as the party town run
	log_m("$kind: $why -> $first at $town (from $here" . (shop_town_of($here) eq $town ? ', on foot' : ', partylead\'s town-run wing') . ")");
	return 1;
}

sub finish {
	my $why = shift;
	return unless $job;
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
			if ($job->{kind} eq 'idle') { finish('done, back at the kafra with the party'); return }
			($job->{phase}, $job->{doneAt}) = ('meet', time);
			log_m("$job->{kind}: shopping done, waiting at the $job->{town} kafra for the party");
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
	# idleErrands_meet seconds: she sets off for the hunting map, the members follow (her own decision, a goto).
	if (shop_town_of($field->baseName) ne $job->{town}) { finish('left town with the party'); return }
	if (time - $job->{doneAt} > cv('idleErrands_meet', 300)) {
		my $hm = $partylead::homeMap || $partylead::huntMap || '';
		if ($hm ne '' && defined &partylead::cmd_partylead) {
			log_m("$job->{kind}: waited " . int(time - $job->{doneAt}) . " s at the kafra, heading back to $hm");
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
	# 1. the party gathers at a Kafra in this town: errand in this town
	if ($TOOLTOWN{$here}) {
		return unless time >= $nextAt && at_kafra($here, $pos);
		my $why = need_reason(bag_state(), thresholds());
		start_job('idle', $here, "waited " . int($idle) . " s at the kafra, $why") or ($nextAt = time + 120) if $why ne '';
		return;
	}
	return if shop_town_of($here) ne ''; # inside a shop building: partylead walks her out
	# 3. AY19 / EC28: the whole party waits far from town with heavy bags: her own party town run (save town)
	if ($char->{party} && $idle >= cv('idleErrands_partyWait', 90) && time >= $nextAuto) {
		my $st = $config{saveMap} || '';
		my $line = cv('idleErrands_partyWeight', 50) / 100;
		my @w = (api_weights(), ($char->{weight_max} ? $char->{weight} / $char->{weight_max} : undef));
		if (town_ok($st) && party_heavy(\@w, $line)) {
			$nextAuto = time + 1200;
			start_job('auto', $st, sprintf('party waiting %d s with heavy bags', $idle));
		}
	}
}

sub cmd {
	my (undef, $args) = @_;
	$args ||= '';
	if ($args =~ /^\s*townrun\b\s*([a-z]*)/i) {
		if ($job) { log_m("busy: a $job->{kind} job in $job->{town} runs"); return }
		my ($t, $note) = townrun_town($field ? $field->baseName : '', lc($1 || ''), $config{saveMap} || '');
		log_m("townrun: $note") if $note;
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
