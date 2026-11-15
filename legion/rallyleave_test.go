package main

import (
	"testing"
	"time"
)

func TestHumanLeftRally(t *testing.T) {
	now := time.Now()
	old := now.Add(-time.Minute)
	if !humanLeftRally("payon", "pay_arche", old, now) {
		t.Fatal("she left payon for the field: the rally ends")
	}
	if humanLeftRally("payon", "payon", old, now) || humanLeftRally("payon", "payon_in01", old, now) {
		t.Fatal("in the town or one of its buildings: the rally goes on")
	}
	if humanLeftRally("payon", "pay_arche", now.Add(-5*time.Second), now) {
		t.Fatal("within the grace: no end yet")
	}
}
