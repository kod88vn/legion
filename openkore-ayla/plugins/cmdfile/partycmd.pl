# partycmd - Ayla commands her party (owner 2026-11-14: "Ayla also needs to command the party. She's been quiet the
# whole time"; AY04 a true party leader protects, guides, waits and talks; PT10). Short, natural commands at each
# decision, one line per decision, rate-limited and varied. Her friends (Legion partycmd.go) obey these words:
#   - she comes onto a new field and friends are not on it:   "all: follow me, im on <map> x,y"
#   - a friend is far (another map 45 s, 20+ cells 25 s):      "<name> come to me, i'm at <map> x,y"
#   - she is hurt and the priest is near:                      "<priest> heal me"
#   - she lacks Blessing / Increase AGI and the priest is near: "<priest> buffs pls"
#   - she sits down to rest with friends around:               "wait here, short rest"
# Lines go through partylead's psay queue (its gap keeps her from spamming). Party chat only; no moves, no orders to
# anyone else. conf partycmd 0 turns it off.
package partycmd;

use strict;
use Plugins;
use Globals qw($char $field %config $monstersList);
use AI;
use Commands;
use Log qw(message);
use Utils qw(blockDistance);
use Time::HiRes qw(time);

Plugins::register('partycmd', 'Ayla gives her party short commands', \&unload);
my $hooks = Plugins::addHooks(['AI_pre', \&tick_guarded]);

my $last = 0;
my ($mapNow, $mapSince) = ('', 0);
my (%farSince, %told, %kindAt);
my $sitSeen = 0;

sub unload { Plugins::delHooks($hooks) }

sub tick_guarded {
	eval { tick() };
	message "[partycmd] error: $@\n", 'system' if $@;
}

sub on { return !defined $config{partycmd} || $config{partycmd} ne '0' }
sub pick { $_[int rand @_] }
sub townmap { my $m = shift || ''; return $m =~ /^(prontera|payon|geffen|morocc|alberta|izlude|aldebaran|yuno|comodo|umbala)$|_in\d*$/ }

# one line per decision: a kind is said at most every $cool seconds, any command line at most every 20 s
my $lastLine = 0;
sub say_cmd {
	my ($kind, $cool, $text) = @_;
	return 0 if time - ($kindAt{$kind} || 0) < $cool || time - $lastLine < 20;
	($kindAt{$kind}, $lastLine) = (time, time);
	if (defined &partylead::psay) {
		partylead::psay($text, 1, 10);
	} else {
		Commands::run("p $text");
	}
	message "[partycmd] $kind: $text\n", 'system';
	return 1;
}

sub friends {
	return defined &partylead::online_friends ? partylead::online_friends() : ();
}

sub busy_errand { return AI::inQueue('sellAuto', 'buyAuto', 'storageAuto', 'NPC') ? 1 : 0 }

sub tick {
	return unless on() && $char && $field && $char->{party} && $char->{party}{users};
	return if time - $last < 2;
	$last = time;
	return if $char->{dead} || !$char->{pos_to} || !defined $char->{pos_to}{x};
	my $here = $field->baseName;
	my ($x, $y) = ($char->{pos_to}{x}, $char->{pos_to}{y});
	if ($here ne $mapNow) { ($mapNow, $mapSince) = ($here, time); %farSince = () }
	my @f = friends();
	return unless @f;
	my %users = map { ($_->{name} => $_) } grep { $_ && $_->{name} } values %{$char->{party}{users}};

	# 1) onto a new field with friends behind: tell everyone to follow
	if (!townmap($here) && time - $mapSince > 3 && time - $mapSince < 40) {
		my @behind = grep { $_->[1] ne '' && $_->[1] ne $here } @f;
		if (@behind && say_cmd("enter:$here", 300, pick("all: follow me, im on $here $x,$y", "follow me guys, im on $here $x,$y", "come to me at $here $x,$y, stay close", "all: follow me to $here $x,$y"))) {
			$told{$_->[0]} = time for @behind;
			return;
		}
	}

	# 2) her own needs: heal / buffs from the priest next to her
	my $priest = defined &partylead::healer_near ? partylead::healer_near($here) : '';
	if ($priest ne '' && $char->{hp_max} && $char->{hp} / $char->{hp_max} < 0.45) {
		return if say_cmd('heal', 30, pick("$priest heal me", "$priest heal pls", "$priest heal me, low"));
	}
	if ($priest ne '' && !AI::inQueue('attack') && !busy_errand() && $char->can('statusActive')) {
		my $bless = $char->statusActive('EFST_BLESSING,Blessing');
		my $agi = $char->statusActive('EFST_INC_AGI,Increase AGI');
		if (!$bless || !$agi) {
			return if say_cmd('buff', 300, pick("$priest buffs pls", "$priest buff me pls", "$priest bless + agi pls"));
		}
	}

	# 3) resting with friends around: hold here
	if ($char->{sitting} && !townmap($here)) {
		$sitSeen ||= time;
		if (time - $sitSeen > 3 && grep { $_->[1] eq $here } @f) {
			return if say_cmd('wait', 240, pick("wait here, short rest", "wait here a sec, resting", "hold here, sitting a bit"));
		}
	} else {
		$sitSeen = 0;
	}

	# 4) a friend far away: call it to her by name, with where she is
	return if busy_errand();
	foreach my $fr (@f) {
		my ($name, $m) = @$fr;
		next if $m eq '';
		my $far = 0;
		if ($m ne $here) {
			$far = 45;
		} elsif (defined &partylead::friend_pos && $users{$name}) {
			my ($pos) = partylead::friend_pos($users{$name}, $here);
			$far = 25 if $pos && defined $pos->{x} && blockDistance($char->{pos_to}, $pos) > 20;
		}
		if (!$far) { delete $farSince{$name}; next }
		$farSince{$name} ||= time;
		next if time - $farSince{$name} < $far || time - ($told{$name} || 0) < 150;
		if (say_cmd("come:$name", 150, pick("$name come to me, i'm at $here $x,$y", "$name come to me, im at $here $x,$y", "$name come to me pls, $here $x,$y", "$name come here, im at $here $x,$y"))) {
			$told{$name} = time;
			return;
		}
	}
}

1;
