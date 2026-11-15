# xkore2fix - makes OpenKore's X-Kore 2 proxy speak the login/char packets of the 2021-01-07 client.
# Reference: what rAthena (packetver 20210107) really sends this client, captured with tools/rogui/realcap.py:
#   login: 0AC4 server list (OpenKore's proxy sent the old 0069, which this client ignores)
#   char:  accountID, 082D, (006B), 09A0, (020D), 08B9; every 09A1 from the client is answered with a 099D page
#          (OpenKore alternated 099D with a PIN-code request); character select answers with 0AC5 (not 0071)
package xkore2fix;

use strict;
use Plugins;
use Globals;
use Log qw(message);

Plugins::register('xkore2fix', 'X-Kore 2 packet formats for the 2021 client', \&unload);
my $hooks = Plugins::addHooks(['mainLoop_pre', \&fix]);

{
	no warnings 'redefine';
	# 0AC4 carries a 16-char web auth token (+NUL) after the sex byte; the "Integrated Account" client needs one
	my $orig = \&Network::PacketParser::reconstruct;
	*Network::PacketParser::reconstruct = sub {
		my ($self, $args) = @_;
		my $msg = $orig->(@_);
		if ($args->{switch} && $args->{switch} eq 'account_server_info' && substr($msg, 0, 2) eq "\xC4\x0A" && length($msg) >= 64) {
			substr($msg, 47, 17) = sprintf('%016x', int(rand(0xFFFFFFFF)) * 65536 + int(rand(65536))) . "\0";
			# blank the ip_port text OpenKore puts in each server entry (rAthena leaves it empty)
			for (my $o = 64; $o + 160 <= length($msg); $o += 160) {
				substr($msg, $o + 32, 128) = "\0" x 128;
			}
		}
		return $msg;
	};
}

# Base::Ragnarok::* are compiled only when X-Kore 2 starts (after plugins load), which would overwrite
# redefinitions made at load time - so these are applied once the proxy exists.
our @deferred;
my $patched;
sub patch_late {
	return if $patched++;
	no warnings 'redefine';
	*Base::Ragnarok::CharServer::sync_received_characters = sub {
		my ($self, $args, $client) = @_;
		$self->sendCharInfo($args, $client);
	};
	# session IDs start at 0 in OpenKore; the client treats a zero session as a failed login
	*Base::Ragnarok::SessionStore::generateSessionID = sub {
		return 0x10000000 + int(rand(0x6FFFFFFF));
	};
	# the proxy closes the login connection right after the server list; this client then shows "Disconnected
	# from Server". rAthena lets the client close it, so closes from the account and char servers are deferred a few seconds.
	my $origClose = \&Base::Server::Client::close;
	*Base::Server::Client::close = sub {
		my ($client) = @_;
		my $caller = caller;
		if ($caller && $caller =~ /AccountServer|CharServer/ && !$client->{xk2fix_closing}) {
			$client->{xk2fix_closing} = time;
			push @deferred, [$client, $origClose];
			return;
		}
		$origClose->(@_);
	};
}

sub close_deferred {
	@deferred = grep {
		my ($c, $close) = @$_;
		if (time - $c->{xk2fix_closing} > 5) { eval { $close->($c) }; 0 } else { 1 }
	} @deferred;
}

my $announced;
sub fix {
	return unless $Network::XKore2::accountServer;
	patch_late();
	close_deferred() if @deferred;
	foreach my $s ($Network::XKore2::accountServer, $Network::XKore2::charServer) {
		my $lut = $s->{recvPacketParser}{packet_lut};
		$lut->{account_server_info} = '0AC4';
		$lut->{received_characters} = '099D';
		$lut->{received_characters_info} = '082D';
		$lut->{received_character_ID_and_Map} = '0AC5';
	}
	message "[xkore2fix] proxy packet formats set for the 2021 client\n" unless $announced++;
}

sub unload {
	Plugins::delHooks($hooks);
}

1;
