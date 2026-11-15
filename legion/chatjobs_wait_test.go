package main

import (
	"testing"
	"time"
)

// a job listed late (deadline 5 s away) gets the grace from the listing, capped at chatJobMaxAge after it was queued
func TestChatJobListedGetsGrace(t *testing.T) {
	setupChatJobs(t, true)
	withTuning(t, func(tn *Tuning) {
		tn.ChatJobsOn, tn.ChatJobWaitMinSec, tn.ChatJobWaitMaxSec, tn.ChatJobListedGraceSec = true, 20, 20, 45
	})
	now := time.Now()
	chatJobsPolled(now)
	id := enqueueChatJobAt(spec("anyone got a clip?"), now)
	if id == "" {
		t.Fatal("the job waits")
	}
	listed := now.Add(15 * time.Second) // 5 s left
	chatJobsListed(listed)
	chatJobsPolled(listed)
	if due := chatJobsDue(now.Add(30 * time.Second)); len(due) != 0 {
		t.Fatal("a listed job must not expire 5 s after the listing")
	}
	if _, lines, err := chatAnswer(id, []ChatLine{{Speaker: "Asker", Text: "spare clip anyone?"}}, known, now.Add(50*time.Second)); err != nil || len(lines) != 1 {
		t.Fatalf("answer 35 s after listing is in time: %v", err)
	}
	// cap: a job listed again and again never waits beyond chatJobMaxAge
	id2 := enqueueChatJobAt(spec("b"), now)
	for s := 0; s <= 300; s += 30 {
		chatJobsPolled(now.Add(time.Duration(s) * time.Second))
		chatJobsListed(now.Add(time.Duration(s) * time.Second))
	}
	if due := chatJobsDue(now.Add(chatJobMaxAge + time.Second)); len(due) != 1 || due[0].ID != id2 {
		t.Fatalf("capped at max age: %+v", due)
	}
}

func TestChatJobListedGraceOff(t *testing.T) {
	setupChatJobs(t, true)
	withTuning(t, func(tn *Tuning) {
		tn.ChatJobsOn, tn.ChatJobWaitMinSec, tn.ChatJobWaitMaxSec, tn.ChatJobListedGraceSec = true, 20, 20, 0
	})
	now := time.Now()
	chatJobsPolled(now)
	enqueueChatJobAt(spec("a"), now)
	chatJobsListed(now.Add(15 * time.Second))
	chatJobsPolled(now.Add(15 * time.Second))
	if due := chatJobsDue(now.Add(21 * time.Second)); len(due) != 1 {
		t.Fatal("grace 0: the deadline stays")
	}
}

// PR38: thanks/accept lines are said by the code at once even with an agent polling
func TestChatJobCodeKinds(t *testing.T) {
	rec := setupChatJobs(t, true)
	withTuning(t, func(tn *Tuning) { tn.ChatJobsOn, tn.ChatJobCodeKinds = true, "thanks, accept" })
	now := time.Now()
	chatJobsPolled(now)
	s := spec("thanks!")
	s.Kind = ChatKindThanks
	if id := enqueueChatJobAt(s, now); id != "" {
		t.Fatal("a code kind is said at once")
	}
	rec.wait(t, 1)
	if chatJobs.counts["template: by code"] != 1 {
		t.Errorf("counter: %v", chatJobs.counts)
	}
	if id := enqueueChatJobAt(spec("ask"), now); id == "" {
		t.Error("an ask still waits for the agent")
	}
}

// an answer after the deadline: the fallback was said; the API reports late, not a conflict
func TestChatJobLateAnswer(t *testing.T) {
	setupChatJobs(t, true)
	now := time.Now()
	chatJobsPolled(now)
	id := enqueueChatJobAt(spec("a"), now)
	chatJobsPolled(now.Add(30 * time.Second))
	chatJobsDue(now.Add(41 * time.Second))
	j, _, err := chatAnswer(id, []ChatLine{{Speaker: "Asker", Text: "late"}}, known, now.Add(50*time.Second))
	if err != errChatLate || j == nil || j.State != "template" {
		t.Fatalf("late: %v %+v", err, j)
	}
	if r := chatLateReply(j); r["late"] != true {
		t.Error("late reply")
	}
	if _, _, err := chatAnswer("m999999", nil, known, now); err == nil || err == errChatLate {
		t.Error("an unknown id is still an error")
	}
}

func TestChatJobDefaultsLonger(t *testing.T) {
	if defaultTuning.ChatJobWaitMinSec < 45 || defaultTuning.ChatJobWaitMaxSec < 60 || defaultTuning.ChatJobListedGraceSec < 30 {
		t.Fatal("chat jobs must wait long enough for a model call")
	}
}

// a kick line keeps its deadline: the expel is sent a fixed time after it
func TestChatJobKickNotExtended(t *testing.T) {
	setupChatJobs(t, true)
	withTuning(t, func(tn *Tuning) {
		tn.ChatJobsOn, tn.ChatJobWaitMinSec, tn.ChatJobWaitMaxSec, tn.ChatJobListedGraceSec = true, 20, 20, 45
	})
	now := time.Now()
	chatJobsPolled(now)
	s := spec("removing x")
	s.Kind = ChatKindKick
	enqueueChatJobAt(s, now)
	chatJobsListed(now.Add(15 * time.Second))
	chatJobsPolled(now.Add(15 * time.Second))
	if due := chatJobsDue(now.Add(21 * time.Second)); len(due) != 1 {
		t.Fatal("kick line resolves by its own deadline")
	}
}
