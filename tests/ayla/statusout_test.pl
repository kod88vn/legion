#!/usr/bin/perl
# Offline test of statusout.pl's exp-gain kill count (xkill_check) and its kpm / kills fields (meter).
# Loads the plugin with stub OpenKore modules in a temp directory (its state file lands there) - no OpenKore, nothing
# sent to Ayla, nothing written to openkore-ayla/logs.
# usage: perl tests/ayla/statusout_test.pl [path/to/statusout.pl]
use strict;
use warnings;
use File::Spec;
use File::Temp qw(tempdir);
use Cwd qw(abs_path);

my $file = abs_path(shift || 'openkore-ayla/plugins/cmdfile/statusout.pl');
BEGIN {
	package Plugins; sub register {1} sub addHook {1} sub addHooks {1} sub delHook {} sub delHooks {}
	package Log; sub addHook {1} sub delHook {}
	package Globals; use Exporter 'import'; our ($char, $field, $net, %config, %statusName); our @EXPORT_OK = qw($char $field $net %config);
	package FakeField; sub new { bless {m => $_[1]}, $_[0] } sub baseName { $_[0]{m} }
	package main;
	$INC{$_} = 1 for qw(Plugins.pm Log.pm Globals.pm);
}
my $dir = tempdir(CLEANUP => 1);
mkdir File::Spec->catdir($dir, 'logs');
chdir $dir or die $!;
do $file or die "load $file: " . ($@ || $!);

my ($pass, $fail) = (0, 0);
sub ok($$) { my ($c, $m) = @_; if ($c) { $pass++ } else { $fail++; print "FAIL: $m\n" } }

$Globals::char = {exp => 100, exp_max => 1000, exp_job => 50, exp_job_max => 500, lv => 83, lv_job => 40};
$Globals::field = FakeField->new('pay_dun01');
my $x = do { no warnings 'once'; \%statusout::sx };
statusout::xkill_check();
ok(($x->{xkills} || 0) == 0, 'first loop only takes the baseline');
$Globals::char->{exp} = 130; $Globals::char->{exp_job} = 60;
statusout::xkill_check();
ok($x->{xkills} == 1, 'a base exp rise is a kill');
statusout::xkill_check();
ok($x->{xkills} == 1, 'no change: no kill');
$Globals::char->{exp_job} = 70;
statusout::xkill_check();
ok($x->{xkills} == 1, 'job exp alone below 99 is not another kill');
$Globals::char->{exp} = 20; $Globals::char->{lv} = 84;
statusout::xkill_check();
ok($x->{xkills} == 2, 'a base level-up is a kill');
$Globals::char->{exp} = 10;
statusout::xkill_check();
ok($x->{xkills} == 2, 'exp lost (death) is no kill');
$Globals::field = FakeField->new('payon');
$Globals::char->{exp} = 500;
statusout::xkill_check();
ok($x->{xkills} == 2, 'no kill counted in a town');
$Globals::field = FakeField->new('pay_dun01');
$Globals::char->{lv} = 99; $Globals::char->{exp} = 0;
statusout::xkill_check();
$Globals::char->{exp_job} = 90;
statusout::xkill_check();
ok($x->{xkills} == 4, 'level 99: the job exp counts (level-up + job rise)');
ok($x->{xsince} > 0, 'kills_since set');
my $m = statusout::meter();
ok($m =~ /"kills":4/ && $m =~ /"kills_since":\d+/ && $m =~ /"kpm":/ && $m =~ /"kpm_own":/ && $m =~ /"kills_src":"exp"/, "meter fields: $m");
# samples from before this version (4 columns) are dropped, not read as 0 kills
$x->{samples} = [[time - 300, 0, 0, 0]];
statusout::meter();
ok(defined $x->{samples}[0][4], 'old samples replaced');
print "statusout_test: $pass passed, $fail failed\n";
chdir File::Spec->rootdir;
exit($fail ? 1 : 0);
