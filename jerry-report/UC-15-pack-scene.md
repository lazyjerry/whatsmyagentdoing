# UC-15 發布場景包

| 項目 | 內容 |
|---|---|
| 主要參與者 | 維護者 |
| 次要參與者 | GitHub releases（tag `scenes`） |
| 前置條件 | 有一個場景資料夾（如 `packs/pirate/`）含 `manifest.json`（`id`、`version`、`name`、`credits`、`icon`）、`models/*.glb`、貼圖等 |
| 觸發 | `node scripts/pack-scene.js packs/<id>` |
| 後置條件 | `dist/<id>-v<version>.tar.gz` 產生；`scenes.json` 新增或更新該條目（size、sha256、url）；維護者上傳檔案並 commit `scenes.json` |
| 相關程式 | `scripts/pack-scene.js`、`scenes.json`、`src/scenes.js`（下載端，UC-08） |

## 主要流程

1. 維護者執行 `node scripts/pack-scene.js packs/pirate`。
2. 讀取 `manifest.json`，驗證 `id` 符合 `^[a-z0-9-]+$`。
3. 遞迴走訪資料夾，只收 `.json/.glb/.png/.txt`，路徑以 `/` 分隔並排序（可重現）。
4. 對每個檔案產生 ustar 標頭（路徑 ≤ 100 bytes、mode 0644、type `0`、checksum）與 512 對齊的內容；最後補 1024 bytes 零。
5. `gzipSync(level 9)`，寫到 `dist/<id>-v<version>.tar.gz`。
6. 計算 SHA-256。
7. 讀 `scenes.json`，以 manifest 的 id、version、name、credits、icon（缺則沿用舊條目）、size、sha256、`https://github.com/<repo>/releases/download/scenes/<file>` 組成條目；移除同 id 舊條目、加入新條目、依 id 排序寫回。
8. 印出檔名、大小、sha256。
9. 維護者把 tar.gz 上傳到 GitHub release `scenes`，commit `scenes.json`。
10. 使用者更新後（UC-04）看到新場景可下載（UC-08）。

## 替代流程

- **A1 id 不合法**：throw，不產生檔案。
- **A2 路徑超過 100 bytes**：throw（自製 tar 不支援 ustar prefix 欄位的寫入）。
- **A3 `scenes.json` 不存在或壞掉**：從空目錄開始。
- **A4 同 id 再打包**：版本相同也會覆寫條目；使用者端 `install()` 以 `version >= catalog.version` 判斷是否重下載，所以版本不加就不會重抓。

## 流程圖

```mermaid
flowchart TD
  A([node scripts/pack-scene.js packs/id]) --> B[讀 manifest.json]
  B --> C{id 符合 a-z0-9-?}
  C -->|否| E1([throw])
  C -->|是| D[walk：只收 json／glb／png／txt，排序]
  D --> E[對每檔：ustar header + body + 512 padding]
  E --> E2{路徑 > 100 bytes?}
  E2 -->|是| E1
  E2 -->|否| F[補 1024 零 → gzip level 9]
  F --> G[寫 dist/id-vN.tar.gz]
  G --> H[sha256]
  H --> I[讀 scenes.json（失敗則空）]
  I --> J[組條目：id、version、name、credits、icon、size、sha256、url]
  J --> K[移除同 id、加入、依 id 排序、寫回]
  K --> L[印 檔名 大小 sha256]
  L --> M([維護者上傳到 release scenes 並 commit scenes.json])
```

## 循序圖

```mermaid
sequenceDiagram
  actor M as 維護者
  participant P as pack-scene.js
  participant FS as 檔案系統
  participant GH as GitHub（release scenes）
  participant G as git

  M->>P: node scripts/pack-scene.js packs/pirate
  P->>FS: 讀 packs/pirate/manifest.json
  P->>FS: walk packs/pirate（json／glb／png／txt）
  P->>P: 建 ustar、gzip、sha256
  P->>FS: 寫 dist/pirate-v3.tar.gz
  P->>FS: 讀 scenes.json
  P->>FS: 寫回 scenes.json（新條目、依 id 排序）
  P-->>M: dist/pirate-v3.tar.gz 138 KB sha256 …
  M->>GH: 上傳 pirate-v3.tar.gz 到 release scenes
  M->>G: commit scenes.json，push
  Note over GH: 之後使用者的 scenes.install 會下載並比對此 sha256（UC-08）
```
