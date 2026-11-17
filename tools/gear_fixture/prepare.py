#!/usr/bin/env python3
"""Extract a minimal, pinned rAthena gear/source fixture for offline Go tests."""

from __future__ import annotations

import argparse
import concurrent.futures
import hashlib
import json
import re
import sys
import urllib.error
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
COMMIT = "1ca3e7700939ea40120cb51a777f8681ba5e84ae"
RAW = f"https://raw.githubusercontent.com/rathena/rathena/{COMMIT}/"
TREE = f"https://api.github.com/repos/rathena/rathena/git/trees/{COMMIT}?recursive=1"
EXPECTED_ITEM_DB = "a2b53925ad2b6138d5c563ec94f1b3f449e8d33435b00687b2f661ecb5322e01"
EXPECTED_MOB_DB = "fe79480381a98953a0281edafd775486926b5e186feec0a99816a95433907136"
EXPECTED_UPSTREAM = "d336bda5853044086d470b3e4cf3178c5bec9c5e6cda4558d02e83c382b940c8"
EXPECTED_NPC_FILES = 187
EXPECTED_NOTICE = "GNU General Public License version 3 or (at your option) any later version"
NPC_PREFIXES = ("npc/merchants/", "npc/re/merchants/", "npc/re/mobs/")
ITEM_FIELDS = {
    "AegisName",
    "Name",
    "Type",
    "SubType",
    "Buy",
    "Weight",
    "Slots",
    "EquipLevelMin",
    "Attack",
    "MagicAttack",
    "Defense",
    "MagicDefense",
    "WeaponLevel",
    "Refineable",
    "Jobs",
    "Locations",
}
MOB_FIELDS = {
    "AegisName",
    "Name",
    "Level",
    "Hp",
    "BaseExp",
    "Defense",
    "WalkSpeed",
    "Element",
    "Race",
    "Ai",
    "Class",
    "MvpExp",
}
NOTICE_RE = re.compile(r"SPDX-License-Identifier|Copyright|licensed under|GNU General Public License", re.I)
SPDX_RE = re.compile(r"SPDX-License-Identifier:\s*(\S+)", re.I)
SHOP_RE = re.compile(r'^\s*(unloadnpc|npcshopdelitem)\s+"([^"]+)"\s*(.*?);')


def fail(message: str) -> "NoReturn":
    raise SystemExit(f"gear fixture: {message}")


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def decode_source(data: bytes) -> str:
    return data.decode("latin-1")


def aggregate_hash(files: dict[str, bytes]) -> str:
    digest = hashlib.sha256()
    for path, data in sorted(files.items()):
        digest.update(path.encode("utf-8") + b"\0")
        digest.update(str(len(data)).encode("ascii") + b"\0")
        digest.update(data + b"\0")
    return digest.hexdigest()


def read_local_inputs() -> dict[str, bytes]:
    names = (
        "rathena/Dockerfile",
        "legion/builds.go",
        "legion/data/buildgear.json",
        "rathena/npc-custom/legion_shops.txt",
        "rathena/db-import/item_db.yml",
        "rathena/conf/import/battle_conf.txt",
    )
    return {f"repo/{name}": (ROOT / name).read_bytes() for name in names}


def source_paths() -> list[str]:
    with urllib.request.urlopen(TREE, timeout=30) as response:
        tree = json.load(response)
    if tree.get("truncated"):
        fail("pinned upstream tree response is truncated")
    paths = sorted(
        entry["path"]
        for entry in tree.get("tree", [])
        if entry.get("type") == "blob"
        and entry["path"].endswith(".txt")
        and entry["path"].startswith(NPC_PREFIXES)
    )
    if len(paths) != EXPECTED_NPC_FILES:
        fail(f"pinned upstream tree has {len(paths)} scoped NPC files, want {EXPECTED_NPC_FILES}")
    return paths


def fetch_url(url: str) -> bytes:
    request = urllib.request.Request(url, headers={"User-Agent": "legion-pinned-gear-fixture"})
    with urllib.request.urlopen(request, timeout=45) as response:
        return response.read()


def fetch_pinned_inputs() -> dict[str, bytes]:
    paths = source_paths()
    urls = {
        "LICENSE": RAW + "LICENSE",
        "db/re/item_db_equip.yml": RAW + "db/re/item_db_equip.yml",
        "db/re/mob_db.yml": RAW + "db/re/mob_db.yml",
        **{path: RAW + path for path in paths},
    }
    fetched: dict[str, bytes] = {}
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
        futures = {pool.submit(fetch_url, url): path for path, url in urls.items()}
        for future in concurrent.futures.as_completed(futures):
            path = futures[future]
            try:
                fetched[path] = future.result()
            except (OSError, urllib.error.URLError) as exc:
                fail(f"could not fetch pinned official input {path}: {exc}")
    return fetched


def read_cached_inputs(cache: Path) -> dict[str, bytes]:
    files = {
        "LICENSE": (cache / "LICENSE").read_bytes(),
        "db/re/item_db_equip.yml": (cache / "item_db_equip.yml").read_bytes(),
        "db/re/mob_db.yml": (cache / "mob_db.yml").read_bytes(),
    }
    npc_root = cache / "upstream"
    for prefix in NPC_PREFIXES:
        base = npc_root / prefix
        if not base.is_dir():
            fail(f"source cache is missing {prefix}")
        for path in sorted(base.rglob("*.txt")):
            relative = path.relative_to(npc_root).as_posix()
            files[relative] = path.read_bytes()
    npc_count = sum(path.endswith(".txt") for path in files)
    if npc_count != EXPECTED_NPC_FILES:
        fail(f"source cache has {npc_count} scoped NPC files, want {EXPECTED_NPC_FILES}")
    return files


def verify_pinned_inputs(upstream: dict[str, bytes], local: dict[str, bytes]) -> None:
    if sha256(upstream["db/re/item_db_equip.yml"]) != EXPECTED_ITEM_DB:
        fail("pinned item_db_equip.yml SHA-256 mismatch")
    if sha256(upstream["db/re/mob_db.yml"]) != EXPECTED_MOB_DB:
        fail("pinned mob_db.yml SHA-256 mismatch")
    aggregate = {
        path: data
        for path, data in upstream.items()
        if path == "LICENSE" or path.startswith(NPC_PREFIXES)
    }
    if aggregate_hash(aggregate) != EXPECTED_UPSTREAM:
        fail("pinned LICENSE/NPC source aggregate SHA-256 mismatch")
    dockerfile = local["repo/rathena/Dockerfile"].decode("utf-8")
    match = re.search(r"(?m)^ARG RATHENA_COMMIT=([0-9a-f]{40})$", dockerfile)
    if not match or match.group(1) != COMMIT:
        fail("repository Dockerfile no longer pins the fixture's rAthena source commit")
    for path, data in upstream.items():
        if path.endswith(".txt") and not path.startswith(NPC_PREFIXES):
            fail(f"unexpected upstream text input {path}")


def extract_notices(upstream: dict[str, bytes]) -> dict[str, list[str]]:
    notices: dict[str, list[str]] = {}
    for path, data in sorted(upstream.items()):
        lines = decode_source(data).splitlines()
        found = []
        for line in lines[:40]:
            if NOTICE_RE.search(line):
                found.append(line.strip())
            match = SPDX_RE.search(line)
            if match and match.group(1) not in ("GPL-3.0-or-later", "GPL-3.0+"):
                fail(f"contrary SPDX notice in pinned source {path}: {line.strip()}")
        if found:
            notices[path] = found
    license_text = decode_source(upstream["LICENSE"])
    if "GNU GENERAL PUBLIC LICENSE" not in license_text or "Version 3, 29 June 2007" not in license_text:
        fail("pinned upstream LICENSE is not the expected GPLv3 notice")
    return notices


def entry_blocks(text: str) -> tuple[list[str], list[list[str]]]:
    lines = text.splitlines()
    body = next((index for index, line in enumerate(lines) if line == "Body:"), None)
    if body is None:
        fail("upstream YAML input has no Body: section")
    header = lines[: body + 1]
    blocks: list[list[str]] = []
    current: list[str] = []
    for line in lines[body + 1 :]:
        if line.startswith("  - Id:"):
            if current:
                blocks.append(current)
            current = [line]
        elif current:
            current.append(line)
    if current:
        blocks.append(current)
    return header, blocks


def field_value(line: str) -> tuple[str, str] | None:
    stripped = line.strip()
    if ":" not in stripped:
        return None
    key, value = stripped.split(":", 1)
    return key.strip(), value.strip()


def extract_demand(local: dict[str, bytes]) -> tuple[dict[str, object], dict[str, object]]:
    source = local["repo/legion/builds.go"].decode("utf-8")
    guide = json.loads(local["repo/legion/data/buildgear.json"])
    guide_builds = guide.get("builds")
    if not isinstance(guide_builds, dict) or len(guide_builds) != 29:
        fail("buildgear.json does not contain the measured 29 guide builds")

    start = source.find("var builds = map[string]*Build{")
    if start < 0:
        fail("could not find the registered builds map in builds.go")
    closing = re.search(r"(?m)^\}", source[start:])
    if not closing:
        fail("could not find the end of the registered builds map")
    builds = source[start : start + closing.start()]
    build_entries = list(re.finditer(r'(?m)^\s*"([^"]+)":\s*\{ID:\s*"([^"]+)"', builds))
    if len(build_entries) != 37:
        fail(f"builds.go has {len(build_entries)} registered build variants, want 37")
    direct_guides: list[str] = []
    aliases: dict[str, str] = {}
    uncovered: list[str] = []
    job_classes: set[int] = set()
    for index, entry in enumerate(build_entries):
        key, build_id = entry.group(1), entry.group(2)
        if key != build_id:
            fail(f"registered build map key {key} does not match Build.ID {build_id}")
        end = build_entries[index + 1].start() if index + 1 < len(build_entries) else len(builds)
        block = builds[entry.start() : end]
        base_match = re.search(r'\bBase:\s*"([^"]+)"', block)
        base = base_match.group(1) if base_match else build_id
        class_match = re.search(r"\bClass:\s*(\d+)", block)
        if class_match:
            job_classes.add(int(class_match.group(1)))
        if build_id in guide_builds:
            direct_guides.append(build_id)
        elif base in guide_builds:
            aliases[build_id] = base
        else:
            uncovered.append(build_id)
    if set(guide_builds) - {entry.group(2) for entry in build_entries}:
        fail("buildgear.json contains a guide build not registered in builds.go")

    route_weapons: set[int] = set()
    for match in re.finditer(r"Weapons:\s*\[\]int\{([^}]*)\}", builds):
        route_weapons.update(map(int, re.findall(r"\d+", match.group(1))))
    if not route_weapons:
        fail("builds.go has no literal weapon routes")

    armor_refs = set(re.findall(r"Armors:\s*([A-Za-z_]\w*)", builds))
    route_armors: set[int] = set()
    for name in armor_refs:
        match = re.search(rf"\b{re.escape(name)}\s*=\s*\[\]int\{{([^}}]*)\}}", source)
        if not match:
            fail(f"could not resolve armor route {name} in builds.go")
        route_armors.update(map(int, re.findall(r"\d+", match.group(1))))

    guide_weapons: set[int] = set()
    guide_armors: set[int] = set()
    for build in guide_builds.values():
        for band in build.get("bands", []):
            guide_weapons.update(band.get("weapon", []))
            guide_armors.update(band.get("armor", []))

    weapons = route_weapons | guide_weapons
    armors = route_armors | guide_armors
    item_ids = sorted(weapons | armors)
    if len(job_classes) != 13:
        fail(f"builds.go has {len(job_classes)} second-job classes, want 13")
    if len(weapons) != 116 or len(armors) != 21 or len(item_ids) != 137:
        fail(
            "gear demand changed: "
            f"{len(weapons)} weapon IDs, {len(armors)} armor IDs, {len(item_ids)} total"
        )
    demand = {
        "build_records": len(guide_builds),
        "weapon_ids": len(weapons),
        "armor_ids": len(armors),
        "weapon_item_ids": sorted(weapons),
        "armor_item_ids": sorted(armors),
        "item_ids": item_ids,
    }
    build_coverage = {
        "registered_build_variants": len(build_entries),
        "registered_second_job_classes": len(job_classes),
        "second_job_class_ids": sorted(job_classes),
        "guide_build_records": len(guide_builds),
        "registered_build_ids": sorted(entry.group(2) for entry in build_entries),
        "direct_guide_build_ids": sorted(direct_guides),
        "base_guide_aliases": dict(sorted(aliases.items())),
        "uncovered_build_ids": sorted(uncovered),
        "route_data": "Weapons and Armors from every registered builds.go variant, including shared armor arrays.",
        "guide_fallback": "buildGearTable uses the variant's guide when present, otherwise its registered Base guide.",
    }
    return demand, build_coverage


def select_item_blocks(raw: bytes, demand: dict[str, object]) -> tuple[bytes, dict[int, dict[str, str]]]:
    header, blocks = entry_blocks(decode_source(raw))
    wanted = set(demand["item_ids"])
    fields_by_id: dict[int, dict[str, str]] = {}
    selected: list[list[str]] = []
    for block in blocks:
        item_id = int(block[0].split(":", 1)[1].strip())
        fields: dict[str, str] = {}
        kept = [block[0]]
        nested = ""
        for line in block[1:]:
            if line.startswith("    ") and not line.startswith("      "):
                value = field_value(line)
                nested = ""
                if value:
                    key, scalar = value
                    if key in ITEM_FIELDS:
                        fields[key] = scalar
                        kept.append(line)
                        if key in ("Jobs", "Locations"):
                            nested = key
            elif line.startswith("      ") and not line.startswith("        ") and nested in ("Jobs", "Locations"):
                if field_value(line):
                    kept.append(line)
        if item_id in wanted:
            fields_by_id[item_id] = fields
            selected.append(kept)
    missing = wanted - fields_by_id.keys()
    if missing:
        fail(f"pinned equipment database is missing demanded IDs {sorted(missing)}")
    for item_id in demand["weapon_item_ids"]:
        item = fields_by_id[item_id]
        if item.get("Type") != "Weapon" or not ("Attack" in item or "MagicAttack" in item):
            fail(f"demanded weapon {item_id} lacks its pinned weapon attributes")
    for item_id in demand["armor_item_ids"]:
        if fields_by_id[item_id].get("Type") != "Armor" or "Defense" not in fields_by_id[item_id]:
            fail(f"demanded armor {item_id} lacks its pinned defense attribute")
    text = "\n".join(header + [line for block in selected for line in block] + [""])
    return text.encode("latin-1"), fields_by_id


def parse_mob_block(block: list[str], by_aegis: dict[str, int], wanted: set[int]) -> tuple[int, dict[str, str], list[tuple[str, list[str], int, bool]]] | None:
    mob_id = int(block[0].split(":", 1)[1].strip())
    fields: dict[str, str] = {}
    drops: list[tuple[str, list[str], int, bool]] = []
    section = ""
    current_item = ""
    current_lines: list[str] = []

    def finish_drop() -> None:
        nonlocal current_item, current_lines
        item_id = by_aegis.get(current_item, 0)
        if item_id in wanted and current_lines:
            rate = 0
            for entry in current_lines:
                value = field_value(entry)
                if value and value[0] == "Rate":
                    try:
                        rate = int(value[1])
                    except ValueError:
                        fail(f"invalid drop rate in mob {mob_id}: {entry.strip()}")
            if rate <= 0:
                fail(f"missing positive drop rate for demanded item {item_id} on mob {mob_id}")
            drops.append((section, current_lines, item_id, section == "MvpDrops"))
        current_item, current_lines = "", []

    for line in block[1:]:
        if line.startswith("    ") and not line.startswith("     "):
            finish_drop()
            value = field_value(line)
            section = ""
            if not value:
                continue
            key, scalar = value
            if key in ("Drops", "MvpDrops"):
                section = key
            elif key in MOB_FIELDS:
                fields[key] = scalar
        elif section and line.strip().startswith("- Item:"):
            finish_drop()
            current_item = line.strip()[7:].strip()
            current_lines = [line]
        elif section and current_lines:
            current_lines.append(line)
    finish_drop()
    if not drops:
        return None
    return mob_id, fields, drops


def select_mob_blocks(raw: bytes, item_fields: dict[int, dict[str, str]]) -> tuple[bytes, dict[int, dict[str, object]]]:
    header, blocks = entry_blocks(decode_source(raw))
    by_aegis = {item["AegisName"]: item_id for item_id, item in item_fields.items() if item.get("AegisName")}
    wanted = set(item_fields)
    selected: list[list[str]] = []
    mob_info: dict[int, dict[str, object]] = {}
    for block in blocks:
        parsed = parse_mob_block(block, by_aegis, wanted)
        if parsed is None:
            continue
        mob_id, fields, drops = parsed
        kept = [block[0]]
        section = ""
        kept_sections: set[str] = set()
        for line in block[1:]:
            if line.startswith("    ") and not line.startswith("     "):
                value = field_value(line)
                section = ""
                if value:
                    key, _ = value
                    if key in MOB_FIELDS:
                        kept.append(line)
                    if key in ("Drops", "MvpDrops") and any(drop[0] == key for drop in drops):
                        kept_sections.add(key)
                        section = key
            elif section and line.strip().startswith("- Item:"):
                # The exact source entry is copied below, avoiding unrelated drops.
                section = ""
        for wanted_section in ("Drops", "MvpDrops"):
            if wanted_section not in kept_sections:
                continue
            kept.append(f"    {wanted_section}:")
            for drop_section, drop_lines, _, _ in drops:
                if drop_section == wanted_section:
                    kept.extend(drop_lines)
        selected.append(kept)
        class_name = fields.get("Class", "")
        is_boss = class_name in ("Boss", "Guardian", "Battlefield") or "MvpExp" in fields or "MvpDrops" in kept_sections
        mob_info[mob_id] = {
            "name": fields.get("Name", ""),
            "level": int(fields.get("Level", "0")),
            "boss": is_boss,
            "drops": [{"item_id": item_id, "rate": drop_rate(drop_lines), "mvp": is_mvp} for _, drop_lines, item_id, is_mvp in drops],
        }
    text = "\n".join(header + [line for block in selected for line in block] + [""])
    return text.encode("latin-1"), mob_info


def drop_rate(lines: list[str]) -> int:
    for line in lines:
        value = field_value(line)
        if value and value[0] == "Rate":
            return int(value[1])
    return 0


def item_list_ids(parts: list[str]) -> tuple[str, list[int]]:
    entries = parts[3].split(",")
    ids = []
    for entry in entries[1:]:
        try:
            ids.append(int(entry.split(":", 1)[0].strip()))
        except ValueError:
            continue
    return entries[0] if entries else "", ids


def filter_shop_line(line: str, wanted: set[int]) -> tuple[str | None, str | None, set[int]]:
    if line.startswith("//"):
        return None, None, set()
    parts = line.split("\t")
    if len(parts) < 4 or parts[1].strip() != "shop" or len(parts[0].split(",")) < 3:
        return None, None, set()
    entries = parts[3].split(",")
    sprite = entries[0] if entries else ""
    kept = [sprite]
    found: set[int] = set()
    for entry in entries[1:]:
        try:
            item_id = int(entry.split(":", 1)[0].strip())
        except ValueError:
            continue
        if item_id in wanted:
            found.add(item_id)
            kept.append(entry)
    if not found:
        return None, parts[2].strip(), set()
    parts[3] = ",".join(kept)
    return "\t".join(parts), parts[2].strip(), found


def collect_shop_files(upstream: dict[str, bytes], local: dict[str, bytes], wanted: set[int]) -> tuple[dict[str, bytes], list[dict[str, object]], set[int]]:
    outputs: dict[str, bytes] = {}
    records: list[dict[str, object]] = []
    for path, data in sorted(upstream.items()):
        if not path.startswith(("npc/merchants/", "npc/re/merchants/")) or not path.endswith(".txt"):
            continue
        if path == "npc/re/merchants/Dealer_Update.txt":
            outputs[f"rathena/{path}"] = data
            continue
        matching = []
        for line in decode_source(data).splitlines():
            filtered, shop_id, ids = filter_shop_line(line, wanted)
            if filtered:
                matching.append((filtered, shop_id, ids))
        if not matching:
            continue
        header = []
        for line in decode_source(data).splitlines():
            if line.startswith("//") or not line.strip():
                header.append(line)
            else:
                break
        outputs[f"rathena/{path}"] = ("\n".join(header + [row[0] for row in matching]) + "\n").encode("latin-1")
        records.extend({"npc_id": row[1], "item_ids": sorted(row[2])} for row in matching)

    local_path = "rathena/npc/custom-local/legion_shops.txt"
    local_data = local["repo/rathena/npc-custom/legion_shops.txt"]
    matching = []
    for line in decode_source(local_data).splitlines():
        filtered, shop_id, ids = filter_shop_line(line, wanted)
        if filtered:
            matching.append((filtered, shop_id, ids))
    if not matching:
        fail("local market shop has no demanded gear items")
    outputs[local_path] = ("\n".join(row[0] for row in matching) + "\n").encode("latin-1")
    records.extend({"npc_id": row[1], "item_ids": sorted(row[2])} for row in matching)
    return outputs, records, {item for record in records for item in record["item_ids"]}


def parse_barter_edits(text: str) -> tuple[set[str], dict[str, set[int]]]:
    inside = False
    unload: set[str] = set()
    deleted: dict[str, set[int]] = {}
    for line in text.splitlines():
        if 'getbattleflag("feature.barter")' in line:
            inside = True
            continue
        if inside and "} else {" in line:
            break
        match = SHOP_RE.search(line)
        if not inside or not match:
            continue
        if match.group(1) == "unloadnpc":
            unload.add(match.group(2))
        else:
            deleted.setdefault(match.group(2), set()).update(
                int(value.strip()) for value in match.group(3).split(",") if value.strip().isdigit()
            )
    return unload, deleted


def available_shop_ids(records: list[dict[str, object]], dealer: bytes) -> set[int]:
    unload, deleted = parse_barter_edits(decode_source(dealer))
    available: set[int] = set()
    for record in records:
        npc_id = str(record["npc_id"])
        if npc_id in unload:
            continue
        available.update(set(record["item_ids"]) - deleted.get(npc_id, set()))
    return available


def collect_spawn_files(upstream: dict[str, bytes], mob_ids: set[int]) -> tuple[dict[str, bytes], set[int]]:
    outputs: dict[str, bytes] = {}
    spawned: set[int] = set()
    for path, data in sorted(upstream.items()):
        if not path.startswith("npc/re/mobs/") or not path.endswith(".txt"):
            continue
        lines = decode_source(data).splitlines()
        rows = []
        for line in lines:
            if line.startswith("//"):
                continue
            parts = line.split("\t")
            if len(parts) < 4 or parts[1].strip() not in ("monster", "boss_monster"):
                continue
            fields = parts[3].split(",")
            try:
                mob_id = int(fields[0].strip())
                count = int(fields[1].strip())
            except (ValueError, IndexError):
                continue
            if mob_id in mob_ids and count > 0:
                rows.append(line)
                spawned.add(mob_id)
        if rows:
            header = []
            for line in lines:
                if line.startswith("//") or not line.strip():
                    header.append(line)
                else:
                    break
            outputs[f"rathena/{path}"] = ("\n".join(header + rows) + "\n").encode("latin-1")
    return outputs, spawned


def source_coverage(shop_ids: set[int], spawned: set[int], mob_info: dict[int, dict[str, object]], demand: dict[str, object]) -> dict[str, object]:
    all_drops: set[int] = set()
    farmable_drops: set[int] = set()
    for mob_id, mob in mob_info.items():
        if mob_id not in spawned:
            continue
        for drop in mob["drops"]:
            item_id = int(drop["item_id"])
            all_drops.add(item_id)
            if not drop["mvp"] and not mob["boss"]:
                farmable_drops.add(item_id)
    demand_ids = set(demand["item_ids"])
    shop_ids &= demand_ids
    all_drops &= demand_ids
    farmable_drops &= demand_ids
    both = shop_ids & all_drops
    neither = demand_ids - shop_ids - all_drops
    return {
        "shop_item_ids": sorted(shop_ids),
        "spawned_drop_item_ids": sorted(all_drops),
        "normal_farm_item_ids": sorted(farmable_drops),
        "both_item_ids": sorted(both),
        "neither_item_ids": sorted(neither),
        "counts": {
            "shop": len(shop_ids),
            "spawned_drop": len(all_drops),
            "normal_farm": len(farmable_drops),
            "both": len(both),
            "neither": len(neither),
        },
    }


def filtered_battle_conf(data: bytes) -> bytes:
    keys = {"item_rate_equip", "item_rate_equip_boss", "item_rate_equip_mvp"}
    lines = data.decode("utf-8").splitlines()
    selected = [line for line in lines if line.split(":", 1)[0].strip() in keys]
    if len(selected) != len(keys):
        fail("local battle_conf does not define all three equipment drop rates")
    return ("\n".join(selected) + "\n").encode()


def write_fixture(output: Path, outputs: dict[str, bytes], manifest: dict[str, object]) -> None:
    if output.exists() and any(output.iterdir()):
        fail(f"output directory is not empty: {output}")
    output.mkdir(parents=True, exist_ok=True)
    file_manifest = {}
    for relative, data in sorted(outputs.items()):
        path = output / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        file_manifest[relative] = {"bytes": len(data), "sha256": sha256(data)}
    manifest["output_files"] = file_manifest
    manifest["output_bytes"] = sum(entry["bytes"] for entry in file_manifest.values())
    (output / "PROVENANCE.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-cache", type=Path, help="use a cache containing item_db_equip.yml, mob_db.yml, and upstream/")
    parser.add_argument("--output", type=Path, default=ROOT / "legion/testdata/gear-fixture")
    args = parser.parse_args()

    local = read_local_inputs()
    upstream = read_cached_inputs(args.source_cache) if args.source_cache else fetch_pinned_inputs()
    verify_pinned_inputs(upstream, local)
    notices = extract_notices(upstream)
    demand, build_coverage = extract_demand(local)

    item_bytes, item_fields = select_item_blocks(upstream["db/re/item_db_equip.yml"], demand)
    mob_bytes, mob_info = select_mob_blocks(upstream["db/re/mob_db.yml"], item_fields)
    mob_ids = set(mob_info)
    spawn_outputs, spawned_mobs = collect_spawn_files(upstream, mob_ids)
    shop_outputs, shop_records, shop_ids = collect_shop_files(upstream, local, set(demand["item_ids"]))
    dealer = upstream["npc/re/merchants/Dealer_Update.txt"]
    shops_after_barter = available_shop_ids(shop_records, dealer)
    availability = source_coverage(shops_after_barter, spawned_mobs, mob_info, demand)

    battle = filtered_battle_conf(local["repo/rathena/conf/import/battle_conf.txt"])
    override = local["repo/rathena/db-import/item_db.yml"]
    outputs: dict[str, bytes] = {
        "rathena/LICENSE": upstream["LICENSE"],
        "rathena/db/re/item_db_equip.yml": item_bytes,
        "rathena/db/re/mob_db.yml": mob_bytes,
        "rathena/db/import/item_db.yml": override,
        "rathena/conf/import/battle_conf.txt": battle,
        **shop_outputs,
        **spawn_outputs,
    }

    inputs = {
        **{f"rathena-source/{path}": sha256(data) for path, data in sorted(upstream.items())},
        **{path: sha256(data) for path, data in sorted(local.items())},
    }
    upstream_total_bytes = sum(len(data) for data in upstream.values())
    manifest = {
        "fixture_kind": "pinned-source-only",
        "source_commit": COMMIT,
        "runtime_image_match": "unknown",
        "source_urls": {
            "commit": f"https://github.com/rathena/rathena/commit/{COMMIT}",
            "tree": TREE,
            "raw": RAW,
        },
        "source_aggregate": {
            "npc_files": EXPECTED_NPC_FILES,
            "files_in_hash": len([path for path in upstream if path == "LICENSE" or path.startswith(NPC_PREFIXES)]),
            "bytes_fetched": upstream_total_bytes,
            "sha256_license_and_npc_sources": aggregate_hash(
                {path: data for path, data in upstream.items() if path == "LICENSE" or path.startswith(NPC_PREFIXES)}
            ),
            "sha256_item_db_equip": sha256(upstream["db/re/item_db_equip.yml"]),
            "sha256_mob_db": sha256(upstream["db/re/mob_db.yml"]),
        },
        "license": {
            "identifier": "GPL-3.0-or-later",
            "upstream_path": "LICENSE",
            "copied_to": "rathena/LICENSE",
            "notice": EXPECTED_NOTICE,
            "file_notices": notices,
        },
        "demand": demand,
        "build_coverage": build_coverage,
        "availability": availability,
        "exclusions": [
            "No map cache, warp graph, map geometry, XP tables, cards, ammo, or unrelated item records.",
            "No scripts or binaries are executed; NPC and item/mob records are parsed as untrusted static text.",
            "Ten demanded items with no shop or spawned drop source remain explicitly unsourced.",
            "The fixture is pinned source data, not a verified match to the running server image.",
        ],
        "input_sha256": inputs,
    }
    write_fixture(args.output.resolve(), outputs, manifest)
    print(
        f"fixture: {args.output.resolve()}\n"
        f"demand: {demand['build_records']} guide builds, {demand['weapon_ids']} weapons, "
        f"{demand['armor_ids']} armors, {len(demand['item_ids'])} unique items\n"
        f"build coverage: {build_coverage['registered_build_variants']} variants, "
        f"{len(build_coverage['direct_guide_build_ids'])} direct guides, "
        f"{len(build_coverage['base_guide_aliases'])} base aliases, "
        f"{len(build_coverage['uncovered_build_ids'])} uncovered\n"
        f"availability: shops={availability['counts']['shop']} spawned_drops={availability['counts']['spawned_drop']} "
        f"normal_farms={availability['counts']['normal_farm']} both={availability['counts']['both']} "
        f"neither={availability['counts']['neither']}\n"
        f"source bytes: {upstream_total_bytes}; output bytes: {manifest['output_bytes']}"
    )


if __name__ == "__main__":
    main()
