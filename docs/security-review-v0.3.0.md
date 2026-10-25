# 安全審查報告：v0.3.0

- **審查日期**：2026-10-25
- **審查對象**：`main` 分支 v0.3.0（commit `c9dc82f`）的完整程式碼，非單一 diff
- **範圍**：`bin/cli.js`、`src/*.js`、`scripts/pack-scene.js`、`scenes.json`、`public/index.html`、`public/office.js`、`public/popout.js`、`public/i18n.js`、`web/office3d.js`
- **方法**：人工程式碼審查，依威脅模型逐項追蹤資料流；不含動態測試與依賴套件掃描
- **排除項目**：DoS、資源耗盡、依賴套件版本、純加固建議（最後一節另列供知悉）

## 結論

**未發現可利用的安全漏洞。** 瀏覽器端與網路端的攻擊面均有正確防護；唯一的設計面觀察只在多使用者共用主機上成立，列為低嚴重度、非漏洞。

## 威脅模型

本工具在本機架 HTTP 伺服器，讀取 `~/.claude`、`~/.codex`、`~/.gemini` 下的 session 紀錄並顯示在瀏覽器。紀錄內容高度敏感（prompt、回覆、工具輸出、工作目錄），且**部分內容可被外部影響**：agent 在惡意 repo 內工作、或收到惡意 MCP／網頁內容時，該文字會落入 session 紀錄，再由本工具渲染。因此重點放在：

1. 外部網頁能否透過使用者的瀏覽器讀取本機 API（DNS rebinding、CORS、CSRF）
2. 紀錄內容能否在瀏覽器端造成 XSS，進而讀走所有 session
3. 請求參數或下載內容能否造成路徑穿越、tar-slip、命令注入
4. 場景包下載與自動更新機制的完整性

## 逐項核對結果

| 攻擊面 | 結論 | 依據 |
|---|---|---|
| 綁定介面 | 僅 `127.0.0.1` | `bin/cli.js` 的 `server.listen` 第二個參數固定為 loopback |
| DNS rebinding | 已擋 | `src/server.js` 在所有路由前比對 Host 標頭白名單（`localhost`、`127.0.0.1`、`[::1]`），不符回 403 |
| 跨來源讀取 session | 不可能 | 未送出任何 `Access-Control-*` 標頭，`/api/sessions` 與 `/events` 無法被跨站讀取 |
| CSRF 觸發 install／update／shutdown | 已擋 | 三個 POST 端點皆要求自訂標頭 `x-whatsmyagentdoing`，跨站請求必觸發 preflight 並失敗 |
| 靜態檔路徑穿越 | 已擋 | `serveFile` 以 `path.normalize` 後要求 `startsWith(root + sep)`，另有副檔名白名單；scene id 須符合 `/^[a-z0-9-]+$/` |
| 場景包下載 | 安全 | URL 固定取自 repo 內 `scenes.json`，強制 https；先比對釘選的 SHA-256，不符即中止，之後才解壓 |
| tar-slip | 已擋 | 自製 ustar reader 只收 regular file、跳過 link、拒絕 `..` 與絕對路徑、副檔名白名單，寫入前再驗 `startsWith(dest + sep)`；解壓目標為全新暫存目錄 |
| 命令注入 | 無 | 僅三處 child_process 呼叫（開瀏覽器、重啟自身、`git pull --ff-only`），全為陣列參數、無 `shell: true`、參數不受請求控制 |
| 紀錄內容流入檔案系統 | 無 | 開檔路徑全來自目錄走訪，不來自紀錄欄位；紀錄欄位只做字串處理；無 deep merge，無 `__proto__` 污染途徑 |
| XSS（title、activity、prompt、reply、error、cwd） | 已擋 | `public/index.html` 所有 `innerHTML` 插值皆經 `esc()`（涵蓋 `& < > " '`，含屬性值）；唯一未 escape 的 `data-s` 屬性值為 `status`，在 `src/parsers.js` 全為字面常數 |
| Pop-out、postMessage、storage | 安全 | Document PiP 以 `createElement` 加同源 iframe 建立，無 HTML 字串注入；無 `message` listener；`storage` 事件值全經 `validPreference()` 白名單驗證 |
| 自動更新 | 安全 | 只讀 GitHub raw `package.json` 的版本字串做比較與顯示；更新動作為 `git pull --ff-only` |

## 低於門檻的觀察

以下皆非可利用漏洞，列出供後續版本評估。

### 1. loopback API 無認證 token

- **嚴重度**：Low
- **位置**：`src/server.js` 的 request handler
- **說明**：存取控制只靠 Host 白名單與自訂標頭，對瀏覽器攻擊完整。但同一台主機上的**其他 OS 使用者或行程**可直接以 `curl` 讀取全部 session 內容，或自帶標頭觸發 `/api/update`、`/api/shutdown`。
- **成立條件**：多使用者主機、共用 CI runner、共用 WSL 或容器環境。單人開發機不成立。
- **建議**：若要支援共用主機，啟動時產生隨機 token 帶在開啟的 URL 中，API 與 SSE 要求攜帶該 token。

### 2. `decodeURIComponent` 未捕捉例外

- **類別**：DoS（依審查規則排除）
- **位置**：`src/server.js` 處理 `/assets/` 與 `/scenes/` 的路徑解碼
- **說明**：非法的 `%` 序列（例如 `GET /assets/%`）會同步丟出 `URIError`，使伺服器行程崩潰。
- **建議**：包在 try/catch 內回 400。

### 3. 無 CSP、無 `X-Frame-Options`

- **類別**：加固
- **說明**：XSS 目前無任何緩解層；外部頁面可以 iframe 嵌入本頁做 clickjacking，誘使點擊「Update」或「下載場景」。因現有 escape 完整、更新與下載皆有完整性驗證，實際影響低。
- **建議**：伺服器對 `/` 回應加上 `Content-Security-Policy: default-src 'self'` 與 `frame-ancestors 'none'`。
