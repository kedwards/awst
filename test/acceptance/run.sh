#!/usr/bin/env bash
# Acceptance smoke for `awst run` — no real AWS calls.

set -euo pipefail

BIN="${BIN:-dist/awst}"
[[ -x "$BIN" ]] || { echo "binary not found at $BIN; run: task build"; exit 2; }

fail() { echo "FAIL: $*" >&2; exit 1; }

# 1. --help works
out=$("$BIN" run --help)
echo "$out" | grep -q "run" || fail "help missing usage: $out"
echo "$out" | grep -q -- "-q" || fail "help missing -q: $out"
echo "$out" | grep -q -- "-d" || fail "help missing -d: $out"

# 2. -d to a nonexistent dir errors
if "$BIN" run -d /no/such/dir >/dev/null 2>&1; then
  fail "missing -d directory should fail"
fi

# 3. List mode with fixture dir
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
cat > "$dir/vpc-cidrs" <<'EOF'
#!/bin/sh
# Show VPC CIDRs across profiles
aws ec2 describe-vpcs
EOF
cat > "$dir/instances" <<'EOF'
#!/bin/sh
# List EC2 instances
aws ec2 describe-instances
EOF
chmod +x "$dir/instances"

out=$("$BIN" run -d "$dir")
echo "$out" | grep -q "vpc-cidrs" || fail "list missing vpc-cidrs: $out"
echo "$out" | grep -q "Show VPC CIDRs" || fail "list missing description: $out"
echo "$out" | grep -q "instances\*" || fail "list missing executable marker: $out"
echo "$out" | grep -q "executable script" || fail "list missing legend: $out"

# 4. Unknown command errors
if "$BIN" run -d "$dir" ghost-cmd >/dev/null 2>&1; then
  fail "unknown command should fail"
fi

# 5. Too many positional args
if "$BIN" run -d "$dir" a b c >/dev/null 2>&1; then
  fail "more than 2 positional args should fail"
fi

echo "acceptance OK"
