# 👀 whatsmyagentdoing

**一個 Claude Code / Codex skill：輸入 `/myoffice`，即時看到你機器上每一個 Claude Code、Codex 與 Gemini CLI agent 正在做什麼——在一間小小的辦公室裡。**

![3D 辦公室：agent 坐在座位上工作，下班的在四處閒晃](docs/office-3d.gif)

| Medium（2D） | 清單 |
|---|---|
| ![2D 辦公室](docs/office-2d.gif) | ![清單檢視](docs/list.gif) |

<sub>以 `--demo`（虛構團隊）錄製，畫面中沒有任何真實 session。</sub>

## 安裝

**最簡單：請你的 agent 幫你裝。** 把這段貼進 Claude Code 或 Codex：

> Install the myoffice skill from https://github.com/lazyjerry/whatsmyagentdoing: clone it with `git clone --depth 1` into `~/.claude/skills/myoffice` (for Codex: `~/.codex/skills/myoffice`), check that `node --version` is 18 or newer, then tell me to start a new session and type /myoffice.

或自己動手：

```bash
# Claude Code
git clone --depth 1 https://github.com/lazyjerry/whatsmyagentdoing ~/.claude/skills/myoffice

# Codex
git clone --depth 1 https://github.com/lazyjerry/whatsmyagentdoing ~/.codex/skills/myoffice
```

或用 [ai-global](https://github.com/lazyjerry/ai-global) 一次裝給所有 AI 工具：

```bash
ai-global add-skill lazyjerry/whatsmyagentdoing
```

skill 會裝進 `~/.ai-global/v-skills/` 並以 symlink 同步給 Claude Code、Codex 等工具；之後用 `ai-global update-skills` 更新，`ai-global remove-skill lazyjerry/whatsmyagentdoing` 移除。

接著輸入 **`/myoffice`**（或直接問「我的 agent 在做什麼？」）。辦公室會在瀏覽器裡開啟。之後要更新就跑 `git -C ~/.claude/skills/myoffice pull`（ai-global 安裝的則用 `ai-global update-skills`）。

需要 Node.js 18 以上。不必 `npm install`——clone 下來就能跑。不想用 skill？從 clone 目錄執行 `node bin/cli.js` 也行。

開啟的是一間等角視角的**辦公室**。正在工作的 agent 坐在隔間座位上：思考時冒出思考泡泡、執行工具時打字且螢幕上有程式碼捲動、需要你介入時會站起來揮手。最近剛完工的幾個 agent 會留在辦公室裡下班閒晃：到茶水間喝咖啡、喝水，坐沙發、懶骨頭，逛書架，在會議室白板前塗寫，打街機、打桌球或手足球（兩個人都有空時），進電話亭，或找張空桌小睡。其他人都回家了。窗景跟著你的本地時間變化（Medium 模式還有一隻辦公室貓）。滑鼠移到任何人身上就能看到它正在處理什麼。

兩種畫風：**Medium** 是手繪風 2D canvas（零依賴）。**High（3D）** 用即時光影渲染同一間辦公室，採用 [Kenney](https://kenney.nl) 的 CC0 *Mini Characters*（含走路／坐下／互動動畫）與 *Furniture Kit*；拖曳旋轉、捲動縮放。3D bundle（three.js，gzip 後約 180 KB）與模型（約 1.3 MB，meshopt 壓縮）只在選擇 High 時才載入。

**場景（3D）：** 辦公室內建；其他場景是可下載的小型場景包（Pirate Cove、Café、Space Station、Castle 各 100–160 KB），只在你從場景按鈕點選、執行 `--scene pirate`、或請 agent 幫你切換時才會抓取。場景包只有資料（一份 manifest 加上 Kenney CC0 模型），安裝到 `~/.whatsmyagentdoing/scenes` 前會先比對釘住的 SHA-256。

偏好純卡片？切到**清單**檢視。不論哪種檢視，每個 session 都是即時更新：

| 狀態 | 意義 |
|---|---|
| 💭 思考中 | 正在推理，或在讀工具結果 |
| ⏳ 執行中 | 有工具呼叫正在跑——會顯示是哪個（`Bash: npm test`、`Edit app.ts`、`github · create_pr`） |
| ✍️ 撰寫回覆 | 已開始回答，但這一輪還沒結束 |
| 🟠 等你回應 | 問了你問題／需要你核准 |
| ✅ 完成 | 這一輪結束，附上最後的回覆 |
| ■ 中斷／❗ 錯誤／– 閒置 | |

介面支援 English、繁體中文、简体中文、日本語、한국어、Español、Français、Deutsch 與 Português。預設跟著你寫 prompt 的語言（沒有就跟瀏覽器語言）；可從 🌐 選單手動指定。

清單檢視中，session 分成 **需要你 → 工作中 → 已完成** 三組，附目前這一輪的即時計時器、resume 指令的複製按鈕（`claude --resume …` / `codex resume …`）、agent 完工時的瀏覽器通知（可選），以及 Claude / Codex / subagent 篩選器。分頁標題會顯示有多少個 agent 在工作。

**Agent 徽章：** 2D 與 3D 中，每個 agent（含 subagent）名字旁都有透明底的 Claude、Codex、Gemini 官方 logo。相同圖示也出現在圖例、清單卡片與篩選器。到 **設定 → Agent 徽章** 可隱藏辦公室標記與圖例，設定會保存。圖示隨套件附帶於本機，圖片載入失敗時以縮寫字母替代。詳見[圖示來源與授權](assets/agents/README.md)。

工具列固定顯示檢視、2D/3D 與彈出視窗控制項。場景選擇、agent 篩選、顯示選項、通知與語言則收在**設定**裡。

**彈出視窗：** 支援 [Document Picture-in-Picture API](https://developer.chrome.com/docs/web-platform/document-picture-in-picture) 的瀏覽器可以把辦公室或清單開成可互動、永遠置頂的視窗。精簡清單只顯示 **需要你** 與 **工作中**。瀏覽器儲存可用時，檢視、畫質、場景、篩選、語言與徽章設定會與主分頁同步。關閉彈出視窗會還原主檢視，且只有主分頁會送通知。

當 Document PiP 不可用時，辦公室檢視的 **彈出** 會改為啟動本機的唯讀影片預覽（2D 或 3D 辦公室）。用它的影片 PiP 按鈕，或瀏覽器自己的影片控制項／右鍵選單（例如 Firefox）把它浮起來。用完請停止預覽。不需要螢幕分享權限，也不會上傳任何影片。背景動畫可能因瀏覽器節流而降低幀率；這個 fallback 不支援清單卡片，也不能點選 agent。

支援 Claude Code（CLI、桌面版、IDE）、Codex（CLI、桌面版、IDE）與 Gemini CLI——只要會把 session log 寫到磁碟的工具都行。（Google Antigravity 把對話存在未公開的二進位儲存裡，目前還不支援。）

## 選項

```
-p, --port <n>    連接埠（預設 4545；被佔用時取下一個可用的）
--hours <n>       只顯示最近 n 小時內有活動的 session（預設 24）
--no-open         不開啟瀏覽器
--private         隱藏 prompt、回覆與標題——只顯示狀態（螢幕分享安全）
--json            輸出一份 JSON 快照後結束
--demo            顯示虛構團隊而非你的 session（截圖、試玩用）
--stop            停止正在執行的 dashboard（或說 /myoffice stop）
--update          git pull 最新版並重啟（或說 /myoffice update）
--scenes          列出 3D 場景，含已安裝與可下載的
--scene <id>      視需要下載場景，並以該場景開啟辦公室
```

執行中再跑一次只會重新開啟分頁。

**更新：** 頁面角落顯示版本號與 **檢查更新** 按鈕；只有按下時才會抓一次本 repo main 分支的 `package.json`，不會自動檢查，有新版時改顯示 **Update** 按鈕。要改成自動（頁面載入與每半小時、CLI 啟動時都檢查），把 `src/config.js` 的 `AUTO_UPDATE_CHECK` 改成 `true`；設定 `WMAD_NO_UPDATE_CHECK=1` 則兩種模式都不檢查。按下按鈕——或執行 `--update`、或說 `/myoffice update`——會在安裝目錄執行 `git pull` 並重啟 dashboard。

## 運作原理

不需要 hook、不需要設定、不需要 API key。這些工具本來就會在工作時把每則訊息與工具呼叫 append 到 JSONL 檔：

- Claude Code：`~/.claude/projects/<project>/<session>.jsonl`（＋ `subagents/`）
- Codex：`~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`
- Gemini CLI：`~/.gemini/tmp/<project>/chats/session-*.jsonl`（專案路徑取自 `~/.gemini/projects.json`）

`whatsmyagentdoing` 以唯讀方式 tail 這些檔案，把最新事件轉成狀態，再透過 Server-Sent Events 推到頁面。檔案位置不同的話，設定 `CLAUDE_CONFIG_DIR` / `CODEX_HOME` / `GEMINI_CLI_HOME` 即可。

**隱私：** 沒有任何東西離開你的機器。伺服器只綁定 `127.0.0.1`，並拒絕非 localhost 的請求。沒有執行期依賴——五分鐘就能讀完 `src/`。（`web/office3d.js` 是 3D 原始碼；`npm run build` 會把它與 three.js 打包成 `public/office3d.js`。）

## 限制

- Log 是以完成的區塊為單位寫入，不是逐 token，所以「撰寫回覆」代表一個回覆區塊已落地且這一輪還沒結束——不是即時的按鍵串流。
- Claude Code 不會記錄權限提示，所以等待你核准的工具會一直顯示「執行中」直到你回應（Codex 與 Gemini CLI 會記錄，因此會顯示「等你回應」）。
- Gemini CLI 把思考過程與回覆一起寫入，所以從你的 prompt 到它第一次工具呼叫或回覆之間，只會顯示「思考中」。
- Log 格式不是公開 API。若更新改了格式，狀態判讀可能退化——請開 issue 回報。

## 開發檢查

開發檢查需要 Node.js 20 以上（dashboard 本身仍只需 Node.js 18 以上）。先 `npm install`，再以 `npm run build` 重建已提交的 3D bundle。若有安裝 Google Chrome，`npm run test:ui` 會對臨時本機伺服器與虛構 session 執行瀏覽器回歸檢查。用 `WMAD_HEADED=1 npm run test:ui` 可看到測試視窗，或設定 `WMAD_SCREENSHOTS` 為輸出資料夾以保存截圖。檢查涵蓋 logo 渲染、精簡檢視、設定同步、通知、PiP 復原、video stream 清理，以及亮／暗主題下的響應式設定。這些檢查使用 Chrome；Firefox 與 Safari 的原生影片控制項仍需手動測試。

## 授權

MIT。`assets/kenney/` 內的 3D 模型由 [Kenney](https://kenney.nl) 製作，以 CC0 釋出（見該目錄的授權檔）。
