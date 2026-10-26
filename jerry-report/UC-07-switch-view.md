# UC-07 切換視圖與畫質

| 項目 | 內容 |
|---|---|
| 主要參與者 | 使用者 |
| 次要參與者 | 瀏覽器（WebGL、動態 import）、伺服器（提供 `/office3d.js`、`/assets/kenney/*.glb`） |
| 前置條件 | 頁面已載入（UC-02） |
| 觸發 | 工具列 Office／List、設定面板的 2D／3D 按鈕；URL `?view=&quality=`；偏好同步（UC-14） |
| 後置條件 | 畫面以選定的視圖與畫質呈現；偏好已存入 localStorage |
| 相關程式 | `public/index.html`（`viewSeg`、`qualSeg`、`render`、`makeOffice`、`setLoading`）、`public/office.js`、`web/office3d.js`、`src/server.js`（`/assets/`） |

## 主要流程

### 切換 Office／List

1. 使用者按 `viewSeg` 的按鈕；`state.view` 更新、`store.set('view')`、關閉 tooltip、`syncControls()`、`render()`。
2. `render()`：office 模式顯示 `#officeWrap` 並 `office.setVisible(true)`；list 模式隱藏辦公室，輸出分組卡片；只有 list 顯示「Idle」切換，只有 office 顯示畫質與 agent 標記選項。

### 切換 2D／3D

3. 使用者在設定面板按 `qualSeg`（只在辦公室檢視出現，預設 3D）；`state.quality` 更新並存檔，呼叫 `makeOffice()`。
4. `makeOffice()` 計算 `kind`：`mid` 或 `high:<scene>`（scene 只在已安裝時生效，否則 `office`）；與目前相同就不重建。
5. 顯示載入遮罩（「Loading 3D office…」或「Setting up the office…」）。
6. 3D：`await import('/office3d.js')` 取得 `Office3D`（three.js bundle 約 180 KB gzip，只在此時下載）；若 scene 非 office，再 `fetch('/scenes/<id>/manifest.json')`。
7. 銷毀舊的 office（`destroy()`），`new Cls(officeEl, opts)`。3D 建構時建立 `WebGLRenderer`，非同步載入 12 個角色與家具 GLB（`/assets/kenney/…`）。
8. `render()` 先把資料餵進去；`await office.ready`（2D：畫出 3 幀後 resolve true；3D：模型載完且畫出幀後 true，WebGL 或模型失敗則 false）。
9. 成功：移除載入遮罩。

## 替代流程

- **A1 3D 建構時 throw**：改用 2D `Office`，`quality` 回寫為 `mid`，`officeKind = 'mid'`。
- **A2 3D `ready` 為 false**（無 WebGL、模型檔缺失）：顯示「Couldn't load the 3D office — showing Medium instead.」，`quality` 設回 `mid`，1.2 秒後再 `makeOffice()` 建 2D。
- **A3 import 失敗**（bundle 404）：catch 後 `Cls = window.Office`，走 2D。
- **A4 切換中又切換**：`officeKind` 已被後來的呼叫改寫，先前的 `makeOffice` 在 import 後檢查 `officeKind !== kind` 直接 return，避免建出過時的 office。
- **A5 PiP 開啟中**：主頁的 `render()` 隱藏辦公室並顯示「儀表板正在子母畫面視窗中顯示」（UC-09）。
- **A6 來自 URL**：`?view=office&quality=high&scene=pirate`（`--scene` 開的網址）在載入時寫入偏好，再從網址移除。

## 流程圖

```mermaid
flowchart TD
  A([按 2D／3D 或偏好同步]) --> B[state.quality 更新並 store.set]
  B --> C[makeOffice]
  C --> D[kind = high ? high:scene : mid]
  D --> E{kind == officeKind?}
  E -->|是| Z([不動])
  E -->|否| F[officeKind = kind；顯示載入遮罩]
  F --> G{high?}
  G -->|否| K
  G -->|是| H[import /office3d.js]
  H --> H1{成功?}
  H1 -->|否| H2[Cls = 2D Office] --> K
  H1 -->|是| I{scene 非 office?}
  I -->|是| I1[fetch /scenes/id/manifest.json → opts.scene]
  I -->|否| J
  I1 --> J{officeKind 仍是 kind?}
  J -->|否| Z
  J -->|是| K[office.destroy 舊實例]
  K --> L[new Cls officeEl, opts]
  L --> L1{建構 throw?}
  L1 -->|是| L2[改建 2D；quality = mid] --> M
  L1 -->|否| M[render；await office.ready]
  M --> N{ready 為 true?}
  N -->|是| O([移除遮罩])
  N -->|否，且 high| P[顯示 failed3d；quality = mid]
  P --> Q[1.2 秒後 makeOffice] --> C
```

## 循序圖

```mermaid
sequenceDiagram
  actor U as 使用者
  participant B as index.html
  participant ST as store（localStorage）
  participant S as server.js
  participant O2 as Office（2D）
  participant O3 as Office3D

  U->>B: 按 3D
  B->>ST: set('quality', 'high')
  B->>B: makeOffice()：kind = high:office
  B->>B: setLoading(true, loading3d)
  B->>S: import /office3d.js
  S-->>B: three.js bundle（ESM）
  opt scene 非 office
    B->>S: GET /scenes/<id>/manifest.json
    S-->>B: manifest
  end
  B->>O2: destroy()
  B->>O3: new Office3D(officeEl, opts)
  O3->>O3: new WebGLRenderer
  alt WebGL 可用
    par 載入模型
      O3->>S: GET /assets/kenney/characters/*.glb
      O3->>S: GET /assets/kenney/furniture/*.glb
    end
    S-->>O3: GLB（meshopt 壓縮）
    B->>O3: update(sessions)（render）
    O3->>O3: 畫出前幾幀 → markReady(true)
    O3-->>B: ready = true
    B->>B: setLoading(false)
    B-->>U: 3D 辦公室
  else WebGL 不可用或模型失敗
    O3-->>B: ready = false
    B->>B: setLoading(true, failed3d)
    B->>ST: set('quality', 'mid')
    B->>B: 1.2 秒後 makeOffice()
    B->>O2: new Office(officeEl, opts)
    O2-->>B: ready = true
    B-->>U: 2D 辦公室
  end
```

## 備註

- 2D 與 3D 共用同一套席位、閒晃與狀態邏輯（`Office3D extends Office`），3D 只換渲染；泡泡與名牌仍由 2D 程式畫在 canvas overlay 上。
- 3D 視窗時間影響天色（`skyNow()`），2D 另有辦公室貓。
