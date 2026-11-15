#!/usr/bin/env python3
"""Stack markers. `up` = the owner said start (gate/sentinel expect heartbeats, keeper may start Ayla).
`down` = the owner stopped the stack (keeper must not start anything; the puppet dispatcher wakes nobody while
stack.hold exists). `minds-pause` / `minds-resume` = only the puppet dispatcher (legion/data/puppet.pause); the
stack markers are untouched, the dispatcher keeps its heartbeat (state "paused"). `status` prints all three."""
import os
import sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C


def main(argv):
    cmd = argv[1] if len(argv) > 1 else "status"
    os.makedirs(C.data_dir(), exist_ok=True)
    up, hold, pause = C.dpath("stack.up"), C.dpath("stack.hold"), C.dpath("puppet.pause")
    if cmd == "up":
        for p in (hold,):
            if os.path.exists(p):
                os.remove(p)
        open(up, "w").write(C.iso() + "\n")
    elif cmd == "down":
        if os.path.exists(up):
            os.remove(up)
        open(hold, "w").write(C.iso() + "\n")
    elif cmd == "minds-pause":
        open(pause, "w").write(C.iso() + "\n")
    elif cmd == "minds-resume":
        if os.path.exists(pause):
            os.remove(pause)
    elif cmd != "status":
        print("usage: stackctl.py up|down|minds-pause|minds-resume|status")
        return 2
    print("stack.up=%s stack.hold=%s puppet.pause=%s" % (os.path.exists(up), os.path.exists(hold), os.path.exists(pause)))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
