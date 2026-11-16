#!/usr/bin/env bash
# gocheck.sh - compile and unit-test the Go engine without Docker (portable Go in WSL ~/go-toolchain).
# usage: bash gocheck.sh [--fast] [-run <regex>] [engine-dir]     (Windows or /g/ style path)
#   --fast   gofmt + vet + build only (no tests)
#   -run RE  passed to go test
# Failures listed in baseline-failures.txt (next to this script; one test name per line, # comments)
# are reported but do not fail the run. Exit 1 on vet/build failure, any non-baseline test failure, or when no test
# ran at all (a -run pattern that matches nothing is a failure: nothing was checked).
# Exit 2 when no route to WSL works or the Go toolchain is missing: that is never a pass (nothing was checked).
#
# Routes to WSL, first that exists: GOCHECK_WSL_EXE (only that one, for tests); `wsl` on PATH;
# /c/Windows/System32/wsl.exe; $SYSTEMROOT/System32/wsl.exe. Already inside WSL: run the checks directly.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
FAST=0; RUN=""; DIR=""
while [ $# -gt 0 ]; do
  case "$1" in
    --fast) FAST=1 ;;
    -run) shift; RUN="$1" ;;
    *) DIR="$1" ;;
  esac
  shift
done
[ -n "$DIR" ] || DIR="$(cd "$HERE/../.." && pwd)/legion"
# to a WSL path
case "$DIR" in
  [A-Za-z]:[\/]*) d="${DIR//\//}"; DIR="/mnt/$(echo "${d:0:1}" | tr A-Z a-z)${d:2}" ;;
  /[a-zA-Z]/*) DIR="/mnt${DIR}" ;;
esac
BASE="$(grep -v '^\s*#' "$HERE/baseline-failures.txt" 2>/dev/null | sed 's/\s*#.*//' | grep -v '^\s*$' | tr '\n' ' ')"
export WSL_DIR="$DIR" WSL_FAST="$FAST" WSL_RUN="$RUN" WSL_BASE="$BASE" WSLENV="WSL_DIR:WSL_FAST:WSL_RUN:WSL_BASE"
INNER='
set -u
export PATH="$HOME/go-toolchain/go/bin:$PATH" GOCACHE="$HOME/.cache/go-build-gocheck" GOFLAGS=-mod=mod GOTOOLCHAIN=local GOPROXY=off
command -v go >/dev/null 2>&1 || { echo "gocheck: no Go toolchain in WSL (~/go-toolchain/go/bin). Nothing was checked."; exit 2; }
SRC="$WSL_DIR"; [ -f "$SRC/go.mod" ] || { echo "no go.mod in $SRC"; exit 2; }
T="$(mktemp -d "$HOME/gocheck.XXXXXX")"; trap "rm -rf $T" EXIT
cd "$SRC" && cp go.mod *.go "$T"/ 2>/dev/null
cp *.html recvpackets.txt "$T"/ 2>/dev/null
cd "$T"
echo "go: $(go version)  src: $SRC"
FMT="$(gofmt -l . )"
if [ -n "$FMT" ]; then echo "gofmt: UNFORMATTED (not fatal): $(echo $FMT | tr "\n" " ")"; else echo "gofmt: clean"; fi
rc=0
if go vet ./... >vet.log 2>&1; then echo "vet: ok"; else echo "vet: FAIL"; head -30 vet.log; rc=1; fi
if [ "$WSL_FAST" = 1 ]; then
  if go build -o /dev/null ./... >build.log 2>&1; then echo "build: ok"; else echo "build: FAIL"; head -30 build.log; rc=1; fi
  exit $rc
fi
ARGS=""; [ -n "$WSL_RUN" ] && ARGS="-run $WSL_RUN"
go test -count=1 -v $ARGS ./... >test.log 2>&1
P=$(grep -c "^--- PASS" test.log); F=$(grep -c "^--- FAIL" test.log); S=$(grep -c "^--- SKIP" test.log)
echo "tests: pass=$P fail=$F skip=$S"
TOT=$(grep -cE "^--- (PASS|FAIL|SKIP)" test.log)
if [ "$TOT" -eq 0 ]; then echo "tests: NO TESTS RAN (-run '$WSL_RUN' matched nothing, or the engine has no tests): nothing was checked"; rc=1; fi
grep -E "^(FAIL|ok|panic:)|build failed|\[setup failed\]" test.log | head -10
NEW=""; BL=""
for t in $(grep "^--- FAIL" test.log | awk "{print \$3}"); do
  case " $WSL_BASE " in *" $t "*) BL="$BL $t" ;; *) NEW="$NEW $t" ;; esac
done
[ -n "$BL" ] && echo "baseline failures (ignored):$BL"
if [ -n "$NEW" ]; then echo "FAILING:$NEW"; rc=1; fi
if grep -q "^FAIL" test.log && [ -z "$NEW" ] && [ -z "$BL" ]; then echo "package FAIL without test failure (build/panic):"; grep -B2 -A15 -E "panic:|build failed|^FAIL" test.log | head -40; rc=1; fi
cp test.log "$HOME/gocheck-last-test.log"
echo "skips: $(grep "^--- SKIP" test.log | awk "{print \$3}" | tr "\n" " ")"
echo "(full log in WSL ~/gocheck-last-test.log)"
exit $rc
'
if grep -qi microsoft /proc/sys/kernel/osrelease 2>/dev/null || [ -n "${WSL_DISTRO_NAME:-}" ]; then
  bash -lc "$INNER"; exit $?                       # already inside WSL
fi
WSL_EXE=""
if [ -n "${GOCHECK_WSL_EXE:-}" ]; then
  [ -x "$GOCHECK_WSL_EXE" ] && WSL_EXE="$GOCHECK_WSL_EXE"
elif command -v wsl >/dev/null 2>&1; then
  WSL_EXE="$(command -v wsl)"
elif [ -x /c/Windows/System32/wsl.exe ]; then
  WSL_EXE=/c/Windows/System32/wsl.exe
elif [ -n "${SYSTEMROOT:-}" ] && [ -x "$(cygpath -u "$SYSTEMROOT" 2>/dev/null)/System32/wsl.exe" ]; then
  WSL_EXE="$(cygpath -u "$SYSTEMROOT")/System32/wsl.exe"
fi
if [ -z "$WSL_EXE" ]; then
  echo "gocheck: no route to WSL (wsl.exe not on PATH, not in /c/Windows/System32 or \$SYSTEMROOT). Nothing was checked." >&2
  exit 2
fi
MSYS_NO_PATHCONV=1 "$WSL_EXE" -d Ubuntu-24.04 --exec bash -lc "$INNER"
exit $?
