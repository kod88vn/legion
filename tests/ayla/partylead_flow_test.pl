#!/usr/bin/perl
# Offline test of partylead.pl's party-flow fixes (2026-11-15, package P2 ayla-partylead-flow):
#   b1794751170937-396  the "wait on X ended" line is logged once per member per map, not every tick
#   b1794751481844-856  rally_away: the members the rally walk must wait for (not on her map, not in the town)
#   b1794749928950-584  gather_closing: a hold is not idle while a friend on her map comes closer
#   b1794737139520-016  nearest_attacker: on a crossing map she still fights back the monster that hits her
#   b1794736237191-004  portal_back_guard: an auto-buy / auto-sell / storage task may cross the portal back
#   b1794735590gts      map_dense_enough: a map with too few spawns for the party is skipped (control/mapdensity.txt)
#   b1794731370342-616  drop_map: the map of a near-death wing is dropped for 2 h and is no way back
# Loads only the named subs - no OpenKore, nothing sent to Ayla.
# usage: perl tests/ayla/partylead_flow_test.pl [path/to/partylead.pl]
use strict;
use warnings;
no warnings 'once';
my $file = shift || 'openkore-ayla/plugins/cmdfile/partylead.pl';
open(my $fh, '<', $file) or die "$file: $!";
my $src = do { local $/; <$fh> };

package AI; our (@Q, $CLEARS, $AGGR); sub inQueue { my %q = map { $_ => 1 } @Q; return scalar grep { $q{$_} } @_ } sub clear { $CLEARS++ }
sub findAction { return (@Q && $Q[0] eq 'attack') ? 'attack' : undef } sub args { return {ID => $main::ATTACK_ID} }
sub ai_getAggressives { return $AGGR || 0 } sub action { '' }
sub is { 0 } sub queue { 0 } sub AUTOLOAD { return 0 } sub DESTROY { }
package main;
package partylead;
use constant DENSITY_PER_MEMBER => 20;
use constant ATTACK_RECENT => 5;
use constant ATTACK_RANGE => 12;
use constant FB_REACH => 2;
use constant FB_STALL => 8;
use constant FB_DROP => 20;
our (%attackSeen, %attackAt, %fbEng, %fbDropped, $atkSeq, $atkDoneSeq, $fbMsgAt, $stickWingAt, $stickFlee);
use constant STICK_DEADLY => 0.20;
our (%config, %droppedUntil, %crowdedAt, $gotoMap, $homeMap, $homeAt, $homeBy, $fitAt, $lmSet, $saved, $char, $field);
our (%waitEndSaid, %gatherDist, $gatherMoved, @FRIENDS, %DIST, $wdLast, $monstersList, %pgCross, $pgPrev, $pgFrom, $pgAt, $errand, $pgLog, $arrInfo, $portalHoldUntil, $portalHoldMap, %MAP_SPAWNS_STOCK, $MAP_RATE, $MAP_DENSITY_LOADED);
$partylead::lastMsg = 0; $partylead::fbMsgAt = 0; # partylead.pl's file-level my/our values are not run here (only sub bodies are eval'd)
sub seen_dist { return $DIST{$_[0]} }
sub wd_report { $wdLast = [@_] }
sub psay {}
sub message {}
sub blockDistance { my ($p,$q)=@_; my $dx=abs($p->{x}-$q->{x}); my $dy=abs($p->{y}-$q->{y}); $dx>$dy?$dx:$dy }
sub is_townmap { $_[0] =~ /^payon\z/ ? 1 : 0 }
sub is_dungeon { 0 }
sub cross_danger { 0 }
sub status_town_run { 0 }
sub pk_retreating { 0 }
sub route_maps { ('payon_in01', 'payon') }
sub online_friends { return @FRIENDS }
our (%CALLS, $BOSS, $STICKRET, $PACKRET);
sub stick_boss { $partylead::BOSS }
sub stick_tick { $partylead::CALLS{stick}++; return $partylead::STICKRET }
sub pack_tick { $partylead::CALLS{pack}++; return $partylead::PACKRET }
sub stick_on { 1 }
sub farm_sweep { 0 }
sub portal_guard { $partylead::CALLS{portal_guard}++; return $partylead::PORTALRET }
our (@pending, $ownSay, $PORTALRET, $AUTOLOAD);
sub AUTOLOAD { my $n = $AUTOLOAD; $n =~ s/.*:://; return if $n eq 'DESTROY'; $partylead::CALLS{$n}++; return 0 }
package NetStub; sub getState { 5 }
package MonStub; sub new { my $c = shift; bless {l => [@_]}, $c } sub getItems { $_[0]{l} }
package FieldStub; sub new { bless {name => $_[1]}, $_[0] } sub baseName { $_[0]{name} }
package main;
sub attack { push @main::ATT, $_[0] }
our (@ATT, @SENT, $ATTACK_ID);
package Commands; sub run { push @main::SENT, $_[0] }
package main;

my ($pass, $fail) = (0, 0);
sub ok($$) { my ($c, $m) = @_; if ($c) { $pass++ } else { $fail++; print "FAIL: $m\n" } }

# a sub missing from the plugin is a failure here (the fix is not in); the others are extracted from the source
for my $sub (qw(wait_end_log_due rally_away gather_closing attacker_sample current_attacker nearest_attacker fight_tick rally_gather_hold
	portal_back_guard drop_map load_map_density map_spawns map_dense_enough)) {
	my ($code) = $src =~ /(sub $sub \{.*?\n\}\n)/s;
	if (!$code) { ok(0, "$sub is missing from partylead.pl"); next }
	eval "package partylead;\n$code\n1" or die "$sub: $@";
}

# ---- b1794751170937-396: the ended-wait line once per member per map
{
	my @r = map { partylead::wait_end_log_due('Zendancer84', $_) } qw(pay_arche pay_arche pay_arche pay_dun00 pay_dun00);
	ok($r[0] == 1 && $r[1] == 0 && $r[2] == 0, 'ended-wait line logs once while the member stays on the same map');
	ok($r[3] == 1 && $r[4] == 0, 'ended-wait line logs again when the member changes map, then not again');
	ok(partylead::wait_end_log_due('Zen.fox', 'pay_dun00') == 1, 'another member is logged on its own');
}

# ---- b1794751481844-856: friends the rally walk waits for
{
	$partylead::field = FieldStub->new('pay_dun02');
	my $r = {town => 'payon'};
	@FRIENDS = (['Zendancer84', 'pay_dun01'], ['Zen.fox', 'pay_dun02'], ['Echo', 'payon'], ['Ghost', ''], ['Lost', 'pay_dun00']);
	my @away = map { $_->[0] } partylead::rally_away($r, 'pay_dun02');
	ok(join(',', @away) eq 'Zendancer84,Lost', "rally_away lists members on another map, not hers or the town (got @away)");
	@FRIENDS = (['Zen.fox', 'pay_dun02'], ['Echo', 'payon']);
	ok(scalar(partylead::rally_away($r, 'pay_dun02')) == 0, 'rally_away is empty when everyone is here or in the town');
}

# ---- b1794749928950-584: a hold is not idle while a friend on her map comes closer
{
	$partylead::field = FieldStub->new('pay_dun03');
	%partylead::gatherDist = (); $partylead::gatherMoved = 0;
	@FRIENDS = (['Zen.fox', 'pay_dun03']);
	%DIST = ('Zen.fox' => 30);
	ok(partylead::gather_closing() == 0, 'no movement yet: not converging');
	%DIST = ('Zen.fox' => 22);
	ok(partylead::gather_closing() == 1, 'friend came 8 cells closer: converging');
	%DIST = ('Zen.fox' => 22);
	ok(partylead::gather_closing() == 1, 'still converging within 30 s of the last step');
	$partylead::gatherMoved = time - 60;
	ok(partylead::gather_closing() == 0, 'no step for 60 s: not converging');
	@FRIENDS = (['Zen.fox', 'pay_dun02']);
	%DIST = ('Zen.fox' => 1);
	$partylead::gatherMoved = 0;
	ok(partylead::gather_closing() == 0, 'a friend on another map does not count');
}

# ---- small followups (review e736281): a ranged or unreachable attacker that deals no damage is dropped after FB_STALL s
{
	# fight_tick reads the stick, boss and friend state; none of it applies here
	$partylead::stickFlee = 0; $partylead::stickWingAt = 0; $partylead::BOSS = undef; $AI::AGGR = 0; @partylead::FRIENDS = ();
	$partylead::char = {name => 'Ayla', pos_to => {x => 10, y => 10}, hp => 100, hp_max => 100};
	my $fire = {ID => 41, name => 'Archer far', pos_to => {x => 18, y => 10}, dmgFromYou => 0};  # 8 cells, no damage
	my $melee = {ID => 42, name => 'Melee near', pos_to => {x => 11, y => 10}, dmgFromYou => 0}; # 1 cell, in reach
	my $hit = {ID => 43, name => 'Hit by me', pos_to => {x => 18, y => 11}, dmgFromYou => 0};   # 8 cells, takes damage
	$partylead::monstersList = MonStub->new($fire, $melee, $hit);
	%partylead::attackSeen = (); %partylead::attackAt = (); %partylead::fbEng = (); %partylead::fbDropped = ();
	$partylead::atkDoneSeq = -1; $partylead::atkSeq = 0; @main::ATT = ();
	$partylead::attackAt{41} = time; $partylead::attackAt{43} = time; # the melee one is not hitting her yet

	step();
	$partylead::attackAt{41} = time; $partylead::attackAt{43} = time;
	ok(partylead::fight_tick(0) == 1 && $main::ATT[-1] == 41, 'stall: the first tick attacks the far archer');
	# 9 s later with no damage dealt: the target is dropped and she goes on (fight_tick returns 0, no attack)
	$partylead::fbEng{41}{since} = time - 9; $partylead::fbEng{41}{last} = time - 1;
	my $before = scalar @main::ATT;
	ok(partylead::fight_tick(0) == 0 && scalar(@main::ATT) == $before, 'stall: 8 s with no damage, out of reach: the archer is dropped');
	ok(($partylead::fbDropped{41} || 0) > time + 15, 'stall: the drop lasts about 20 s');
	ok(!defined partylead::nearest_attacker() || partylead::nearest_attacker()->{ID} != 41, 'stall: a dropped target is not chosen');
	$partylead::attackAt{41} = time; $partylead::attackAt{43} = time;
	# after the 20 s the archer is a target again
	$partylead::fbDropped{41} = time - 1;
	ok(partylead::nearest_attacker()->{ID} == 41, 'stall: after the drop time the archer counts again');

	# a melee monster in reach is never dropped, however long it stands
	%partylead::fbEng = ();
	$partylead::monstersList = MonStub->new($melee);
	$partylead::attackAt{42} = time;
	partylead::fight_tick(0);
	$partylead::fbEng{42}{since} = time - 30; $partylead::fbEng{42}{last} = time - 1;
	ok(partylead::fight_tick(0) == 1 && !$partylead::fbDropped{42}, 'stall: a target in reach is fought, not dropped');

	# damage dealt restarts the clock: a target that takes damage is not dropped
	%partylead::fbEng = (); %partylead::fbDropped = ();
	$partylead::monstersList = MonStub->new($hit);
	$partylead::attackAt{43} = time;
	partylead::fight_tick(0);
	$partylead::fbEng{43}{since} = time - 9; $partylead::fbEng{43}{last} = time - 1;
	$hit->{dmgFromYou} = 12;
	ok(partylead::fight_tick(0) == 1 && !$partylead::fbDropped{43}, 'stall: damage dealt by her restarts the stall clock');
}
# ---- fightback 2026-11-15 (review 96e0953): sampling once per tick, recency, range, pruning
# a sample runs once per tick: step() bumps the tick counter the way tick does
sub step { $partylead::atkSeq++ }
{
	$partylead::char = {name => 'Ayla', pos_to => {x => 10, y => 10}};
	my $m1 = {ID => 1, name => 'Near idle', pos_to => {x => 11, y => 10}, dmgToYou => 0};
	my $m2 = {ID => 2, name => 'Ranged far', pos_to => {x => 18, y => 10}, missedYou => 0}; # 8 cells
	my $m3 = {ID => 3, name => 'Out of range', pos_to => {x => 25, y => 10}, dmgToYou => 0}; # 15 cells
	my $dead = {ID => 4, name => 'Dead', pos_to => {x => 10, y => 11}, dmgToYou => 0, dead => 1};
	$partylead::monstersList = MonStub->new($m1, $m2, $m3, $dead);
	%partylead::attackSeen = (); %partylead::attackAt = (); $partylead::atkDoneSeq = -1; $partylead::atkSeq = 0;
	step();
	ok(!defined partylead::nearest_attacker(), 'fightback: no target before any monster hits or misses her');
	step(); $m2->{dmgToYou} = 5; $m3->{missedYou} = 2;
	my $t = partylead::nearest_attacker();
	ok($t && $t->{name} eq 'Ranged far', 'fightback: a monster that just hit her (8 cells, ranged) is the target');
	ok(partylead::current_attacker($m2) == 1, 'fightback: a hit within 5 s is current at any distance');
	ok(partylead::current_attacker($m3) == 1, 'fightback: a 15-cell attacker is current (any distance)');
	$partylead::monstersList = MonStub->new($m3);
	step();
	ok(!defined partylead::nearest_attacker(), 'fightback: a new target must be within 12 cells');
	$partylead::monstersList = MonStub->new($m1, $m2, $m3, $dead);
	$partylead::attackAt{2} = time - 10;
	ok(partylead::current_attacker($m2) == 0, 'fightback: an old hit (10 s) is no longer current');
	ok(partylead::current_attacker($dead) == 0, 'fightback: a dead monster is never current');
}
{
	# once per tick: a second sample in the same tick changes nothing
	%partylead::attackSeen = (); %partylead::attackAt = (); $partylead::atkDoneSeq = -1; $partylead::atkSeq = 7;
	my $m = {ID => 21, name => 'Once', pos_to => {x => 11, y => 10}, dmgToYou => 3};
	$partylead::monstersList = MonStub->new($m);
	partylead::attacker_sample();
	$m->{dmgToYou} = 9;
	partylead::attacker_sample();
	ok($partylead::attackSeen{21} == 3, 'sampling: a second sample in the same tick is skipped');
	$partylead::atkSeq++;
	partylead::attacker_sample();
	ok($partylead::attackSeen{21} == 9 && defined $partylead::attackAt{21}, 'sampling: the next tick reads the rise as a hit');
}
{
	# a first sample with a total above 0 counts as a hit (review 96e0953)
	%partylead::attackSeen = (); %partylead::attackAt = (); $partylead::atkDoneSeq = -1; $partylead::atkSeq = 1;
	my $m5 = {ID => 5, name => 'Seen late', pos_to => {x => 11, y => 11}, dmgToYou => 7};
	$partylead::monstersList = MonStub->new($m5);
	partylead::attacker_sample();
	ok(defined $partylead::attackAt{5} && partylead::current_attacker($m5) == 1, 'sampling: a first sample with a total above 0 is a hit');
}
{
	# pruning: monsters gone from the list, and hits older than 30 s
	%partylead::attackSeen = (9 => 4); %partylead::attackAt = (9 => time, 8 => time - 40);
	$partylead::atkDoneSeq = -1; $partylead::atkSeq = 2;
	$partylead::monstersList = MonStub->new({ID => 1, pos_to => {x => 1, y => 1}, dmgToYou => 0});
	partylead::attacker_sample();
	ok(!exists $partylead::attackSeen{9} && !exists $partylead::attackAt{9}, 'pruning: a monster gone from the list is dropped');
	ok(!exists $partylead::attackAt{8}, 'pruning: a hit older than 30 s is dropped');
}

# ---- tick itself: the fight comes before a pack wait, a stick wait and the retreat rules (review 96e0953)
# tick is extracted and run through its early part; the stick / pack / boss / wing rules are stubs that record calls
{
	my ($code) = $src =~ /(sub tick \{.*?\n\}\n)/s;
	if (!$code) { ok(0, 'tick is missing from partylead.pl') }
	else { eval "no strict; package partylead;\n$code\n1" or die "tick: $@" }
	my $mon = {ID => 30, name => 'Zombie', pos_to => {x => 12, y => 10}, dmgToYou => 0};
	my $tick_run = sub { $partylead::last = 0; $partylead::atkSeq++; partylead::tick() };
	my $reset = sub {
		%partylead::attackSeen = (); %partylead::attackAt = (); $partylead::atkDoneSeq = -1; $partylead::fbMsgAt = 0;
		@AI::Q = (); $AI::AGGR = 0; $partylead::BOSS = undef; $partylead::STICKRET = 0; $partylead::PACKRET = 1;
		$mon->{dmgToYou} = 0; $mon->{missedYou} = 0;
		%partylead::CALLS = (); @main::ATT = (); $partylead::stickWingAt = 0; $partylead::stickFlee = undef;
		@FRIENDS = (['Zen.fox', 'pay_dun03']);
		$partylead::char = {name => 'Ayla', pos_to => {x => 10, y => 10}, hp => 90, hp_max => 100, dead => 0,
			party => {users => {1 => {name => 'Zen.fox', online => 1}}}};
		$partylead::net = bless({}, 'NetStub');
		$partylead::field = FieldStub->new('pay_dun03');
		$partylead::monstersList = MonStub->new();
	};
	my $hit = sub { my $m = shift; $m->{dmgToYou} = ($m->{dmgToYou} || 0) + 2 };
	# baseline tick (no hit yet), then the record is cleared and one hit lands before the tick that counts
	my $attack_run = sub { my $m = shift; $tick_run->(); %partylead::CALLS = (); @main::ATT = (); $hit->($m); $tick_run->() };
	# (a) a pack wait with an attacker: she attacks, the pack rule is not reached
	$reset->(); $partylead::monstersList = MonStub->new($mon); $partylead::PACKRET = 1;
	$attack_run->($mon);
	ok(scalar(@main::ATT) == 1 && $main::ATT[0] == 30, 'tick: with an attacker, a pack wait does not stop her fighting back');
	ok(!$partylead::CALLS{pack}, 'tick: the pack wait is not reached while she is attacked');
	# (b) the same pack wait with nobody attacking: the wait holds, nothing is attacked
	$reset->(); $partylead::monstersList = MonStub->new($mon); $partylead::PACKRET = 1;
	$tick_run->();
	ok(!@main::ATT && ($partylead::CALLS{pack} || 0) == 1, 'tick: no attacker, the pack wait holds as before');
	# (c) 3 on her with friends online: the retreat (stick_tick) goes first, no attack
	$reset->(); $partylead::monstersList = MonStub->new($mon); $AI::AGGR = 3; $partylead::STICKRET = 1;
	$attack_run->($mon);
	ok(!@main::ATT && ($partylead::CALLS{stick} || 0) >= 1, 'tick: with 3 on her and friends online, the retreat (stick_tick) goes first');
	# (d) the retreat does nothing: she fights the attacker in that same tick, before the pack rule
	$reset->(); $partylead::monstersList = MonStub->new($mon); $AI::AGGR = 3; $partylead::STICKRET = 0; $partylead::PACKRET = 1;
	$attack_run->($mon);
	ok(scalar(@main::ATT) == 1 && $main::ATT[0] == 30 && !$partylead::CALLS{pack}, 'tick: when the retreat does nothing, the attacker is fought');
	# (e) a boss in sight: the deliberate pull-back keeps priority
	$reset->(); $partylead::monstersList = MonStub->new($mon); $partylead::BOSS = {name => 'Boss'};
	$attack_run->($mon);
	ok(!@main::ATT, 'tick: a boss in sight keeps the pull-back, no fight');
	# (f) the PT31 Butterfly Wing just used keeps priority
	$reset->(); $partylead::monstersList = MonStub->new($mon); $partylead::stickWingAt = time;
	$attack_run->($mon);
	ok(!@main::ATT, 'tick: the PT31 wing keeps priority');
	# (g) nearly dead: the wing decision (stick_tick) runs, no fight
	$reset->(); $partylead::monstersList = MonStub->new($mon); $partylead::char->{hp} = 10;
	$attack_run->($mon);
	ok(!@main::ATT && ($partylead::CALLS{stick} || 0) >= 1, 'tick: under 20% HP the stick / wing rule decides, no fight');
	# (h) a fleeing commitment is not broken by a fight
	$reset->(); $partylead::monstersList = MonStub->new($mon); $partylead::stickFlee = {map => 'pay_dun01', from => 'pay_dun03', at => time};
	$attack_run->($mon);
	ok(!@main::ATT, 'tick: a PT31 fall-back in progress is not interrupted');
	# (i) already attacking this very monster: kept, not re-issued
	$reset->(); $partylead::monstersList = MonStub->new($mon); @AI::Q = ('attack'); $main::ATTACK_ID = 30;
	$attack_run->($mon);
	ok(!@main::ATT, 'tick: an attack already on her attacker is kept, not re-issued');
}

# ---- rally gather wait: per absence (goneAt and gatherAt reset once everyone is back)
{
	$partylead::field = FieldStub->new('pay_dun02');
	my $r = {town => 'prontera'};
	@FRIENDS = (['Zendancer84', 'pay_dun01']);
	ok(partylead::rally_gather_hold($r, 'pay_dun02') == 1, 'gather: holds for a member on another map');
	$r->{gatherAt} = time - 100;
	ok(partylead::rally_gather_hold($r, 'pay_dun02') == 0 && $r->{goneAt}, 'gather: after 90 s she goes on without it');
	@FRIENDS = (['Zendancer84', 'pay_dun02']);
	ok(partylead::rally_gather_hold($r, 'pay_dun02') == 0 && !$r->{goneAt}, 'gather: back on her map resets the wait');
	@FRIENDS = (['Zendancer84', 'pay_dun01']);
	ok(partylead::rally_gather_hold($r, 'pay_dun02') == 1, 'gather: a later absence gets its own 90 s');
	ok(partylead::rally_gather_hold({town => 'prontera'}, 'prontera') == 0, 'gather: no wait in the rally town');
}

# ---- b1794736237191-004: an auto-buy / sell / storage task may cross the portal back
{
	$partylead::field = FieldStub->new('payon_in01');
	$partylead::char = {name => 'Ayla', pos_to => {x => 5, y => 5}, hp => 100, hp_max => 100};
	%partylead::pgCross = (); $partylead::pgPrev = 'payon_in01'; $partylead::pgFrom = 'payon'; $partylead::pgAt = time - 5;
	$partylead::errand = 0; $partylead::pgLog = 0;
	@AI::Q = ();
	$AI::CLEARS = 0;
	partylead::portal_back_guard();
	ok($AI::CLEARS == 1, 'portal hold: a walk back to the map she came from is held (no task)');
	@AI::Q = ('buyAuto');
	$AI::CLEARS = 0;
	partylead::portal_back_guard();
	ok($AI::CLEARS == 0, 'portal hold: a buyAuto task may cross back (route not cleared)');
	@AI::Q = ('sellAuto');
	partylead::portal_back_guard();
	ok($AI::CLEARS == 0, 'portal hold: a sellAuto task may cross back');
	@AI::Q = ();
}

# ---- b1794735590gts: spawn density per party member (control/mapdensity.txt, read from openkore-ayla)
{
	chdir 'openkore-ayla' or die "no openkore-ayla dir";
	%partylead::MAP_SPAWNS_STOCK = (); $partylead::MAP_RATE = undef; $partylead::MAP_DENSITY_LOADED = 0;
	ok(partylead::map_spawns('pay_dun02') == 205 * 4, 'density: pay_dun02 is 205 stock spawns at mob_count_rate 400 = 820');
	ok(partylead::map_dense_enough('pay_dun02', 7) == 1, 'density: a 7-member party may farm pay_dun02');
	ok(!defined partylead::map_spawns('no_such_map_xyz'), 'density: an unknown map is unknown (no gate)');
	ok(partylead::map_dense_enough('no_such_map_xyz', 7) == 1, 'density: an unknown map is never skipped');
	$partylead::MAP_SPAWNS_STOCK{tiny_map} = 6; # 6 stock x 4 = 24 spawns
	ok(partylead::map_dense_enough('tiny_map', 1) == 1, 'density: 24 spawns is enough for one member');
	ok(partylead::map_dense_enough('tiny_map', 2) == 0, 'density: 24 spawns is too few for two members (needs 40)');
	chdir '..' or die;
}

# ---- b1794731370342-616: the near-death map is dropped for 2 h
{
	%partylead::droppedUntil = (); %partylead::crowdedAt = (); %partylead::config = (lockMap => 'pay_dun02');
	$partylead::gotoMap = 'pay_dun02'; $partylead::homeMap = 'pay_dun02'; $partylead::homeAt = 1; $partylead::homeBy = 'auto';
	$partylead::saved = {lockMap => 'pay_dun02'}; $partylead::fitAt = 5; $partylead::lmSet = 'pay_dun02';
	partylead::drop_map('pay_dun02', 'near-death wing');
	ok(($partylead::droppedUntil{pay_dun02} || 0) > time + 7000, 'the near-death map is off for about 2 h');
	ok($partylead::homeMap eq '' && $partylead::gotoMap eq '' && $partylead::config{lockMap} eq '', 'the dropped map is no longer her destination');
	ok(($partylead::saved->{lockMap} || '') eq '', 'the dropped map is no longer the saved lockMap');
}

# ---- tick_guarded (post-mortem 20261115 cause 3): the party lines, the town-run check, her map choice and the watchdog
# run every tick, whatever tick() returned early with. tick_guarded and tick are extracted; every other rule the wrapper
# or the tick body reaches is an AUTOLOAD stub that counts the call and returns 0; the rules under test are explicit.
{
	my ($code) = $src =~ /(sub tick_guarded \{.*?\n\}\n)/s;
	if (!$code) { ok(0, 'tick_guarded is missing from partylead.pl') }
	else { eval "no strict; package partylead;\n$code\n1" or die "tick_guarded: $@" }
	my ($fcode) = $src =~ /(sub flush_say \{.*?\n\}\n)/s;
	if (!$fcode) { ok(0, 'flush_say is missing from partylead.pl') }
	else { eval "no strict; package partylead;\n$fcode\n1" or die "flush_say: $@" }
	my $guard_run = sub { $partylead::last = 0; partylead::tick_guarded() };
	my $guard_reset = sub {
		%partylead::attackSeen = (); %partylead::attackAt = (); $partylead::atkDoneSeq = -1; $partylead::fbMsgAt = 0;
		@AI::Q = (); $AI::AGGR = 0; $partylead::BOSS = undef; $partylead::STICKRET = 0; $partylead::PACKRET = 0;
		$partylead::PORTALRET = 0; %partylead::CALLS = (); @main::SENT = (); @main::ATT = (); @partylead::pending = ();
		$partylead::stickWingAt = 0; $partylead::lootBlockUntil = 0; $partylead::pgFrom = ''; $partylead::lastPos = ''; $partylead::holdReleasedAt = 0; $partylead::stickFlee = undef; @FRIENDS = (['Zen.fox', 'pay_dun03']);
		$partylead::char = {name => 'Ayla', pos_to => {x => 10, y => 10}, hp => 90, hp_max => 100, dead => 0,
			party => {users => {1 => {name => 'Zen.fox', online => 1}}}};
		$partylead::net = bless({}, 'NetStub');
		$partylead::field = FieldStub->new('pay_dun03');
		$partylead::mon = {ID => 30, name => 'Zombie', pos_to => {x => 12, y => 10}, dmgToYou => 0, missedYou => 0};
		$partylead::monstersList = MonStub->new($partylead::mon);
	};
	# a party line queued now (due at once), as psay queues it
	my $queue_line = sub { push @partylead::pending, [time - 1, $_[0]] };

	# (1) a party line queued during a fight is sent, and the watchdog ran
	$guard_reset->();
	$guard_run->();                           # baseline sample, nobody hits her
	%partylead::CALLS = (); @main::SENT = ();
	$partylead::mon->{dmgToYou} = 2;          # a monster hits her: the fight
	$queue_line->('im on it, hold on');
	$guard_run->();
	ok(scalar(@main::ATT) == 1, 'tail: she is fighting the monster that hit her');
	ok(scalar(grep { $_ eq 'p im on it, hold on' } @main::SENT) == 1, 'tail: a party line queued during a fight is sent');
	ok(($partylead::CALLS{watchdog_tick} || 0) == 1, 'tail: the watchdog ran during the fight');
	ok(($partylead::CALLS{fit_tick} || 0) == 1 && ($partylead::CALLS{town_req_tick} || 0) == 1, 'tail: the map choice and the town-run check ran during the fight');

	# (2) a stick wait (the tick returns at the stick rule): the same tail runs
	$guard_reset->(); $partylead::STICKRET = 1;
	$queue_line->('regroup at the kafra');
	$guard_run->();
	ok(scalar(grep { $_ eq 'p regroup at the kafra' } @main::SENT) == 1, 'tail: a party line is sent during a stick wait');
	ok(($partylead::CALLS{watchdog_tick} || 0) == 1, 'tail: the watchdog ran during a stick wait');
	ok(!($partylead::CALLS{travel_mode_tick} || 0), 'tail: the tick body stopped at the stick rule');

	# (3) a pack wait with nobody attacking: the tail runs too
	$guard_reset->(); $partylead::PACKRET = 1;
	$queue_line->('waiting for zen');
	$guard_run->();
	ok(scalar(grep { $_ eq 'p waiting for zen' } @main::SENT) == 1 && ($partylead::CALLS{watchdog_tick} || 0) == 1, 'tail: a pack wait still sends lines and runs the watchdog');

	# (4) the portal_guard path: the tick body returns at the portal hold, the tail still runs, travel is not reached
	$guard_reset->(); $partylead::PORTALRET = 1;
	$queue_line->('portal hold, wait here');
	$guard_run->();
	ok(($partylead::CALLS{portal_guard} || 0) == 1, 'portal: the portal hold rule was reached');
	ok(!($partylead::CALLS{travel_mode_tick} || 0), 'portal: the tick body returned at the portal hold (travel not reached)');
	ok(scalar(grep { $_ eq 'p portal hold, wait here' } @main::SENT) == 1, 'portal: a party line is still sent when the portal hold returns');
	ok(($partylead::CALLS{watchdog_tick} || 0) == 1, 'portal: the watchdog still runs when the portal hold returns');

	# (5) nothing is sent twice: a line is sent once, and a dead step does not stop the others
	$guard_reset->(); $partylead::STICKRET = 1;
	$queue_line->('once');
	$guard_run->(); $guard_run->();
	ok(scalar(grep { $_ eq 'p once' } @main::SENT) == 1, 'tail: a queued line is sent once');
}

print "partylead_flow_test: $pass passed, $fail failed\n";
exit($fail ? 1 : 0);
