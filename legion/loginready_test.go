package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAccessibleMapsErr(t *testing.T) {
	// 0x0840 with two entries: prontera down (status 1), izlude up (status 0)
	p := newPkt(0x0840).u16(0).u32(1).str("prontera.gat", 16).u32(0).str("izlude.gat", 16)
	err := accessibleMapsErr(p.varlen())
	s := err.Error()
	if !strings.HasPrefix(s, "char select: map server not ready (0840") || !strings.Contains(s, "1 maps unavailable") || !strings.Contains(s, "prontera,izlude") {
		t.Fatalf("got %q", s)
	}
	if !serverStarting(err) {
		t.Fatal("0840 should count as server starting")
	}
	if mapServerFull(err) {
		t.Fatal("0840 must not trigger the leader bump (mapServerFull)")
	}
	// empty list still gives a clear reason
	if s := accessibleMapsErr(newPkt(0x0840).u16(0).varlen()).Error(); s != "char select: map server not ready (0840)" {
		t.Fatalf("empty: %q", s)
	}
}

func TestLoginRefusedErr(t *testing.T) {
	e1 := loginRefusedErr(0x0081, 1)
	if !strings.HasPrefix(e1.Error(), "login refused (0081, code 1") || !serverStarting(e1) {
		t.Fatalf("code 1: %q", e1)
	}
	e8 := loginRefusedErr(0x0081, 8)
	if serverStarting(e8) || !strings.Contains(e8.Error(), "code 8: server still recognises") {
		t.Fatalf("code 8: %q", e8)
	}
	if s := loginRefusedErr(0x0081, 99).Error(); s != "login refused (0081, code 99)" {
		t.Fatalf("unknown: %q", s)
	}
	if !serverStarting(errors.New("dial tcp 127.0.0.1:6900: connect: connection refused")) || serverStarting(nil) {
		t.Fatal("dial refused")
	}
}

func TestNoteLoginOKMovesLastErr(t *testing.T) {
	b := &Bot{LastErr: "login refused (0081, code 1: server closed (char server not up yet))"}
	now := time.Unix(1794711155, 0)
	b.noteLoginOK(now)
	if b.LastErr != "" || !strings.Contains(b.PrevErr, "0081") || !b.PrevErrAt.Equal(now) {
		t.Fatalf("got last=%q prev=%q at=%v", b.LastErr, b.PrevErr, b.PrevErrAt)
	}
	// a clean login keeps the earlier PrevErr
	b.noteLoginOK(now.Add(time.Hour))
	if b.PrevErr == "" || !b.PrevErrAt.Equal(now) {
		t.Fatal("clean login wiped PrevErr")
	}
	if prevErrAt(time.Time{}) != "" || prevErrAt(now) == "" {
		t.Fatal("prevErrAt")
	}
}
