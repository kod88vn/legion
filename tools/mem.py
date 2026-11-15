import sys, json, subprocess
for name in sys.argv[1:]:
    out = subprocess.run([sys.executable, "tools/legionctl.py", "memory", name, "--diary", "6"], capture_output=True, text=True, cwd="G:/RO/server").stdout
    try:
        m = json.loads(out)
    except Exception:
        print(name, "?? ", out[:300]); continue
    p = m.get("persona", {}); n = m.get("now", {})
    print(f"== {name}: {n.get('class')} lv{n.get('level')} {n.get('map')} | {n.get('doing')} | party={n.get('party')} guild={n.get('guild')} z={n.get('zeny')} online={n.get('online')}")
    print(f"   style: {p.get('typing_style')} | {p.get('age')} {p.get('country')} {p.get('archetype')} | {(m.get('facts') or [''])[0]}")
    ppl = [f"{x['name']}({x['relation']})" for x in (m.get("people") or []) if x['relation'] not in ('stranger','partymate')][:6]
    print("   knows:", ", ".join(ppl))
    for d in (m.get("diary") or [])[-6:]:
        print("   -", d["t"][11:16], d["text"][:150])
