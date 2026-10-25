---
name: myoffice
description: 開啟一個即時的「辦公室」畫面，顯示這台機器上每個 Claude Code、Codex 與 Gemini CLI agent 的狀態：誰在思考、誰在執行工具、誰在撰寫回覆、誰在等使用者回應、誰已經完成。當使用者輸入 /myoffice、問自己的 agent 在做什麼、想看所有執行中的 session（工作階段）、或想知道哪個 agent 卡住了或已完成（「我的 agent 在幹嘛」、「show my agents」）時使用。
---

# myoffice

啟動儀表板。它會在本機架起一個網頁，並用瀏覽器開啟。
請**在背景執行**，讓它持續運作、不阻塞對話
（本 skill 的基底目錄會顯示在上方的「Base directory for this skill」）：

```bash
node "<base directory>/bin/cli.js"
```

若儀表板已在執行中，同一道指令只會重新開啟瀏覽器分頁。

要**更新**時（使用者輸入 `/myoffice update`，或要求更新、升級）：
執行 `node "<base directory>/bin/cli.js" --update`（不要在背景執行），
回報它印出的版本號，再依上述方式重新啟動。頁面角落也會顯示版本號，
按「檢查更新」後若有新版會出現「Update」按鈕。

要**關閉**時（使用者輸入 `/myoffice stop`，或要求停止、關閉、收掉辦公室）：
執行下列指令（不要在背景執行）：

```bash
node "<base directory>/bin/cli.js" --stop
```

然後告知已停止。
接著用一行話告知使用者，辦公室已在印出的網址開啟。
若輸出中還出現 **「Update available: vA → vB」**（只有 `src/config.js` 的 `AUTO_UPDATE_CHECK` 開啟時才會印），
告知使用者有新版可用，並提議幫忙更新（見上方的 `/myoffice update`）。
需要 Node.js 18 以上；若找不到 `node`，直接說明並停止。

## 場景（3D）

除了內建的辦公室，還有可下載的 3D 場景（例如海盜灣）。當使用者要換一個場景、
換個外觀、提到「DLC」，或直接指名（「海盜船」、「pirate」）時：

1. `node "<base directory>/bin/cli.js" --scenes`：列出所有場景、是否已安裝、
   下載大小。請使用它印出的 id，不要用猜的。
2. `node "<base directory>/bin/cli.js" --scene <id>`（在背景執行）：需要時先下載
   （經總和檢查碼驗證，只含資料檔），再以該場景開啟辦公室。`--scene office`
   可切回辦公室。

場景只在使用者要求時才下載。`--remove-scene <id>` 可刪除場景。

使用者可能會要求的選項：

- `--private`：隱藏 prompt（提示詞）與回覆，只顯示狀態（適合螢幕分享時使用）
- `--hours 4`：只顯示最近 4 小時內有活動的 session（預設 24 小時）
- `--port 5000`：改用其他連接埠（預設 4545）
- `--stop`：停止執行中的儀表板
- `--json`：印出一次性的快照，不開啟網頁。當使用者在對話中詢問其他 agent
  在做什麼時使用：執行 `node "<base directory>/bin/cli.js" --json --hours 2`，
  再依狀態摘要說明。

所有資料都從本機的 `~/.claude/projects`、`~/.codex/sessions` 與 `~/.gemini/tmp` 讀取；
伺服器只監聽 127.0.0.1，不會把任何資料送到外部。
