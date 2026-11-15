package main

import "testing"

func TestRestFollowNeeded(t *testing.T) {
	if !restFollowNeeded("pay_dun02", "pay_dun03") || restFollowNeeded("pay_dun02", "pay_dun02") || restFollowNeeded("pay_dun02", "") {
		t.Fatal("follow first only when the leader is on another (known) map")
	}
}
