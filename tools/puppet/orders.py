#!/usr/bin/env python3
"""Owner orders for Ayla (rule AY20): the owner tells Ayla something in plain words, her Haiku helper carries it out at
once through her guarded ayla.sh commands, ahead of everything else.

The orders file (legion/data/ayla_orders.jsonl, append-only, one JSON object per line):
  {"kind": "order",  "id": "o1114153012a3", "t": 1800000000.0, "iso": "...", "from": "owner", "text": "do a town run"}
  {"kind": "result", "id": "o1114153012a3", "t": ..., "iso": "...", "by": "ayla-helper", "result": "townrun started"}
An order is open while no result row carries its id. Every result is also written to her log
(openkore-ayla/logs/owner_orders.log) as "[owner] <order> -> <action>".

  python3 tools/puppet/orders.py add "do a town run with the party"     # (ayla.sh tell)
  python3 tools/puppet/orders.py done <id> "rally prontera, party told"  # (ayla.sh done; the helper reports)
  python3 tools/puppet/orders.py list [N]                                # open orders + last N results (ayla.sh orders)
Env AYLA_ORDERS / AYLA_ORDERS_LOG override the two paths (tests). Standard library only.
"""
import json
import os
import random
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
MAX_TEXT = 400
sys.path.insert(0, os.path.join(HERE, "..", "system"))
import common as C  # noqa: E402  the one resolver for live locations (rule G23)


def orders_path():
    if os.environ.get("AYLA_ORDERS"):
        return C.refuse_live(os.environ["AYLA_ORDERS"], "AYLA_ORDERS")
    return C.refuse_live(os.path.join(C.data_dir(), "ayla_orders.jsonl"), "the orders file")


def log_path():
    if os.environ.get("AYLA_ORDERS_LOG"):
        return C.refuse_live(os.environ["AYLA_ORDERS_LOG"], "AYLA_ORDERS_LOG")
    return C.refuse_live(os.path.join(REPO, "openkore-ayla", "logs", "owner_orders.log"), "Ayla's owner log")


def clean(text):
    """one line, no control characters, bounded"""
    s = "".join(c if c >= " " else " " for c in str(text or ""))
    return " ".join(s.split())[:MAX_TEXT]


def read(path=None):
    rows = []
    try:
        with open(path or orders_path(), encoding="utf-8") as f:
            for line in f:
                try:
                    r = json.loads(line)
                except ValueError:
                    continue
                if isinstance(r, dict) and r.get("id"):
                    rows.append(r)
    except OSError:
        pass
    return rows


def _append(row, path):
    os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
    with open(path, "a", encoding="utf-8") as f:
        f.write(json.dumps(row, ensure_ascii=False) + "\n")


def add(text, who="owner", path=None, now=None):
    text = clean(text)
    if not text:
        raise ValueError("empty order")
    now = time.time() if now is None else now
    oid = "o" + time.strftime("%m%d%H%M%S", time.localtime(now)) + "%02x" % random.randrange(256)
    row = {"kind": "order", "id": oid, "t": now, "iso": time.strftime("%Y-%m-%dT%H:%M:%S", time.localtime(now)),
           "from": who, "text": text}
    _append(row, path or orders_path())
    return row


def open_orders(path=None):
    """orders without a result, oldest first"""
    rows = read(path)
    done = {r["id"] for r in rows if r.get("kind") == "result"}
    return [r for r in rows if r.get("kind") == "order" and r["id"] not in done]


def results(path=None, n=10):
    rows = read(path)
    text = {r["id"]: r.get("text", "") for r in rows if r.get("kind") == "order"}
    out = [dict(r, order=text.get(r["id"], "?")) for r in rows if r.get("kind") == "result"]
    return out[-n:] if n else out


def done(oid, result, by="ayla-helper", path=None, log=None, now=None):
    """close an open order with what was done; also '[owner] <order> -> <action>' to her log. None if not open."""
    o = next((r for r in open_orders(path) if r["id"] == oid), None)
    if o is None:
        return None
    now = time.time() if now is None else now
    result = clean(result) or "(no details)"
    row = {"kind": "result", "id": oid, "t": now, "iso": time.strftime("%Y-%m-%dT%H:%M:%S", time.localtime(now)),
           "by": by, "result": result}
    _append(row, path or orders_path())
    line = f"{row['iso']} [owner] {o['text']} -> {result}\n"
    try:
        lp = log or log_path()
        os.makedirs(os.path.dirname(os.path.abspath(lp)), exist_ok=True)
        with open(lp, "a", encoding="utf-8") as f:
            f.write(line)
    except OSError:
        pass
    return row


def signature(path=None):
    """cheap change detector for the dispatcher (size, mtime) of the orders file"""
    try:
        st = os.stat(path or orders_path())
        return (st.st_size, st.st_mtime)
    except OSError:
        return None


def briefing_section(path=None):
    """the top of the helper's briefing while owner orders are open ('' when none)"""
    op = open_orders(path)
    if not op:
        return ""
    lines = [f"- [{o['id']}] ({o.get('iso', '?')}) {o['text']}" for o in op]
    return ("== OWNER ORDER - do this now (rule AY20: ahead of everything else) ==\n" + "\n".join(lines) + "\n"
            "Carry each one out at once with your allowed ayla.sh commands, then report it: "
            "`ayla.sh done <id> \"<what you did>\"` (or `ayla.sh done <id> \"cannot: <why>\"`).\n")


def main(argv):
    if not argv or argv[0] not in ("add", "done", "list"):
        print(__doc__.strip().splitlines()[0])
        print("usage: orders.py add TEXT | done ID RESULT | list [N]")
        return 1
    cmd, rest = argv[0], argv[1:]
    if cmd == "add":
        try:
            r = add(" ".join(rest))
        except ValueError:
            print("usage: ayla.sh tell \"<plain text>\"")
            return 1
        print(f"order {r['id']} for Ayla queued at {r['iso']}: {r['text']}")
        print("her helper wakes within about 30 s; see the result with: ayla.sh orders")
        return 0
    if cmd == "done":
        if len(rest) < 2:
            print("usage: ayla.sh done ID \"what you did\"")
            return 1
        r = done(rest[0], " ".join(rest[1:]))
        if r is None:
            print(f"no open order {rest[0]}")
            return 1
        print(f"order {rest[0]} closed: {r['result']}")
        return 0
    n = int(rest[0]) if rest and rest[0].isdigit() else 10
    op = open_orders()
    print("open orders:" if op else "open orders: none")
    for o in op:
        print(f"  [{o['id']}] {o.get('iso', '?')} {o['text']}")
    for r in results(n=n):
        print(f"{r.get('iso', '?')} [owner] {r['order']} -> {r.get('result', '')}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
