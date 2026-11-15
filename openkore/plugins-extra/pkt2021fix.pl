# pkt2021fix: packets this OpenKore build parses with the old layouts. RefineUI uses 2-byte item ids; this server (packetver 20210107, rAthena
# PACKETVER >= 20181121) sends/expects 4-byte ids: ZC_REFINE_ADD_MATERIAL entries are itemId(V) chance(C) zeny(V),
# CZ_REFINE_ITEM_REQUEST is index(v) itemId(V) blessing(C). Without this the material list is garbage and every
# "refineui refine" is refused locally ("not valid refine material"). 2026-11-14 travel/services QA.
package pkt2021fix;
use strict;
use Plugins;
use Globals qw($messageSender $packetParser $char $refineUI);
use Log qw(message);

Plugins::register('pkt2021fix', 'packet layouts of client 20210107: RefineUI, NPC market, RODEX read', \&unload);
my $hooks = Plugins::addHooks(['start3', \&patch], ['mainLoop_pre', \&patch]);
my $done = 0;

sub patch {
	return if $done || !$messageSender || !$packetParser;
	$messageSender->{packet_list}{'0AA3'} = ['refineui_refine', 'a2 V C', [qw(index catalyst bless)]];
	$messageSender->{packet_lut}{refineui_refine} = '0AA3';
	# NPC markets (Dietrich's ores, ...: ZC_NPC_MARKET_OPEN 09D5 / CZ_NPC_MARKET_PURCHASE 09D6) use 4-byte ids too
	$messageSender->{send_buy_bulk_market_pack} = 'V2';
	$packetParser->{npc_market_info_pack} = 'V C V2 v' if $packetParser;
	# RODEX read answer: this server sends 0B63 (ZC_ACK_READ_RODEX with item grade, 4-byte ids); OpenKore only knows
	# 09EB, so 'rodex read' printed "Unknown switch: 0B63", current_read stayed unset, getzeny/getitems refused and
	# Ayla's aylagear never saw a mail ("mailbox: no answer at step 3").
	$packetParser->{packet_list}{'0B63'} = ['rodex_read_mail', 'v C V2 v V2 C', [qw(len type mailID1 mailID2 text_len zeny1 zeny2 itemCount)]];
	$packetParser->{rodex_read_mail_item_pack} = 'v V C3 a16 a4 C a4 a25 C';
	no warnings 'redefine';
	my $orig = \&Network::Receive::refineui_info;
	*Network::Receive::refineui_info = sub {
		my ($self, $args) = @_;
		if (defined $args->{materials} && length($args->{materials}) % 9 == 0) {
			my $m = '';
			for my $e (unpack '(a9)*', $args->{materials}) {
				my ($id, $ch, $z) = unpack 'V C V', $e;
				$m .= pack 'v C V', ($id > 65535 ? 65535 : $id), $ch, $z;
			}
			$args->{materials} = $m;
		}
		return $orig->($self, $args);
	};
	$done = 1;
	message "[pkt2021fix] RefineUI / NPC market / RODEX read (0B63) layouts patched\n", 'system';
}

sub unload { Plugins::delHooks($hooks) }
1;
