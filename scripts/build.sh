#!/usr/bin/env bash
# 重建 Go 版執行檔：先把 skills/myoffice/ 的前端資源同步進 go-app/static/，再 go build。
#   scripts/build.sh
# 產物：go-app/whatsmyagentdoing（Windows 為 .exe），已列入 .gitignore。
# 要讓 SKILL.md 叫得到，接著跑 scripts/installSkill.sh 把產物複製進 skill 的 bin/。
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
app="$root/go-app"

if ! command -v go >/dev/null 2>&1; then
  cat >&2 <<'EOF'
找不到 go 指令。建置需要 Go 1.22 以上：
  macOS:    brew install go
  其他平台: https://go.dev/dl/ 下載安裝
已經裝過的話，多半是 PATH 沒含 Go 的 bin 目錄（/usr/local/go/bin 或 ~/go/bin）；開新終端機或補 PATH 後重跑。
EOF
  exit 127
fi

out="whatsmyagentdoing$(go env GOEXE)"
echo "using $(go version)"
"$app/scripts/sync-static.sh"
(cd "$app" && go build -o "$out" .)
echo "built $app/$out (v$("$app/$out" --version))"
