#!/usr/bin/env bash
# gocheck_test.sh - the WSL route of gocheck.sh, offline. Run: bash tools/dev/tests/gocheck_test.sh
# Checks: a missing route exits non-zero and is not reported as a pass; a route found on PATH or by
# GOCHECK_WSL_EXE is used with the WSL arguments and its exit code is passed on; the real run passes.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
GC="$HERE/../gocheck.sh"
T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
fails=0
ok()   { echo "ok   $1"; }
bad()  { echo "FAIL $1"; fails=$((fails+1)); }

inside_wsl=0
if grep -qi microsoft /proc/sys/kernel/osrelease 2>/dev/null || [ -n "${WSL_DISTRO_NAME:-}" ]; then inside_wsl=1; fi

# a stub wsl that records its arguments and exits with $STUB_RC
cat > "$T/wsl-stub" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STUB_LOG"
exit "${STUB_RC:-0}"
STUB
chmod +x "$T/wsl-stub"
export STUB_LOG="$T/args.log"

# 1. no route: exit non-zero, say nothing was checked, never print "vet: ok"
if [ "$inside_wsl" = 1 ]; then
  ok "no-route case skipped: inside WSL the checks run directly"
else
  out="$(GOCHECK_WSL_EXE="$T/missing-wsl.exe" bash "$GC" --fast 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ] && echo "$out" | grep -q "no route to WSL" && ! echo "$out" | grep -q "vet: ok"; then
    ok "no route: exit $rc, not a pass"
  else
    bad "no route: exit $rc, output: $out"
  fi
fi

# 2. GOCHECK_WSL_EXE is used with the WSL arguments, and its exit code is passed on
if [ "$inside_wsl" = 1 ]; then
  ok "stub route case skipped inside WSL"
else
  : > "$STUB_LOG"; STUB_RC=3 GOCHECK_WSL_EXE="$T/wsl-stub" bash "$GC" --fast >/dev/null 2>&1; rc=$?
  if [ "$rc" -eq 3 ] && grep -q -- "-d Ubuntu-24.04 --exec bash -lc" "$STUB_LOG"; then
    ok "GOCHECK_WSL_EXE: stub called with the WSL arguments, exit 3 passed on"
  else
    bad "GOCHECK_WSL_EXE: exit $rc, args: $(cat "$STUB_LOG")"
  fi
fi

# 3. wsl on PATH is found when GOCHECK_WSL_EXE is not set
if [ "$inside_wsl" = 1 ]; then
  ok "PATH case skipped inside WSL"
else
  mkdir -p "$T/bin" && cp "$T/wsl-stub" "$T/bin/wsl"
  : > "$STUB_LOG"; env -u GOCHECK_WSL_EXE PATH="$T/bin:$PATH" STUB_RC=0 bash "$GC" --fast >/dev/null 2>&1; rc=$?
  if [ "$rc" -eq 0 ] && grep -q -- "--exec bash -lc" "$STUB_LOG"; then
    ok "wsl on PATH: used"
  else
    bad "wsl on PATH: exit $rc, args: $(cat "$STUB_LOG")"
  fi
fi

# 4. the real route, when this host has one: the toolchain runs and vet passes
out="$(bash "$GC" --fast 2>&1)"; rc=$?
if [ "$rc" -eq 0 ] && echo "$out" | grep -q "vet: ok"; then
  ok "real run: vet ok, exit 0"
elif echo "$out" | grep -q "no route to WSL"; then
  bad "real run: no route to WSL on this host"
else
  bad "real run: exit $rc: $out"
fi

# 5. a -run pattern that matches no test is a failure, never a pass (nothing was checked)
if [ "$inside_wsl" = 1 ] || command -v wsl >/dev/null 2>&1 || [ -x /c/Windows/System32/wsl.exe ]; then
  out="$(bash "$GC" -run 'ZZNoSuchTestGocheck$' 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ] && echo "$out" | grep -q "NO TESTS RAN"; then
    ok "-run matches nothing: exit $rc, not a pass"
  else
    bad "-run matches nothing: exit $rc, output tail: $(echo "$out" | tail -5)"
  fi
else
  ok "-run case skipped: no WSL route on this host"
fi

echo "gocheck_test: $fails failure(s)"
[ "$fails" -eq 0 ]
