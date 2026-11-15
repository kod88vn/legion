"""Masterminds: the strategy layer on top of Legion.

Each guild (guild01..guild10) has a leader mind, and two more minds look after the independent players. A mind
wakes up every few minutes, reads its members and recent events from the Legion API, asks Claude (Haiku) for
strategic decisions as structured JSON, and applies them through the API: where each group trains, short chat
lines in the members' own voices, and a running strategy note it keeps for next time. A separate fast loop
answers whispers and chat aimed at bots, in persona.

Day-to-day play (walking, fighting, looting, resting) stays in Legion; the LLM only decides strategy.

Without ANTHROPIC_API_KEY it runs a rule-based stand-in brain so the loop can be exercised end to end.
"""
import json
import logging
import os
import random
import threading
import time
import urllib.request
import zlib
from collections import defaultdict
from typing import List, Optional

from pydantic import BaseModel, Field

API = os.environ.get("LEGION_API", "http://127.0.0.1:8090")
MODEL = os.environ.get("MASTERMIND_MODEL", "claude-haiku-5-5")
CYCLE_SEC = int(os.environ.get("MASTERMIND_CYCLE_SEC", "300"))
CHAT_SEC = int(os.environ.get("MASTERMIND_CHAT_SEC", "20"))
PERSONAS = os.environ.get("PERSONAS", "/data/personas.json")
STATE_DIR = os.environ.get("STATE_DIR", "/state")

log = logging.getLogger("mastermind")
logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s %(message)s")

GUILDS = {
    "guild01": ("Crimson Oath", "Blood for glory.", "fiery, competitive, wants the guild to dominate WoE"),
    "guild02": ("Silver Lanterns", "Light the way for newcomers.", "patient mentor, recruits and helps newbies"),
    "guild03": ("Moonlit Bazaar", "Every drop has a price.", "merchant prince, cares about loot, zeny and trade"),
    "guild04": ("Iron Wolves", "The pack hunts together.", "disciplined, organises parties and dungeon runs"),
    "guild05": ("Starfall Wanderers", "Every map, every secret.", "curious explorer, sends members to new places"),
    "guild06": ("Kitty Café", "Good vibes only.", "laid-back social host, chatty, keeps everyone happy"),
    "guild07": ("Obsidian Edge", "Efficiency is everything.", "cold min-maxer, optimises exp per hour"),
    "guild08": ("Dawnbreakers", "Rise together.", "earnest underdog, encourages members, celebrates level-ups"),
    "guild09": ("Thorned Rose", "Elegance in battle.", "proud roleplayer, speaks in flowery fantasy style"),
    "guild10": ("Pudding Patrol", "Mostly harmless.", "goofy and chaotic, jokes a lot, still gets things done"),
}
INDEPENDENT_MINDS = {
    "indie-a": "independent players A (no guild): solo grinders and casuals",
    "indie-b": "independent players B (no guild): drifters, newbies and traders",
}


# ------------------------------------------------------------------ Legion API

def api(path, body=None):
    req = urllib.request.Request(API + path, data=json.dumps(body).encode() if body is not None else None,
                                 headers={"Content-Type": "application/json"}, method="POST" if body is not None else "GET")
    with urllib.request.urlopen(req, timeout=15) as r:
        return json.loads(r.read())


# ------------------------------------------------------------------ decisions schema (structured output)

class Assignment(BaseModel):
    members: List[str] = Field(description="exact bot names this applies to")
    mode: str = Field(description="'hunt' (train on map), 'idle' (stay/socialise in place) or 'follow'")
    map: str = Field(description="map to go to (for hunt/idle); must come from the suggested list when hunting")
    follow: str = Field(default="", description="for mode=follow: exact name of the member to follow")
    reason: str = Field(description="one short sentence")


class ChatLine(BaseModel):
    bot: str = Field(description="exact name of the member who speaks")
    channel: str = Field(description="'say' (public, nearby) or 'whisper'")
    to: str = Field(default="", description="for whisper: recipient name")
    text: str = Field(description="what they say, in their own typing style, max 100 characters")


class Decision(BaseModel):
    strategy_note: str = Field(description="your updated plan/memory for next time, max 600 characters")
    assignments: List[Assignment]
    chat: List[ChatLine] = Field(description="0-4 short lines to make the guild feel alive; not every cycle")


class Reply(BaseModel):
    bot: str
    channel: str
    to: str = ""
    text: str


class Replies(BaseModel):
    replies: List[Reply] = Field(description="one reply per message that deserves an answer (can be empty)")


# ------------------------------------------------------------------ brains

class ClaudeBrain:
    """Calls Claude through the official SDK with structured outputs."""

    def __init__(self):
        import anthropic
        self.anthropic = anthropic
        self.client = anthropic.Anthropic()

    def ask(self, system, prompt, schema, max_tokens=4000):
        try:
            resp = self.client.messages.parse(
                model=MODEL, max_tokens=max_tokens, system=system,
                messages=[{"role": "user", "content": prompt}],
                output_config={"effort": "low"},
                output_format=schema,
            )
        except self.anthropic.RateLimitError as e:
            log.warning("rate limited: %s", e)
            return None
        except self.anthropic.APIStatusError as e:
            log.warning("API error %s: %s", e.status_code, e)
            return None
        except self.anthropic.APIConnectionError as e:
            log.warning("connection error: %s", e)
            return None
        if resp.stop_reason == "refusal":
            log.warning("model declined (%s)", getattr(resp, "stop_details", None))
            return None
        u = resp.usage
        log.info("claude: in=%s out=%s", u.input_tokens, u.output_tokens)
        return resp.parsed_output


class RuleBrain:
    """Stand-in when no API key is configured: sensible but dull decisions, canned chat."""

    def ask(self, system, prompt, schema, max_tokens=0):
        ctx = json.loads(prompt.split("CONTEXT_JSON:", 1)[1])
        if schema is Replies:
            out = []
            for m in ctx.get("messages", []):
                out.append(Reply(bot=m["bot"], channel="whisper" if m["kind"] == "whisper" else "say",
                                 to=m.get("from", ""), text=random.choice(["hey!", "hi there :)", "busy grinding, sec", "o/"])))
            return Replies(replies=out[:3])
        assigns = []
        bands = defaultdict(list)
        for m in ctx["members"]:
            if m["online"]:
                bands[m["level"] // 10].append(m)
        for band, ms in bands.items():
            spots = ctx["spots"].get(str(band * 10 + 5), [])
            if spots and random.random() < 0.3:  # occasionally move a level band together
                assigns.append(Assignment(members=[m["name"] for m in ms], mode="hunt", map=random.choice(spots),
                                          reason="train together at a level-appropriate map"))
        chat = []
        online = [m for m in ctx["members"] if m["online"]]
        if online and random.random() < 0.5:
            b = random.choice(online)
            chat.append(ChatLine(bot=b["name"], channel="say", text=random.choice(
                ["anyone want to party?", "lvl up soon!", "this map is crowded lol", "brb potions", "gg"])))
        return Decision(strategy_note=ctx.get("note", "") or "keep members training at level-appropriate maps",
                        assignments=assigns, chat=chat)


def make_brain():
    if os.environ.get("ANTHROPIC_API_KEY") or os.environ.get("ANTHROPIC_AUTH_TOKEN"):
        log.info("using Claude (%s)", MODEL)
        return ClaudeBrain()
    log.warning("no ANTHROPIC_API_KEY: using the rule-based stand-in brain")
    return RuleBrain()


# ------------------------------------------------------------------ minds

def persona_index():
    try:
        return {p["name"]: p for p in json.load(open(PERSONAS))}
    except Exception as e:
        log.warning("personas: %s", e)
        return {}


def spots_by_level():
    out = {}
    for lv in range(5, 100, 10):
        try:
            out[str(lv)] = sorted({s["Map"] for s in api(f"/api/spots?level={lv}")})[:12]
        except Exception:
            out[str(lv)] = []
    return out


SYSTEM_LEADER = """You are the leader of a guild of players in Ragnarok Online (a classic 2D MMORPG, renewal rules).
Your members are run by an automation engine that walks, fights, loots and rests by itself; you only make
strategic decisions for them: where groups train (hunting maps), who follows whom, and a few chat lines that
make the guild feel like real people. Respect each member's persona (archetype, risk, sociability, typing style).
Keep players on maps that fit their level; move a member that keeps dying to a safer map; group social members
together; let explorers roam further; don't reassign everyone every time - stability is realistic.
Only use member names and map names that appear in the context. Chat lines must be short, in the speaker's
typing style, never mention bots, AI, automation or this game's server internals."""

SYSTEM_CHAT = """You write in-game chat replies for players in Ragnarok Online. Each player has a persona with a
typing style; reply as that player would, briefly (max 100 characters), casual MMO tone. Answer questions
helpfully if the persona would. Never reveal or hint that the player is a bot or AI. Skip messages that don't
need a reply."""


class Mind:
    def __init__(self, key, brain, personas, guild=None, members_filter=None):
        self.key, self.brain, self.personas = key, brain, personas
        self.guild = guild
        self.members_filter = members_filter
        self.state_path = os.path.join(STATE_DIR, f"{key}.json")
        try:
            self.state = json.load(open(self.state_path))
        except Exception:
            self.state = {"note": "", "cycles": 0}

    def members(self):
        if self.guild:
            ms = api(f"/api/bots?guild={self.guild}") or []
        else:
            ms = [m for m in (api("/api/bots") or []) if not m.get("guild") and self.members_filter(m["name"])]
        return ms

    def cycle(self, events):
        ms = self.members()
        if not ms:
            return
        names = {m["name"] for m in ms}
        roster = []
        for m in ms:
            p = self.personas.get(m["name"], {})
            roster.append({"name": m["name"], "class": m["class"], "level": m["level"], "online": m["online"],
                           "map": m["map"], "doing": m["state"][:40], "kills": m["kills"], "deaths": m["deaths"],
                           "archetype": p.get("archetype"), "risk": round(p.get("risk", .5), 1),
                           "social": round(p.get("sociability", .5), 1), "typing": p.get("typing_style", "")})
        my_events = [e for e in events if e.get("bot") in names][-40:]
        if self.guild:
            title, motto, style = GUILDS[self.guild]
            who = f"You lead the guild '{title}' (motto: \"{motto}\"). Your leadership style: {style}."
        else:
            who = f"You coordinate {INDEPENDENT_MINDS[self.key]}; they are not a guild, nudge them gently."
        ctx = {"note": self.state.get("note", ""), "members": roster, "recent_events": my_events,
               "spots": spots_by_level()}
        prompt = (f"{who}\nYour notes from last time: {self.state.get('note') or '(first session)'}\n"
                  f"Decide this round's strategy.\nCONTEXT_JSON:{json.dumps(ctx, separators=(',', ':'))}")
        d = self.brain.ask(SYSTEM_LEADER, prompt, Decision)
        if d is None:
            return
        applied = 0
        for a in d.assignments:
            for n in a.members:
                if n not in names:
                    continue
                goal = {"mode": a.mode if a.mode in ("hunt", "idle", "follow") else "hunt", "map": a.map,
                        "who": a.follow}
                try:
                    api(f"/api/bots/{urllib.request.quote(n)}/goal", goal)
                    applied += 1
                except Exception as e:
                    log.warning("%s goal %s: %s", self.key, n, e)
        for c in d.chat[:4]:
            if c.bot not in names:
                continue
            try:
                if c.channel == "whisper" and c.to:
                    api(f"/api/bots/{urllib.request.quote(c.bot)}/whisper", {"to": c.to, "text": c.text[:100]})
                else:
                    api(f"/api/bots/{urllib.request.quote(c.bot)}/say", {"text": c.text[:100]})
            except Exception as e:
                log.warning("%s chat: %s", self.key, e)
        self.state["note"] = d.strategy_note[:600]
        self.state["cycles"] = self.state.get("cycles", 0) + 1
        os.makedirs(STATE_DIR, exist_ok=True)
        json.dump(self.state, open(self.state_path, "w"), indent=1)
        log.info("%s: %d members, %d goals set, %d chat lines | note: %s", self.key, len(ms), applied,
                 len(d.chat), d.strategy_note[:120])


def chat_loop(brain, personas, bot_names, stop):
    """Answer whispers to bots and public chat that mentions a bot, in persona. Ignores bot-to-bot chatter
    (except rarely), so bots don't talk each other into endless loops."""
    since = 0
    try:
        since = api("/api/events?since=999999999")["next"]
    except Exception:
        pass
    replied = {}
    while not stop.is_set():
        time.sleep(CHAT_SEC)
        try:
            r = api(f"/api/events?since={since}")
        except Exception:
            continue
        since = r["next"]
        todo = []
        for e in r["events"]:
            if e["kind"] == "whisper":
                sender = e.get("from", "")
                if sender in bot_names and random.random() > 0.1:
                    continue
                todo.append({"bot": e["bot"], "kind": "whisper", "from": sender, "text": e["text"]})
            elif e["kind"] == "chat" and " : " in e["text"]:
                speaker, text = e["text"].split(" : ", 1)
                if speaker in bot_names:
                    continue
                low = text.lower()
                if e["bot"].lower() in low or "?" in text:  # addressed to this bot, or a question nearby
                    key = (speaker, text)
                    if key in replied:
                        continue  # many bots hear the same line; answer once
                    replied[key] = e["bot"]
                    todo.append({"bot": e["bot"], "kind": "chat", "from": speaker, "text": text})
        if not todo:
            continue
        todo = todo[:8]
        ctx = {"messages": [dict(m, persona={k: personas.get(m["bot"], {}).get(k) for k in
                                             ("archetype", "typing_style", "class_name", "base_level", "bio")})
                            for m in todo]}
        rep = brain.ask(SYSTEM_CHAT, "Reply to these messages.\nCONTEXT_JSON:" + json.dumps(ctx), Replies, 2000)
        for x in (rep.replies if rep else []):
            if x.bot not in bot_names:
                continue
            try:
                if x.channel == "whisper" and x.to:
                    api(f"/api/bots/{urllib.request.quote(x.bot)}/whisper", {"to": x.to, "text": x.text[:100]})
                else:
                    api(f"/api/bots/{urllib.request.quote(x.bot)}/say", {"text": x.text[:100]})
            except Exception as e:
                log.warning("reply: %s", e)
        if len(replied) > 2000:
            replied.clear()


def main():
    brain = make_brain()
    personas = persona_index()
    for _ in range(60):
        try:
            api("/api/stats")
            break
        except Exception:
            time.sleep(5)
    bots = api("/api/bots") or []
    bot_names = {b["name"] for b in bots}
    minds = [Mind(g, brain, personas, guild=g) for g in GUILDS]
    minds.append(Mind("indie-a", brain, personas, members_filter=lambda n: zlib.crc32(n.encode()) % 2 == 0))
    minds.append(Mind("indie-b", brain, personas, members_filter=lambda n: zlib.crc32(n.encode()) % 2 == 1))
    stop = threading.Event()
    threading.Thread(target=chat_loop, args=(brain, personas, bot_names, stop), daemon=True).start()
    log.info("%d minds, cycle %ds, chat poll %ds", len(minds), CYCLE_SEC, CHAT_SEC)
    since = 0
    while True:
        for i, m in enumerate(minds):
            try:
                r = api(f"/api/events?since={since}")
                events = r["events"]
                m.cycle(events)
            except Exception as e:
                log.warning("%s cycle failed: %s", m.key, e)
            time.sleep(CYCLE_SEC / len(minds))  # stagger minds across the cycle
        try:
            since = api("/api/events?since=999999999")["next"]
        except Exception:
            pass


if __name__ == "__main__":
    main()
