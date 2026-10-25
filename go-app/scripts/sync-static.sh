#!/usr/bin/env bash
# 把 Node skill（skills/myoffice/）的前端資源同步成 go-app/static/ 的內嵌副本。
#   scripts/sync-static.sh          同步（有差異才覆寫）
#   scripts/sync-static.sh --check  只比對，有漂移則以非零結束
# 真源永遠是 skills/myoffice/ 的 public/、assets/、scenes.json、package.json；不要直接編輯 static/。
# 只在開發時用；建好的執行檔不依賴這些檔案。
set -euo pipefail

here="$(cd "$(dirname "$0")/.." && pwd)"
root="$(cd "$here/../skills/myoffice" && pwd)"
dst="$here/static"
check=0
[ "${1:-}" = "--check" ] && check=1

for p in public assets scenes.json package.json; do
  [ -e "$root/$p" ] || { echo "missing $root/$p" >&2; exit 2; }
done

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/public" "$tmp/assets"
# 只收網頁真的會用到的檔案；.DS_Store 這類隱藏檔 go:embed 本來就會跳過，但不要讓它進版控。
(cd "$root/public" && find . -type f ! -name '.*' -print0 | xargs -0 -I{} sh -c 'mkdir -p "$0/$(dirname "{}")" && cp "{}" "$0/{}"' "$tmp/public")
(cd "$root/assets" && find . -type f ! -name '.*' -print0 | xargs -0 -I{} sh -c 'mkdir -p "$0/$(dirname "{}")" && cp "{}" "$0/{}"' "$tmp/assets")
cp "$root/scenes.json" "$tmp/scenes.json"
# 版本號只取 package.json 的 version 一行，Go 端讀這個檔，不解析 package.json。
sed -n 's/^[[:space:]]*"version":[[:space:]]*"\([^"]*\)".*/\1/p' "$root/package.json" | head -1 > "$tmp/VERSION"
[ -s "$tmp/VERSION" ] || { echo "cannot read version from $root/package.json" >&2; exit 2; }

if [ "$check" = 1 ]; then
  if [ -d "$dst" ] && diff -rq "$tmp" "$dst" >/dev/null; then
    echo "static/ is in sync"
    exit 0
  fi
  echo "static/ drifted from the clone root; run scripts/sync-static.sh" >&2
  [ -d "$dst" ] && diff -rq "$tmp" "$dst" >&2 || true
  exit 1
fi

if [ -d "$dst" ] && diff -rq "$tmp" "$dst" >/dev/null; then
  echo "static/ already in sync"
  exit 0
fi
rm -rf "$dst"
mv "$tmp" "$dst"
trap - EXIT
echo "synced static/ (version $(cat "$dst/VERSION"))"
