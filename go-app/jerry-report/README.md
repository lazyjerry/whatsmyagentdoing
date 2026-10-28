# whatsmyagentdoing Go 版系統分析報告

依 `go-app/README.md` 與 Go 原始碼整理的系統結構、use case（使用案例）與 UML 圖。圖以 Mermaid 撰寫，GitHub 與 VS Code 的 Markdown 預覽可直接渲染。

- 版本依據：`static/VERSION` `0.3.0`（同步自 `skills/myoffice/package.json`）、`go.mod` 宣告 Go 1.22
- 分析範圍：`go-app/*.go`（11 檔，約 3100 行）、`scripts/sync-static.sh`、`static/`（內嵌副本，只看 `VERSION`）
- 不在範圍：`static/public/` 的網頁與 `static/assets/` 的模型。它們是 `skills/myoffice/` 的逐位元副本，瀏覽器端行為請看上層 [jerry-report](../../jerry-report/README.md)，本報告不重複分析。例外是 UC-07：夜市場景是 Node 版之後才有的，上層沒有對應文件，前端行為一併記在本報告

## 閱讀順序

1. [structure.md](structure.md)：定位、目錄、執行時架構、各檔職責、並行模型、資料模型、紀錄解析規則、狀態機、HTTP API、內嵌與建置管線、安全邊界、環境變數、與 Node 版的差異
2. [use-cases.md](use-cases.md)：參與者、use case 圖、總表、彼此關係、哪些 use case 由前端承擔
3. 各 use case 獨立檔案：每份含摘要表、主要流程、替代流程、流程圖（activity 風格 flowchart）、循序圖、備註

## Use case 一覽

| ID | 名稱 | 主要參與者 | 檔案 |
|---|---|---|---|
| UC-01 | 啟動儀表板 | 使用者／AI Agent | [UC-01-start-dashboard.md](UC-01-start-dashboard.md) |
| UC-02 | 即時監看 agent 狀態 | 使用者 | [UC-02-live-monitoring.md](UC-02-live-monitoring.md) |
| UC-03 | 停止儀表板 | 使用者／AI Agent | [UC-03-stop-dashboard.md](UC-03-stop-dashboard.md) |
| UC-04 | 檢查新版並更新 | 使用者／AI Agent | [UC-04-check-and-update.md](UC-04-check-and-update.md) |
| UC-05 | 取得 JSON 快照 | AI Agent | [UC-05-json-snapshot.md](UC-05-json-snapshot.md) |
| UC-06 | Demo 模式 | 使用者 | [UC-06-demo-mode.md](UC-06-demo-mode.md) |
| UC-07 | 夜市場景 | 使用者 | [UC-07-night-market-scene.md](UC-07-night-market-scene.md) |
| UC-08 | 隱私模式 | 使用者 | [UC-08-private-mode.md](UC-08-private-mode.md) |
| UC-09 | 建置與同步內嵌資源 | 維護者 | [UC-09-build-and-sync.md](UC-09-build-and-sync.md) |

## 與上層 jerry-report 的對應

Go 版只重寫後端。下表說明上層報告的 16 個 use case 在 Go 版的落點。

| 上層 | 名稱 | Go 版 |
|---|---|---|
| UC-01 | 啟動儀表板 | 本報告 UC-01（`main.go`） |
| UC-02 | 即時監看 | 本報告 UC-02（`watcher.go`、`parsers.go`、`server.go`） |
| UC-03 | 停止儀表板 | 本報告 UC-03 |
| UC-04 | 檢查新版並更新 | 本報告 UC-04。更新內容從 `git pull` 變成 `git pull` 加同步加 `go build` |
| UC-05 | JSON 快照 | 本報告 UC-05 |
| UC-06 | Demo 模式 | 本報告 UC-06（`demo.go`） |
| UC-07 | 切換視圖與畫質 | 前端。畫質與 2D 已移除，只剩辦公室（3D 夜市）與列表兩種檢視。見 [上層 UC-07](../../jerry-report/UC-07-switch-view.md) |
| UC-08 | 管理 3D 場景 | 已移除。場景包與內建辦公室改為單一內建的夜市場景，見本報告 UC-07（`dialogues.go`、`/api/street`） |
| UC-09 | 子母畫面 | 前端，無差異。見 [上層 UC-09](../../jerry-report/UC-09-popout.md) |
| UC-10 | 選擇介面語言 | 前端。伺服器端的語言猜測在 `lang.go`，見 structure.md 第 9 節 |
| UC-11 | 完成通知 | 前端，無差異。見 [上層 UC-11](../../jerry-report/UC-11-notifications.md) |
| UC-12 | 隱私模式 | 本報告 UC-08（`view()`） |
| UC-13 | 複製 resume 指令 | 前端，無差異。見 [上層 UC-13](../../jerry-report/UC-13-copy-resume.md) |
| UC-14 | 偏好設定同步 | 前端，無差異。見 [上層 UC-14](../../jerry-report/UC-14-preferences-sync.md) |
| UC-15 | 發布場景包 | 已移除（`scripts/pack-scene.js` 與 `scenes.json` 已刪除） |
| UC-16 | 開發檢查 | 本報告 UC-09 取代：Go 版的檢查是同步內嵌資源、`gofmt`、`go vet`、`go build`；前端打包與 Playwright 測試仍在 Node 側 |

## 圖例約定

- 流程圖：`flowchart TD`，菱形為判斷，圓角矩形為起訖
- 循序圖：`sequenceDiagram`，實線為同步呼叫，虛線為回應，`loop`／`alt`／`opt`／`par` 為控制區塊；`par` 標示不同 goroutine 同時進行
- 路徑一律為 `go-app/` 的相對路徑；上層倉庫以 `<clone>` 表示，使用者家目錄以 `~` 表示
- 「Node 版」指 `skills/myoffice/` 的 `bin/cli.js` 與 `src/`
