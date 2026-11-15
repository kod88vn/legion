#!/usr/bin/perl
# Offline test of partylead.pl's rest hold (party_resting / rest_near, 2026-11-15): a friend's rest holds Ayla only when
# that friend is on her map within TOGETHER (20) cells, and every hold ends after the 3-minute cap.
# Loads only the own_rest / rest_near / party_resting subs - no OpenKore, nothing sent to Ayla.
# usage: perl tests/ayla/rest_test.pl [path/to/partylead.pl]
use strict;
use warnings;
no warnings 'once';
my $file = shift || 'openkore-ayla/plugins/cmdfile/partylead.pl';
open(my $fh, '<', $file) or die "$file: $!";
my $src = do { local $/; <$fh> };

# stand-ins for OpenKore: Chebyshev block distance, a field, a player list, the AI queue
package AI; sub inQueue { 0 }
package FieldStub; sub new { bless {name => $_[1]}, $_[0] } sub baseName { $_[0]{name} }
package PlayersStub; sub new { bless {p => {}}, $_[0] } sub getByName { $_[0]{p}{$_[1]} }
package partylead;
use constant TOGETHER => 20;
our (%apiPos, %restAnn, $char, $field, $playersList);
sub on_errand { 0 }
sub friend_dead { 0 }
sub blockDistance { my ($p, $q) = @_; my $dx = abs($p->{x} - $q->{x}); my $dy = abs($p->{y} - $q->{y}); $dx > $dy ? $dx : $dy }
package main;
for my $sub (qw(own_rest rest_near party_resting)) {
	my ($code) = $src =~ /(sub $sub \{.*?\n\}\n)/s;
	next unless $code; # rest_near is new: an older plugin has no such sub, and the tests then show the old behaviour
	eval "package partylead;\n$code\n1" or die "$sub: $@";
}

my ($pass, $fail) = (0, 0);
sub ok($$) { my ($c, $m) = @_; if ($c) { $pass++ } else { $fail++; print "FAIL: $m\n" } }

# a fresh world: Ayla at (100,100) on gef_fild07, a party of one friend Zen.fox
sub setup {
	my (%o) = @_;
	%partylead::apiPos = (); %partylead::restAnn = ();
	$partylead::field = FieldStub->new('gef_fild07');
	$partylead::playersList = PlayersStub->new;
	$partylead::char = {name => 'Ayla', pos_to => {x => 100, y => 100}, hp => 100, hp_max => 100, sp => 100, sp_max => 100,
		sitting => 0, party => {users => {1 => {name => 'Zen.fox', online => 1}}}};
	if (exists $o{api}) { $partylead::apiPos{'Zen.fox'} = {map => $o{api}{map}, x => $o{api}{x}, y => $o{api}{y},
		state => $o{api}{state} || '', at => time} }
	$partylead::playersList->{p}{'Zen.fox'} = {pos_to => {x => $o{pl}{x}, y => $o{pl}{y}}, sitting => $o{pl}{sitting}}
		if $o{pl};
}
sub pr { partylead::party_resting() ? 1 : 0 }

# (1) a rest announced by a friend far away (60 cells, same map): no hold
setup(api => {map => 'gef_fild07', x => 160, y => 100, state => 'farming'});
$partylead::restAnn{'Zen.fox'} = time;
ok(pr() == 0, 'announced rest, friend 60 cells away: no hold');

# (2) a rest announced by a friend on another map, even close in coordinates: no hold
setup(api => {map => 'geffen', x => 102, y => 100, state => 'farming'});
$partylead::restAnn{'Zen.fox'} = time;
ok(pr() == 0, 'announced rest, friend on another map: no hold');

# (3) a rest announced by a friend 12 cells away on her map: hold
setup(api => {map => 'gef_fild07', x => 112, y => 100, state => 'farming'});
$partylead::restAnn{'Zen.fox'} = time;
ok(pr() == 1, 'announced rest, friend 12 cells away: hold');

# (4) the 3-minute cap still ends a hold, even when the friend is near
setup(api => {map => 'gef_fild07', x => 112, y => 100, state => 'farming'});
$partylead::restAnn{'Zen.fox'} = time - 200;
ok(pr() == 0, 'announced rest older than 3 min: released');

# (5) a friend resting by state at 25 cells on her map: no hold; at 19 cells: hold
setup(api => {map => 'gef_fild07', x => 125, y => 100, state => 'resting for sp'});
ok(pr() == 0, 'resting friend 25 cells away: no hold');
setup(api => {map => 'gef_fild07', x => 119, y => 100, state => 'resting for sp'});
ok(pr() == 1, 'resting friend 19 cells away: hold');

# (6) a resting friend on another map at 5 cells: no hold
setup(api => {map => 'payon', x => 105, y => 100, state => 'sitting'});
ok(pr() == 0, 'sitting friend on another map: no hold');

# (7) no fresh Legion position, seen in the player list: distance from the list position applies
setup(pl => {x => 140, y => 100, sitting => 1});
ok(pr() == 0, 'sitting in view 40 cells away: no hold');
setup(pl => {x => 110, y => 104, sitting => 1});
ok(pr() == 1, 'sitting in view 10 cells away: hold');

# (8) a friend with no known position at all: no hold
setup();
$partylead::restAnn{'Zen.fox'} = time;
ok(pr() == 0, 'announced rest, position unknown: no hold');

# (9) her own rest still holds the party (unchanged)
setup();
$partylead::char->{hp} = 40;
ok(pr() == 1, 'own rest holds');

print "pass=$pass fail=$fail\n";
exit($fail ? 1 : 0);
