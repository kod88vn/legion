package main

// chatjobs_wait.go (2026-11-14, bug queue: chat jobs expired before the chat mind could answer). The deadline ran
// from the moment the engine queued a job (20-40 s), but the chat mind only lists jobs on its own schedule (at least
// 30 s apart) and a model call takes 10-40 s, so a listed job often had 4-6 s left and the answer came back 409.
//
//   - the wait is longer (chat_job_wait_min_sec..max_sec, default 45-90 s);
//   - a job the agent lists (GET /api/chat-jobs without peek) gets at least chat_job_listed_grace_sec more from the
//     listing, never beyond chatJobMaxAge after it was queued (the conversation must stay timely); kick lines keep
//     their deadline (the expel is sent chat_job_wait_max_sec+2 s after the line was queued);
//   - kinds in chat_job_code_kinds (default thanks, accept) are said by the code at once: their template is the
//     natural line and the agent adds nothing (PR38: code decides; the agent's time goes to the lines that need it);
//   - an answer that arrives after the deadline is not an error: the template (the natural fallback line) was
//     already said, so the API says so in a 200 reply instead of a 409.
//
// Lock: everything here takes only chatJobs.mu (a leaf lock) or reads tune().

import (
	"errors"
	"strings"
	"time"
)

const chatJobMaxAge = 3 * time.Minute

var errChatLate = errors.New("too late: the line was already said at its deadline")

// chatJobByCode: this job kind is said by the code at once, never held for the agent.
func chatJobByCode(kind string) bool {
	if kind == "" {
		return false
	}
	for _, k := range strings.Split(tune().ChatJobCodeKinds, ",") {
		if strings.EqualFold(strings.TrimSpace(k), kind) {
			return true
		}
	}
	return false
}

// chatJobsListed: the agent just listed the open jobs; each gets at least the grace from now (capped).
func chatJobsListed(now time.Time) {
	grace := time.Duration(tune().ChatJobListedGraceSec) * time.Second
	if grace <= 0 {
		return
	}
	chatJobs.Lock()
	defer chatJobs.Unlock()
	for _, j := range chatJobs.open {
		if j.Kind == ChatKindKick {
			continue // the expel follows the line after a fixed wait (guildchat.go dutyChatKick): never later than that
		}
		want := now.Add(grace)
		if lim := j.T.Add(chatJobMaxAge); want.After(lim) {
			want = lim
		}
		if want.After(j.Deadline) {
			j.Deadline = want
			chatJobs.counts["deadline extended on listing"]++
		}
	}
}

// chatJobLateLocked (chatJobs.mu held): the recent job with this id, if it was already resolved.
func chatJobLateLocked(id string) *ChatJob {
	for i := len(chatJobs.recent) - 1; i >= 0; i-- {
		if j := chatJobs.recent[i]; j.ID == id {
			chatJobs.counts["agent answer late"]++
			c := *j
			return &c
		}
	}
	return nil
}

// chatLateReply: the API answer for a late agent answer.
func chatLateReply(j *ChatJob) map[string]any {
	return map[string]any{"ok": false, "late": true, "id": j.ID, "state": j.State,
		"note": "already said at its deadline (" + j.State + "); nothing to do, answer newer jobs first"}
}
