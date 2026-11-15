package main

// kafraload.go - Kafra teleport destinations from the server's own F_KafSet / F_KafTele (PR16: one map truth). The
// table in kafra.go stays as the fallback; at start the engine reads npc/kafras/functions_kafras.txt (the Legion copy
// rathena/npc-custom/functions_kafras.txt, mounted over the stock file for rathena and the legion services) and,
// when it parses, uses it, logging every difference to the built-in table. A price or destination change on the
// server then reaches the bots with the next start instead of drifting.

import (
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	kafMapIf   = regexp.MustCompile(`\.@map\$\s*==\s*"([\w@-]+)"`)
	kafNames   = regexp.MustCompile(`setarray\s+@wrpD\$\[0\]\s*,\s*(.*);`)
	kafPrices  = regexp.MustCompile(`setarray\s+@wrpP\[0\]\s*,\s*(.*);`)
	kafTeleIf  = regexp.MustCompile(`@wrpD\$\[\.@j\]\s*==\s*"([^"]+)"`)
	kafTeleTo  = regexp.MustCompile(`warp\s+"([\w@-]+)"\s*,\s*(\d+)\s*,\s*(\d+)`)
	kafQuoted  = regexp.MustCompile(`"([^"]*)"`)
	kafFuncHdr = regexp.MustCompile(`^function\s+script\s+(\w+)\s*\{`)
)

// parseKafraTable: town -> destinations from functions_kafras.txt text; nil when it doesn't parse.
func parseKafraTable(text string) map[string][]kafraDest {
	text = stripBlockComments(text)
	funcs := map[string][]string{}
	cur := ""
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		if m := kafFuncHdr.FindStringSubmatch(l); m != nil {
			cur = m[1]
			continue
		}
		if cur != "" {
			if strings.HasPrefix(l, "}") {
				cur = ""
				continue
			}
			if t := strings.TrimSpace(l); !strings.HasPrefix(t, "//") {
				funcs[cur] = append(funcs[cur], l)
			}
		}
	}
	// F_KafTele: menu name -> landing (the first warp after the name's test: the Renewal branch of checkre)
	land := map[string]kafraDest{}
	pending := ""
	for _, l := range funcs["F_KafTele"] {
		if m := kafTeleIf.FindStringSubmatch(l); m != nil {
			pending = m[1]
		}
		if m := kafTeleTo.FindStringSubmatch(l); m != nil && pending != "" {
			if _, ok := land[pending]; !ok {
				x, _ := strconv.Atoi(m[2])
				y, _ := strconv.Atoi(m[3])
				land[pending] = kafraDest{Name: pending, Map: m[1], X: x, Y: y}
			}
			pending = ""
		}
	}
	out := map[string][]kafraDest{}
	town := ""
	var names []string
	var prices []int
	flush := func() {
		if town == "" || len(names) == 0 || len(names) != len(prices) {
			return
		}
		var ds []kafraDest
		for i, n := range names {
			d, ok := land[n]
			if !ok {
				return // a destination without a landing: don't trust this town's list
			}
			d.Price = prices[i]
			ds = append(ds, d)
		}
		out[town] = ds
	}
	for _, l := range funcs["F_KafSet"] {
		if m := kafMapIf.FindStringSubmatch(l); m != nil {
			flush()
			town, names, prices = m[1], nil, nil
		}
		if m := kafNames.FindStringSubmatch(l); m != nil {
			names = nil
			for _, q := range kafQuoted.FindAllStringSubmatch(m[1], -1) {
				names = append(names, q[1])
			}
		}
		if m := kafPrices.FindStringSubmatch(l); m != nil {
			prices = nil
			for _, p := range strings.Split(m[1], ",") {
				n, err := strconv.Atoi(strings.TrimSpace(p))
				if err != nil {
					prices = nil
					break
				}
				prices = append(prices, n)
			}
		}
	}
	flush()
	if len(out) < 5 {
		return nil
	}
	return out
}

// kafraTableDiff: human-readable differences between two tables (empty = same).
func kafraTableDiff(a, b map[string][]kafraDest) []string {
	var diffs []string
	towns := map[string]bool{}
	for t := range a {
		towns[t] = true
	}
	for t := range b {
		towns[t] = true
	}
	var ts []string
	for t := range towns {
		ts = append(ts, t)
	}
	sort.Strings(ts)
	for _, t := range ts {
		x, y := a[t], b[t]
		same := len(x) == len(y)
		for i := 0; same && i < len(x); i++ {
			same = x[i] == y[i]
		}
		if !same {
			diffs = append(diffs, t)
		}
	}
	return diffs
}

// loadKafraTable: use the server's Kafra table when it parses (the server's is the truth).
func loadKafraTable(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Printf("kafra: %s not readable (%v): built-in destination table", path, err)
		return
	}
	if !strings.Contains(string(raw), "Legion copy") {
		// the stock file (official prices) means the compose mount of rathena/npc-custom/functions_kafras.txt is
		// missing; the map server runs the Legion copy, so the built-in table is closer to the truth
		log.Printf("kafra: %s is the stock file, not the Legion copy the server runs: built-in destination table", path)
		return
	}
	t := parseKafraTable(string(raw))
	if t == nil {
		log.Printf("kafra: %s did not parse: built-in destination table", path)
		return
	}
	// only towns where the engine knows the Kafra Employees' spots are usable
	for town := range t {
		if len(kafraSpots[town]) == 0 {
			delete(t, town)
		}
	}
	if d := kafraTableDiff(kafraDests, t); len(d) > 0 {
		log.Printf("kafra: server table differs from the built-in one for %v: using the server's", d)
	}
	kafraDests = t
}
