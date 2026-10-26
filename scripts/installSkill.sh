#!/usr/bin/env bash
# 把 scripts/build.sh 建好的執行檔複製進 skill 的 bin/，讓 SKILL.md 呼叫。
#   scripts/installSkill.sh               複製到本倉庫的 skills/myoffice/bin/
#   scripts/installSkill.sh <skill 目錄>  複製到別處的 myoffice skill（例如 ai-global 裝出來的那份）
# 先寫暫存檔再改名：執行中的舊儀表板不受影響，下次啟動就是新版。
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
app="$root/go-app"
skill="${1:-$root/skills/myoffice}"

src=""
for f in "$app/whatsmyagentdoing" "$app/whatsmyagentdoing.exe"; do
  if [ -f "$f" ]; then
    src="$f"
    break
  fi
done
[ -n "$src" ] || { echo "找不到 $app/whatsmyagentdoing，先跑 scripts/build.sh" >&2; exit 2; }
[ -f "$skill/SKILL.md" ] || { echo "$skill 不是 skill 目錄（沒有 SKILL.md）" >&2; exit 2; }

dst="$skill/bin/$(basename "$src")"
mkdir -p "$skill/bin"
tmp="$dst.tmp.$$"
cp "$src" "$tmp"
chmod +x "$tmp"
mv -f "$tmp" "$dst"
echo "installed $dst (v$("$dst" --version))"
