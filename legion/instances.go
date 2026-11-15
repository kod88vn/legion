package main

// Two Legion instances share the server (owner: "why are you restarting server all the time? This disrupts too
// often"): a deploy of Legion logs out every bot it runs, so the bots of the owner's core test party (Ayla's
// friends) run in their own small instance that is redeployed only when party code changes.
//
//	main:  legion run ... -exclude "Zendancer84,Echobunny,Zen.fox"        API :8090 (dashboard, minds bus)
//	core:  legion run -only "Zendancer84,Echobunny,Zen.fox" -api 127.0.0.1:8091 -pprof ""
//
// Who owns what in the shared /data:
//   - human-parties.json: the instance without -exclude (the core one) restores and writes it; the main instance
//     neither restores nor writes it, so it never forms or touches that party.
//   - guild_roster.json, minds.json, guild leaders: main only (the core instance doesn't serve the minds bus and
//     doesn't pick guild leaders).
//   - memory/<account>.json: per account, each instance writes only its own bots.
//   - tuning.json: read by both.
// The main instance forwards /api/human-party and /api/bots/<core member> to the core instance and merges the core
// instance's human-led parties into /api/parties/detail (partydash.go), so tools and the dashboard keep using :8090.

import (
	"flag"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

var (
	onlyNames    = flag.String("only", "", "run: run only the profiles that have a character with one of these names (comma-separated)")
	excludeNames = flag.String("exclude", "", "run: skip the profiles that have a character with one of these names (comma-separated)")
	coreAPI      = flag.String("core-api", "", "run: API of the core instance to merge/forward human-party data from (default http://127.0.0.1:8091 when -exclude is set)")
	pprofAddr    = flag.String("pprof", "127.0.0.1:6060", "run: net/http/pprof listen address (empty: off)")
)

// coreInstance: this instance runs only a chosen few profiles (-only), e.g. the owner's core test party.
func coreInstance() bool { return *onlyNames != "" }

// ownsHumanParties: this instance restores and saves human-parties.json (not the main instance running -exclude).
func ownsHumanParties() bool { return *excludeNames == "" }

func coreAPIURL() string {
	if *coreAPI != "" {
		return strings.TrimRight(*coreAPI, "/")
	}
	if *excludeNames != "" {
		return "http://127.0.0.1:8091"
	}
	return ""
}

func nameSet(csv string) map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Split(csv, ",") {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			m[n] = true
		}
	}
	return m
}

func personaHasChar(p *Persona, set map[string]bool) bool {
	for _, c := range p.Chars {
		if set[strings.ToLower(c.Name)] {
			return true
		}
	}
	return false
}

// selectProfiles applies -only / -exclude. -only picks from all personas (ignoring -n/-offset, so the core
// members are found wherever they are in the list); -exclude filters the already-sliced list.
func selectProfiles(all, sliced []*Persona) []*Persona {
	var out []*Persona
	switch {
	case *onlyNames != "":
		set := nameSet(*onlyNames)
		for _, p := range all {
			if personaHasChar(p, set) {
				out = append(out, p)
			}
		}
	case *excludeNames != "":
		set := nameSet(*excludeNames)
		for _, p := range sliced {
			if !personaHasChar(p, set) {
				out = append(out, p)
			}
		}
	default:
		return sliced
	}
	return out
}

// coreForward wraps the main instance's API: requests about the core instance's bots go to the core instance.
func coreForward(next http.Handler) http.Handler {
	base := coreAPIURL()
	if base == "" {
		return next
	}
	u, err := url.Parse(base)
	if err != nil {
		return next
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "core instance unreachable: "+err.Error(), http.StatusBadGateway)
	}
	core := nameSet(*excludeNames)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// aylaoverride.go: the owner overrides of Ayla live with her party on the core instance, which alone reads and
		// writes their store (all methods)
		if p == "/api/ayla/overrides" || strings.HasPrefix(p, "/api/ayla/overrides/") {
			proxy.ServeHTTP(w, r)
			return
		}
		if p == "/api/human-party" || p == "/api/party-turn" { // lfg_human.go turns live on the core instance
			proxy.ServeHTTP(w, r)
			return
		}
		for _, pre := range []string{"/api/bots/", "/api/memory/"} {
			if !strings.HasPrefix(p, pre) {
				continue
			}
			name := strings.SplitN(strings.TrimPrefix(p, pre), "/", 2)[0]
			if n, err := url.PathUnescape(name); err == nil && handoffCoreName(core, n) { // handoff.go: live moves
				proxy.ServeHTTP(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
