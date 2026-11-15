package main

// meterpersist.go: the meter's counters survive an engine restart (deploy, crash). Before this every deploy wiped all
// kills/min, zeny/h and time budgets (they live only in each Bot's b.meter): a league run 5 min after the 07:45 deploy
// had n=0 in every cohort (a bot counts in the 1-hour window only after 15 online minutes in it), and the hourly
// meter.jsonl row after a deploy held only the minutes since it.
//
// The meter goroutine writes <data>/meter_state.json.gz (core instance: meter_state.core.json.gz) every
// The snapshot runs every meter_persist_sec (tuning, 300 s): each metered bot's last 60 one-minute slots, its session total and start, and Ayla's samples. The
// write is atomic (temp file in the same directory, fsync, rename): a reader or a crash never sees half a file.
// main() loads it once before any bot starts (meterRestoreLoad); a bot's saved counters are put back the first time it
// logs in with that character (meterLogin -> meterRestoreInto). Time while the engine was down is not online time
// (no tick), so a restart does not dilute kills/min; slots older than 60 min fall out on their own. Counters of bots
// that have not logged in again yet are carried into the next snapshot unchanged, so a slow start loses nothing.
//
// Lock discipline as meter.go: meterRestoreInto runs with b.mu held and touches only sync.Map; the snapshot copies
// one bot at a time under its own lock; files are written by the meter goroutine only.

import (
	"compress/gzip"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	meterSessKeep     = 2 * time.Hour // a snapshot older than this restores the 1-hour slots only, not the session
	meterStateVersion = 1
)

type meterSaveBot struct {
	Char string      `json:"c"`
	From int64       `json:"f"` // session start (unix seconds)
	Sess meterSlot   `json:"s"`
	Ring []meterSlot `json:"r"` // slots of the last 60 minutes (empty ones left out)
}

type meterSaveAyla struct {
	T      int64  `json:"t"`
	Zeny   int    `json:"z"`
	Bucket string `json:"b"`
	Lv     int    `json:"l"`
	Online bool   `json:"o"`
	Kills  int64  `json:"k"`
	KSince int64  `json:"ks"`
}

type meterSaveFile struct {
	V        int             `json:"v"`
	Saved    int64           `json:"saved"`
	Instance string          `json:"instance"`
	Bots     []meterSaveBot  `json:"bots"`
	Ayla     []meterSaveAyla `json:"ayla"`
	AylaSess aylaWin         `json:"ayla_sess"`
}

// meterPending: lower-case char name -> *meterSaveBot loaded at start and not yet put back into a bot.
var meterPending sync.Map

// meterPendingSaved: when the loaded snapshot was written (decides whether the session total is restored).
var meterPendingSaved time.Time

func meterStatePath() string {
	name := "meter_state.json.gz"
	if coreInstance() {
		name = "meter_state.core.json.gz"
	}
	return filepath.Join(*dataDir, name)
}

// meterRestoreLoad: once in main() before any bot runs. A missing or broken file starts empty.
func meterRestoreLoad() {
	// a crash between the temp write and the rename leaves ".meter_state*.tmp" (several MB): removed at start. Only
	// this instance's own base name, so the core never deletes the main's temp file or the reverse.
	if tmps, _ := filepath.Glob(filepath.Join(*dataDir, "."+filepath.Base(meterStatePath())+".*.tmp")); len(tmps) > 0 {
		for _, p := range tmps {
			os.Remove(p)
		}
		log.Printf("meter: removed %d orphan temp file(s)", len(tmps))
	}
	f, err := meterReadState(meterStatePath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("meter: state not restored: %v", err)
		}
		return
	}
	n := meterApplyState(f, time.Now())
	log.Printf("meter: restored counters of %d characters from %s (saved %s ago)", n, filepath.Base(meterStatePath()),
		time.Since(time.Unix(f.Saved, 0)).Round(time.Second))
}

func meterReadState(path string) (*meterSaveFile, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	gz, err := gzip.NewReader(fh)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	var f meterSaveFile
	if err := json.NewDecoder(gz).Decode(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

// meterApplyState fills meterPending and Ayla's samples from a snapshot; returns the number of characters pending.
func meterApplyState(f *meterSaveFile, now time.Time) int {
	if f == nil || f.V != meterStateVersion {
		return 0
	}
	saved := time.Unix(f.Saved, 0)
	if now.Sub(saved) > meterSessKeep {
		return 0 // nothing in it is still inside any window
	}
	meterPendingSaved = saved
	n := 0
	for i := range f.Bots {
		sb := f.Bots[i]
		if sb.Char == "" {
			continue
		}
		meterPending.Store(strings.ToLower(sb.Char), &sb)
		n++
	}
	a := &meterAylaS
	for _, x := range f.Ayla {
		t := time.Unix(x.T, 0)
		if now.Sub(t) > time.Hour {
			continue
		}
		a.samples = append(a.samples, aylaSample{t: t, zeny: x.Zeny, bucket: x.Bucket, lv: x.Lv, online: x.Online, kills: x.Kills, ksince: x.KSince})
	}
	if now.Sub(saved) <= meterSessKeep {
		a.sess = f.AylaSess
		if a.sess.Sec == nil {
			a.sess.Sec = map[string]float64{}
		}
	}
	return n
}

// meterRestoreInto (b.mu held, counters just reset for char): puts back the saved counters of that character.
func meterRestoreInto(c *meterCounters, char string, now time.Time) bool {
	v, ok := meterPending.LoadAndDelete(strings.ToLower(char))
	if !ok {
		return false
	}
	sb := v.(*meterSaveBot)
	nowMin := now.Unix() / 60
	for _, s := range sb.Ring {
		if s.Min > nowMin-meterRingMin && s.Min <= nowMin {
			c.ring[int(s.Min%meterRingMin)] = s
		}
	}
	if now.Sub(meterPendingSaved) <= meterSessKeep && sb.From > 0 {
		c.sess, c.sessFrom = sb.Sess, time.Unix(sb.From, 0)
	}
	return true
}

// meterSaveOf copies one bot's counters under its lock (ok=false: never metered).
func meterSaveOf(b *Bot, nowMin int64) (meterSaveBot, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := &b.meter
	if c.sessFrom.IsZero() || c.char == "" {
		return meterSaveBot{}, false
	}
	sb := meterSaveBot{Char: c.char, From: c.sessFrom.Unix(), Sess: c.sess}
	for _, s := range c.ring {
		if s.Min > nowMin-meterRingMin && s.Min <= nowMin {
			sb.Ring = append(sb.Ring, s)
		}
	}
	return sb, true
}

// meterPersist (meter goroutine): one snapshot of every bot plus the still pending ones, written atomically.
func meterPersist(bots []*Bot, now time.Time) {
	nowMin := now.Unix() / 60
	f := meterSaveFile{V: meterStateVersion, Saved: now.Unix(), Instance: "main"}
	if coreInstance() {
		f.Instance = "core"
	}
	have := map[string]bool{}
	for _, b := range bots {
		if sb, ok := meterSaveOf(b, nowMin); ok && !have[strings.ToLower(sb.Char)] {
			have[strings.ToLower(sb.Char)] = true
			f.Bots = append(f.Bots, sb)
		}
	}
	meterPending.Range(func(k, v interface{}) bool {
		if sb := v.(*meterSaveBot); !have[k.(string)] {
			keep := *sb
			keep.Ring = nil
			for _, s := range sb.Ring {
				if s.Min > nowMin-meterRingMin {
					keep.Ring = append(keep.Ring, s)
				}
			}
			if len(keep.Ring) > 0 || now.Sub(meterPendingSaved) <= meterSessKeep {
				f.Bots = append(f.Bots, keep)
			}
		}
		return true
	})
	for _, s := range meterAylaS.samples {
		f.Ayla = append(f.Ayla, meterSaveAyla{T: s.t.Unix(), Zeny: s.zeny, Bucket: s.bucket, Lv: s.lv, Online: s.online, Kills: s.kills, KSince: s.ksince})
	}
	f.AylaSess = meterAylaS.sess
	if err := meterWriteState(meterStatePath(), &f); err != nil {
		log.Printf("meter: state not saved: %v", err)
	}
}

// meterWriteState: gzip JSON to a temp file in the same directory, fsync, rename over the old file.
func meterWriteState(path string, f *meterSaveFile) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	gz := gzip.NewWriter(tmp)
	if err = json.NewEncoder(gz).Encode(f); err != nil {
		return err
	}
	if err = gz.Close(); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), 0644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
