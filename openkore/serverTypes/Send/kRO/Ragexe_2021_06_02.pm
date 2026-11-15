# kRO main Ragexe 2021-06-02 (Gravity full client Ragnarok_210607.zip) against rAthena PACKETVER 20210602 (main).
# Same as RagexeRE_2021_11_03 except map login 0x0436 is still 19 bytes (no 'unknown' field; 23 bytes only from main 20220330).
package Network::Send::kRO::Ragexe_2021_06_02;
use strict;
use base qw(Network::Send::kRO::RagexeRE_2021_11_03);
use Log qw(debug);
use Utils qw(getTickCount);

sub new {
	my ($class) = @_;
	my $self = $class->SUPER::new(@_);
	$self->{packet_list}{'0436'} = ['map_login', 'a4 a4 a4 V C', [qw(accountID charID sessionID tick sex)]]; #19
	return $self;
}

sub sendMapLogin {
	my ($self, $accountID, $charID, $sessionID, $sex) = @_;
	$sex = 0 if ($sex > 1 || $sex < 0);
	my $msg = $self->reconstruct({ switch => 'map_login', accountID => $accountID, charID => $charID,
		sessionID => $sessionID, tick => getTickCount, sex => $sex });
	$self->sendToServer($msg);
	debug "Sent sendMapLogin (19)\n", "sendPacket", 2;
}

1;
