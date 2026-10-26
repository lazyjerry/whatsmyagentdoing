#!/usr/bin/env bash
# 停掉執行中的儀表板，等它真的離開再結束——start.sh 的反向操作。
#   scripts/stop.sh                 停掉預設埠 4545（執行檔的 --stop 會順便掃到 4555，預設埠被占時它會往後找）
#   scripts/stop.sh --port 5000     指定埠
# 不重建、不動 skill 的 bin/：只送關閉請求給執行中的程序。
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$root/skills/myoffice/bin/whatsmyagentdoing"
port=4545
while [ $# -gt 0 ]; do
  case "$1" in
    -p|--port) port="$2"; shift 2 ;;
    --port=*) port="${1#--port=}"; shift ;;
    *) echo "用法：scripts/stop.sh [--port <n>]" >&2; exit 1 ;;
  esac
done
case "$port" in ''|*[!0-9]*) echo "埠必須是數字：$port" >&2; exit 1 ;; esac

ping() { curl -sf --max-time 1 "http://127.0.0.1:$port/api/ping" 2>/dev/null | grep -q whatsmyagentdoing; }

[ -x "$bin" ] || { echo "找不到 $bin，先跑 scripts/build.sh && scripts/installSkill.sh" >&2; exit 2; }

"$bin" --stop --port "$port"
for _ in $(seq 1 30); do ping || break; sleep 0.2; done
if ping; then
  echo "port $port 上的儀表板沒有停下來，請手動結束" >&2
  exit 3
fi
exit 0
