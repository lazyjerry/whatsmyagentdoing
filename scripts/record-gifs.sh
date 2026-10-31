#!/usr/bin/env bash
# 重錄 README 的三張示範 GIF：docs/office-3d.gif、docs/office-2d.gif、docs/list.gif。
#   scripts/record-gifs.sh               重建 + 安裝執行檔，再以 --demo（虛構團隊）錄製
#   scripts/record-gifs.sh --no-build    沿用 skills/myoffice/bin/ 裡現有的執行檔
# 需要 Google Chrome、ffmpeg，以及 skills/myoffice/ 裡 npm install 過的 playwright。
# 錄製本身在 record-gifs.cjs：英文介面、亮色主題、每秒 10 格、寬 800。
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"

if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "找不到 ffmpeg（macOS: brew install ffmpeg）" >&2
  exit 127
fi
if [ ! -d "$root/skills/myoffice/node_modules/playwright" ]; then
  echo "找不到 playwright：先在 skills/myoffice/ 執行 npm install" >&2
  exit 1
fi

if [ "${1:-}" != "--no-build" ]; then
  "$root/scripts/build.sh"
  "$root/scripts/installSkill.sh"
fi

node "$root/scripts/record-gifs.cjs" "$root"
