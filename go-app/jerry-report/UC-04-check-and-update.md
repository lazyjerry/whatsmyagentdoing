# UC-04 檢查新版並更新

| 項目 | 內容 |
|---|---|
| 主要參與者 | 使用者（頁面按鈕）或 AI Agent（`--update`） |
| 次要參與者 | GitHub（raw `package.json`、git remote）、工具鏈（`git`、`bash`、`go`） |
| 前置條件 | 檢查：未設 `WMAD_NO_UPDATE_CHECK`。更新：執行檔還在 clone 的 `go-app/` 裡（或 `WMAD_SRC` 指過去），`go` 在 PATH 上，clone 可 fast-forward |
| 觸發 | 頁面「檢查更新」按鈕、Update 按鈕、`--update`；`AutoUpdateCheck` 開啟時另加頁面載入、每 30 分鐘、CLI 啟動 |
| 後置條件 | clone 為最新版、`static/` 已同步、執行檔已重建；頁面路徑會以新執行檔重啟，CLI 路徑停掉舊實例 |
| 相關程式 | `update.go`（`Updater`、`CheckLatest`、`Info`、`Update`）、`util.go`（`sourceDir`）、`config.go`（`LatestURL`、`AutoUpdateCheck`）、`server.go`（`/api/version`、`/api/update`）、`main.go`（`--update`、`printUpdateHint`、`onRestart`）、`scripts/sync-static.sh` |

## 主要流程

### 檢查

1. 頁面載入 GET `/api/version`；`AutoUpdateCheck` 關閉（預設）時只回已知資訊，不連外。使用者按「檢查更新」才 GET `/api/version?check=1`。
2. `CheckLatest(force)`：`WMAD_NO_UPDATE_CHECK` 設定 → 回空；`WMAD_LATEST_VERSION` 設定 → 直接採用；非強制且 30 分鐘內有結果 → 用快取；否則若沒有在飛的抓取就開一條 goroutine `fetchLatest`（GET `LatestURL`，逾時 4 秒、上限 64 KB、解析 `version`），所有同時呼叫者等同一個 `inflight` channel。
3. `Info()`：`{version: embeddedVersion, latest, updateAvailable: newer(latest, version), canUpdate: sourceDir() != "", auto: AutoUpdateCheck}`。`newer` 逐段比較三節數字。
4. 前端依結果顯示 `v0.3.0`、「檢查更新」、「已是最新版」、「Update to vX」或「用 git pull 更新」（canUpdate 為 false 時）。

### 從頁面更新

5. 使用者按 Update；POST `/api/update`，帶標頭 `x-whatsmyagentdoing: update`。
6. `Updater.Update()`：
   - `sourceDir()` 找不到 → 回錯誤「not inside its go-app/ source folder…」
   - 在 clone 根（`src` 的上一層）執行 `git pull --ff-only --quiet`，逾時 60 秒；失敗以輸出為錯誤訊息
   - 在 `src` 執行 `bash scripts/sync-static.sh`，逾時 60 秒
   - `exec.LookPath("go")` 找不到 → 回錯誤並附手動指令 `cd <src> && go build -o <exe> .`
   - 在 `src` 執行 `go build -o <exe> .`（`exe` 為解過 symlink 的 `os.Executable()`），逾時 5 分鐘
   - 讀 `src/static/VERSION` 回傳新版本號（目前行程內嵌的仍是舊的）
7. 回 `{ok: true, version}`；`closeClients()`；`time.AfterFunc(100ms, onRestart)`。
8. `onRestart`：`src.Stop()`、`app.Close()`、`httpServer.Shutdown`；以 `os.Executable()` 加原參數（去掉 `--no-open` 與 `--scene*`，再加 `--no-open`）`exec.Command(...).Start()`；`os.Exit(0)`。
9. 新執行檔啟動（UC-01），`embeddedVersion` 為新版；頁面每秒 GET `/api/version`，版本變了或超過 20 秒就 reload。

### 從 CLI 更新

10. `./whatsmyagentdoing --update`：記下 `before = LocalVersion()`，呼叫 `Update()`，印「Updated vA → vB」或「Already up to date (vX)」。
11. 對 `port … port+10` 偵測執行中的儀表板並 `stopAt`，印「Restart it with /myoffice (it was on port p)」。
12. AI Agent 回報版本，再依 UC-01 重新啟動。

## 替代流程

- **A1 關閉檢查**：`WMAD_NO_UPDATE_CHECK` 設定時 `CheckLatest` 回空，`latest` 為 null，頁面顯示「檢查失敗」。
- **A2 網路不通／非 200／JSON 壞掉**：`fetchLatest` 回空，`latest` 與 `checkedAt` 保留上次的值。
- **A3 執行檔不在原始碼目錄**（複製到別處）：`canUpdate = false`，頁面不顯示 Update 鈕；硬打 `/api/update` 回 400 與錯誤訊息；CLI `--update` 印錯誤以 1 離開。
- **A4 `git pull` 失敗**（非 fast-forward、無網路）：回 400，訊息為 git 輸出；不同步、不重建。
- **A5 同步腳本失敗**（真源缺檔、`package.json` 讀不到版本）：回「sources updated, but syncing static/ failed」；clone 已是新版但執行檔未變。
- **A6 `go` 不在 PATH**：回「sources updated, but `go` is not on PATH; rebuild by hand: …」。
- **A7 建置失敗**：回「sources updated, but rebuild failed」；舊執行檔仍在，不重啟。
- **A8 Windows**：無法覆寫執行中的 exe，`go build -o` 會失敗 → A7。README 要求先 `--stop` 再 `--update`。
- **A9 自動模式**：`AutoUpdateCheck = true` 時不帶 `check` 的 GET 也會 `CheckLatest(false)`（走 30 分鐘快取）；`printUpdateHint` 在 CLI 啟動與「已在執行」時強制檢查並印「Update available」。
- **A10 測試**：`WMAD_LATEST_VERSION` 假造最新版本。

## 流程圖

```mermaid
flowchart TD
  subgraph check["檢查"]
    A([按「檢查更新」]) --> B["GET /api/version?check=1"]
    A2([自動模式：頁面載入／每 30 分鐘／CLI 啟動]) --> B2["GET /api/version 或 printUpdateHint"]
    B --> C{WMAD_NO_UPDATE_CHECK?}
    B2 --> C
    C -->|是| C1[latest 為空]
    C -->|否| C2{WMAD_LATEST_VERSION?}
    C2 -->|是| C3[latest = 該值]
    C2 -->|否| C4{非強制且 30 分鐘內有結果?}
    C4 -->|是| F
    C4 -->|否| E["fetchLatest goroutine（並發共用 inflight）"]
    E --> E1{200 且可解析?}
    E1 -->|是| E2[latest = version；checkedAt = now]
    E1 -->|否| E3[保留舊值]
    E2 --> F["Info：version、latest、updateAvailable、canUpdate = sourceDir 找得到、auto"]
    E3 --> F
    C1 --> F
    C3 --> F
    F --> G{updateAvailable?}
    G -->|否| G1[顯示 vX 與「檢查更新」]
    G -->|是| H{canUpdate?}
    H -->|是| H1[顯示 Update 按鈕]
    H -->|否| H2[提示回 clone 重建]
  end

  subgraph upd["更新（Update 函式）"]
    H1 --> I["POST /api/update（update 標頭）"]
    I --> J{標頭正確?}
    J -->|否| J1([403])
    J -->|是| K{sourceDir 找得到?}
    K -->|否| K1["400：not inside go-app source folder"]
    K -->|是| L["git pull --ff-only --quiet（clone 根，60 秒）"]
    L --> L1{成功?}
    L1 -->|否| L2["400：git 輸出"]
    L1 -->|是| M["bash scripts/sync-static.sh（60 秒）"]
    M --> M1{成功?}
    M1 -->|否| M2["400：syncing static failed"]
    M1 -->|是| N{go 在 PATH?}
    N -->|否| N1["400：rebuild by hand 指令"]
    N -->|是| O["go build -o exe .（5 分鐘）"]
    O --> O1{成功?}
    O1 -->|否| O2["400：rebuild failed"]
    O1 -->|是| P["讀 static/VERSION → 200 {ok, version}"]
    P --> Q["closeClients → AfterFunc 100 ms → onRestart"]
    Q --> R["Stop、Close、Shutdown → exec 新執行檔 --no-open → exit"]
    R --> S([新行程 UC-01；頁面輪詢後 reload])
  end
```

## 循序圖

```mermaid
sequenceDiagram
  actor U as 使用者
  participant B as 瀏覽器
  participant S as server.go
  participant UP as update.go
  participant GH as GitHub
  participant GIT as git
  participant SH as sync-static.sh
  participant GO as go build
  participant NEW as 新執行檔

  B->>S: GET /api/version（頁面載入，不連外）
  S-->>B: {version, latest: null, canUpdate, auto}
  U->>B: 按「檢查更新」
  B->>S: GET /api/version?check=1
  S->>UP: CheckLatest(true)
  UP->>GH: GET raw main/package.json（4 秒）
  GH-->>UP: {version: latest}
  UP-->>S: Info()
  S-->>B: {version, latest, updateAvailable, canUpdate}
  B->>B: 顯示 Update 按鈕

  U->>B: 按 Update
  B->>S: POST /api/update（x-whatsmyagentdoing: update）
  S->>UP: Update()
  UP->>UP: sourceDir()
  UP->>GIT: git pull --ff-only --quiet（clone 根）
  alt 成功
    GIT-->>UP: exit 0
    UP->>SH: bash scripts/sync-static.sh
    SH-->>UP: synced static/
    UP->>GO: go build -o exe .
    GO-->>UP: exit 0
    UP->>UP: 讀 static/VERSION
    UP-->>S: 新版本號
    S-->>B: 200 {ok: true, version}
    S->>S: closeClients；AfterFunc(100ms, onRestart)
    S->>S: src.Stop、app.Close、Shutdown
    S->>NEW: exec.Command(exe, args…, --no-open).Start()
    S->>S: os.Exit(0)
    NEW->>NEW: UC-01 以新版監聽同一連接埠
    loop 每 1 秒，最多 20 秒
      B->>NEW: GET /api/version
      NEW-->>B: {version: 新版}
    end
    B->>B: location.reload()
  else 任一步失敗
    UP-->>S: error（git 輸出／sync failed／go 不在 PATH／rebuild failed）
    S-->>B: 400 {ok: false, error}
    B-->>U: Couldn't update: …
  end
```

### CLI 路徑

```mermaid
sequenceDiagram
  actor U as 使用者
  participant AG as AI Agent
  participant CLI as main.go（--update）
  participant UP as update.go
  participant S as 執行中的 server

  U->>AG: /myoffice update
  AG->>CLI: ./whatsmyagentdoing --update
  CLI->>UP: LocalVersion() → before
  CLI->>UP: Update()（git pull → sync → go build）
  UP-->>CLI: after（讀 static/VERSION）
  CLI-->>AG: Updated vA → vB ／ Already up to date
  loop p = port … port+10
    CLI->>S: GET /api/ping
    opt 有儀表板
      CLI->>S: POST /api/shutdown（stop 標頭）
      CLI-->>AG: Restart it with /myoffice (it was on port p)
    end
  end
  AG-->>U: 版本號
  AG->>CLI: ./whatsmyagentdoing（背景，UC-01）
```

## 備註

- 只讀 GitHub 上的版本字串，不下載任何執行檔；更新本身是 `git pull --ff-only`，有本機 commit 會失敗。不從 Release 下載 binary 是 ADR-0002 的決定，理由是避免程式自己下載並執行二進位檔。
- `sourceDir()` 的順序：`WMAD_SRC` → 執行檔所在目錄（解 symlink）→ 工作目錄；條件是同時有 `go.mod`、`main.go` 且上一層有 `.git`。所以 `go build -o whatsmyagentdoing .` 留在 `go-app/` 的執行檔可以更新，複製走的不能。
- `Update()` 回傳的版本讀磁碟上的 `static/VERSION`，不是 `embeddedVersion`；行程內嵌的版本要到重啟才會變，這也是 CLI 路徑印「Updated vA → vB」卻需要重啟的原因。
- 三個外部指令都用 `exec.CommandContext` 加逾時與 `CombinedOutput`；錯誤訊息直接把輸出帶給前端顯示。
