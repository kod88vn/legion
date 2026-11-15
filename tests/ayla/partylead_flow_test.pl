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

package AI; our (@Q, $CLEARS); sub inQueue { my %q = map { $_ => 1 } @Q; return scalar grep { $q{$_} } @_ } sub clear { $CLEARS++ }
package main;
package partylead;
use constant DENSITY_PER_MEMBER => 20;
our (%config, %droppedUntil, %crowdedAt, $gotoMap, $homeMap, $homeAt, $homeBy, $fitAt, $lmSet, $saved, $char, $field);
our (%waitEndSaid, %gatherDist, $gatherMoved, @FRIENDS, %DIST, $wdLast, $monstersList, %pgCross, $pgPrev, $pgFrom, $pgAt, $errand, $pgLog, $arrInfo, $portalHoldUntil, $portalHoldMap, %MAP_SPAWNS_STOCK, $MAP_RATE, $MAP_DENSITY_LOADED);
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
package MonStub; sub new { my $c = shift; bless {l => [@_]}, $c } sub getItems { $_[0]{l} }
package FieldStub; sub new { bless {name => $_[1]}, $_[0] } sub baseName { $_[0]{name} }
package main;

my ($pass, $fail) = (0, 0);
sub ok($$) { my ($c, $m) = @_; if ($c) { $pass++ } else { $fail++; print "FAIL: $m\n" } }

# a sub missing from the plugin is a failure here (the fix is not in); the others are extracted from the source
for my $sub (qw(wait_end_log_due rally_away gather_closing nearest_attacker portal_back_guard drop_map
	load_map_density map_spawns map_dense_enough)) {
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

# ---- b1794737139520-016 follow-up: the monster that hits her is the one she fights back
{
	my $me = {name => 'Ayla', pos_to => {x => 10, y => 10}};
	my @mons = ({name => 'Zombie far', pos_to => {x => 20, y => 10}, dmgToYou => 1},
		{name => 'Zombie idle', pos_to => {x => 11, y => 10}},
		{name => 'Zombie near', pos_to => {x => 12, y => 10}, missedYou => 1},
		{name => 'Zombie dead', pos_to => {x => 10, y => 11}, dmgToYou => 1, dead => 1});
	$partylead::char = $me;
	$partylead::monstersList = MonStub->new(@mons);
	my $t = partylead::nearest_attacker();
	ok($t && $t->{name} eq 'Zombie near', 'nearest_attacker: the nearest monster that hit or missed her (not idle, not dead)');
	$partylead::monstersList = MonStub->new(@mons[1, 3]);
	ok(!defined partylead::nearest_attacker(), 'nearest_attacker: none when nothing attacks her');
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

print "partylead_flow_test: $pass passed, $fail failed\n";
exit($fail ? 1 : 0);
