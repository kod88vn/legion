# aylagear - Ayla's gear path and her purse (2026-11-14; owner: "Ayla's party gears up first, every member including
# Ayla; buy when there's money, else farm; Ayla funds members").
#
#   give NAME ZENY [why]  - mail zeny to a party friend by RODEX (rAthena takes a 2% fee) and say it in party chat
#                           ("sent Echobunny 22k for the Bastard Sword"). Names with spaces in quotes: give "Silent rogue" 15000 hammer
#   gear                  - print her gear plan (gearPlan_list in config.txt) with what she owns / can afford / where it sells
#
# The town-run part (which town, which NPCs, the gear slot) is called by partylead.pl: aylagear::run_start() when a real
# town run begins, aylagear::run_end() when it is over.
package aylagear;

use strict;
use Plugins;
use Globals qw($char $field %config $net $messageSender %items_lut $rodexList $rodexWrite @venderListsID %venderLists
	$venderItemList $venderID $venderCID $playersList $masterServer);
use AI;
use Commands;
use Misc ();
use Log qw(message warning error);
use Utils qw(timeOut blockDistance);
use Time::HiRes qw(time);
use IO::Socket::INET;
use JSON::PP;

# owner rule (RULES 5, 2026-11-14): Ayla always keeps at least 10,000z - a hard floor for every zeny that leaves her
use constant FLOOR => 10000;
sub can_spend { my $cost = shift; return (($char->{zeny} || 0) - $cost) >= FLOOR }
our $run; # the current town run: {town, saved => {key => old}, target => entry, at}
our $mailjob;          # a mailbox check in progress (mail_tick): {stage, at, todo => [ids], cur, pays => []}
our (%offers, %mailDone, %readMail); # seller (lc) -> {id, item, price, at}; mail id -> 1 handled; mail id -> rodex_mail hook args
our $mailNext = time + 30;           # next mailbox check
our %giftItems;                      # item gifts to friends (gift_tick)
our $quietUntil = 0;                 # popups: server answers kept from the watch client until then (see quiet_now)
our $quietNpcUntil = 0;              # NPC dialogs / shops / storage kept from the watch client until then (partylead's Kafra save sets it)
Plugins::register('aylagear', "Ayla's gear plan, town choice, vendor buys and zeny gifts", \&unload);
my $hooks = Plugins::addHooks(['AI_pre', \&tick], ['packet_privMsg', \&heard_pm], ['rodex_mail', \&got_mail],
	['rodex_unread_mail', sub { $mailNext = time + 5 if $mailNext > time + 5 }],
	['Network::Receive/willMangle', \&will_mangle], ['Network::Receive/mangle', \&do_mangle]);

my $cmds = Commands::register(
	['give', 'mail zeny to a party friend: give NAME ZENY [why]', \&cmd_give],
	['gear', "Ayla's gear plan: gear | gear vendor (buy the next player-only piece from a Prontera vendor now)", \&cmd_gear],
);

sub unload {
	Plugins::delHooks($hooks);
	Commands::unregister($cmds) if $cmds;
	run_end() if $run && defined &run_end;
}

# ---------------------------------------------------------------- small helpers
sub say_party {
	my $t = shift;
	Commands::run("p $t");
}
sub kfmt { my $z = shift; return $z >= 1000 ? (($z % 1000) ? sprintf('%.1fk', $z / 1000) : ($z / 1000) . 'k') : "${z}z" }

sub http_get {
	my ($port, $path, $wait) = @_;
	my $sock = IO::Socket::INET->new(PeerAddr => '127.0.0.1', PeerPort => $port, Proto => 'tcp', Timeout => 0.5) or return undef;
	print $sock "GET $path HTTP/1.0\r\nHost: 127.0.0.1\r\n\r\n";
	my $raw = '';
	my $deadline = time + ($wait || 1.0);
	while (time < $deadline) {
		my $n = sysread($sock, my $buf, 65536);
		last unless $n;
		$raw .= $buf;
	}
	close $sock;
	my ($body) = $raw =~ /\r\n\r\n(.*)$/s;
	return undef unless $body;
	return eval { JSON::PP::decode_json($body) };
}
sub urlq { my $s = shift; $s =~ s/([^A-Za-z0-9._-])/sprintf("%%%02X", ord($1))/ge; return $s }

# ---------------------------------------------------------------- give: zeny to a friend by RODEX
# OpenKore 'rodex' flow: open the mailbox -> 'write NAME' (server checks the name: $rodexWrite->{target}{char_id}) ->
# zeny/title/body -> send (result: $rodexWrite undef = sent) -> cancel-write (the server refuses item use - potions! - and
# trades while a mail is open) -> close. One step per tick; any step stuck 15 s fails the gift.
my @gives;   # queue: {to, zeny, why}
my $give;    # the one in progress: {to, zeny, why, stage, at, tries}
sub cmd_give {
	my (undef, $args) = @_;
	$args = '' unless defined $args;
	my ($to, $z, $why);
	if ($args =~ /^\s*"([^"]+)"\s+(\d+)\s*(.*)$/ || $args =~ /^\s*(\S+)\s+(\d+)\s*(.*)$/) { ($to, $z, $why) = ($1, $2, $3) }
	if (!$to || !$z) { error "[aylagear] usage: give NAME ZENY [why]   (quote names with spaces)\n"; return }
	if ($z < 100) { error "[aylagear] give: at least 100z\n"; return }
	my $cost = $z + int($z / 50); # 2% fee
	if (!can_spend($cost)) { error "[aylagear] give BLOCKED: Ayla must keep 10k - $to $z needs ${cost}z with the fee, she has $char->{zeny}z\n"; return }
	push @gives, {to => $to, zeny => $z, why => $why};
	message "[aylagear] give: queued ${z}z for $to" . ($why ? " ($why)" : '') . "\n", 'system';
}

sub give_fail {
	my $why = shift;
	error "[aylagear] give: mail to $give->{to} failed: $why\n";
	# a payment for an item she already took must not be lost: retry it later (3 tries, then log it for the mind)
	if ($give->{sale}) {
		if (($give->{tries} || 0) < 3) { push @gives, {%$give, tries => ($give->{tries} || 0) + 1, notBefore => time + 60} }
		else { deal_log("UNPAID $give->{to} $give->{zeny}z for $give->{why} - mail failed 3 times, pay by hand: ayla.sh give \"$give->{to}\" $give->{zeny} $give->{why}") }
	}
	eval { $messageSender->rodex_cancel_write_mail() } if defined $rodexWrite;
	eval { $messageSender->rodex_close_mailbox() } if defined $rodexList;
	undef $rodexWrite; undef $rodexList;
	$give = undef;
	$quietUntil = time + 5;
}

sub give_tick {
	if (!$give) {
		return unless @gives;
		return if $mailjob || AI::inQueue('NPC', 'sellAuto', 'buyAuto', 'storageAuto', 'deal');
		return if $gives[0]{notBefore} && time < $gives[0]{notBefore};
		$give = shift @gives;
		@$give{qw(stage at)} = (0, time);
	}
	my $g = $give;
	if (time - $g->{at} > 15) { give_fail("no answer at step $g->{stage}"); return }
	if ($g->{stage} == 0) {
		if (defined $rodexWrite) { eval { $messageSender->rodex_cancel_write_mail() }; undef $rodexWrite }
		if (!defined $rodexList) { Commands::run('rodex open') }
		@$g{qw(stage at)} = (1, time);
	} elsif ($g->{stage} == 1) {
		return unless defined $rodexList;
		Commands::run("rodex write $g->{to}");
		@$g{qw(stage at)} = (2, time);
	} elsif ($g->{stage} == 2) {
		return unless defined $rodexWrite && $rodexWrite->{target} && $rodexWrite->{target}{char_id};
		my $cost = $g->{zeny} + int($g->{zeny} / 50) + ($g->{item} ? 100 : 0);
		if (!can_spend($cost)) { # the 10k floor: a seller's payment waits (retried), a gift / top-up is dropped
			deal_log("HELD " . ($g->{sale} ? "payment" : "gift") . " to $g->{to} (" . ($g->{item} || "$g->{zeny}z") . "): Ayla must keep 10k, she has $char->{zeny}z") if $g->{sale} || $g->{item} || 1;
			give_fail("Ayla must keep 10k (has $char->{zeny}z, needs $cost)");
			return;
		}
		if ($g->{item} && !$g->{added}) { # an item gift: attach it first (rodex add <inventory #> 1), then send
			my ($it) = grep { $_->{name} eq $g->{item} && !$_->{equipped} } inv_items();
			if (!$it) { give_fail("no $g->{item} in the bag"); return }
			if (!$g->{addSent}) { $g->{addSent} = 1; Commands::run("rodex add $it->{binID} 1"); $g->{at} = time; return }
			return unless $rodexWrite->{items} && $rodexWrite->{items}->size;
			$g->{added} = 1;
		}
		$rodexWrite->{zeny} = $g->{zeny};
		$rodexWrite->{title} = substr($g->{why} ? "for $g->{why}" : 'from Ayla', 0, 24);
		$rodexWrite->{title} .= ' ' x (4 - length $rodexWrite->{title}) if length $rodexWrite->{title} < 4;
		$rodexWrite->{body} = $g->{why} ? "for your $g->{why} - Ayla" : 'for your gear - Ayla';
		$g->{zenyBefore} = $char->{zeny};
		Commands::run('rodex send');
		@$g{qw(stage at)} = (3, time);
	} elsif ($g->{stage} == 3) {
		return if defined $rodexWrite; # rodex_write_result undefs it on success
		eval { $messageSender->rodex_cancel_write_mail() };
		Commands::run('rodex close') if defined $rodexList;
		my $k = kfmt($g->{zeny});
		message "[aylagear] give: mailed ${k} to $g->{to}" . ($g->{why} ? " for $g->{why}" : '') . "\n", 'success';
		if ($g->{item}) {
			say_party("sent $g->{to} my $g->{item}, check ur mail");
			deal_log("GAVE $g->{to} $g->{item} by mail" . ($g->{why} ? " ($g->{why})" : ''));
			delete $giftItems{$g->{item}};
		} elsif ($g->{sale}) { # paying a seller (a #trade deal): a thank-you whisper, not a party line
			Commands::run("pm \"$g->{to}\" " . pick_line("sent $k for the $g->{why}, ty!", "paid $k by mail, thx for the $g->{why} :)", "money's in ur mail, ty for the $g->{why}"));
			deal_log("PAID $g->{to} ${k} ($g->{zeny}z) for $g->{why}");
		} else {
			say_party($g->{why} ? "sent $g->{to} $k for the $g->{why}, check ur mail" : "sent $g->{to} $k by mail, go get ur gear");
		}
		$give = undef;
		$quietUntil = time + 5;
	}
}

# ---------------------------------------------------------------- gear plan
# config.txt: gearPlan_list "IDS=BUDGET; IDS=BUDGET; ..." in the order she wants them. IDS = the item id to get, then any
# ids that count as owning it (e.g. "2405,2406" = Boots, Boots [1] also fine); "CARD>ITEM" = a card that goes into that
# item (compounded when both are in the bag). BUDGET = zeny she must hold before buying (NPC price + a reserve) or, for
# a player-only piece, the most she pays a vendor. Sellers come from Legion's game data (GET :8090/api/lookup/item):
# an item with NPC shops is bought on a real-need town run in the closest town that sells it; one without is bought
# from a player vendor at the Prontera market (/api/market) or asked for in #trade ("B> ...", every 30 min at most).
my %TOOL = (prontera => 'prontera 164 57', payon => 'payon_in01 5 49', geffen => 'geffen_in 77 167',
	morocc => 'morocc 146 103', alberta => 'alberta_in 182 97', aldebaran => 'aldeba_in 94 56', izlude => 'izlude_in 57 110',
	yuno => 'yuno 218 97', comodo => 'cmd_in01 79 182', umbala => 'um_in 104 124');
my %KAFRA_ST = (prontera => 'prontera 151 29', payon => 'payon 181 104', geffen => 'geffen 120 62', morocc => 'morocc 156 97',
	alberta => 'alberta 113 60', aldebaran => 'aldebaran 143 119', yuno => 'yuno 152 187', comodo => 'comodo 195 150',
	umbala => 'umbala 87 160');
my %SHOPTOWN = (alberta_in => 'alberta', aldeba_in => 'aldebaran', prt_in => 'prontera', izlude_in => 'izlude',
	payon_in01 => 'payon', payon_in02 => 'payon', morocc_in => 'morocc', geffen_in => 'geffen', cmd_in01 => 'comodo',
	um_in => 'umbala', yuno_in01 => 'yuno');
# towns by Kafra distance from each hunting town (one Kafra hop first; Prontera reaches every town)
my %NEAR = (
	geffen    => [qw(geffen prontera aldebaran izlude alberta payon morocc)],
	payon     => [qw(payon prontera alberta morocc izlude geffen aldebaran)],
	prontera  => [qw(prontera izlude geffen payon morocc alberta aldebaran)],
	morocc    => [qw(morocc prontera payon alberta izlude geffen aldebaran)],
	alberta   => [qw(alberta payon prontera izlude morocc geffen aldebaran)],
	aldebaran => [qw(aldebaran geffen prontera yuno izlude payon morocc alberta)],
	izlude    => [qw(izlude prontera alberta geffen payon morocc aldebaran)],
);

sub shop_town { my $m = shift; return $SHOPTOWN{$m} || ($TOOL{$m} ? $m : '') }
sub pick_line { return $_[int rand @_] }

our $friendRunAt = 0; # last town run steered by a friend's shop piece
our %itemCache; # id -> {id, name, price, type, shops => [{town, npc}]}
# the lookups are kept in logs/itemcache.json: after a reload the plan is not re-fetched item by item inside the main
# loop (each call can block up to 1.5 s; QA 21:14 "Timeout on Map Server" right after a reload)
my $httpBudgetAt = 0;
if (!%itemCache && open(my $cf, '<', 'logs/itemcache.json')) { local $/; my $j = <$cf>; close $cf; my $d = eval { JSON::PP::decode_json($j) }; %itemCache = %$d if ref $d eq 'HASH' }
sub cache_save { if (open(my $f, '>', 'logs/itemcache.json')) { print $f JSON::PP::encode_json(\%itemCache); close $f } }
sub item_info {
	my $id = shift;
	return $itemCache{$id} if $itemCache{$id};
	return undef if time - $httpBudgetAt < 1; # at most one uncached lookup per second (never a burst in one tick)
	$httpBudgetAt = time;
	my $d = http_get(8090, "/api/lookup/item?q=$id", 1.5) or return undef;
	my $r = $d->{result} or return undef;
	return undef unless ($r->{id} || 0) == $id;
	my @shops;
	foreach my $s (@{$r->{npc_shops} || []}) {
		my $t = shop_town($s->{map}) or next;
		push @shops, {town => $t, npc => "$s->{map} $s->{x} $s->{y}"};
	}
	my $name = $items_lut{$id} || $r->{name};
	$name .= " [$r->{slots}]" if $r->{slots} && $name !~ /\[\d\]$/;
	$itemCache{$id} = {id => $id, name => $name, price => ($r->{npc_buy} || 0),
		type => ($r->{type} || ''), slots => ($r->{slots} || 0), shops => \@shops};
	cache_save();
	return $itemCache{$id};
}

sub plan_entries {
	my @out;
	foreach my $part (split /\s*;\s*/, ($config{gearPlan_list} || '')) {
		next unless $part =~ /^\s*(\d+)(?:>(\d+))?((?:,\d+)*)\s*=\s*(\d+)\s*$/;
		my ($id, $into, $alts, $budget) = ($1, $2, $3, $4);
		push @out, {id => $id, into => $into, ids => [$id, grep { $_ } split /,/, $alts], budget => $budget};
	}
	return @out;
}

sub card_ids_of {
	my $it = shift;
	return () unless defined $it->{cards} && length $it->{cards};
	my $u = ($masterServer && $masterServer->{itemListType}) ? 'V' : 'v';
	return grep { $_ && $_ < 65280 } unpack("${u}4", $it->{cards});
}
sub inv_items { return ($char && $char->inventory) ? @{$char->inventory->getItems()} : () }
sub count_id {
	my $id = shift;
	my $n = 0;
	foreach my $it (inv_items()) {
		$n += ($it->{amount} || 1) if $it->{nameID} == $id;
		$n++ if grep { $_ == $id } card_ids_of($it); # a card already compounded into an item
	}
	return $n;
}
sub owned { my $e = shift; foreach my $id (@{$e->{ids}}) { return 1 if count_id($id) > 0 } return 0 }
# a plan piece (unowned in the bag) that sits in Kafra storage, from the storage list OpenKore saw last: the item or ()
sub stored_piece {
	my $stor = eval { $char->storage } or return ();
	foreach my $e (plan_entries()) {
		next if owned($e);
		foreach my $id (@{$e->{ids}}) {
			my $it = eval { $stor->getByNameID($id) };
			return $it if $it;
		}
	}
	return ();
}

# the Prontera market (player vendors): {vendor, x, y} selling an item of this base name
sub vendor_with {
	my $name = shift;
	(my $base = $name) =~ s/\s*\[\d+\]\s*$//;
	my $d = http_get(8090, '/api/market', 1.0) or return undef;
	foreach my $s (@$d) {
		next unless $s->{open} && $s->{items};
		foreach my $i (@{$s->{items}}) {
			(my $n = $i) =~ s/\s*x\d+$//;
			return {vendor => $s->{bot}, x => $s->{x}, y => $s->{y}} if lc $n eq lc $base;
		}
	}
	return undef;
}

# the next piece she can act on now: (entry, info, how, vendor) - how = 'npc' (zeny for it, an NPC sells it) or 'vendor'
sub in_storage { my $e = shift; my $stor = eval { $char->storage } or return 0; foreach my $id (@{$e->{ids}}) { return 1 if eval { $stor->getByNameID($id) } } return 0 }
sub next_piece {
	my $town = shift || ''; # the run's town: NPC pieces it doesn't sell and vendor pieces outside Prontera are skipped
	my $zeny = $char->{zeny} || 0;
	# the gear turn (Legion core /api/party-turn, main 2026-11-14): while a friend's turn piece is unpaid, Ayla buys
	# nothing for herself - her zeny tops up the turn member instead (turn_topup)
	return () if turn_unmet();
	foreach my $e (plan_entries()) {
		next if owned($e) || in_storage($e); # a stored piece is owned: fetched, not bought again
		my $i = item_info($e->{id}) or next;
		if (@{$i->{shops}}) {
			next if $town && !grep { $_->{town} eq $town } @{$i->{shops}};
			return ($e, $i, 'npc') if $zeny >= $e->{budget} && can_spend($i->{price});
		} elsif ((!$town || $town eq 'prontera') && $zeny >= $e->{budget} + 2000) {
			my $v = vendor_with($i->{name});
			return ($e, $i, 'vendor', $v) if $v;
		}
	}
	return ();
}

# a friend's gear piece it can pay for now that an NPC sells (Legion core /api/bots/<name> gear_next)
sub friend_piece {
	return () unless $char->{party} && $char->{party}{users};
	foreach my $u (values %{$char->{party}{users}}) {
		next if !$u || !$u->{online} || $u->{name} eq $char->{name};
		my $d = http_get(8091, '/api/bots/' . urlq($u->{name}), 0.6) or next;
		my $g = $d->{gear_next} or next;
		next unless ref $g;
		my ($town) = ($g->{shop} || '') =~ /^([a-z]+)/;
		next unless $town && ($g->{status} || '') =~ /can pay/i;
		return ($u->{name}, $g->{item}, $town);
	}
	return ();
}

my ($turnAt, $turnData) = (0, undef);
sub turn_info {
	if (time - $turnAt > 60) {
		$turnAt = time;
		my $d = http_get(8091, '/api/party-turn?human=' . urlq($char->{name}), 1.0);
		$turnData = ($d && $d->{parties} && $d->{parties}[0]) ? $d->{parties}[0] : undef;
	}
	return $turnData;
}
# the friend whose gear turn it is and still can't pay: {name, next, price, have} or undef
sub turn_unmet {
	my $t = turn_info() or return undef;
	my $who = $t->{gear_pool_turn} || '';
	foreach my $g (@{$t->{gear} || []}) {
		next unless $g->{name} eq $who;
		return $g if ($g->{have} || 0) < ($g->{price} || 0);
	}
	return undef;
}
# her zeny tops up the turn member's piece (owner: "Ayla funds members"): the shortfall + 5% by mail, keeping 5k for
# pots and the mail fee, once per member per 30 min
# one top-up per member until the topped-up piece is really bought (QA 20:15/20:28: 14,170z for Claire Suits went on a
# Mantle + Bandana, then another 13,853z for a Crossbow) - checked in the member's inventory (core /api/bots/<name>);
# kept across reloads in logs/topups.json. Her own reserve stays: 10k or her restock cost, whichever is higher.
our %toppedFor; # name -> {item, zeny, at}
sub topups_load { if (open(my $f, '<', 'logs/topups.json')) { local $/; my $j = <$f>; close $f; my $d = eval { JSON::PP::decode_json($j) }; %toppedFor = %$d if ref $d eq 'HASH' } }
sub topups_save { if (open(my $f, '>', 'logs/topups.json')) { print $f JSON::PP::encode_json(\%toppedFor); close $f } }
topups_load() unless %toppedFor;
sub friend_has {
	my ($name, $item) = @_;
	my $d = http_get(8091, '/api/bots/' . urlq($name), 1.0) or return undef;
	my $inv = JSON::PP::encode_json($d->{inventory} || []);
	(my $base = $item) =~ s/\s*\[\d+\]\s*$//;
	return index(lc $inv, lc $base) >= 0 ? 1 : 0;
}
sub reserve_z {
	my $restock = 40 * 50 + 5 * 300 + 2000; # 40 Orange Potions, 5 Butterfly Wings, Fly Wings / Kafra
	return $restock > 10000 ? $restock : 10000;
}
sub turn_topup {
	my $g = turn_unmet() or return;
	if (my $prev = $toppedFor{$g->{name}}) {
		my $has = friend_has($g->{name}, $prev->{item});
		if (!$has) {
			deal_log("NO TOPUP for $g->{name}: the " . kfmt($prev->{zeny}) . " for its $prev->{item} is not spent on it yet") if time - ($prev->{said} || 0) > 1800;
			$prev->{said} = time; topups_save();
			return;
		}
		delete $toppedFor{$g->{name}}; # bought: the next piece may be helped
	}
	my $short = int((($g->{price} || 0) - ($g->{have} || 0)) * 1.05) + 1;
	my $z = $char->{zeny} || 0;
	my $free = $z - (reserve_z() > FLOOR ? reserve_z() : FLOOR) - int($z / 50);
	$short = $free if $short > $free;
	return if $short < 500;
	$toppedFor{$g->{name}} = {item => $g->{next}, zeny => $short, at => time};
	topups_save();
	push @gives, {to => $g->{name}, zeny => $short, why => $g->{next}};
	deal_log("TOPUP $g->{name} ${short}z for its turn piece $g->{next} ($g->{have}/$g->{price}z; keeps " . kfmt(reserve_z()) . ")");
}

# items to hand to a friend (config giftItems "Item Name=Friend; ..."): fetched from storage on the next town run, mailed
# as soon as it is in her bag (2026-11-14: xXRinfangXx's turn piece is a Great Bow and she has one in storage)
sub gifts_from_config { foreach my $p (split /\s*;\s*/, ($config{giftItems} || '')) { my ($i, $f) = split /\s*=\s*/, $p; $giftItems{$i} = $f if $i && $f } }
# a gift goes by direct trade (no zeny, no fee) when the friend stands near her; by mail (100z fee) only while she is
# above the 10k floor (main 2026-11-14). The Legion friend accepts a trade from its party's human, walks next to her,
# presses OK after she does and trades (bot.go 0x00e5 / 0x00ec).
our $dealjob; # {to, item, stage, at}
sub gift_tick {
	gifts_from_config();
	return if $give || @gives || $mailjob || $dealjob;
	return if AI::inQueue('attack', 'NPC', 'sellAuto', 'buyAuto', 'storageAuto', 'deal') || $char->{dead};
	foreach my $i (keys %giftItems) {
		my ($it) = grep { $_->{name} eq $i && !$_->{equipped} } inv_items() or next;
		my $to = $giftItems{$i};
		my $p = $playersList ? $playersList->getByName($to) : undef;
		if ($p && $p->{pos_to} && blockDistance($char->{pos_to}, $p->{pos_to}) <= 12) {
			$dealjob = {to => $to, item => $i, stage => 0, at => time, had => scalar(grep { $_->{name} eq $i } inv_items())};
			deal_log("TRADING $i to $to (next to her, no fee)");
			return;
		}
		next unless can_spend(100); # mail only above the floor; else wait until the friend is near
		push @gives, {to => $to, zeny => 0, item => $i, why => 'ur turn piece'};
		gift_done($i, $to);
		return;
	}
}
sub gift_done {
	my ($i, $to) = @_;
	$toppedFor{$to} = {item => ($i =~ s/\s*\[\d+\]$//r), zeny => 0, at => time}; topups_save(); # counts as its turn help
	delete $giftItems{$i};
	$config{giftItems} = join('; ', map { "$_=$giftItems{$_}" } keys %giftItems);
}
sub deal_tick {
	my $d = $dealjob or return;
	my $end = sub { my $why = shift; Commands::run('deal no') if %Globals::currentDeal || %Globals::outgoingDeal; deal_log("TRADE of $d->{item} to $d->{to} failed: $why") if $why; $dealjob = undef; $quietUntil = time + 5 };
	if (time - $d->{at} > 45) { $end->("timed out at step $d->{stage}"); return }
	if ($d->{stage} == 0) {
		Commands::run("deal \"$d->{to}\"");
		@$d{qw(stage st)} = (1, time);
	} elsif ($d->{stage} == 1) { # wait until the trade window is open
		return unless %Globals::currentDeal;
		my ($it) = grep { $_->{name} eq $d->{item} && !$_->{equipped} } inv_items();
		if (!$it) { $end->('item gone'); return }
		Commands::run("deal add $it->{binID} 1");
		@$d{qw(stage st)} = (2, time);
	} elsif ($d->{stage} == 2) {
		return if time - $d->{st} < 2;
		Commands::run('deal'); # finalize (OK)
		@$d{qw(stage st)} = (3, time);
	} elsif ($d->{stage} == 3) {
		if (%Globals::currentDeal && $Globals::currentDeal{you_finalize} && $Globals::currentDeal{other_finalize} && !$Globals::currentDeal{final}) { Commands::run('deal'); return } # trade
		my $now = scalar(grep { $_->{name} eq $d->{item} } inv_items());
		if (!%Globals::currentDeal && $now < $d->{had}) {
			deal_log("GAVE $d->{to} $d->{item} by trade");
			Commands::run("p $d->{to} theres ur $d->{item}, enjoy");
			gift_done($d->{item}, $d->{to});
			$dealjob = undef; $quietUntil = time + 5;
		} elsif (!%Globals::currentDeal && time - $d->{st} > 5) {
			$end->('trade closed without the item leaving');
		}
	}
}

sub hunt_town {
	my $m = shift || '';
	return partylead::town_of_map($m) if defined &partylead::town_of_map;
	return 'payon';
}

# called by partylead.pl when a real-need town run starts (sellAuto/buyAuto queued). Picks the town, points the sell /
# tool / storage NPCs there and enables the gear slot for the piece she buys. Returns {town, item, friend, line}.
sub run_start {
	my ($hunt, $dry) = @_; # $dry: only say what it would do ('gear dry')
	run_end() if $run && !$dry;
	# the town is ALWAYS the town of her current hunting map (lockMap, else the map she hunts on, else her save point):
	# a gear piece is bought only when that town sells it; no detour to another town for gear (main 2026-11-14: a
	# "prontera" run from stale context took her away from the party, back to Payon, then to Prontera again)
	my $home = hunt_town($hunt);
	$home = $config{saveMap} if !$TOOL{$home} && $TOOL{$config{saveMap} || ''};
	my ($e, $i, $how, $v) = next_piece($home);
	turn_topup() unless $dry;
	my ($fn, $fitem, $ftown) = friend_piece();
	($fn, $fitem, $ftown) = () if $fn && $ftown ne $home;
	my ($town, $npc) = ($home, undef);
	if ($e && $how eq 'npc') {
		my ($s) = grep { $_->{town} eq $home } @{$i->{shops}};
		$npc = $s->{npc} if $s;
	}
	$town = 'prontera' unless $TOOL{$town};
	$friendRunAt = time if $fn && $town eq $ftown && !$dry;
	if ($dry) {
		message "[aylagear] dry run (hunting $hunt, home $home): town $town; piece " . ($e ? "$i->{name} ($how" . ($npc ? " at $npc" : $v ? " from $v->{vendor}" : '') . ")" : 'none')
			. ($fn ? "; friend $fn can buy its $fitem in $ftown" : '') . "
", 'system';
		return {town => $town};
	}
	my %old;
	my %setv;
	my $set = sub { my ($k, $val) = @_; $old{$k} = $config{$k} unless exists $old{$k}; $config{$k} = $val; $setv{$k} = $val };
	$set->('sellAuto_npc', $TOOL{$town});
	for (my $n = 0; exists $config{"buyAuto_$n"}; $n++) {
		next unless $config{"buyAuto_$n"};
		$set->("buyAuto_${n}_npc", $TOOL{$town}) if ($config{"buyAuto_${n}_townShop"} || '') eq 'tool';
		if ($config{"buyAuto_${n}_gearSlot"}) {
			if ($npc) {
				$set->("buyAuto_$n", $e->{id});
				$set->("buyAuto_${n}_npc", $npc);
				$set->("buyAuto_${n}_maxAmount", 1);
				$set->("buyAuto_${n}_zeny", ">= " . ($i->{price} + FLOOR)); # gear only above the 10k floor
				$set->("buyAuto_${n}_disabled", 0);
			} else {
				$set->("buyAuto_${n}_disabled", 1);
			}
		}
	}
	# never a cross-map auto-buy trip: any slot still pointing at an NPC outside the run's town is off for this run
	# (live 2026-11-15: a slot pinned to prontera 146 57 turned a Payon restock into a solo walk to Prontera)
	for (my $n = 0; exists $config{"buyAuto_$n"}; $n++) {
		next unless $config{"buyAuto_$n"} && !$config{"buyAuto_${n}_disabled"};
		my ($nm) = split / /, ($config{"buyAuto_${n}_npc"} || '');
		next unless defined $nm && $nm ne '';
		(my $nt = $nm) =~ s/_in\d*$//;
		$nt = 'payon' if $nt eq 'payon_in'; $nt = 'aldebaran' if $nt eq 'aldeba';
		$set->("buyAuto_${n}_disabled", 1) if $nt ne $town && hunt_town($nm) ne $town;
	}
	if ($KAFRA_ST{$town}) { $set->('storageAuto_npc', $KAFRA_ST{$town}) } else { $set->('storageAuto', 0) }
	# a plan piece lying in Kafra storage (stored before items_control kept gear in the bag) comes back out on this
	# run's storage visit (getAuto) and equip_tick wears it
	if (my ($st) = stored_piece()) {
		$set->('getAuto_0', $st->{name}); $set->('getAuto_0_minAmount', 1); $set->('getAuto_0_maxAmount', 1); $set->('getAuto_0_passive', 0);
		deal_log("FETCH $st->{name} from Kafra storage on this town run");
	}
	gifts_from_config();
	if (my ($gi) = grep { my $n = $_; my $stor = eval { $char->storage }; $stor && eval { $stor->getByName($n) } } keys %giftItems) {
		$set->('getAuto_2', $gi); $set->('getAuto_2_minAmount', 1); $set->('getAuto_2_maxAmount', 1); $set->('getAuto_2_passive', 0);
		deal_log("FETCH $gi from Kafra storage for $giftItems{$gi}");
	}
	$run = {town => $town, saved => \%old, set => \%setv, at => time, entry => $e, info => $i, how => ($how || ''), vendor => $v,
		had => ($e ? count_id($e->{id}) : 0), friend => $fn, fitem => $fitem};
	my $what = $e ? ($how eq 'npc' ? "buying $i->{name} at $npc" : "$i->{name} from $v->{vendor}'s shop") : 'no gear piece';
	message "[aylagear] town run to $town (hunting $hunt, home $home): $what" . ($fn ? "; $fn can buy its $fitem in $ftown" : '') . "\n", 'system';
	my $line = '';
	if ($e) { $line = $how eq 'npc' ? "i grab my " . lc($i->{name}) . " there" : "i check the shops for a " . lc($i->{name}) }
	if ($fn && $ftown eq $town) { $line .= ($line ? ', ' : '') . "$fn can get its $fitem" }
	return {town => $town, item => ($e ? $i->{name} : ''), friend => $fn, line => $line};
}

# called by partylead.pl when the run is over: NPCs back, the new piece equipped, a vendor buy started in Prontera
our $equipAt = 0;
our $vbuy; # a vendor buy in progress: {entry, info, vendor, x, y, stage, at, had}
sub run_end {
	return unless $run;
	my $r = $run;
	$run = undef;
	foreach my $k (keys %{$r->{saved}}) {
		next if ($config{$k} // '') ne ($r->{set}{$k} // ''); # changed meanwhile (the mind's conf): keep its value
		$config{$k} = $r->{saved}{$k};
	}
	if ($r->{entry} && $r->{how} eq 'npc') {
		my $got = count_id($r->{entry}{id}) > $r->{had};
		message "[aylagear] town run over: " . ($got ? "bought $r->{info}{name}" : "$r->{info}{name} not bought (zeny $char->{zeny})") . "\n", 'system';
		if ($got) { Commands::run('p ' . pick_line("got my new " . lc($r->{info}{name}) . " :D", "finally, new " . lc($r->{info}{name}) . ", ty for waiting")); $equipAt = 0 }
	} elsif ($r->{entry} && $r->{how} eq 'vendor' && $field && $field->baseName eq 'prontera') {
		vendor_start($r->{entry}, $r->{info});
	}
}

# ---------------------------------------------------------------- equip / compound what she owns
# equipAuto (config.txt) keeps re-wearing the pieces it names, so a new piece is written into equipAuto_0_<slot> first
# (configModify: memory + config.txt), and a new weapon also replaces the old one in every *_whenEquipped condition
# (Sonic Blow is "whenEquipped Jur [2]"); then 'eq'. A card going into a worn item: its equipAuto slot is cleared, the
# item taken off, the card compounded (card forceuse), and the carded item worn again on the next pass.
sub equip_slot {
	my $it = shift;
	my $t = $it->{type_equip} || 0;
	return 'rightHand' if $t & 2;
	return 'armor' if $t & 16;
	return 'robe' if $t & 4;
	return 'shoes' if $t & 64;
	return 'topHead' if $t & 256;
	return 'midHead' if $t & 512;
	return 'lowHead' if $t & 1;
	if ($t & 136) {
		foreach my $s (qw(rightAccessory leftAccessory)) { return $s if !$char->{equipment}{$s} }
		return 'rightAccessory';
	}
	return 'leftHand' if $t & 32;
	return '';
}
sub set_conf { my ($k, $v) = @_; return if ($config{$k} // '') eq ($v // ''); eval { Misc::configModify($k, $v, 1) }; $config{$k} = $v }
sub wear {
	my $it = shift;
	my $slot = equip_slot($it);
	if ($slot) {
		my $old = $config{"equipAuto_0_$slot"} // '';
		set_conf("equipAuto_0_$slot", $it->{name});
		if ($slot eq 'rightHand' && $old ne '' && $old ne $it->{name}) {
			foreach my $k (grep { /_whenEquipped$/ && ($config{$_} // '') eq $old } keys %config) { set_conf($k, $it->{name}) }
		}
	}
	message "[aylagear] equipping $it->{name}" . ($slot ? " ($slot)" : '') . "\n", 'system';
	Commands::run("eq $it->{binID}");
}
sub equip_tick {
	return if time - $equipAt < 10;
	$equipAt = time;
	return if $run || $vbuy || AI::inQueue('attack', 'NPC', 'sellAuto', 'buyAuto', 'storageAuto', 'deal') || $char->{dead};
	foreach my $e (plan_entries()) {
		if ($e->{into}) { # a card for an item: compound when both are in the bag (take the item off first)
			my ($card) = grep { $_->{nameID} == $e->{id} } inv_items();
			my ($tgt) = grep { $_->{nameID} == $e->{into} && !card_ids_of($_) } inv_items();
			if ($card && $tgt) {
				if ($tgt->{equipped}) {
					foreach my $s (qw(rightAccessory leftAccessory topHead armor robe shoes rightHand)) {
						set_conf("equipAuto_0_$s", '') if ($config{"equipAuto_0_$s"} // '') eq $tgt->{name};
					}
					message "[aylagear] taking off $tgt->{name} to put the card in\n", 'system';
					Commands::run("uneq $tgt->{binID}");
					return;
				}
				message "[aylagear] compounding $card->{name} into $tgt->{name}\n", 'system';
				Commands::run("card forceuse $card->{binID} $tgt->{binID}");
				Commands::run('p ' . pick_line("put a " . lc($card->{name}) . " in my " . lc($tgt->{name}) . " :D", "finally, " . lc($card->{name}) . " in my " . lc($tgt->{name})));
				return;
			}
			# the carded item is in the bag but not worn (after compounding): wear it
			my ($done) = grep { $_->{nameID} == $e->{into} && !$_->{equipped} && grep { $_ == $e->{id} } card_ids_of($_) } inv_items();
			if ($done) { wear($done); return }
			next;
		}
		my @mine = grep { my $it = $_; grep { $it->{nameID} == $_ } @{$e->{ids}} } inv_items();
		next if !@mine || grep { $_->{equipped} } @mine;
		my ($it) = sort { card_ids_of($b) <=> card_ids_of($a) } @mine; # a carded copy first
		wear($it);
		return;
	}
}

# ---------------------------------------------------------------- buy from a player vendor (Prontera market)
sub busy { return $vbuy ? 1 : 0 }
sub vendor_start {
	my ($e, $i) = @_;
	my $v = vendor_with($i->{name});
	if (!$v) { message "[aylagear] no vendor sells $i->{name} right now\n", 'system'; return 0 }
	$vbuy = {entry => $e, info => $i, %$v, stage => 0, at => time, had => count_id($e->{id})};
	message "[aylagear] going to $v->{vendor}'s shop ($v->{x},$v->{y}) for $i->{name} (max " . kfmt($e->{budget}) . ")\n", 'system';
	return 1;
}
sub vendor_done {
	my ($ok, $why) = @_;
	message "[aylagear] vendor buy " . ($ok ? 'done' : "given up: $why") . "\n", 'system';
	Commands::run('vender end') if $venderID;
	$vbuy = undef;
	$equipAt = 0;
	$quietUntil = time + 5;
}
sub vendor_tick {
	my $vb = $vbuy or return;
	if (time - $vb->{at} > 90) { vendor_done(0, 'took too long'); return }
	return if AI::inQueue('attack');
	if ($field->baseName ne 'prontera') { vendor_done(0, 'not in prontera'); return }
	if ($vb->{stage} == 0) {
		if (blockDistance($char->{pos_to}, {x => $vb->{x}, y => $vb->{y}}) > 4) {
			if (!AI::inQueue('route', 'move') || time - ($vb->{routeAt} || 0) > 10) {
				$vb->{routeAt} = time;
				Commands::run('stand') if $char->{sitting};
				AI::clear('route', 'move');
				main::ai_route('prontera', $vb->{x}, $vb->{y}, distFromGoal => 3, maxRouteTime => 30);
			}
			return;
		}
		AI::clear('route', 'move');
		my ($id) = grep { my $a = Actor::get($_); $a && ($a->{name} || '') eq $vb->{vendor} } @venderListsID;
		return if !$id && time - $vb->{at} < 60;
		if (!$id) { vendor_done(0, "can't see $vb->{vendor}'s shop"); return }
		$vb->{vid} = $id;
		$messageSender->sendEnteringVender($id);
		@$vb{qw(stage stageAt)} = (1, time);
	} elsif ($vb->{stage} == 1) {
		if (!defined $venderID || $venderID ne $vb->{vid} || !$venderItemList || !$venderItemList->size) {
			vendor_done(0, 'shop did not open') if time - $vb->{stageAt} > 10;
			return;
		}
		my ($it) = sort { $a->{price} <=> $b->{price} } grep { $_->{nameID} == $vb->{entry}{id} } @{$venderItemList->getItems()};
		if (!$it) { vendor_done(0, "$vb->{vendor} has no $vb->{info}{name} (slots differ)"); return }
		if ($it->{price} > $vb->{entry}{budget} || !can_spend($it->{price})) {
			Commands::run("p $vb->{info}{name} at $vb->{vendor} is " . kfmt($it->{price}) . ", too much for now");
			vendor_done(0, "price $it->{price} > budget $vb->{entry}{budget}");
			return;
		}
		message "[aylagear] buying $it->{name} from $vb->{vendor} for $it->{price}z\n", 'system';
		$vb->{price} = $it->{price};
		$messageSender->sendBuyBulkVender($venderID, [{itemIndex => $it->{ID}, amount => 1}], $venderCID);
		@$vb{qw(stage stageAt)} = (2, time);
	} elsif ($vb->{stage} == 2) {
		if (count_id($vb->{entry}{id}) > $vb->{had}) {
			Commands::run('p ' . pick_line("got a " . lc($vb->{info}{name}) . " from $vb->{vendor} :D", "bought a " . lc($vb->{info}{name}) . " at the market, nice"));
			vendor_done(1);
		} elsif (time - $vb->{stageAt} > 10) {
			vendor_done(0, 'no item after buying');
		}
	}
}

# ---------------------------------------------------------------- #trade: ask for a player-only piece nobody vends
my $tradeAt = time - 1500; # first ad ~5 min after load
sub trade_tick {
	return if time - $tradeAt < 1800;
	$tradeAt = time;
	my $zeny = $char->{zeny} || 0;
	foreach my $e (plan_entries()) {
		next if owned($e);
		my $i = item_info($e->{id}) or next;
		next if @{$i->{shops}};               # NPC piece: bought on a town run
		last if $zeny < $e->{budget};          # can't pay the first player piece yet: no ad
		next if vendor_with($i->{name});       # a vendor has it: the next Prontera run buys it
		Commands::run("pm \"#trade\" B> $i->{name}, paying up to " . kfmt($e->{budget}) . ", pm Ayla");
		message "[aylagear] asked #trade for $i->{name} (up to $e->{budget}z)\n", 'system';
		last;
	}
}

sub cmd_gear {
	my (undef, $args) = @_;
	$args = '' unless defined $args;
	# in-memory config edits without a 'reload config' (which would reset partylead's holds and the mind's lockMap):
	# gear set KEY VALUE | gear unset KEY - config.txt itself is edited by hand for the next start
	if ($args =~ /^\s*set\s+(\S+)\s*(.*?)\s*$/) { $config{$1} = $2; message "[aylagear] config $1 = '$2' (memory only)\n", 'system'; return }
	if ($args =~ /^\s*unset\s+(\S+)/) { delete $config{$1}; message "[aylagear] config $1 removed (memory only)\n", 'system'; return }
	if ($args =~ /^\s*dry\s*(\S*)/) { my $m = $1 || $config{lockMap} || ($partylead::huntMap || $field->baseName); run_start($m, 1); return }
	if ($args =~ /^\s*applytest\s*(\S*)/) { # apply a run's NPC switches, print them, put them back at once (testing)
		return message "[aylagear] a town run is in progress\n", 'system' if $run;
		my $m = $1 || $config{lockMap} || $field->baseName;
		my $fr = $friendRunAt;
		run_start($m);
		message "[aylagear] applied: " . join(' | ', map { "$_=" . ($config{$_} // '') } sort keys %{$run->{saved}}) . "\n", 'system';
		$run->{entry} = undef; # no "not bought" line
		run_end();
		$friendRunAt = $fr;
		message "[aylagear] restored: " . join(' | ', map { "$_=" . ($config{$_} // '') } qw(sellAuto_npc buyAuto_0_npc buyAuto_1_npc buyAuto_2 buyAuto_2_npc buyAuto_2_disabled storageAuto_npc)) . "\n", 'system';
		return;
	}
	if ($args =~ /^\s*vendor/) {
		my ($e, $i, $how) = next_piece();
		if ($e && $how eq 'vendor') { vendor_start($e, $i) } else { message "[aylagear] no player-only piece to buy from a vendor now\n", 'system' }
		return;
	}
	my $zeny = $char->{zeny} || 0;
	message "[aylagear] gear plan (zeny $zeny):\n", 'system';
	foreach my $e (plan_entries()) {
		my $i = item_info($e->{id});
		my $name = $i ? $i->{name} : "item $e->{id}";
		my %seen;
		my $src = !$i ? '?' : @{$i->{shops}} ? 'NPC ' . kfmt($i->{price}) . ' in ' . join('/', grep { !$seen{$_}++ } map { $_->{town} } @{$i->{shops}})
			: 'player-only (vendor / #trade)';
		my $st = owned($e) ? 'OWNED' : $zeny >= $e->{budget} ? 'can buy' : 'saving';
		message sprintf("[aylagear]   %-8s %-24s budget %-7s %s%s\n", $st, $name, kfmt($e->{budget}), $src, $e->{into} ? " -> into item $e->{into}" : ''), 'system';
	}
	my ($e, $i, $how) = next_piece();
	message "[aylagear] next: " . ($e ? "$i->{name} ($how)" : 'nothing affordable yet') . ($run ? "; town run to $run->{town} in progress" : '') . "\n", 'system';
}

# ---------------------------------------------------------------- #trade deals (Legion humanwant.go, economy run 9)
# Her "B> X, paying up to Nk" gets a whispered offer from a bot ("hey, i have X. 12k ok? can mail it to u" / "X in my
# shop at prt south gate, 12k"). She accepts an offer within the plan budget, refuses one above it; a mail seller then
# RODEXes the item ("X for 12000z, pay by mail pls"). Every 3 min (sooner after "sent, check ur mail" or an unread-mail
# notice) she opens the mailbox, takes the items of wanted pieces at an agreed / budgeted price (and any zeny), and pays
# the seller by mail with a thank-you whisper (give queue, sale => 1). equip_tick then wears / compounds the piece.
# One line per deal goes to the console ("[aylagear] DEAL: ...") and logs/deals.log for the ayla mind (ayla.sh deals).
sub deal_log {
	my $t = shift;
	message "[aylagear] DEAL: $t\n", 'success';
	if (open(my $f, '>>', 'logs/deals.log')) {
		my @l = localtime;
		printf $f "%04d-%02d-%02d %02d:%02d:%02d %s\n", $l[5] + 1900, $l[4] + 1, @l[3, 2, 1, 0], $t;
		close $f;
	}
}
sub plan_entry_for_id { my $id = shift; foreach my $e (plan_entries()) { return $e if grep { $_ == $id } @{$e->{ids}} } return undef }
sub price_of { my ($n, $u) = @_; $n =~ s/,/./g; $u = lc($u || ''); return int($n * 1000000) if $u eq 'm'; return int($n * 1000) if $u eq 'k'; return int($n) }

sub heard_pm {
	my (undef, $args) = @_;
	my $who = defined $args->{privMsgUser} ? $args->{privMsgUser} : '';
	my $lm = lc(defined $args->{privMsg} ? $args->{privMsg} : '');
	return if $who eq '' || !$char || $who eq $char->{name} || $who =~ /^#/;
	$mailNext = time + 8 if $lm =~ /\bsent\b|mailed|in ur mail|mailbox|check ur mail/ && $mailNext > time + 8;
	return unless $lm =~ /(\d+(?:[.,]\d+)?)\s*(k|m|z)\b/;
	my $price = price_of($1, $2);
	return if $price < 100;
	foreach my $e (plan_entries()) {
		next if owned($e);
		my $i = item_info($e->{id}) or next;
		(my $base = lc $i->{name}) =~ s/\s*\[\d+\]\s*$//;
		next unless index($lm, $base) >= 0;
		my $key = lc $who;
		if ($price > $e->{budget} || !can_spend($price + int($price / 50))) {
			Commands::run("pm \"$who\" " . pick_line("too much for me rn, sorry", "cant do " . kfmt($price) . " atm, sorry")) unless $offers{$key} && $offers{$key}{refused};
			$offers{$key} = {id => $e->{id}, item => $i->{name}, price => $price, at => time, refused => 1};
			deal_log("REFUSED $who: $i->{name} for ${price}z (budget $e->{budget}z, zeny $char->{zeny})");
			return;
		}
		if (my $tu = turn_unmet()) { # a friend's gear turn comes first (main 2026-11-14)
			Commands::run("pm \"$who\" " . pick_line("not now sorry, saving up for a friend's gear", "maybe later, my zeny goes to a friend's gear first")) unless $offers{$key} && $offers{$key}{refused};
			$offers{$key} = {id => $e->{id}, item => $i->{name}, price => $price, at => time, refused => 1};
			deal_log("REFUSED $who: $i->{name} for ${price}z - $tu->{name}'s turn ($tu->{next}) comes first");
			return;
		}
		my $shop = $lm =~ /shop|vend/ ? 1 : 0;
		$offers{$key} = {id => $e->{id}, item => $i->{name}, price => $price, at => time, shop => $shop};
		if ($shop) {
			Commands::run("pm \"$who\" ok ty, ill pass by ur shop next time im in prt");
			deal_log("OFFER $who (vend): $i->{name} for ${price}z - bought at the Prontera market on the next Prontera run");
		} else {
			Commands::run("pm \"$who\" " . pick_line("deal, mail it pls, i pay as soon as it arrives", "ok deal! send it, ill pay by mail"));
			deal_log("ACCEPTED $who: $i->{name} for ${price}z by mail");
			$mailNext = time + 60 if $mailNext > time + 60;
		}
		return;
	}
}

sub got_mail { my (undef, $args) = @_; $readMail{$args->{mailID}} = $args if $args && $args->{mailID} }

sub mail_end {
	my $why = shift;
	my $j = $mailjob;
	message "[aylagear] mailbox: $why\n", 'system' if $why;
	Commands::run('rodex close') if defined $rodexList;
	push @gives, @{$j->{pays}} if $j && $j->{pays};
	$mailjob = undef;
	$equipAt = 0;
	$quietUntil = time + 5;
}

sub mail_tick {
	if (!$mailjob) {
		return if time < $mailNext || $give || (@gives && !$gives[0]{notBefore});
		return if $run || $vbuy || AI::inQueue('attack', 'NPC', 'sellAuto', 'buyAuto', 'storageAuto', 'deal') || $char->{dead};
		$mailNext = time + 180;
		$mailjob = {stage => 0, at => time, pays => []};
	}
	my $j = $mailjob;
	if (time - $j->{at} > 20) { mail_end("no answer at step $j->{stage}"); return }
	return if $j->{wait} && time < $j->{wait};
	if ($j->{stage} == 0) {
		if (defined $rodexList) { Commands::run('rodex refresh') } else { Commands::run('rodex open') }
		@$j{qw(stage at wait)} = (1, time, time + 2);
	} elsif ($j->{stage} == 1) {
		return unless defined $rodexList;
		my $m = $rodexList->{mails} || {};
		$j->{todo} = [grep { !$mailDone{$_} && ($m->{$_}{attach} || '') =~ /^(i|z|z\+i|gift)$/ } sort keys %$m];
		if (!@{$j->{todo}}) { mail_end(''); return }
		@$j{qw(stage at)} = (2, time);
	} elsif ($j->{stage} == 2) {
		# 2026-11-14: reading a mail does not work on this server version (its answer, 0x0B63, is unknown to this
		# OpenKore: "Packet Parser: Unknown switch: 0B63", the step timed out and the box closed - the Creamy Card from
		# lunaqueen sat in the mailbox for an hour). The mail list already says who sent what (sender, title = the item
		# for humanwant sales, attachment type), and the price is the one agreed in the whisper: take it without reading.
		my $id = shift @{$j->{todo}};
		if (!defined $id) { mail_end('done'); return }
		my $mail = $rodexList->{mails}{$id} || {};
		my $from = $mail->{sender} || '?';
		my $title = $mail->{title} || '';
		my $att = $mail->{attach} || '';
		$j->{cur} = $id;
		# OpenKore's rodex commands read 1-3 digit arguments as the list index: give small ids by their index
		my $arg = $id > 999 ? $id : $mail->{page_index};
		if ($att eq 'z') {
			$mailDone{$id} = 1;
			Commands::run("rodex getzeny $arg");
			deal_log("GOT zeny by mail from $from ($title)");
			@$j{qw(stage at wait)} = (2, time, time + 2);
			return;
		}
		my $o = $offers{lc $from};
		my $isFriend = grep { $_ && $_->{name} eq $from } values %{($char->{party} && $char->{party}{users}) || {}};
		my $wpct = $char->{weight_max} ? 100 * $char->{weight} / $char->{weight_max} : 0;
		if ($o && !$o->{refused} && !$o->{shop}) {
			my $e = plan_entry_for_id($o->{id});
			my $cost = $o->{price} + int($o->{price} / 50);
			if (!$e || owned($e) || $wpct >= 85 || $cost + 1000 > ($char->{zeny} || 0)) {
				$mailDone{$id} = 1;
				deal_log("LEFT in the mailbox: $title from $from (agreed $o->{price}z) - " . (!$e ? 'not on her plan any more' : owned($e) ? 'already owned' : $wpct >= 85 ? "weight $wpct%" : "zeny $char->{zeny}"));
				@$j{qw(stage at wait)} = (2, time, time + 1);
				return;
			}
			$j->{deal} = {id => $o->{id}, item => $o->{item}, price => $o->{price}, from => $from, had => count_id($o->{id}), mail => $id};
			deal_log("TAKING $o->{item} from ${from}'s mail $id (agreed $o->{price}z)");
			Commands::run("rodex getitems $arg");
			Commands::run("rodex getzeny $arg") if $att eq 'z+i';
			@$j{qw(stage at wait)} = (3, time, time + 2);
			return;
		}
		if ($isFriend && $wpct < 85) { # a party friend's gift (no deal behind it)
			$mailDone{$id} = 1;
			Commands::run("rodex getitems $arg");
			Commands::run("rodex getzeny $arg") if $att eq 'z+i';
			deal_log("GOT $title from $from by mail (a gift)");
			Commands::run("pm \"$from\" ty for the $title :)");
			@$j{qw(stage at wait)} = (2, time, time + 2);
			return;
		}
		$mailDone{$id} = 1;
		deal_log("LEFT in the mailbox: $title from $from (no agreed deal)");
		@$j{qw(stage at wait)} = (2, time, time + 1);
	} elsif ($j->{stage} == 3) {
		# the item must really arrive before she pays
		my $d = $j->{deal};
		if (count_id($d->{id}) > $d->{had}) {
			$mailDone{$d->{mail}} = 1;
			delete $offers{lc $d->{from}};
			push @{$j->{pays}}, {to => $d->{from}, zeny => $d->{price}, why => $d->{item}, sale => 1};
			deal_log("GOT $d->{item} from $d->{from} by mail, paying $d->{price}z");
			$j->{deal} = undef;
			@$j{qw(stage at wait)} = (2, time, time + 1);
		} elsif (time - $j->{at} > 10) {
			deal_log("FAILED to take $d->{item} out of $d->{from}'s mail $d->{mail} (no item arrived) - retry next check");
			$j->{deal} = undef;
			@$j{qw(stage at wait)} = (2, time, time + 1);
		}
	}
}

# offers survive a plugin reload (package vars); after an OpenKore restart the accepted deals still waiting for their
# mail come back from logs/deals.log (ACCEPTED lines with no later GOT / LEFT for that seller and item)
sub offers_from_log {
	open(my $f, '<', 'logs/deals.log') or return;
	my %open;
	while (my $l = <$f>) {
		if ($l =~ /ACCEPTED (.+?): (.+?) for (\d+)z by mail/) { $open{lc $1} = {item => $2, price => $3, who => $1} }
		elsif ($l =~ /(?:GOT|LEFT in the mailbox:) (.+?) from (\S.*?)(?: by mail| \(|$)/) { my ($it, $w) = ($1, lc $2); delete $open{$w} if $open{$w} && $open{$w}{item} eq $it }
	}
	close $f;
	foreach my $k (keys %open) {
		next if $offers{$k};
		my ($e) = grep { my $i = item_info($_->{id}); $i && $i->{name} eq $open{$k}{item} } plan_entries();
		next unless $e;
		$offers{$k} = {id => $e->{id}, item => $open{$k}{item}, price => $open{$k}{price}, at => time};
		message "[aylagear] deal still open from the log: $open{$k}{who} $open{$k}{item} for $open{$k}{price}z\n", 'system';
	}
}
offers_from_log() if $char;

# ---------------------------------------------------------------- tick
my $last = 0;
sub tick {
	return if time - $last < 0.3;
	$last = time;
	return unless $net && $net->getState() == 5 && $char && $field;
	return if $char->{dead}; # nothing while dead (mail / equip / vendor actions would displace OpenKore's dead task)
	give_tick();
	gift_tick();
	deal_tick() if $dealjob;
	mail_tick() unless $give || $dealjob;
	vendor_tick() if $vbuy;
	equip_tick();
	trade_tick();
}

# owner rule (RULES 5, 2026-11-14 "remove the blocking panel on the screen. Must always remove those popups"): the owner's
# watch client (X-Kore 2) gets every server packet, so the mail she writes / reads or a vendor shop she opens popped up
# as a window over the game (the Write Mail window stayed open after a 'give'). While the plugin drives such an action,
# the server's answers for it are not passed to the client (OpenKore still reads them): no window ever opens.
sub quiet_now { return $give || $mailjob || $vbuy || $dealjob || time < $quietUntil }
sub quiet_name { my $n = shift || ''; return $n =~ /^rodex_/ || $n =~ /^vender_(items_list|buy_fail|found)$|^vending_/ || ($dealjob && $n =~ /^deal_/) }
# NPC dialogs, NPC shops and storage the bot uses on its own (town runs: sellAuto / buyAuto / storageAuto; the Kafra save)
sub quiet_npc { return time < $quietNpcUntil || AI::inQueue('sellAuto', 'buyAuto', 'storageAuto') }
sub npc_name { my $n = shift || ''; return $n =~ /^npc_(talk|store_|sell_list|image|market_)|^storage_(opened|closed|items_|item_added|item_removed)/ }
sub drop_it { my $n = shift; return (quiet_now() && quiet_name($n)) || (quiet_npc() && npc_name($n)) }
sub will_mangle { my (undef, $h) = @_; $h->{return} = 1 if drop_it($h->{name}) }
sub do_mangle { my (undef, $h) = @_; $h->{return} = 2 if drop_it($h->{messageName}) }

1;
