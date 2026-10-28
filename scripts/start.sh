#!/usr/bin/env bash
# 重建執行檔、複製進 skill，再（重新）啟動儀表板——改完前端或 Go 之後一條指令看到結果。
#   scripts/start.sh                 重建 + 安裝 + 重啟（預設埠 4545）
#   scripts/start.sh --port 5000     指定埠；其餘旗標原樣傳給執行檔（--private、--hours…）
#   scripts/start.sh --no-build      跳過重建，只重啟（例如剛跑過 build.sh）
# 執行檔把前端 embed 進去，所以舊程序永遠送建置當下的頁面：重建後一定要重啟才看得到改動。
# 原本就在跑的話，重啟時不再開新分頁（既有分頁重新整理即可）；原本沒在跑才開瀏覽器。
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$root/skills/myoffice/bin/whatsmyagentdoing"
port=4545
build=1
args=()
while [ $# -gt 0 ]; do
  case "$1" in
    -p|--port) port="$2"; shift 2 ;;
    --port=*) port="${1#--port=}"; shift ;;
    --no-build) build=0; shift ;;
    *) args+=("$1"); shift ;;
  esac
done
case "$port" in ''|*[!0-9]*) echo "埠必須是數字：$port" >&2; exit 1 ;; esac

ping() { curl -sf --max-time 1 "http://127.0.0.1:$port/api/ping" 2>/dev/null | grep -q whatsmyagentdoing; }

if [ "$build" = 1 ]; then
  "$root/scripts/build.sh"
  "$root/scripts/installSkill.sh"
fi
[ -x "$bin" ] || { echo "找不到 $bin，先跑 scripts/build.sh && scripts/installSkill.sh" >&2; exit 2; }

was_running=0
if ping; then
  was_running=1
  echo "stopping the dashboard on port ${port}…"
  "$bin" --stop --port "$port" >/dev/null || true
  for _ in $(seq 1 30); do ping || break; sleep 0.2; done
  if ping; then
    echo "port $port 上的儀表板沒有停下來，請手動結束後再跑一次" >&2
    exit 3
  fi
fi

# 原本有在跑就不開新分頁；使用者自己給了 --no-open 也照辦。
if [ "$was_running" = 1 ]; then
  case " ${args[*]:-} " in *" --no-open "*) ;; *) args+=(--no-open) ;; esac
fi
tmp="${TMPDIR:-/tmp}"; log="${tmp%/}/whatsmyagentdoing-$port.log"
nohup "$bin" --port "$port" ${args[@]+"${args[@]}"} >"$log" 2>&1 &
pid=$!
for _ in $(seq 1 50); do ping && break; sleep 0.2; done
if ! ping; then
  echo "儀表板沒有在 port $port 起來，看 $log" >&2
  cat "$log" >&2 || true
  exit 4
fi
echo "dashboard v$("$bin" --version) running at http://localhost:$port (pid $pid, log $log)"
[ "$was_running" = 1 ] && echo "原本就在執行，已重啟；重新整理既有分頁即可。"
exit 0
