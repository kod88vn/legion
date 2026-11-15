#!/usr/bin/perl
# Dry logic test for openkore-ayla/plugins/cmdfile/idleerrands.pl (no game, no network, no live data).
# Run inside the OpenKore image with a COPY of the plugin piped in (never Ayla's running instance):
#   cat openkore-ayla/plugins/cmdfile/idleerrands.pl | docker exec -i ro-openkore-ayla sh -c 'cat > /tmp/ie.pl'
#   cat tools/dev/idleerrands_test.pl | docker exec -i ro-openkore-ayla sh -c 'cat > /tmp/ie_test.pl; cd /openkore && perl -Isrc -Isrc/deps /tmp/ie_test.pl /tmp/ie.pl'
use strict;
use warnings;
no warnings 'once';
use lib 'src';
use lib 'src/deps';
use Plugins;
use Commands;
{
	no warnings 'redefine';
	*Plugins::register = sub { 1 };
	*Plugins::addHooks = sub { 'hooks' };
	*Commands::register = sub { 'cmds' };
}
my $file = shift or die "usage: $0 idleerrands.pl\n";
do $file or die "load failed: " . ($@ || $!);

my ($ok, $bad) = (0, 0);
sub is { my ($got, $want, $name) = @_; if (($got // '') eq ($want // '')) { $ok++ } else { $bad++; print "FAIL $name: got '" . ($got // 'undef') . "' want '$want'\n" } }

# towns
is(idleerrands::shop_town_of('geffen'), 'geffen', 'town itself');
is(idleerrands::shop_town_of('geffen_in'), 'geffen', 'shop building');
is(idleerrands::shop_town_of('gef_fild07'), '', 'field is no town');
is(idleerrands::at_kafra('geffen', {x => 118, y => 66}), 1, 'at the geffen kafra');
is(idleerrands::at_kafra('geffen', {x => 30, y => 119}), 0, 'geffen west gate is no kafra');
is(idleerrands::at_kafra('gef_fild07', {x => 120, y => 62}), 0, 'a field never counts');

# town run town: this town on foot, else the save town (partylead's wing); never walking off elsewhere
is(join('|', idleerrands::townrun_town('geffen', '', 'geffen')), 'geffen|', 'in town, default');
is((idleerrands::townrun_town('payon', '', 'geffen'))[0], 'payon', 'in another town: that town');
is((idleerrands::townrun_town('gef_fild07', '', 'geffen'))[0], 'geffen', 'field: save town');
is((idleerrands::townrun_town('gef_fild07', 'payon', 'geffen'))[0], 'geffen', 'field, other town asked: save town');
is((idleerrands::townrun_town('geffen_in', '', 'payon'))[0], 'geffen', 'shop building: its town');

# need
my $c = {weight => 40, slots => 85};
is(idleerrands::need_reason({wpct => 45, slots => 30, sellable => 1, low => []}, $c), 'bag 45%', 'heavy with loot');
is(idleerrands::need_reason({wpct => 45, slots => 30, sellable => 0, storable => 1, low => []}, $c), 'bag 45%', 'heavy, store only');
is(idleerrands::need_reason({wpct => 80, slots => 30, sellable => 0, storable => 0, low => []}, $c), '', 'heavy but all kept items');
is(idleerrands::need_reason({wpct => 20, slots => 90, sellable => 1, low => []}, $c), 'bag 90 slots', 'slots full');
is(idleerrands::need_reason({wpct => 20, slots => 30, sellable => 1, low => ['orange potions'], zeny => 34912}, $c), 'low on orange potions', 'low potions');
is(idleerrands::need_reason({wpct => 20, slots => 30, sellable => 1, low => ['orange potions'], zeny => 2000}, $c), '', 'low potions, no money');
is(idleerrands::need_reason({wpct => 20, slots => 30, sellable => 1, low => []}, $c), '', 'nothing to do');

# AY19
is(idleerrands::party_heavy([0.5, 0.6, 0.2, 0.1], 0.45), 1, 'half heavy');
is(idleerrands::party_heavy([0.5, 0.2, 0.2, 0.1], 0.45), 0, 'one heavy');
is(idleerrands::party_heavy([0.9], 0.45), 0, 'one weight only');
is(idleerrands::party_heavy([undef, 0.9, 0.1], 0.45), 1, 'undef skipped');

# wrapper: passes the job's town only while a job runs
{
	no warnings 'redefine';
	my @seen;
	*aylagear::run_start = sub { push @seen, $_[0]; return {town => $_[0]} };
	is(idleerrands::install_wrap(), 1, 'wrap installed');
	aylagear::run_start('gl_church');
	$idleerrands::job = {town => 'geffen'};
	aylagear::run_start('pay_dun00');
	aylagear::run_start('pay_dun00', 1); # dry run untouched
	$idleerrands::job = undef;
	is(join(',', @seen), 'gl_church,geffen,pay_dun00', 'wrapper town override');
	is(idleerrands::install_wrap(), 1, 'second install is a no-op');
	*aylagear::run_start = sub { 'reloaded' }; # aylagear.pl reloaded
	idleerrands::install_wrap();
	is(aylagear::run_start('x'), 'reloaded', 'reinstall after reload keeps the new original');
}
print "idleerrands dry test: $ok ok, $bad failed\n";
exit($bad ? 1 : 0);
