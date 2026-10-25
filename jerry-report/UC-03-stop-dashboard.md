# UC-03 停止儀表板

| 項目 | 內容 |
|---|---|
| 主要參與者 | 使用者或 AI Agent |
| 前置條件 | 儀表板在 port 到 port+10 之間的某個連接埠執行中（或未執行） |
| 觸發 | `/myoffice stop`、「關掉辦公室」、`node bin/cli.js --stop`、終端機 Ctrl+C |
| 後置條件 | 伺服器行程結束；SSE 連線關閉；瀏覽器頁面顯示斷線 |
| 相關程式 | `bin/cli.js`（`alreadyRunning`、`stopAt`）、`src/server.js`（`/api/shutdown`）、`onShutdown` hook |

## 主要流程

1. 使用者輸入 `/myoffice stop`；AI Agent 執行 `node bin/cli.js --stop`（不在背景）。
2. CLI 對 port、port+1、…、port+10 逐一 GET `/api/ping`，收集回應含 `whatsmyagentdoing` 的連接埠。
3. 對每個命中的連接埠 POST `/api/shutdown`，帶標頭 `x-whatsmyagentdoing: stop`。
4. 伺服器檢查 Host 白名單與該標頭；通過後回 `{ok: true}`，結束所有 SSE 連線，50 ms 後呼叫 `onShutdown`。
5. `onShutdown`：`watcher.stop()`（清掉 interval）、`server.close()`、`process.exit(0)`。
6. CLI 印「Stopped http://localhost:<port>…」；AI Agent 告知已停止。

## 替代流程

- **A1 未執行**：步驟 2 無命中 → 印「Not running.」。
- **A2 標頭不符**：步驟 4 回 403；不關閉。這擋掉跨站網頁的 CSRF。
- **A3 多個實例**：步驟 2 可能命中多個連接埠（見 UC-01 備註），會全部停掉。
- **A4 Ctrl+C**：行程直接結束，不經 `/api/shutdown`；效果相同。
- **A5 `--update` 後的停止**：UC-04 的 CLI 路徑更新完成後也走步驟 2 到 5，讓舊程式碼的實例下線。

## 流程圖

```mermaid
flowchart TD
  A([--stop]) --> B[ports = 空陣列]
  B --> C[p = port]
  C --> D{p <= port+10?}
  D -->|是| E[GET 127.0.0.1:p/api/ping]
  E --> F{回應含 whatsmyagentdoing?}
  F -->|是| G[ports.push p]
  F -->|否| H
  G --> H[p = p+1] --> D
  D -->|否| I{ports 為空?}
  I -->|是| J([印 Not running])
  I -->|否| K[對每個 p：POST /api/shutdown 帶 stop 標頭]
  K --> L{伺服器：Host 白名單且標頭 = stop?}
  L -->|否| M[403，不關閉]
  L -->|是| N[200 ok]
  N --> O[結束所有 SSE 連線]
  O --> P[50ms 後 onShutdown]
  P --> Q[watcher.stop／server.close／process.exit 0]
  Q --> R([印 Stopped url…])
  M --> R
```

## 循序圖

```mermaid
sequenceDiagram
  actor U as 使用者
  participant AG as AI Agent
  participant CLI as bin/cli.js
  participant S as server.js
  participant W as Watcher
  participant B as 瀏覽器

  U->>AG: /myoffice stop
  AG->>CLI: node bin/cli.js --stop
  loop p = port … port+10
    CLI->>S: GET /api/ping
    alt 有儀表板
      S-->>CLI: {app: whatsmyagentdoing}
      CLI->>CLI: ports.push(p)
    else 無
      S-->>CLI: 錯誤／逾時
    end
  end
  alt ports 非空
    loop 每個 p
      CLI->>S: POST /api/shutdown（x-whatsmyagentdoing: stop）
      S->>S: 驗 Host 與標頭
      S-->>CLI: 200 {ok: true}
      S-->>B: 結束 SSE 連線
      B->>B: onerror → 顯示 Disconnected
      S->>S: setTimeout(onShutdown, 50)
      S->>W: watcher.stop()
      S->>S: server.close()，process.exit(0)
    end
    CLI-->>AG: Stopped http://localhost:p
  else
    CLI-->>AG: Not running.
  end
  AG-->>U: 已停止
```
