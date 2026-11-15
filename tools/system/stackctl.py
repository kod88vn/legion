#!/usr/bin/env python3
"""Stack markers. `up` = the owner said start (gate/sentinel expect heartbeats, keeper may start Ayla).
`down` = the owner stopped the stack (keeper must not start anything). `status` prints both."""
import os
import sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C


def main(argv):
    cmd = argv[1] if len(argv) > 1 else "status"
    os.makedirs(C.data_dir(), exist_ok=True)
    up, hold = C.dpath("stack.up"), C.dpath("stack.hold")
    if cmd == "up":
        for p in (hold,):
            if os.path.exists(p):
                os.remove(p)
        open(up, "w").write(C.iso() + "\n")
    elif cmd == "down":
        if os.path.exists(up):
            os.remove(up)
        open(hold, "w").write(C.iso() + "\n")
    print("stack.up=%s stack.hold=%s" % (os.path.exists(up), os.path.exists(hold)))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
