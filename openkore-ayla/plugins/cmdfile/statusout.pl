# statusout - every ~3 s, write Ayla's own stats to logs/status.json so Legion's dashboard can show the party
# leader's HP/SP/zeny/weight (owner: "why does the party dashboard not display the leader's stats?"). Legion has
# no other source for a human-played character's own stats. Read-only: it never sends commands or touches the AI.
# The file is written to a temp name and renamed, so a reader never sees half a file. Legion mounts
# openkore-ayla/logs read-only and ignores the file when "ts" is older than 30 s.
# Weights are as OpenKore keeps them (the client's figure, i.e. server units / 10).
package statusout;

use strict;
use Plugins;
use Globals qw($char $field $net);
use Time::HiRes qw(time);
use Log;
use JSON::PP;

Plugins::register('statusout', 'write own stats to logs/status.json for the Legion dashboard', \&unload);
my $hook = Plugins::addHook('mainLoop_post', \&tick);
# dashboard extras (owner 2026-11-14 "since OpenKore crashed I have no way to know what Ayla is trying to do"): her
# intent (partylead.pl status_intent), the last [partylead]/[watchdog] decision, the last order (control/commands.txt via
# cmdfile.pl) and its result, exp %/h and kills/min over 10 min, deaths today, OpenKore uptime and restarts today.
# Everything runs inside eval: a failure here never reaches OpenKore. The in-memory state survives a plugin reload
# (our vars); deaths and restarts survive an OpenKore restart in logs/statusout_state.json.
our %sx; # last_decision, last_watchdog, last_order, samples, prev exp, kills
my $xhooks = Plugins::addHooks(['target_died', sub { eval { $sx{kills}++ } }], ['self_died', sub { eval { day_state(); $sx{st}{deaths}++; save_state() } }]);
my $loghook = Log::addHook(sub { eval { logline(@_) } });
my $last = 0;
my $file = 'logs/status.json';

sub num { my $v = shift; return (defined $v && $v =~ /^-?\d+(\.\d+)?$/) ? $v + 0 : 0 }

sub str {
	my $s = shift;
	$s = '' unless defined $s;
	$s =~ s/([\\"])/\\$1/g;
	$s =~ s/[\x00-\x1f]//g;
	return "\"$s\"";
}

# her active status effects from OpenKore's $char->{statuses} (Actor::setStatus: {time, tick ms}): EFST_* icons and
# the BODYSTATE_/HEALTHSTATE_ ailments (stun, poison, curse, silence...), with OpenKore's display name and the seconds
# left (-1 = no timer). Skill delays and map properties are left out.
sub effects {
	my @out;
	my $st = $char->{statuses};
	if (ref $st eq 'HASH') {
		for my $h (sort keys %$st) {
			next unless $h =~ /^(EFST_|BODYSTATE_|HEALTHSTATE_)/ && $h !~ /_DELAY$/;
			my $e = $st->{$h};
			my $left = -1;
			if (ref $e && $e->{tick} && $e->{time}) {
				$left = $e->{time} + $e->{tick} / 1000 - time;
				next if $left <= 0;
				$left = int($left * 10) / 10;
			}
			my $name = defined $Globals::statusName{$h} ? $Globals::statusName{$h} : '';
			push @out, '{"handle":' . str($h) . ',"name":' . str($name) . ',"left_s":' . $left . '}';
		}
	}
	return '[' . join(',', @out) . ']';
}

# ---- extras
my $stateFile = 'logs/statusout_state.json';
sub today { my @l = localtime; return sprintf('%04d-%02d-%02d', $l[5] + 1900, $l[4] + 1, $l[3]) }
sub load_state {
	return if ref $sx{st};
	$sx{st} = {};
	if (open(my $f, '<', $stateFile)) { local $/; my $raw = <$f>; close $f; my $d = eval { JSON::PP::decode_json($raw) }; $sx{st} = $d if ref $d eq 'HASH' }
}
sub day_state {
	load_state();
	my $d = today();
	%{$sx{st}} = (day => $d, deaths => 0, restarts => 0, start => $sx{st}{start}, prev_start => $sx{st}{prev_start}) if ($sx{st}{day} || '') ne $d;
}
sub save_state {
	my $tmp = "$stateFile.tmp";
	open(my $f, '>', $tmp) or return;
	print $f JSON::PP->new->canonical->encode($sx{st});
	close $f;
	rename($tmp, $stateFile);
}
# a new OpenKore process ($^T = its start time) since the last one recorded: a restart (a crash or a manual restart)
sub start_state {
	day_state();
	my $st = $sx{st};
	return if ($st->{start} || 0) == $^T;
	if ($st->{start}) { $st->{restarts}++; $st->{prev_start} = $st->{start} }
	$st->{start} = $^T;
	save_state();
}
sub clean { my $t = shift; $t = '' unless defined $t; $t =~ s/\e\[[0-9;?]*[a-zA-Z]//g; $t =~ s/^\s*\[\d\d:\d\d:\d\d\]\s*//; $t =~ s/\s+$//; return substr($t, 0, 300) }
# every console line: keep the last decision / watchdog event / order and the first reply to that order
sub logline {
	my ($type, $domain, $level, $verb, $msg) = @_;
	return unless defined $msg;
	my $t = clean($msg);
	return if $t eq '';
	my $now = time;
	if ($t =~ /\[cmdfile\]\s*(?:>\s*)?(.*)$/) {
		my $o = $1;
		$sx{last_order} = {text => $o, ts => int($now), result => ($o =~ /^BLOCKED/ ? 'blocked' : '')};
		return;
	}
	if ($t =~ /\[(partylead|watchdog)\]/) {
		$sx{last_decision} = {text => $t, ts => int($now)};
		$sx{last_watchdog} = {text => $t, ts => int($now)} if $1 eq 'watchdog';
	}
	my $o = $sx{last_order};
	if ($o && $o->{result} eq '' && $now - $o->{ts} < 20 && ($type =~ /^(error|warning)$/ || $t =~ /\[(partylead|watchdog|aylagear)\]/ || $t =~ /^(Syntax|Unknown|Error|Usage|You )/)) {
		$o->{result} = $t;
	}
}
# kills counted from her exp gains (the rule partylead.pl's drained check uses in plan_track: every kill she or the
# party makes shares exp with her; the target_died hook only sees some of her own). Checked every main loop (not only
# every 3 s), so two kills 3 s apart count twice. One kill = one rise of her base exp (or a base level-up); the job
# exp packet that follows is not counted again. At level 99 base exp stands still, so the job exp counts instead.
# No kill is counted in a town (same town list as partylead.pl is_townmap). $sx{xkills} is a running total since
# $sx{xsince} (survives a plugin reload; a new OpenKore process starts a new total - Legion's meter sees kills_since
# change and counts no jump).
sub xkill_check {
	my ($b, $lv, $j, $jl) = map { num($char->{$_}) } qw(exp lv exp_job lv_job);
	$sx{xsince} ||= int(time);
	my $p = $sx{xprev};
	$sx{xprev} = [$b, $lv, $j, $jl];
	return unless $p;
	my $gain = $lv > $p->[1] || ($lv == $p->[1] && $b > $p->[0]);
	$gain = 1 if !$gain && $lv >= 99 && $lv == $p->[1] && $b == $p->[0] && ($jl > $p->[3] || ($jl == $p->[3] && $j > $p->[2]));
	return unless $gain;
	my $m = ($field && eval { $field->baseName }) || '';
	my $town = defined &partylead::is_townmap ? partylead::is_townmap($m) : ($m =~ /^(prontera|payon|geffen|morocc|alberta|izlude|aldebaran|yuno|comodo|umbala)$|_in\d*$/);
	$sx{xkills}++ unless $town;
}
# exp/h and kills/min over the last 10 min (samples every ~3 s; a level-up adds the rest of the old level).
# kpm = kills from her exp gains (xkill_check); kpm_own = the target_died hook (her own killing blows it saw).
sub meter {
	my $now = time;
	my ($b, $bm, $j, $jm, $lv, $jl) = map { num($char->{$_}) } qw(exp exp_max exp_job exp_job_max lv lv_job);
	my $p = $sx{prev};
	if ($p) {
		my $gb = $lv == $p->[4] ? $b - $p->[0] : ($lv > $p->[4] ? ($p->[1] - $p->[0]) + $b : 0);
		my $gj = $jl == $p->[5] ? $j - $p->[2] : ($jl > $p->[5] ? ($p->[3] - $p->[2]) + $j : 0);
		$sx{gb} += $gb if $gb > 0;
		$sx{gj} += $gj if $gj > 0;
	}
	$sx{prev} = [$b, $bm, $j, $jm, $lv, $jl];
	@{$sx{samples}} = () if $sx{samples} && @{$sx{samples}} && !defined $sx{samples}[0][4]; # samples from before this version
	push @{$sx{samples}}, [$now, $sx{gb} || 0, $sx{gj} || 0, $sx{kills} || 0, $sx{xkills} || 0];
	shift @{$sx{samples}} while @{$sx{samples}} > 1 && $now - $sx{samples}[0][0] > 600;
	my $f = $sx{samples}[0];
	my $min = ($now - $f->[0]) / 60;
	my ($eh, $jh, $kpm, $own) = (0, 0, 0, 0);
	if ($min >= 1) {
		$eh = int((($sx{gb} || 0) - $f->[1]) / $min * 60);
		$jh = int((($sx{gj} || 0) - $f->[2]) / $min * 60);
		$own = int((($sx{kills} || 0) - $f->[3]) / $min * 100) / 100;
		$kpm = int((($sx{xkills} || 0) - $f->[4]) / $min * 100) / 100;
	}
	return join(',', '"base_exp_pct":' . ($bm ? int(1000 * $b / $bm) / 10 : 0), '"job_exp_pct":' . ($jm ? int(1000 * $j / $jm) / 10 : 0),
		'"exp_h":' . $eh, '"job_exp_h":' . $jh, '"kpm":' . $kpm, '"kpm_own":' . $own, '"meter_min":' . int($min * 10) / 10,
		'"kills":' . ($sx{xkills} || 0), '"kills_since":' . num($sx{xsince}), '"kills_src":"exp"');
}
sub ev { my $e = shift; return 'null' unless ref $e; return '{' . join(',', map { "\"$_\":" . ($_ eq 'ts' ? num($e->{$_}) : str($e->{$_})) } sort keys %$e) . '}' }
sub extras {
	day_state();
	my $st = $sx{st};
	my ($intent, $since) = defined &partylead::status_intent ? partylead::status_intent() : ('', 0);
	return join(',', meter(),
		'"intent":' . str($intent), '"intent_since":' . num($since),
		'"last_decision":' . ev($sx{last_decision}), '"last_watchdog":' . ev($sx{last_watchdog}), '"last_order":' . ev($sx{last_order}),
		'"deaths_today":' . num($st->{deaths}), '"ok_start":' . num($^T), '"restarts_today":' . num($st->{restarts}),
		'"prev_start":' . num($st->{prev_start}));
}

my $lastMap = '';
sub rally_json {
	my @r = defined &partylead::status_rally ? (eval { partylead::status_rally() }) : ();
	my $dep = (defined &partylead::status_departing && eval { partylead::status_departing() }) ? 'true' : 'false';
	return '"rally_town":' . str($r[0] // '') . ',"rally_x":' . num($r[1]) . ',"rally_y":' . num($r[2]) . ',"departing":' . $dep;
}
sub tick {
	return unless $char && $char->{name};
	# queue b1794721505e1m: during a map change OpenKore isn't in game yet and $field / the position are the old map's
	# - no write then (the reader drops a file older than 30 s); a new map is written at once, not after the 3 s step
	return unless $net && $net->getState() == Network::IN_GAME() && $field;
	eval { xkill_check() }; # every loop: the exp-gain kill count (before the 3 s write step)
	my $nowMap = eval { $field->baseName } || '';
	return if time - $last < 3 && $nowMap eq $lastMap;
	$last = time;
	$lastMap = $nowMap;
	my $ex = eval { extras() };
	$ex = '"extras_error":' . str(substr($@ || 'unknown', 0, 200)) unless defined $ex;
	my $online = ($net && $net->getState() == Network::IN_GAME()) ? 1 : 0;
	my ($x, $y) = (0, 0);
	my $pos = eval { $char->position };
	$pos = $char->{pos_to} unless ref $pos;
	($x, $y) = (num($pos->{x}), num($pos->{y})) if ref $pos;
	my $map = '';
	$map = eval { $field->baseName } || '' if $field;
	my $json = '{' . join(',',
		'"name":' . str($char->{name}),
		'"online":' . ($online ? 'true' : 'false'),
		'"hp":' . num($char->{hp}), '"max_hp":' . num($char->{hp_max}),
		'"sp":' . num($char->{sp}), '"max_sp":' . num($char->{sp_max}),
		'"zeny":' . num($char->{zeny}),
		'"weight":' . num($char->{weight}), '"max_weight":' . num($char->{weight_max}),
		'"base_level":' . num($char->{lv}), '"job_level":' . num($char->{lv_job}),
		'"map":' . str($map), '"x":' . $x, '"y":' . $y,
		'"lock_map":' . str(defined $partylead::huntMap && $partylead::huntMap ne '' ? $partylead::huntMap : ($Globals::config{lockMap} || '')),
		'"save_map":' . str($Globals::config{saveMap} || ''), # Legion: her friends save where she saved
		'"sitting":' . ($char->{sitting} ? 'true' : 'false'), # Legion together.go: friends hold while she rests
		'"guild":' . str(ref $char->{guild} ? $char->{guild}{name} : ''), # Legion humanguild.go: her guild (recruiting)
		'"job_id":' . num($char->{jobID}),
		'"dest":' . str(defined &partylead::status_dest ? (eval { partylead::status_dest() } || '') : ''), # Legion partydest.go (PT30)
		'"town_run":' . ((defined &partylead::status_town_run && eval { partylead::status_town_run() }) ? 'true' : 'false'),
		rally_json(), # Legion rallysync.go: her rally (rally_town / rally_x / rally_y) and departing
		'"effects":' . effects(), # Legion effects.go: her buffs/debuffs for the party dashboard
		$ex, # dashboard extras: intent, last decision / order, exp and kill meter, deaths, restarts
		'"ts":' . int(time)) . "}\n";
	my $tmp = "$file.tmp";
	open(my $out, '>', $tmp) or return;
	print $out $json;
	close $out;
	rename($tmp, $file);
}

sub unload { Plugins::delHook($hook); Plugins::delHooks($xhooks); Log::delHook($loghook) }
eval { start_state() }; # after every top-level 'my' above is set
1;
