# guildrecruit - Ayla runs her guild Starveil from OpenKore without the game client (owner 2026-11-14: "Ayla should also
# start a guild and recruit all different jobs", "every guild should tax to level up", "definitely tax the guild to
# open more slots"). Never touches the AI or movement.
#   ginvite <name> | <account_id> [name]   guild invitation (a name: account id from sight or Legion's API) (CZ_REQ_JOIN_GUILD 0x0168 - rAthena checks no
#                                 distance; OpenKore's `guild request` needs the player on screen)
#   gtax <member%> [master%]      EXP tax on every guild position (CZ_REG_CHANGE_GUILD_POSITIONINFO 0x0161; names and
#                                 permissions kept). The target is saved in control/guildtax.txt and re-applied every
#                                 10 min if it drifts (also after a restart). "gtax off" stops the re-check.
#   gskill [auto|off|NAME]        guild skills: spend points on the plan (Guild Extension first: +6 members a level),
#                                 or one named skill (GD_...). "auto" (the default after load) checks every 10 min.
#   ginfo                         guild level, EXP, members/max, skill points, tax per position (from the server)
#   grecruit [auto|off|now]       automatic recruiting (owner rule AY19, 2026-11-14: Starveil is led with no AI around):
#                                 every 4-8 min one recruit Legion suggests by role gap (GET :8090 /api/guild-recruits?n=1
#                                 &mark=1: online, unguilded, never another guild's leader), invited as with ginvite.
#                                 Default auto; "off" is kept in control/guildrecruit.txt. A full guild waits an hour.
#   (orders)                      every 30 s the plugin takes Legion's Starveil orders (GET :8090 /api/human-guild/orders,
#                                 starveillead.go): "say" / "warn" = one guild chat line, "kick" = the line, then the expel
#                                 (CZ_REQ_BAN_GUILD 0x015b) 5-10 s later - the same guild-duty rules as every guild (G25).
#                                 Never anyone in her own party (G20), only in her own guild, only as its master.
# Packets: CZ_REQ_GUILD_MENU 0x014f (0 info, 2 positions, 3 skills); ZC_POSITION_ID_NAME_INFO 0x0166, ZC_POSITION_INFO
# 0x0160, ZC_GUILD_SKILLINFO 0x0162, ZC_GUILD_INFO 0x0b7b; CZ_UPGRADE_SKILLLEVEL 0x0112 (guild skill ids >= 10000).
package guildrecruit;

use strict;
use Plugins;
use Commands;
use Globals qw($char $messageSender $charID $net %guild);
use Log qw(message error);
use Time::HiRes qw(time);
use IO::Socket::INET;
use JSON::PP;

Plugins::register('guildrecruit', 'Starveil: invitations, EXP tax, guild skills', \&unload);
my @cmds = (
	Commands::register(['ginvite', 'guild invite: ginvite <name> | ginvite <account_id> [name]', \&cmd_ginvite]),
	Commands::register(['gtax', 'guild EXP tax: gtax <member%> [master%] | gtax off', \&cmd_gtax]),
	Commands::register(['gskill', 'guild skills: gskill [auto|off|GD_NAME]', \&cmd_gskill]),
	Commands::register(['ginfo', 'guild level / exp / members / skill points / tax', \&cmd_ginfo]),
	Commands::register(['grecruit', 'automatic guild recruiting: grecruit [auto|off|now]', \&cmd_grecruit]),
);
my $hooks = Plugins::addHooks(
	['packet/guild_members_title_list', \&on_titles],
	['packet/guild_member_setting_list', \&on_settings],
	['packet/guild_skills_list', \&on_skills],
	['packet/guild_info', \&on_info],
	['packet/guild_invite_result', \&on_invite_result],
	['mainLoop_post', \&tick],
);

my $taxFile = 'control/guildtax.txt';
my $recruitFile = 'control/guildrecruit.txt';
my $recruitMode = 'auto';                      # auto | off
my ($nextRecruit, $fullUntil, $inviteSentAt) = (time + 120 + rand(180), 0, 0);
my $nextOrders = time + 30;
my @pendingKicks;                              # [time, order] - the expel follows the line
my @SKILL_PLAN = (
	['GD_EXTENSION', 10], ['GD_APPROVAL', 1], ['GD_GUARDRESEARCH', 1], ['GD_BATTLEORDER', 1], ['GD_REGENERATION', 1],
	['GD_EMERGENCYCALL', 1], ['GD_GLORYWOUNDS', 1], ['GD_SOULCOLD', 1], ['GD_LEADERSHIP', 1], ['GD_HAWKEYES', 1],
	['GD_GUILD_STORAGE', 5], ['GD_LEADERSHIP', 5], ['GD_GLORYWOUNDS', 5], ['GD_SOULCOLD', 5], ['GD_HAWKEYES', 5],
	['GD_KAFRACONTRACT', 1], ['GD_DEVELOPMENT', 1], ['GD_REGENERATION', 3], ['GD_RESTORE', 1],
);

my ($taxMember, $taxMaster) = (0, 0);   # 0 = no target
my $skillMode = 'auto';                 # auto | off | GD_NAME (one-shot)
my (@posName, @posMode, @posRate);
my ($gotTitles, $gotSettings) = (0, 0);
my %skill;                              # name -> {id, lv, up}
my $skillPoints = 0;
my ($nextCheck, $pendingTax, $pendingSkill, $infoWanted) = (time + 20, 0, 0, 0);

if (open(my $f, '<', $recruitFile)) {
	my $l = <$f>;
	close $f;
	$recruitMode = 'off' if defined $l && $l =~ /^\s*off/i;
}
if (open(my $f, '<', $taxFile)) {
	my $l = <$f>;
	close $f;
	($taxMember, $taxMaster) = ($1, $2 // $1) if defined $l && $l =~ /^\s*(\d+)(?:\s+(\d+))?/;
}

sub inGame { return $char && $messageSender && $net && $net->getState() == Network::IN_GAME() }
sub req { my $t = shift; $messageSender->sendToServer(pack('v V', 0x014f, $t)) }

# a character name -> its account id: a player in sight, else Legion's API (/api/bots/<name> persona.account_id, main
# instance :8090, then the core :8091). 2026-11-14: the mind's "ginvite LilRyuheart" only got the usage line.
sub aid_of {
	my $name = shift;
	if ($Globals::playersList) {
		my $p = $Globals::playersList->getByName($name);
		return unpack('V', $p->{ID}) if $p && $p->{ID};
	}
	(my $q = $name) =~ s/([^A-Za-z0-9._-])/sprintf("%%%02X", ord($1))/ge;
	foreach my $port (8090, 8091) {
		my $sock = IO::Socket::INET->new(PeerAddr => '127.0.0.1', PeerPort => $port, Proto => 'tcp', Timeout => 0.5) or next;
		print $sock "GET /api/bots/$q HTTP/1.0\r\nHost: 127.0.0.1\r\n\r\n";
		my $raw = ''; my $deadline = time + 1.0;
		while (time < $deadline) { my $n = sysread($sock, my $buf, 65536); last unless $n; $raw .= $buf }
		close $sock;
		my ($body) = $raw =~ /\r\n\r\n(.*)$/s or next;
		my $d = eval { JSON::PP::decode_json($body) } or next;
		my $aid = $d->{persona} && $d->{persona}{account_id};
		return $aid if $aid;
	}
	return 0;
}
# GET a Legion API path on the main instance (:8090) -> decoded JSON or undef. Short timeouts: called from the main loop
# every 30 s at most (as aid_of).
sub legion_get {
	my $path = shift;
	my $sock = IO::Socket::INET->new(PeerAddr => '127.0.0.1', PeerPort => 8090, Proto => 'tcp', Timeout => 0.5) or return;
	print $sock "GET $path HTTP/1.0\r\nHost: 127.0.0.1\r\n\r\n";
	my $raw = ''; my $deadline = time + 1.0;
	while (time < $deadline) { my $n = sysread($sock, my $buf, 65536); last unless $n; $raw .= $buf }
	close $sock;
	my ($body) = $raw =~ /\r\n\r\n(.*)$/s or return;
	return eval { JSON::PP::decode_json($body) };
}

# Ayla leads her own guild: in a guild, and its master when the server told us who that is
sub isMaster {
	return 0 unless $char->{guild} && $char->{guild}{name};
	return 0 if defined $guild{master} && $guild{master} ne '' && $guild{master} ne $char->{name};
	return 1;
}

# a name in her own party (G20: never kicked)
sub inMyParty {
	my $name = lc(shift // '');
	return 0 unless $char->{party} && $char->{party}{users};
	foreach my $u (values %{$char->{party}{users}}) {
		return 1 if $u && defined $u->{name} && lc($u->{name}) eq $name;
	}
	return 0;
}

sub cmd_grecruit {
	my (undef, $args) = @_;
	$args = '' unless defined $args;
	$args =~ s/^\s+|\s+$//g;
	if ($args =~ /^(auto|on)$/i) {
		$recruitMode = 'auto';
		unlink $recruitFile;
	} elsif ($args =~ /^off$/i) {
		$recruitMode = 'off';
		if (open(my $f, '>', $recruitFile)) { print $f "off\n"; close $f }
	} elsif ($args =~ /^now$/i) {
		($nextRecruit, $fullUntil) = (0, 0);
	}
	message "[guildrecruit] automatic recruiting: $recruitMode" . ($fullUntil > time ? ' (guild full, waiting)' : '') . "\n", 'success';
}

# one suggested recruit, invited like ginvite
sub autoRecruit {
	my $d = legion_get('/api/guild-recruits?n=1&mark=1') or return;
	return unless $d->{guild} && $char->{guild}{name} && lc($d->{guild}) eq lc($char->{guild}{name});
	my $r = $d->{recruits} && $d->{recruits}[0] or return;
	return unless $r->{account_id} && $r->{name};
	message "[guildrecruit] recruiting $r->{name} ($r->{class} lv$r->{level}, $r->{why})\n", 'info';
	$inviteSentAt = time;
	cmd_ginvite(undef, "$r->{account_id} $r->{name}");
}

# Legion's Starveil orders (starveillead.go)
sub takeOrders {
	my $d = legion_get('/api/human-guild/orders') or return;
	my $mine = $char->{guild}{name} // '';
	foreach my $o (@{$d->{orders} || []}) {
		next unless ref $o eq 'HASH' && defined $o->{guild} && lc($o->{guild}) eq lc($mine);
		my $line = $o->{line} // '';
		$line =~ s/[\r\n]+/ /g;
		if ($o->{kind} eq 'kick') {
			if (!$o->{char} || inMyParty($o->{char}) || lc($o->{char}) eq lc($char->{name})) {
				error "[guildrecruit] expel of '" . ($o->{char} // '?') . "' REFUSED: in my party or myself\n";
				next;
			}
			next unless $o->{guild_id} && $o->{account_id} && $o->{char_id};
			Commands::run("g $line") if $line ne '';
			push @pendingKicks, [time + 5 + rand(5), $o];
		} elsif ($o->{kind} eq 'warn' || $o->{kind} eq 'say') {
			next if $o->{kind} eq 'warn' && inMyParty($o->{char});
			Commands::run("g $line") if $line ne '';
		}
	}
}

sub doKick {
	my $o = shift;
	return unless inGame() && isMaster() && !inMyParty($o->{char});
	my $reason = substr($o->{reason} // 'not helping the guild', 0, 39);
	$messageSender->sendToServer(pack('v V V V a40', 0x015b, $o->{guild_id}, $o->{account_id}, $o->{char_id}, $reason));
	message "[guildrecruit] expelled $o->{char} from $o->{guild} (guild duty: $reason)\n", 'success';
}

my %invited; # account id -> name (for the result line)
sub cmd_ginvite {
	my (undef, $args) = @_;
	$args = '' unless defined $args;
	my ($aid, $name) = $args =~ /^\s*(\d+)\s*(.*?)\s*$/;
	if (!$aid) { # a bare name (quotes optional)
		($name = $args) =~ s/^\s*"?|"?\s*$//g;
		if ($name eq '') { error "[guildrecruit] usage: ginvite <name> | ginvite <account_id> [name]\n"; return }
		$aid = aid_of($name);
		if (!$aid) { error "[guildrecruit] ginvite FAILED: no account id found for '$name' (not in sight, unknown to Legion)\n"; return }
	}
	if (!inGame()) {
		error "[guildrecruit] ginvite FAILED: not in game\n";
		return;
	}
	$name = '' unless defined $name;
	$invited{$aid} = $name ne '' ? $name : "account $aid";
	$messageSender->sendGuildJoinRequest(pack('V', $aid), $charID);
	message "[guildrecruit] guild invitation SENT to $invited{$aid} (account $aid) - result follows as 'ginvite RESULT'\n", 'success';
}
# the server's answer to an invitation (ZC_ACK_REQ_JOIN_GUILD 0x0169): one clear line the mind can grep
sub on_invite_result {
	my (undef, $h) = @_;
	my %t = (0 => 'already in a guild', 1 => 'declined', 2 => 'ACCEPTED - joined Starveil', 3 => 'guild is full');
	$fullUntil = time + 3600 if defined $h->{type} && $h->{type} == 3;
	$inviteSentAt = 0;
	my $who = join(', ', values %invited) || '?';
	message "[guildrecruit] ginvite RESULT for $who: " . ($t{$h->{type}} // "unknown ($h->{type})") . "\n", 'success';
	%invited = ();
}

sub cmd_gtax {
	my (undef, $args) = @_;
	$args = '' unless defined $args;
	if ($args =~ /^\s*off\s*$/i) {
		($taxMember, $taxMaster) = (0, 0);
		unlink $taxFile;
		message "[guildrecruit] guild tax re-check off (rates stay as they are)\n", 'success';
		return;
	}
	my ($m, $l) = $args =~ /^\s*(\d+)(?:\s+(\d+))?\s*$/;
	if (!defined $m || $m < 1 || $m > 50 || (defined $l && ($l < 1 || $l > 50))) {
		error "[guildrecruit] usage: gtax <member% 1-50> [master% 1-50]   (server cap guild_exp_limit 50)\n";
		return;
	}
	($taxMember, $taxMaster) = ($m, $l // $m);
	if (open(my $f, '>', $taxFile)) { print $f "$taxMember $taxMaster\n"; close $f }
	return error "[guildrecruit] not in game\n" unless inGame();
	($gotTitles, $gotSettings, $pendingTax) = (0, 0, time + 5);
	req(2);
	message "[guildrecruit] reading the guild positions, then setting tax $taxMember% (master $taxMaster%)\n", 'success';
}

sub cmd_gskill {
	my (undef, $args) = @_;
	$args = 'auto' unless defined $args && $args =~ /\S/;
	$args =~ s/^\s+|\s+$//g;
	$skillMode = $args =~ /^(auto|off)$/i ? lc $args : uc $args;
	message "[guildrecruit] guild skills: $skillMode\n", 'success';
	return if $skillMode eq 'off';
	return error "[guildrecruit] not in game\n" unless inGame();
	$pendingSkill = time + 5;
	req(3);
}

sub cmd_ginfo {
	return error "[guildrecruit] not in game\n" unless inGame();
	$infoWanted = time + 5;
	req(0); req(3); req(2);
}

sub on_titles {
	my (undef, $pa) = @_;
	my ($msg, $n) = ($pa->{RAW_MSG}, $pa->{RAW_MSG_SIZE});
	for (my $i = 4; $i + 28 <= $n; $i += 28) {
		my ($id, $name) = unpack('V Z24', substr($msg, $i, 28));
		$posName[$id] = $name if $id < 20;
	}
	$gotTitles = 1;
}

sub on_settings {
	my (undef, $pa) = @_;
	my ($msg, $n) = ($pa->{RAW_MSG}, $pa->{RAW_MSG_SIZE});
	for (my $i = 4; $i + 16 <= $n; $i += 16) {
		my ($id, $mode, undef, $rate) = unpack('V4', substr($msg, $i, 16));
		($posMode[$id], $posRate[$id]) = ($mode, $rate) if $id < 20;
	}
	$gotSettings = 1;
	applyTax() if $taxMember;
	if ($infoWanted) {
		message "[guildrecruit] tax per position: " . join(' ', map { "$_:" . ($posRate[$_] // '?') . '%' } 0 .. 19) . "\n", 'info';
	}
}

sub target { my $id = shift; return $id == 0 ? $taxMaster : $taxMember }

sub applyTax {
	return unless $taxMember && $gotTitles && $gotSettings && inGame();
	my $body = '';
	my $n = 0;
	for my $id (0 .. 19) {
		next unless defined $posMode[$id];
		my $want = target($id);
		next if defined $posRate[$id] && $posRate[$id] == $want;
		my $name = defined $posName[$id] && $posName[$id] ne '' ? $posName[$id] : "Position " . ($id + 1);
		$body .= pack('V V V V a24', $id, $posMode[$id], $id, $want, $name);
		$n++;
	}
	$pendingTax = 0;
	return unless $n; # already right
	$messageSender->sendToServer(pack('v v', 0x0161, 4 + length($body)) . $body);
	message "[guildrecruit] guild EXP tax set on $n positions: members $taxMember%, master $taxMaster%\n", 'success';
	($gotTitles, $gotSettings) = (0, 0);
}

sub on_skills {
	my (undef, $pa) = @_;
	my ($msg, $n) = ($pa->{RAW_MSG}, $pa->{RAW_MSG_SIZE});
	$skillPoints = unpack('v', substr($msg, 4, 2));
	%skill = ();
	for (my $i = 6; $i + 37 <= $n; $i += 37) {
		my ($id, undef, $lv, undef, undef, $name, $up) = unpack('v V v3 Z24 C', substr($msg, $i, 37));
		$skill{$name} = {id => $id, lv => $lv, up => $up};
	}
	if ($infoWanted) {
		message "[guildrecruit] guild skill points: $skillPoints; skills: " .
			join(', ', map { "$_ $skill{$_}{lv}" } grep { $skill{$_}{lv} } sort keys %skill) . "\n", 'info';
	}
	return if $skillMode eq 'off' || $skillPoints <= 0 || !inGame();
	my @plan = $skillMode eq 'auto' ? @SKILL_PLAN : ([$skillMode, 99]);
	for my $p (@plan) {
		my $s = $skill{$p->[0]} or next;
		next if $s->{lv} >= $p->[1] || !$s->{up} || $s->{id} < 10000;
		$messageSender->sendToServer(pack('v v', 0x0112, $s->{id}));
		message "[guildrecruit] guild skill up: $p->[0] (lv $s->{lv} -> " . ($s->{lv} + 1) . "), $skillPoints point(s) left before\n", 'success';
		$skillMode = 'auto' if $skillMode ne 'auto';
		$pendingSkill = time + 3; # read again for the next point
		return;
	}
}

sub on_info {
	return unless $infoWanted;
	message sprintf("[guildrecruit] %s: level %s, exp %s/%s, members %s/%s online/max, avg lv %s\n",
		$guild{name} // '?', $guild{lv} // '?', $guild{exp} // '?', $guild{exp_next} // '?',
		$guild{conMember} // '?', $guild{maxMember} // '?', $guild{average} // '?'), 'info';
}

sub tick {
	my $now = time;
	$infoWanted = 0 if $infoWanted && $now > $infoWanted;
	return unless inGame();
	if ($pendingSkill && $now > $pendingSkill) { $pendingSkill = 0; req(3) if $skillMode ne 'off' }
	if ($pendingTax && $now > $pendingTax) { $pendingTax = 0; req(2) if $taxMember }
	if (@pendingKicks && $now > $pendingKicks[0][0]) { my $k = shift @pendingKicks; doKick($k->[1]) }
	if ($now > $nextOrders) {
		$nextOrders = $now + 30;
		takeOrders() if isMaster();
	}
	if ($recruitMode eq 'auto' && $now > $nextRecruit && $now > $fullUntil && isMaster()) {
		$nextRecruit = $now + 240 + rand(240); # one every 4-8 min
		autoRecruit() unless $inviteSentAt && $now - $inviteSentAt < 60;
	}
	return if $now < $nextCheck;
	$nextCheck = $now + 600;
	return unless $char->{guild} && $char->{guild}{name};
	req(2) if $taxMember;           # re-applies the tax if a position drifted (or after a restart)
	req(3) if $skillMode eq 'auto'; # spends new guild skill points
}

sub unload {
	Commands::unregister($_) for grep { $_ } @cmds;
	Plugins::delHooks($hooks) if $hooks;
}
1;
