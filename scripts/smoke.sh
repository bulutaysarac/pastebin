#!/usr/bin/env bash
# End-to-end check against a running stack: docker compose up -d && ./scripts/smoke.sh
set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
fail() { echo "FAIL: $*" >&2; exit 1; }

# 1. Home page
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/")
[ "$code" = 200 ] || fail "home page returned $code"
echo "ok   home page"

# 2. Create a paste that never expires. Follow nothing; read the redirect.
content='hello from smoke.sh 🚀 <b>not bold</b>'
loc=$(curl -s -o /dev/null -w '%{redirect_url}' \
  --data-urlencode "content=$content" --data-urlencode 'expires=never' "$BASE/paste")
id="${loc##*/}"
[ "${#id}" = 8 ] || fail "expected an 8-char id in the redirect, got '$loc'"
echo "ok   created /$id"

# 3. Read it back
headers=$(mktemp); body=$(curl -s -D "$headers" "$BASE/$id")
grep -q '200' <(head -1 "$headers") || fail "GET /$id: $(head -1 "$headers")"
grep -qi '^cache-control: private, max-age=86400' "$headers" || fail "unexpected Cache-Control: $(grep -i cache-control "$headers")"
grep -q 'hello from smoke.sh 🚀 &lt;b&gt;not bold&lt;/b&gt;' <<<"$body" || fail "content missing or not escaped"
echo "ok   read /$id (Cache-Control: private, max-age=86400)"

# 4. IDs are case-sensitive: the same letters in another case must not match
other=$(tr 'a-zA-Z' 'A-Za-z' <<<"$id")
if [ "$other" != "$id" ]; then
  code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/$other")
  [ "$code" = 404 ] || fail "/$other (case-swapped) returned $code, want 404"
  echo "ok   /$other (case-swapped) is 404"
fi

# 5. A 10 minute paste is cached at most 600s
loc=$(curl -s -o /dev/null -w '%{redirect_url}' --data-urlencode 'content=short' --data-urlencode 'expires=10m' "$BASE/paste")
maxage=$(curl -s -D - -o /dev/null "$BASE/${loc##*/}" | tr -d '\r' | sed -n 's/^[Cc]ache-[Cc]ontrol: private, max-age=//p')
[ "$maxage" -gt 0 ] && [ "$maxage" -le 600 ] || fail "10m paste max-age=$maxage, want 1..600"
echo "ok   10m paste max-age=$maxage"

# 6. Unknown id and bad expiry
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/NOPE1234")
[ "$code" = 404 ] || fail "unknown id returned $code"
code=$(curl -s -o /dev/null -w '%{http_code}' --data 'content=x&expires=forever' "$BASE/paste")
[ "$code" = 400 ] || fail "bad expiry returned $code"
echo "ok   unknown id → 404, bad expiry → 400"

echo "all smoke checks passed"
