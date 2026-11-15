#!/usr/bin/perl
# Offline test of partylead.pl's party-flow fixes (2026-11-15, package P2 ayla-partylead-flow):
#   b1794751170937-396  the "wait on X ended" line is logged once per member per map, not every tick
#   b1794751481844-856  rally_away: the members the rally walk must wait for (not on her map, not in the town)
#   b1794749928950-584  gather_closing: a hold is not idle while a friend on her map comes closer
#   b1794737139520-016  transit_hp_ok: no fights on a crossing map at half HP or more
#   b1794731370342-616  drop_map: the map of a near-death wing is dropped for 2 h and is no way back
# Loads only the named subs - no OpenKore, nothing sent to Ayla.
# usage: perl tests/ayla/partylead_flow_test.pl [path/to/partylead.pl]
use strict;
use warnings;
no warnings 'once';
my $file = shift || 'openkore-ayla/plugins/cmdfile/partylead.pl';
open(my $fh, '<', $file) or die "$file: $!";
my $src = do { local $/; <$fh> };

package AI; sub inQueue { 0 }
package main;
package partylead;
our (%config, %droppedUntil, %crowdedAt, $gotoMap, $homeMap, $homeAt, $homeBy, $fitAt, $lmSet, $saved, $char, $field);
our (%waitEndSaid, %gatherDist, $gatherMoved, @FRIENDS, %DIST, $wdLast);
sub online_friends { return @FRIENDS }
sub seen_dist { return $DIST{$_[0]} }
sub wd_report { $wdLast = [@_] }
sub psay {}
sub message {}
package FieldStub; sub new { bless {name => $_[1]}, $_[0] } sub baseName { $_[0]{name} }
package main;

my ($pass, $fail) = (0, 0);
sub ok($$) { my ($c, $m) = @_; if ($c) { $pass++ } else { $fail++; print "FAIL: $m\n" } }

# a sub missing from the plugin is a failure here (the fix is not in); the others are extracted from the source
for my $sub (qw(wait_end_log_due rally_away gather_closing transit_hp_ok drop_map)) {
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

# ---- b1794737139520-016: no fights on a crossing map at half HP or more
{
	$partylead::char = {hp => 60, hp_max => 100};
	ok(partylead::transit_hp_ok() == 1, 'transit: 60% HP keeps the no-fight crossing');
	$partylead::char = {hp => 49, hp_max => 100};
	ok(partylead::transit_hp_ok() == 0, 'transit: under 50% HP she fights again');
	$partylead::char = {hp => 0, hp_max => 0};
	ok(partylead::transit_hp_ok() == 0, 'transit: no max HP reading is no crossing rule');
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
