# whatsmyagentdoing 系統分析報告

依 README 與原始碼整理的系統結構、use case（使用案例）與 UML 圖。圖以 Mermaid 撰寫，GitHub 與 VS Code 的 Markdown 預覽可直接渲染。

- 版本依據：package.json `0.3.0`
- 分析範圍：`bin/`、`src/`、`public/`、`web/`、`scripts/`、`tests/`、`scenes.json`、`SKILL.md`、README

## 閱讀順序

1. [structure.md](structure.md)：專案結構、執行時架構、模組職責、資料模型、狀態機、HTTP API、安全邊界
2. [use-cases.md](use-cases.md)：參與者、use case 總表、use case 圖與彼此關係
3. 各 use case 獨立檔案：每份含摘要表、主要流程、替代流程、流程圖（activity 風格 flowchart）、循序圖

## Use case 一覽

| ID | 名稱 | 主要參與者 | 檔案 |
|---|---|---|---|
| UC-01 | 啟動儀表板 | 使用者／AI Agent | [UC-01-start-dashboard.md](UC-01-start-dashboard.md) |
| UC-02 | 即時監看 agent 狀態 | 使用者 | [UC-02-live-monitoring.md](UC-02-live-monitoring.md) |
| UC-03 | 停止儀表板 | 使用者／AI Agent | [UC-03-stop-dashboard.md](UC-03-stop-dashboard.md) |
| UC-04 | 檢查新版並更新 | 使用者／AI Agent | [UC-04-check-and-update.md](UC-04-check-and-update.md) |
| UC-05 | 取得 JSON 快照 | AI Agent | [UC-05-json-snapshot.md](UC-05-json-snapshot.md) |
| UC-06 | Demo 模式 | 使用者 | [UC-06-demo-mode.md](UC-06-demo-mode.md) |
| UC-07 | 切換視圖與畫質 | 使用者 | [UC-07-switch-view.md](UC-07-switch-view.md) |
| UC-08 | 管理 3D 場景 | 使用者／AI Agent | [UC-08-manage-scenes.md](UC-08-manage-scenes.md) |
| UC-09 | 子母畫面（Pop out） | 使用者 | [UC-09-popout.md](UC-09-popout.md) |
| UC-10 | 選擇介面語言 | 使用者 | [UC-10-language.md](UC-10-language.md) |
| UC-11 | 完成通知 | 使用者 | [UC-11-notifications.md](UC-11-notifications.md) |
| UC-12 | 隱私模式 | 使用者 | [UC-12-private-mode.md](UC-12-private-mode.md) |
| UC-13 | 複製 resume 指令 | 使用者 | [UC-13-copy-resume.md](UC-13-copy-resume.md) |
| UC-14 | 偏好設定同步 | 使用者 | [UC-14-preferences-sync.md](UC-14-preferences-sync.md) |
| UC-15 | 發布場景包 | 維護者 | [UC-15-pack-scene.md](UC-15-pack-scene.md) |
| UC-16 | 開發檢查（build／test:ui） | 維護者 | [UC-16-dev-checks.md](UC-16-dev-checks.md) |

## 圖例約定

- 流程圖：`flowchart TD`，菱形為判斷，圓角矩形為起訖
- 循序圖：`sequenceDiagram`，實線為同步呼叫，虛線為回應，`loop`／`alt`／`opt` 為控制區塊
- 路徑一律為專案根的相對路徑；使用者家目錄以 `~` 表示

## 場景包報告

[scenes/](scenes/) 放四個可下載場景包的副本與逐包報告：內容結構、manifest 欄位與程式對應、道具／工作位／休息點統計、從下載到渲染的流程圖與循序圖、一致性檢查。

| 檔案 | 場景 | 報告 |
|---|---|---|
| `scenes/cafe-v1.tar.gz` | 咖啡廳 | [scenes/cafe-v1.md](scenes/cafe-v1.md) |
| `scenes/castle-v1.tar.gz` | 城堡 | [scenes/castle-v1.md](scenes/castle-v1.md) |
| `scenes/pirate-v3.tar.gz` | 海盜灣 | [scenes/pirate-v3.md](scenes/pirate-v3.md) |
| `scenes/station-v1.tar.gz` | 太空站 | [scenes/station-v1.md](scenes/station-v1.md) |
