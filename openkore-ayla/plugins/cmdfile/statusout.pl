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

Plugins::register('statusout', 'write own stats to logs/status.json for the Legion dashboard', \&unload);
my $hook = Plugins::addHook('mainLoop_post', \&tick);
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

sub tick {
	return if time - $last < 3;
	$last = time;
	return unless $char && $char->{name};
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
		'"effects":' . effects(), # Legion effects.go: her buffs/debuffs for the party dashboard
		'"ts":' . int(time)) . "}\n";
	my $tmp = "$file.tmp";
	open(my $out, '>', $tmp) or return;
	print $out $json;
	close $out;
	rename($tmp, $file);
}

sub unload { Plugins::delHook($hook) }
1;
