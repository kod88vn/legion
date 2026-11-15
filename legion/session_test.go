package main

import (
	"bufio"
	"errors"
	"net"
	"testing"
	"time"
)

// 2026-11-14 11:34Z: a session ended (logoutSignal) while readLoop handled a buffered packet; readLoop then re-read
// b.conn, which run() had set to nil -> SIGSEGV in (*Conn).Read(nil), legion down. readLoop now owns its conn.

func pipeConn() (*Conn, net.Conn) {
	a, z := net.Pipe()
	return &Conn{c: a, r: bufio.NewReaderSize(a, 16384)}, z
}

func waitDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit")
		return nil
	}
}

func TestReadLoopNilConn(t *testing.T) {
	b := &Bot{P: &Persona{Name: "SessT"}}
	done := make(chan error, 1)
	b.readLoop(nil, make(chan struct{}), done)
	if err := waitDone(t, done); err == nil {
		t.Fatal("want an error for a nil conn")
	}
}

func TestReadLoopSessionEndsWhileBufferedPacket(t *testing.T) {
	const op = 0xfff0
	pktLen[op] = 4
	defer delete(pktLen, op)
	c, peer := pipeConn()
	defer peer.Close()
	b := &Bot{P: &Persona{Name: "SessT"}}
	b.wmu.Lock()
	b.conn = c
	b.wmu.Unlock()
	done := make(chan error, 1)
	quit := make(chan struct{})
	go b.readLoop(c, quit, done)
	close(quit)
	b.wmu.Lock()
	b.conn = nil // what run() does at session end - the old readLoop re-read this and crashed
	b.wmu.Unlock()
	go peer.Write([]byte{0xf0, 0xff, 0, 0}) // a packet arriving after the session ended
	if err := waitDone(t, done); !errors.Is(err, errSessionEnded) {
		t.Fatalf("want errSessionEnded, got %v", err)
	}
	if b.pktsIn != 0 {
		t.Fatalf("a packet was handled after the session ended")
	}
	c.Close()
}

func TestEndSessionUnblocksReadAndKeepsNewerConn(t *testing.T) {
	c, peer := pipeConn()
	defer peer.Close()
	b := &Bot{P: &Persona{Name: "SessT"}}
	done := make(chan error, 1)
	quit := make(chan struct{})
	aiDone := make(chan struct{})
	close(aiDone)
	go b.readLoop(c, quit, done)
	newer, peer2 := pipeConn()
	defer peer2.Close()
	b.conn = newer // another conn is current: ending the old session must not clear it
	close(quit)
	b.endSession(c)
	start := time.Now()
	b.waitSessionLoops(done, false, aiDone)
	if time.Since(start) > 2*time.Second {
		t.Fatal("read loop not unblocked by endSession")
	}
	if b.conn != newer {
		t.Fatal("endSession cleared a conn that isn't its own")
	}
	newer.Close()
}

func TestRunOnlyOncePerBot(t *testing.T) {
	b := &Bot{P: &Persona{Name: "SessT"}}
	b.running.Store(true) // a run loop is active
	fin := make(chan struct{})
	go func() {
		b.run("127.0.0.1:1", make(chan struct{}))
		close(fin)
	}()
	select {
	case <-fin:
	case <-time.After(2 * time.Second):
		t.Fatal("second run loop started")
	}
	if !b.running.Load() {
		t.Fatal("the refused run cleared the active loop's flag")
	}
}
