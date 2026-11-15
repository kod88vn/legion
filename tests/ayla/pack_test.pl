#!/usr/bin/perl
# Offline test of partylead.pl's PT04 invariant (pack_decide, 2026-11-14): never travel or farm with no friend within
# 20 cells; leave town only with the priest (or 2 friends); cross gef_fild10 only with the priest + one more within 10.
# Loads only the pure block between the "pack begin/end" markers - no OpenKore, nothing sent to Ayla.
# usage: perl tests/ayla/pack_test.pl [path/to/partylead.pl]
use strict;
use warnings;
my $file = shift || 'openkore-ayla/plugins/cmdfile/partylead.pl';
open(my $fh, '<', $file) or die "$file: $!";
my $src = do { local $/; <$fh> };
my ($code) = $src =~ /(# ---- pack begin.*?# ---- pack end)/s or die "no pack block";
eval "$code\n1" or die $@;
my ($pass, $fail) = (0, 0);
sub ok { my ($c, $m) = @_; if ($c) { $pass++ } else { $fail++; print "FAIL: $m\n" } }
sub act { my ($a, $an) = pack_decide(shift); return $a . ($an ? ":$an->[0]" : '') }
my $pr = sub { ['Zen.fox', 'Priest', @_] };
my $kn = sub { ['Echobunny', 'Knight', @_] };
my $wz = sub { ['Silent rogue', 'Wizard', @_] };
# (5) nobody online: solo
ok(act({here => 'gef_fild07', friends => []}) eq 'solo', 'solo');
# (2) travelling: one friend within 20 is enough; all > 20 -> wait; after 3 min -> go to the priest
ok(act({here => 'gef_fild07', friends => [$pr->('gef_fild07', 30), $kn->('gef_fild07', 12)]}) =~ /^ok/, 'travel ok with a friend at 12');
ok(act({here => 'gef_fild07', friends => [$pr->('gef_fild07', 30), $kn->('geffen', undef)]}) eq 'wait:Zen.fox', 'travel: friend 30 behind -> wait');
ok(act({here => 'gef_fild07', friends => [$pr->('geffen', undef)], waited => 179}) eq 'wait:Zen.fox', 'wait under 3 min');
ok(act({here => 'gef_fild07', friends => [$pr->('geffen', undef)], waited => 180}) eq 'goto:Zen.fox', 'after 3 min go to the priest');
ok(act({here => 'gef_fild07', friends => [$kn->('geffen', undef), $wz->('gef_fild07', 40)], waited => 200}) eq 'goto:Silent rogue', 'no priest: go to the nearest');
# (1) leaving town: the priest beside her, or 2 friends when the priest is offline
ok(act({here => 'geffen', leaving => 1, friends => [$pr->('geffen', 8), $kn->('geffen', 40)]}) =~ /^ok/, 'leave with the priest');
ok(act({here => 'geffen', leaving => 1, friends => [$pr->('geffen', 25), $kn->('geffen', 5)]}) eq 'wait:Zen.fox', 'no leaving without the priest');
ok(act({here => 'geffen', leaving => 1, friends => [$kn->('geffen', 5), $wz->('geffen', 9)]}) =~ /^ok/, 'priest offline: 2 friends');
ok(act({here => 'geffen', leaving => 1, friends => [$kn->('geffen', 5), $wz->('geffen', 30)]}) =~ /^wait/, 'priest offline: 1 friend is not enough');
# (3) farm map: within 20 of the priest, else walk back to him
ok(act({here => 'orcsdun02', farm => 1, friends => [$pr->('orcsdun02', 15), $kn->('orcsdun02', 40)]}) =~ /^ok/, 'farm near the priest');
ok(act({here => 'orcsdun02', farm => 1, friends => [$pr->('orcsdun02', 28), $kn->('orcsdun02', 3)]}) eq 'back:Zen.fox', 'farm: back to the priest');
ok(act({here => 'orcsdun02', farm => 1, friends => [$pr->('geffen', undef), $kn->('orcsdun02', 10)]}) =~ /^ok/, 'farm: priest away, a friend near');
# (4) crossing gef_fild10: before it priest + one more within 10; on it the priest within 10
ok(act({here => 'gef_fild07', cross => 'pre', friends => [$pr->('gef_fild07', 6), $kn->('gef_fild07', 14)]}) =~ /^wait/, 'pre-cross: 2nd friend at 14 -> wait');
ok(act({here => 'gef_fild07', cross => 'pre', friends => [$pr->('gef_fild07', 6), $kn->('gef_fild07', 9)]}) =~ /^ok/, 'pre-cross: both within 10 -> go');
ok(act({here => 'gef_fild10', cross => 'on', friends => [$pr->('gef_fild10', 12), $kn->('gef_fild10', 2)]}) eq 'wait:Zen.fox', 'on gef_fild10: priest 12 behind -> stop');
ok(act({here => 'gef_fild10', cross => 'on', friends => [$pr->('gef_fild10', 7), $kn->('gef_fild10', 30)]}) =~ /^ok/, 'on gef_fild10: priest at 7 -> go on');
print "pass=$pass fail=$fail\n";
exit($fail ? 1 : 0);
