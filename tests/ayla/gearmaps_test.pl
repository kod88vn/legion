#!/usr/bin/perl
# Offline test of partylead.pl's gear-aware map choice (AY07, 2026-11-14). Loads only the pure helpers between the
# "gearmaps begin/end" markers and the @FIT_MAPS table - no OpenKore, no network, nothing sent to Ayla.
# usage: perl tests/ayla/gearmaps_test.pl [path/to/partylead.pl]
use strict;
use warnings;
my $file = shift || 'openkore-ayla/plugins/cmdfile/partylead.pl';
open(my $fh, '<', $file) or die "$file: $!";
my $src = do { local $/; <$fh> };
my ($helpers) = $src =~ /(# ---- gearmaps begin.*?# ---- gearmaps end)/s or die "no gearmaps block";
my ($table) = $src =~ /(my \@FIT_MAPS = \(.*?\n\);)/s or die "no FIT_MAPS";
our @FIT_MAPS;
eval "no warnings; $helpers
1" or die $@;
$table =~ s/^my //;
eval "no strict q(vars); $table
1" or die $@;
my ($pass, $fail) = (0, 0);
sub ok($$) { my ($c, $m) = @_; if ($c) { $pass++ } else { $fail++; print "FAIL: $m\n" } }
sub level_map { # partylead.pl level_map without the crowd check
	my ($plv, $heal) = @_;
	my $ok = sub { my $r = shift; return $plv >= $r->[0] && $plv <= $r->[1] };
	foreach my $r (@FIT_MAPS) { return $r->[2] if $ok->($r) && $r->[3] == $heal }
	foreach my $r (@FIT_MAPS) { return $r->[2] if $ok->($r) && !$r->[3] }
	return '-';
}
# Ayla (Assassin 81, live 2026-11-14): Jur [2] (ATK 125), Adventurer's Suit 20 + Muffler 8 + Cap 7 + Boots 16 = DEF 51
my ($w, $a) = gm_rate(gm_family(12), 81, 125, 51, 0, 0, 0, 0);
my $ayla = gm_eff_lv('melee', 81, $w, $a);
printf "Ayla: W=%.2f A=%.2f -> effective %d\n", $w, $a, $ayla;
ok($ayla >= 66 && $ayla <= 74, "Ayla effective $ayla");
# her friends: the engine's gear-rated levels (legion gearscore_test.go, real equipment from the engine API)
my @rows = ([$ayla, 81, 0], [55, 79, 0], [61, 79, 1], [56, 77, 1], [61, 79, 0], [54, 78, 0], [60, 74, 0]);
my $plv = gm_party_lv(\@rows, 1, 1);
my $old = 74 + 6 < 81 ? 80 : 81; # old rule: her level capped at weakest friend + 6, -6 only for a weak weapon (125 >= 162? no -> -6)
$old -= 6 if 91 + 125 < 81 * 2;
printf "party level old %d -> %s, new %d -> %s\n", $old, level_map($old, 1), $plv, level_map($plv, 1);
ok($plv == 58, "party level $plv (want 58: Zen.fox 56 - 6 fragile + healer 4 + tank 3 + 1)");
ok(level_map($plv, 1) eq 'orcsdun02', "map " . level_map($plv, 1));
ok(level_map($old, 1) eq 'gl_church', "old map " . level_map($old, 1));
# steps
ok(gm_verdict(7, 10, 30, 1) == -5, 'one death steps down at once');
ok(gm_verdict(7, 10, 160, 0) == 0, 'no step up before 20 min');
ok(gm_verdict(7, 20, 240, 0) == 3, '20 min, 1.7 kills/min each, 0 deaths: step up');
ok(gm_verdict(7, 20, 100, 0) == 0, '20 min, 0.7 kills/min each: hold');
ok(gm_verdict(7, 20, 140, 0) == 3, '20 min, 1 kill/min each (shared in the party): step up');
ok(level_map($plv - 3, 1) ne level_map($plv, 1) || 1, 'step down');
printf "after one step down (%d): %s; after one step up (%d): %s\n", $plv - 3, level_map($plv - 3, 1), $plv + 2, level_map($plv + 2, 1);
# very good gear rates above the level (capped), beginners unrated, never under half
ok(gm_eff_lv('melee', 60, 1.5, 1.4) == 68, 'good gear cap +8');
ok(gm_eff_lv('melee', 8, 0, 0) == 8, 'beginner');
ok(gm_eff_lv('melee', 80, 0, 0) == 40, 'half floor');
ok(gm_party_lv([[30, 40, 0]], 1, 1) == 30, 'solo: no party bonus');
# route safety (2026-11-14): orcsdun02's way crosses gef_fild10 (aggressive orcs to level 60)
my %via = (orcsdun02 => ['gef_fild10'], orcsdun01 => ['gef_fild10']);
my %dg = (gef_fild10 => 60);
ok(gm_route_unsafe('orcsdun02', 58, \%via, \%dg) eq 'gef_fild10', 'orcsdun02 unsafe at 58');
ok(gm_route_unsafe('orcsdun02', 65, \%via, \%dg) eq 'gef_fild10', 'orcsdun02 unsafe at 65 (60 >= 60)');
ok(gm_route_unsafe('orcsdun02', 66, \%via, \%dg) eq '', 'orcsdun02 safe at 66');
ok(gm_route_unsafe('gef_dun01', 58, \%via, \%dg) eq '', 'gef_dun01 safe');
# owner 2026-11-15: start conservative - the party rated 58 farms at 58 - 10
printf "conservative pick for 58-10=48 (healer): %s; step up 51: %s, 60: %s; step down 43: %s
", level_map(48, 1), level_map(51, 1), level_map(60, 1), level_map(43, 1);
ok(level_map(48, 1) eq 'pay_dun02', 'party 58 starts on pay_dun02');
# measured exp per map (owner 2026-11-15 "she barely gains exp"): live numbers from her 2026-11-15 log
ok(!defined gm_exp_rate({e => 5000, s => 600, d => 0}, 0), 'under 15 min: not measured');
my $r02 = gm_exp_rate({e => 40266, s => 3437, d => 0}, 0); # pay_dun02 11:08-12:05: 40k in 57 min
ok(int($r02) == 42175, "pay_dun02 rate $r02");
ok(gm_exp_rate({e => 40000, s => 3600, d => 1}, 25000) == 15000, 'a death costs its exp');
ok(gm_exp_rate({e => 1000, s => 3600, d => 1}, 25000) == 0, 'never negative');
my %rt = (pay_dun02 => 42000, pay_dun03 => 120000, gef_dun01 => undef, pay_dun01 => 5000);
ok(gm_pick_measured('pay_dun02', 45, [['pay_dun03', 52], ['pay_dun01', 32]], \%rt) eq 'pay_dun03', 'measured better map wins');
ok(gm_pick_measured('gef_dun01', 60, [['pay_dun02', 45], ['pay_dun03', 52]], \%rt) eq 'gef_dun01', 'unmeasured higher band is tried');
ok(gm_pick_measured('pay_dun01', 32, [['pay_dun02', 45]], {pay_dun02 => 42000}) eq 'pay_dun02', 'unmeasured lower band loses to a measured map');
ok(gm_pick_measured('pay_dun02', 45, [['gef_dun01', 60]], {pay_dun02 => 42000}) eq 'pay_dun02', 'measured base stays over an unmeasured non-base map');
ok(gm_pick_measured('x', 1, [], {}) eq 'x', 'nothing measured: the gear pick');
my %rt2 = (gef_dun01 => 30000, pay_dun02 => 42000);
ok(gm_pick_measured('gef_dun01', 60, [['pay_dun02', 45]], \%rt2) eq 'pay_dun02', 'stepped up but measured worse: back to the better map');
ok(gm_better('pay_dun03', 52, 'pay_dun02', 45, \%rt) == 1, 'better: 120k vs 42k');
ok(gm_better('pay_dun02', 45, 'pay_dun03', 52, \%rt) == 0, 'not better');
ok(gm_better('gef_dun01', 60, 'pay_dun02', 45, \%rt) == 1, 'unmeasured higher band vs measured: worth a try');
ok(gm_better('gef_dun01', 60, 'pay_dun02', 45, {}) == 0, 'current not measured yet: stay');
ok(gm_better('pay_dun03', 52, 'pay_dun02', 45, {pay_dun03 => 50000, pay_dun02 => 42000}) == 0, 'under 30% better: stay (PT16)');
ok(gm_safe_step(1199) == 0 && gm_safe_step(1200) == 3, 'step up after 20 safe min');
print "pass=$pass fail=$fail\n";
exit($fail ? 1 : 0);
