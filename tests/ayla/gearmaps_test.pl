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
ok(gm_verdict(7, 20, 140, 0) == 0, '20 min, 1 kill/min each: hold');
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
print "pass=$pass fail=$fail\n";
exit($fail ? 1 : 0);
