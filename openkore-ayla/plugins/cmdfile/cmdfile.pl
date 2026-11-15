# cmdfile: run OpenKore console commands written to control/commands.txt (one per line), so an outside agent can
# steer the bot without attaching to its terminal. The file is emptied after each read.
package cmdfile;
use strict;
use Plugins;
use Commands;
use Log qw(message);
use Time::HiRes qw(time);

Plugins::register('cmdfile', 'run commands from control/commands.txt', \&unload);
my $hook = Plugins::addHook('mainLoop_post', \&tick);
my $last = 0;
my $file = 'control/commands.txt';
my ($meetTown, $meetAt) = ('', 0); # last meeting town a party line named (decisive-leader guard)

sub tick {
	return if time - $last < 0.5;
	$last = time;
	return unless -s $file;
	open(my $in, '<', $file) or return;
	my @lines = <$in>;
	close $in;
	open(my $out, '>', $file); close $out;
	for my $l (@lines) {
		$l =~ s/[\r\n]+$//;
		next unless length $l;
		# guard: a bare 'ai' TOGGLES Ayla's AI off; 'ai off'/'ai manual' stop her too. Only 'ai on'/'ai auto' pass.
		if ($l =~ /^\s*ai\s*$/i || $l =~ /^\s*ai\s+(off|manual)\b/i) {
			message "[cmdfile] BLOCKED '$l' (would stop Ayla's AI)\n", 'system';
			next;
		}
		# guard: Ayla always keeps at least 10,000z (owner rule RULES 5, 2026-11-14) - zeny by mail or trade from a command
		# never takes her under it ('give' is checked by aylagear.pl itself)
		if ($l =~ /^\s*rodex\s+setzeny\s+(\d+)/i || $l =~ /^\s*deal\s+add\s+z\s+(\d+)/i) {
			my $amt = $1 + ($l =~ /rodex/i ? int($1 / 50) : 0);
			my $z = ($Globals::char && $Globals::char->{zeny}) || 0;
			if ($z - $amt < 10000) {
				message "[cmdfile] BLOCKED '$l' (blocked: Ayla must keep 10k - she has ${z}z)\n", 'system';
				next;
			}
		}
		# guard: the leader is decisive (owner rule) - once a party line has named a meeting town, a different town
		# within 10 min is refused (QA 2026-11-14 12:16-12:17: "everyone to the Kafra in Payon" then 53 s later "change
		# of plan: we all meet in Prontera" split the party ~150 cells for 6+ min). Only party lines ('p ...') are checked.
		if ($l =~ /^\s*p\s+(.*)$/i) {
			my $t = lc $1;
			if ($t =~ /\b(meet|regroup|gather|everyone to|all to|change of plan)\b/
				&& $t =~ /\b(prontera|payon|geffen|morocc|alberta|izlude|aldebaran|yuno|comodo|umbala)\b/) {
				my $town = $1;
				if ($meetTown ne '' && $town ne $meetTown && time - $meetAt < 600) {
					message "[cmdfile] BLOCKED '$l' (meeting point already $meetTown " . int(time - $meetAt) . "s ago; never switch halfway)\n", 'system';
					next;
				}
				($meetTown, $meetAt) = ($town, time);
			}
		}
		message "[cmdfile] > $l\n", 'system';
		Commands::run($l);
	}
}

sub unload { Plugins::delHook($hook) }
1;
