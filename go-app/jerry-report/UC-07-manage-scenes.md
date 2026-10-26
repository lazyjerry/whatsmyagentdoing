# UC-07 管理 3D 場景

| 項目 | 內容 |
|---|---|
| 主要參與者 | 使用者（High 3D 的場景選單、CLI）或 AI Agent（`/myoffice scene <id>`） |
| 次要參與者 | GitHub（releases 的場景包）、使用者資料目錄 `~/.whatsmyagentdoing/scenes/` |
| 前置條件 | 列出與移除不需要網路；安裝需要能連 GitHub（或 `WMAD_SCENES_LOCAL`） |
| 觸發 | 頁面場景選單、`--scenes`、`--scene <id>`、`--remove-scene <id>`；頁面 POST `/api/scenes/install` |
| 後置條件 | 場景包解在 `~/.whatsmyagentdoing/scenes/<id>/`，頁面透過 `/scenes/<id>/**` 載入 manifest 與模型；或場景目錄被移除 |
| 相關程式 | `scenes.go`（`Catalog`、`InstalledList`、`SceneDir`、`download`、`untar`、`versionAtLeast`、`Install`、`Remove`、`sceneName`）、`config.go`（`sceneReleaseURL`）、`server.go`（`/api/scenes`、`/api/scenes/install`、`/scenes/<id>/**`、`serveFile`）、`main.go`（三個旗標）、`static/scenes.json` |

## 主要流程

### 列出

1. `Catalog()`：從內嵌 `scenes.json` 讀 `scenes` 陣列，每筆補 `URL = RepoURL/releases/download/scenes/<file>`。
2. `InstalledList()`：掃 `~/.whatsmyagentdoing/scenes/` 下名稱符合 `^[a-z0-9-]+$` 且有 `manifest.json` 的目錄，讀 manifest 的 `name`、`version`、`credits`、`icon`。
3. `/api/scenes` 回 `{installed, available}`；已安裝的從 available 排除；icon 缺省時先找目錄中的同 id，再退回 🗺️。CLI `--scenes` 印同樣兩組，內建 `office` 永遠在第一行，可下載的附 KB 大小。

### 安裝

4. 頁面 POST `/api/scenes/install?id=<id>`，標頭 `x-whatsmyagentdoing: install`；或 CLI `--scene <id>`（非 `office`）。
5. `Install(id)`：在目錄中找不到 → 錯誤「unknown scene」。
6. 已安裝且 `versionAtLeast(have.Version, entry.Version)` → 直接回傳已安裝的那筆（數字比較；非數字退回字串比較）。
7. `download(entry.URL)`：`WMAD_SCENES_LOCAL` 設定時改讀本機 `<dir>/<file>`；否則要求 `https://`、`User-Agent: whatsmyagentdoing`、逾時 5 分鐘、非 200 報錯、`LimitReader` 到 30 MB + 1，超過即「scene pack too large」。
8. SHA-256 與 `scenes.json` 的 `sha256` 比對，不符 → 「checksum mismatch」。
9. `untar` 到暫存目錄 `~/.whatsmyagentdoing/scenes/.<id>-<pid>`：只收 `TypeReg`；`path.Clean` 後擋 `.`、`..` 開頭、絕對路徑；副檔名白名單 `.json`、`.glb`、`.png`、`.txt`；寫入前再驗目的路徑以暫存目錄為前綴；單檔也限 30 MB。
10. 暫存目錄沒有 `manifest.json` → 刪掉並報錯。
11. `os.RemoveAll(final)` 後 `os.Rename(tmp, final)`；再掃 `InstalledList()` 找出該 id 回傳。
12. 頁面收到 `{ok: true, id}` 後切換場景，瀏覽器載入 `/scenes/<id>/manifest.json` 與 `/scenes/<id>/models/*.glb`。

### 服務場景檔

13. `/scenes/<id>/<rest>`：`SceneDir(id)` 先驗 id 格式再找目錄，沒有 → 404。
14. `serveFile(dir, rest)`：`filepath.Clean` 後要求以 `dir + 分隔符` 為前綴、副檔名在 `mimeTypes`（`.js`、`.glb`、`.png`、`.json`）、是一般檔；回 `Cache-Control: no-cache`。

### 移除

15. `--remove-scene <id>`：`Remove(id)` 驗 id 格式、目錄存在則 `os.RemoveAll`；印「Removed」或「isn't a downloaded scene」。

## 替代流程

- **A1 `--scene office`**：不安裝，直接以內建辦公室開啟。
- **A2 `--scene <id>` 已安裝**：步驟 6 命中，印「Scene <id> is ready.」；未安裝則印「Downloaded scene <id>.」。
- **A3 安裝失敗**：CLI 印錯誤以 1 離開；頁面收到 400 `{ok: false, error}`。
- **A4 目錄中有新版本**：`versionAtLeast` 為 false，重新下載並覆蓋。
- **A5 缺標頭**：`/api/scenes/install` 回 403。
- **A6 場景包含 `.txt`**（例如授權檔）：會被解開，但 `serveFile` 的 `mimeTypes` 沒有 `.txt`，透過 HTTP 要不到（404）。
- **A7 tar 內有 symlink、目錄項、`../` 路徑或其他副檔名**：靜默跳過，不報錯。
- **A8 本機測試**：`WMAD_SCENES_LOCAL=<dir>` 讀 `<dir>/<file>`，仍做 SHA-256 比對。

## 流程圖

```mermaid
flowchart TD
  A([--scene id 或 POST /api/scenes/install]) --> B{id 在 Catalog?}
  B -->|否| B1([錯誤：unknown scene])
  B -->|是| C{已安裝且版本 ≥ 目錄?}
  C -->|是| C1([回傳已安裝的那筆])
  C -->|否| D{WMAD_SCENES_LOCAL?}
  D -->|是| D1[讀本機檔]
  D -->|否| D2{https?}
  D2 -->|否| D3([錯誤：only https])
  D2 -->|是| D4["GET（5 分鐘，30 MB 上限）"]
  D4 --> D5{200 且未超限?}
  D5 -->|否| D6([錯誤：HTTP n／too large])
  D5 -->|是| E
  D1 --> E{sha256 相符?}
  E -->|否| E1([錯誤：checksum mismatch])
  E -->|是| F["untar 到 .id-pid 暫存目錄"]
  F --> F1{每個 entry}
  F1 -->|非一般檔／逃出／副檔名不在白名單| F2[跳過] --> F1
  F1 -->|合法| F3[MkdirAll；WriteFile] --> F1
  F1 -->|EOF| G{有 manifest.json?}
  G -->|否| G1([刪暫存；錯誤])
  G -->|是| H["RemoveAll(final)；Rename(tmp, final)"]
  H --> I([InstalledList 找回該 id → 回傳])
```

## 循序圖

```mermaid
sequenceDiagram
  actor U as 使用者
  participant B as 瀏覽器
  participant S as server.go
  participant SC as scenes.go
  participant GH as GitHub releases
  participant FS as ~/.whatsmyagentdoing/scenes/

  B->>S: GET /api/scenes
  S->>SC: Catalog()、InstalledList()
  SC-->>S: 目錄、已安裝
  S-->>B: {installed, available}
  U->>B: 選一個可下載的場景
  B->>S: POST /api/scenes/install?id=cafe（install 標頭）
  S->>SC: Install("cafe")
  SC->>SC: 在 Catalog 找到；檢查已安裝版本
  SC->>GH: GET releases/download/scenes/cafe-v1.tar.gz
  GH-->>SC: tar.gz
  SC->>SC: sha256 比對
  SC->>FS: untar 到 .cafe-<pid>
  SC->>FS: 驗 manifest.json；RemoveAll(cafe)；Rename
  SC->>FS: InstalledList()
  FS-->>SC: cafe 的 manifest
  SC-->>S: Installed{ID: cafe, …}
  S-->>B: 200 {ok: true, id: cafe}
  B->>S: GET /scenes/cafe/manifest.json
  S->>SC: SceneDir("cafe")
  SC-->>S: 目錄路徑
  S->>FS: 讀檔（serveFile）
  S-->>B: manifest.json
  loop 每個模型
    B->>S: GET /scenes/cafe/models/x.glb
    S-->>B: model/gltf-binary
  end
```

## 備註

- 內建辦公室的模型走 `/assets/kenney/**`，從內嵌檔案系統服務；下載的場景包走 `/scenes/<id>/**`，從磁碟服務。兩者的路徑穿越檢查方式不同（`fs.ValidPath` 對 `filepath` 前綴），但結果一致。
- 場景包只含資料（manifest、glb、png），安裝從不執行程式碼，這是 ADR-0002 不從 Release 下載執行檔的對照立場。
- `Scenes` 是空 struct，所有狀態都在磁碟與內嵌檔案系統；`dirs()` 目前只回使用者目錄一項，Node 版的「倉庫內建場景目錄」這一層已拿掉（ADR-0004）。
- 暫存目錄以 `.` 開頭，所以 `InstalledList` 的 `^[a-z0-9-]+$` 檢查不會把安裝中的目錄列進來。
