package main

// srvwarps.go - the warp graph from the npc files the map server really loads (PR16: one map truth for the whole
// Legion, OpenKore and engine alike).
//
// Before: loadNPCs walked every .txt under npc/re/warps and npc/warps. That missed every warp outside those folders
// (quest/other/city scripts: louyang 129,121 -> lou_in02, the auction halls, ein_fild01, lhz_in01 ...), every
// duplicate() of a warp and every plain OnTouch WARPNPC script (~250 non-instance warps), and it counted warps inside
// /* block comments */ that rAthena skips (aldebaran 35,140 was read as -> te_alde_gld, a gate commented out in
// npc/re/warps/guildcastles.txt; the server's warp there goes to alde_gld).
//
// Same rules as openkore/gen_portals_from_npc.py (which builds OpenKore's portals-srv.txt): files from
// npc/re/scripts_main.conf (npc:/import:/delete:/delnpc:), npc/scripts_custom.conf, then npc/custom-local/*.txt;
// `warp`/`warp2` lines, duplicate()s of them, and unconditional single-target OnTouch WARPNPC scripts. Keep the two
// in step. Falls back to the old folder walk when the image carries no scripts_main.conf.

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	srvConfLine = regexp.MustCompile(`^(npc|import|delete|delnpc):\s*(\S+)`)
	srvHdr      = regexp.MustCompile(`^([\w@-]+),(\d+),(\d+),\d+\t(warp2?|script|duplicate\(([^)]+)\)|shop|cashshop|itemshop|pointshop|marketshop|trader)\t([^\t]+)\t(.*)$`)
	srvWarpArgs = regexp.MustCompile(`^(\d+),(\d+),([\w@-]+),(\d+),(\d+)`)
	srvTouchHdr = regexp.MustCompile(`^(WARPNPC|45|HIDDEN_WARP_NPC|139),(\d+),(\d+),\{`)
	srvWarpCmd  = regexp.MustCompile(`warp\s+"([\w@-]+)",\s*(\d+),\s*(\d+)`)
)

// serverNPCFiles: the npc script files the map server loads, in load order (paths relative to root).
func serverNPCFiles(root string) []string {
	var files []string
	seen := map[string]bool{}
	var conf func(path string)
	conf = func(path string) {
		p := filepath.Join(root, path)
		if seen[path] {
			return
		}
		f, err := os.Open(p)
		if err != nil {
			return
		}
		seen[path] = true
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(strings.SplitN(sc.Text(), "//", 2)[0])
			m := srvConfLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			switch m[1] {
			case "npc":
				files = append(files, m[2])
			case "import":
				conf(m[2])
			default: // delete / delnpc: the first listing goes
				for i, v := range files {
					if v == m[2] {
						files = append(files[:i], files[i+1:]...)
						break
					}
				}
			}
		}
		f.Close()
	}
	conf("npc/re/scripts_main.conf")
	conf("npc/scripts_custom.conf")
	if ents, err := os.ReadDir(filepath.Join(root, "npc", "custom-local")); err == nil {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		for _, n := range names {
			files = append(files, "npc/custom-local/"+n)
		}
	}
	return files
}

// stripBlockComments blanks /* ... */ (newlines kept) the way rAthena's parser skips them; a /* inside a // comment
// or a "string" opens nothing. Same scanner as gen_portals_from_npc.py strip_block_comments.
func stripBlockComments(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	n, instr := len(text), false
	for i := 0; i < n; {
		c := text[i]
		switch {
		case instr:
			out.WriteByte(c)
			if c == '\\' && i+1 < n {
				out.WriteByte(text[i+1])
				i += 2
				continue
			}
			if c == '"' || c == '\n' {
				instr = false
			}
			i++
		case c == '"':
			instr = true
			out.WriteByte(c)
			i++
		case strings.HasPrefix(text[i:], "//"):
			e := strings.IndexByte(text[i:], '\n')
			if e < 0 {
				e = n - i
			}
			out.WriteString(text[i : i+e])
			i += e
		case strings.HasPrefix(text[i:], "/*"):
			e := strings.Index(text[i+2:], "*/")
			end := n
			if e >= 0 {
				end = i + 2 + e + 2
			}
			out.WriteString(strings.Repeat("\n", strings.Count(text[i:end], "\n")))
			i = end
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

// parseServerWarps reads the warps of the given files (relative to root), first definition of a source cell wins.
func parseServerWarps(root string, files []string) []Warp {
	type dest struct {
		m      string
		x, y   int
		xs, ys int
	}
	byName := map[string]dest{}
	seen := map[string]bool{}
	var out []Warp
	add := func(m string, x, y int, d dest) {
		key := m + "," + strconv.Itoa(x) + "," + strconv.Itoa(y)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Warp{From: m, X: x, Y: y, To: d.m, ToX: d.x, ToY: d.y, XS: d.xs, YS: d.ys})
	}
	for _, fn := range files {
		if !strings.HasSuffix(fn, ".txt") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, fn))
		if err != nil {
			continue
		}
		lines := strings.Split(stripBlockComments(string(raw)), "\n")
		for i := 0; i < len(lines); {
			line := strings.TrimRight(lines[i], "\r")
			i++
			h := srvHdr.FindStringSubmatch(line)
			if h == nil {
				continue
			}
			sm, typ, dup, name, rest := h[1], h[4], h[5], h[6], h[7]
			sx, _ := strconv.Atoi(h[2])
			sy, _ := strconv.Atoi(h[3])
			switch {
			case strings.HasPrefix(typ, "warp"):
				r := srvWarpArgs.FindStringSubmatch(rest)
				if r == nil {
					continue
				}
				xs, _ := strconv.Atoi(r[1])
				ys, _ := strconv.Atoi(r[2])
				dx, _ := strconv.Atoi(r[4])
				dy, _ := strconv.Atoi(r[5])
				d := dest{r[3], dx, dy, xs, ys}
				byName[name] = d
				add(sm, sx, sy, d)
			case dup != "":
				d, ok := byName[dup]
				if !ok {
					continue
				}
				// "sprite,xs,ys" or "xs,ys": the duplicate's own trigger span, else the original's
				if a := strings.Split(strings.TrimSpace(rest), ","); len(a) >= 2 {
					xs, e1 := strconv.Atoi(strings.TrimSpace(a[len(a)-2]))
					ys, e2 := strconv.Atoi(strings.TrimSpace(a[len(a)-1]))
					if e1 == nil && e2 == nil {
						d.xs, d.ys = xs, ys
					}
				}
				add(sm, sx, sy, d)
			case typ == "script":
				t := srvTouchHdr.FindStringSubmatch(rest)
				if t == nil {
					continue
				}
				var body []string
				for i < len(lines) && !strings.HasPrefix(lines[i], "}") {
					body = append(body, lines[i])
					i++
				}
				b := strings.Join(body, "\n")
				ws := srvWarpCmd.FindAllStringSubmatch(b, -1)
				if len(ws) == 0 || !strings.Contains(b, "OnTouch") || strings.Contains(b, "select") || strings.Contains(b, "if") {
					continue
				}
				one := true
				for _, w := range ws[1:] {
					if w[1] != ws[0][1] || w[2] != ws[0][2] || w[3] != ws[0][3] {
						one = false
					}
				}
				if !one {
					continue
				}
				dx, _ := strconv.Atoi(ws[0][2])
				dy, _ := strconv.Atoi(ws[0][3])
				xs, _ := strconv.Atoi(t[2])
				ys, _ := strconv.Atoi(t[3])
				d := dest{ws[0][1], dx, dy, xs, ys}
				byName[name] = d
				add(sm, sx, sy, d)
			}
		}
	}
	return out
}

// loadServerWarps fills warpsFrom from the server's loaded npc files; false (nothing loaded) = use the folder walk.
func loadServerWarps(root string) bool {
	files := serverNPCFiles(root)
	if len(files) == 0 {
		return false
	}
	ws := parseServerWarps(root, files)
	if len(ws) == 0 {
		return false
	}
	for _, w := range ws {
		warpsFrom[w.From] = append(warpsFrom[w.From], w)
	}
	log.Printf("warps: %d from %d npc files the map server loads (srvwarps.go)", len(ws), len(files))
	return true
}
