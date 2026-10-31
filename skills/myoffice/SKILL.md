---
name: myoffice
description: 開啟一個即時的「辦公室」畫面，顯示這台機器上每個 Claude Code、Codex、Gemini CLI 與 OpenCode agent 的狀態：誰在思考、誰在執行工具、誰在撰寫回覆、誰在等使用者回應、誰已經完成。當使用者輸入 /myoffice、問自己的 agent 在做什麼、想看所有執行中的 session（工作階段）、或想知道哪個 agent 卡住了或已完成（「我的 agent 在幹嘛」、「show my agents」）時使用。
---

# myoffice

啟動儀表板。它會在本機架起一個網頁，並用瀏覽器開啟。
請**在背景執行**，讓它持續運作、不阻塞對話
（本 skill 的基底目錄會顯示在上方的「Base directory for this skill」）：

```bash
"<base directory>/bin/whatsmyagentdoing"
```

若儀表板已在執行中，同一道指令只會重新開啟瀏覽器分頁。
接著用一行話告知使用者，辦公室已在印出的網址開啟。
若輸出中還出現 **「Update available: vA → vB」**（只有 `go-app/config.go` 的 `AutoUpdateCheck` 開啟時才會印），
告知使用者有新版可用，並提議幫忙更新（見下方的 `/myoffice update`）。

## 執行檔不存在時（首次安裝，或 clone 剛更新）

`bin/whatsmyagentdoing` 是從本倉庫 `go-app/` 建出來的 Go 執行檔，不進版控，要在本機建一次：

1. 先確認有 Go：執行 `go version`。**找不到 `go` 就停下來**，告知使用者需要安裝 Go 1.22 以上
   （macOS：`brew install go`；其他平台到 https://go.dev/dl/ 下載），裝好後再輸入一次 `/myoffice`。
2. 有 Go 就建置並複製進 skill（不要在背景執行）：

   ```bash
   root="$(cd -P "<base directory>/../.." && pwd)"
   [ -x "$root/scripts/build.sh" ] || { git clone --depth 1 https://github.com/lazyjerry/whatsmyagentdoing ~/.whatsmyagentdoing/src && root=~/.whatsmyagentdoing/src; }
   "$root/scripts/build.sh" && "$root/scripts/installSkill.sh" "<base directory>"
   ```

   skill 本身就在 clone 的 `skills/myoffice/` 裡時（symlink 安裝），`cd -P` 會解到 clone 根目錄；
   只有被複製出來的 skill（例如 ai-global 裝的那份）才會走 clone 那一步。
3. 回到上面的啟動指令。

要**更新**時（使用者輸入 `/myoffice update`，或要求更新、升級）：
執行 `"<base directory>/bin/whatsmyagentdoing" --update`（不要在背景執行）。
它會 `git pull` 後重新 `go build`，同樣需要 Go；找不到 `go` 就照上面第 1 點提示安裝。
回報它印出的版本號，再依上述方式重新啟動。頁面設定面板最下方顯示版本號與「檢查更新」按鈕，
按下後若有新版會出現「Update」按鈕。
若它回報執行檔不在 clone 裡（not inside its go-app/ source folder），
改走「執行檔不存在時」的第 2 點重建一次。

要**關閉**時（使用者輸入 `/myoffice stop`，或要求停止、關閉、收掉辦公室）：
執行下列指令（不要在背景執行）：

```bash
"<base directory>/bin/whatsmyagentdoing" --stop
```

然後告知已停止。

## 場景

畫面只有內建的辦公室（設定裡可切 2D／3D），沒有其他場景可以切換或下載。
使用者要換場景、提到「DLC」或舊的 `--scene`／`--scenes`／`--remove-scene` 時，
告知這些旗標已移除。舊版下載的場景包留在 `~/.whatsmyagentdoing/scenes/`，不再使用，
使用者同意後才可以刪除。

使用者可能會要求的選項：

- `--private`：隱藏 prompt（提示詞）與回覆，只顯示狀態（適合螢幕分享時使用）
- `--hours 4`：只顯示最近 4 小時內有活動的 session（預設 24 小時）
- `--port 5000`：改用其他連接埠（預設 4545）
- `--tv-dir <資料夾>`：沙發區電視輪播這個資料夾裡的 GIF（預設 `~/.whatsmyagentdoing/tv`；
  沒有 GIF 時播內建的公有領域片段）
- `--stop`：停止執行中的儀表板
- `--json`：印出一次性的快照，不開啟網頁。當使用者在對話中詢問其他 agent
  在做什麼時使用：執行 `"<base directory>/bin/whatsmyagentdoing" --json --hours 2`，
  再依狀態摘要說明。

所有資料都從本機的 `~/.claude/projects`、`~/.codex/sessions`、`~/.gemini/tmp`、`~/.gemini/antigravity/conversation_summaries.db`、`~/.gemini/antigravity-cli/log` 與 `~/.local/share/opencode/opencode.db` 讀取；
伺服器只監聽 127.0.0.1，不會把任何資料送到外部。
