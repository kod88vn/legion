#!/usr/bin/env python3
"""Hard gate for Claude Code sessions on this project (owner: "the llm minds are the core of the server, the watch
dog is supposed to watch out for them and keep them alive. I need a hard gate to make sure you don't forget").

Checks the Legion minds bus (GET http://127.0.0.1:8090/api/minds). A required mind not seen for > 5 minutes (or
never) is DOWN.
  mindgate.py prompt  -> UserPromptSubmit hook: injects a warning into the model's context listing down minds.
  mindgate.py stop    -> Stop hook: blocks the turn from ending while required minds are down (once per stop chain,
                         stop_hook_active prevents loops).
Wired in G:\\RO\\.claude\\settings.json and G:\\RO\\legion\\.claude\\settings.json.
"""
import json, os, sys, urllib.request

REQUIRED = ["ayla", "chat", "life", "market", "watchdog"] + ["guild%02d" % i for i in range(1, 11)]
STALE_S = 300


def down_minds():
    try:
        with urllib.request.urlopen("http://127.0.0.1:8090/api/minds", timeout=4) as r:
            minds = {m.get("name"): m for m in json.load(r)}
    except Exception as e:
        return None, "Legion API unreachable (%s) - is ro-legion running?" % e
    down = []
    for n in REQUIRED:
        m = minds.get(n)
        if not m or m.get("seen_ago_s") is None or m.get("seen_ago_s", 1e9) < 0 or m.get("seen_ago_s", 1e9) > STALE_S:
            ago = (m or {}).get("seen_ago_s")
            down.append("%s (%s)" % (n, "never seen" if ago is None or ago < 0 else "silent %d min" % (ago // 60)))
    return down, ""


def main():
    # Pause flag: while a file "mindgate.paused" sits next to this script the gate is silent (exit 0, no output).
    if os.path.exists(os.path.join(os.path.dirname(os.path.abspath(__file__)), "mindgate.paused")):
        sys.exit(0)
    mode = sys.argv[1] if len(sys.argv) > 1 else "prompt"
    try:
        inp = json.load(sys.stdin)
    except Exception:
        inp = {}
    down, err = down_minds()
    if err:
        msg = "MIND GATE: " + err
    elif down:
        msg = ("MIND GATE: the LLM minds are the core of this server and these required minds are DOWN: " + ", ".join(down) +
               ". Before anything else, restart each one as a background Haiku agent following its skill in "
               "G:\\RO\\legion\\.claude\\skills (legion-chat, legion-life, legion-market, legion-guild; Ayla's mind brief "
               "and the watchdog brief are in G:\\RO\\legion\\HANDOVER.md), and keep a watchdog agent running that reports "
               "minds going silent.")
    else:
        sys.exit(0)
    if mode == "stop":
        if inp.get("stop_hook_active"):
            print(json.dumps({"systemMessage": msg}))
            sys.exit(0)
        print(json.dumps({"decision": "block", "reason": msg}))
        sys.exit(0)
    print(json.dumps({"hookSpecificOutput": {"hookEventName": "UserPromptSubmit", "additionalContext": msg},
                      "systemMessage": msg}))
    sys.exit(0)


if __name__ == "__main__":
    main()
