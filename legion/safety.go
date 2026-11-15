package main

import (
	"log"
	"runtime/debug"
	"sync"
	"time"
)

// Safety net: one bot's panic must never take down the whole service (2026-11-14: an index out of range in
// travel() <- saveDetour() <- think() crashed legion and logged out ~785 bots). Each per-bot loop calls its tick
// through safely(): a panic is logged with the bot's name and stack, that bot's navigation state is reset, and the
// loop goes on.
//
// Locking: think() and handle() take b.mu with `defer b.mu.Unlock()`, so by the time the recover below runs the
// deferred unlock has already released it. The reset takes b.mu with TryLock (short retry) so a lock that is
// somehow still held can never deadlock the loop.

var panicCounts sync.Map // *Bot -> *int (guarded by panicMu)
var panicMu sync.Mutex

func (b *Bot) safely(what string, f func()) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		panicMu.Lock()
		v, _ := panicCounts.LoadOrStore(b, new(int))
		n := v.(*int)
		*n++
		count := *n
		panicMu.Unlock()
		name := "?"
		if b.P != nil {
			name = b.P.Name
		}
		if count <= 3 || count%100 == 0 { // a bot panicking every tick must not flood the log
			log.Printf("PANIC (recovered, #%d) bot %s in %s: %v\n%s", count, name, what, r, debug.Stack())
		}
		for i := 0; i < 20; i++ {
			if b.mu.TryLock() {
				b.resetAfterPanic()
				b.mu.Unlock()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		log.Printf("PANIC bot %s: could not take its lock to reset its state", name)
	}()
	f()
}

// resetAfterPanic (b.mu held): drop every in-progress plan so the next tick starts clean.
func (b *Bot) resetAfterPanic() {
	b.route, b.path = nil, nil
	b.trip, b.kafra = nil, nil
	b.viaTown, b.viaFor = "", ""
	b.target = 0
	b.Goal = Goal{}
}
