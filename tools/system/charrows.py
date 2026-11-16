"""charrows.py - the one builder of a new level-1 Novice's seed rows, shared by tools/pairchars.py and
tools/system/addchars.py so the two cannot drift. The rows are the same as legion/persona.go seedSQL: the char row,
the official starter kit (inventory) and the legion_first / legion_branch registers.

PLANS copies First, Branch and Class from legion/builds.go for the plans these tools create; a test
(tests/system/test_charrows.py) reads builds.go and fails if they differ.
"""
TABLE_ALLOW = ("char", "inventory", "char_reg_num")  # the only tables a seed statement may name
PLANS = {  # plan id -> (legion_first, legion_branch, target class); legion/builds.go First / Branch / Class
    "hunter": (3, 0, 11),
    "blacksmith": (5, 0, 10),
    "alchemist": (5, 1, 18),
}
TOWNS = {  # town -> spawn cell (the seed's townSpawn for the beginner towns)
    "prontera": (156, 191), "izlude": (128, 114), "payon": (162, 233), "geffen": (119, 59), "alberta": (116, 57),
}
SEX_OK = ("M", "F")


def esc(s):
    return str(s).replace("\\", "\\\\").replace("'", "''")


def plan_first_branch(plan):
    """(legion_first, legion_branch, target class) for a plan id from PLANS."""
    if plan not in PLANS:
        raise KeyError("no seed row for plan %r" % (plan,))
    return PLANS[plan]


def seed_rows(char_id, account_id, slot, name, town, sex, rng, first, branch):
    """The three INSERT statements for one new Novice, in the order char, inventory, char_reg_num. rng is any object
    with randrange (the same calls in the same order as the original pairchars code, so output is unchanged)."""
    if town not in TOWNS:
        raise ValueError("unknown town %r" % (town,))
    tx, ty = TOWNS[town]
    sex = sex if sex in SEX_OK else "M"
    cid = int(char_id)
    rows = [
        "INSERT INTO `char` (char_id,account_id,char_num,name,class,base_level,job_level,str,agi,vit,`int`,dex,luk,"
        "max_hp,hp,max_sp,sp,status_point,skill_point,hair,hair_color,clothes_color,zeny,weapon,last_map,last_x,"
        "last_y,save_map,save_x,save_y,sex,`option`) VALUES (%d,%d,%d,'%s',0,1,1,1,1,1,1,1,1,40,40,11,11,48,0,"
        "%d,%d,%d,%d,1201,'%s',%d,%d,'%s',%d,%d,'%s',0);" % (
            cid, int(account_id), int(slot), esc(name), 1 + rng.randrange(22), rng.randrange(9), rng.randrange(4),
            1500 + rng.randrange(1500), town, tx, ty, town, tx, ty, sex),
        "INSERT INTO inventory (char_id,nameid,amount,equip,identify) VALUES (%d,1201,1,2,1),(%d,2301,1,16,1),"
        "(%d,569,%d,0,1),(%d,11500,%d,0,1),(%d,501,%d,0,1),(%d,601,%d,0,1),(%d,602,%d,0,1);" % (
            cid, cid, cid, 120 + rng.randrange(60), cid, 10 + rng.randrange(10), cid, 10 + rng.randrange(10),
            cid, 5 + rng.randrange(5), cid, 2 + rng.randrange(2)),
        "INSERT INTO char_reg_num (char_id,`key`,`index`,value) VALUES (%d,'legion_first',0,%d),"
        "(%d,'legion_branch',0,%d);" % (cid, int(first), cid, int(branch)),
    ]
    return rows


def table_of(stmt):
    """Table name of an INSERT INTO statement, or None when the form is not 'INSERT INTO <t> (' ."""
    import re
    m = re.match(r"^INSERT INTO\s+`?([A-Za-z_]+)`?\s*\(", stmt.strip())
    return m.group(1) if m else None
