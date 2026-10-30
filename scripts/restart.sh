#!/usr/bin/env bash
# 依序跑 build.sh → installSkill.sh → start.sh：重建執行檔、複製進 skill 的 bin/，再（重新）啟動儀表板。
#   scripts/restart.sh                 重建 + 安裝 + 重啟（預設埠 4545）
#   scripts/restart.sh --port 5000     旗標原樣傳給 start.sh（--port、--private、--hours…）
# start.sh 已經在這裡重建過，所以帶 --no-build 呼叫，不重複建置。
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"

"$root/scripts/build.sh"
"$root/scripts/installSkill.sh"
"$root/scripts/start.sh" --no-build "$@"
