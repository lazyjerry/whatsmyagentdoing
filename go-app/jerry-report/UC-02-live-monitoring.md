# UC-02 即時監看 agent 狀態

| 項目 | 內容 |
|---|---|
| 主要參與者 | 使用者（看儀表板） |
| 次要參與者 | Session 紀錄（Claude Code、Codex、Gemini CLI 寫在磁碟上的 JSONL）、瀏覽器 |
| 前置條件 | 伺服器已啟動（UC-01）且非 demo |
| 觸發 | 瀏覽器載入 `/` 後自動連 `/events` |
| 後置條件 | 畫面持續反映每個 agent 的狀態、工具、prompt 與回覆；紀錄有變化時數秒內更新 |
| 相關程式 | `watcher.go`（`Tick`、`track`、`open`、`feed`、`Sessions`）、`parsers.go`（三個 apply 函式、`clean`）、`server.go`（`broadcastLoop`、`push`、`snapshot`、`view`、`serveEvents`）、`lang.go` |

## 主要流程

三條 goroutine 以 channel 串起來：Watcher 讀紀錄、broadcastLoop 算快照、每個 SSE 連線各自送出。

### 讀紀錄（Watcher ticker goroutine，每 1 秒）

1. `Tick()` 取鎖；`loadCodexIndex`、`loadGeminiProjects` 比對 mtime，變了才重讀（Codex 索引只讀最後 2 MB；Gemini 把 `projects.json` 的 slug 與 `sha256(path)` 都對回專案路徑）。
2. 距上次走訪超過 5 秒：重新走訪三家目錄得到 `candidates`（Codex 只走時間窗內的日期目錄）；否則沿用上次的清單。
3. 對每個 candidate 呼叫 `track`：
   - 在 `failed` 且未到重試時間 → 跳過
   - 沒有 tracker 且 mtime 在時間窗內 → `open`：建 `Session`，子代理填 `ParentID`、`Subagent`；檔案不超過 896 KB 整檔讀，否則讀頭 128 KB（取 cwd、title、model）加尾 768 KB（取目前狀態，丟掉第一行殘缺）；Gemini 舊版 `.json` 整份解析並標 `whole`
   - 已有 tracker 且檔案變長 → `readRange(offset, size)` 讀增量
   - 檔案變短 → 丟掉 tracker，下一輪重開
   - `whole` 且 mtime 變了 → 整檔重讀
   - 讀取失敗 → 記入 `failed`，60 秒後重試，刪掉 tracker
4. `feed` 把 `partial` 加新內容依 `\n` 切行，最後一段留作下次的 `partial`；每行 `json.Unmarshal` 成 `any`，交給該 agent 的 apply 函式更新 `*Session`（規則見 structure.md 第 8 節）。
5. 移除 `LastEventAt`（或 mtime）早於時間窗的 tracker。
6. 放鎖；任一步有變更就對 `changes`（緩衝 1）做非阻塞送出。

### 算快照（broadcastLoop goroutine）

7. `select` 收到 `changes` 或 5 秒 ticker 到期，呼叫 `push(false)`。
8. `snapshot()`：`src.Sessions()` 取得 struct 副本（補 Gemini cwd／mtime、Codex 名稱）；每筆 `view()` 做時效修正與隱私；依 `LastEventAt` 降冪；`promptLanguage` 猜語言。
9. `json.Marshal` 後把開頭的 `{"now":…,` 換成 `{` 當簽章；與上次相同就不送。
10. 不同則更新 `last`，對每個 client channel 非阻塞送出整份 JSON；塞滿（8 幀）的 client 丟掉這一幀。

### 送出（每個 `/events` 連線一條 goroutine）

11. 連線時立刻送一次快照，再建 `chan string`（緩衝 8）登記到 `clients`。
12. 迴圈 `select`：收到 body 就寫 `data: …\n\n` 並 flush；20 秒沒事寫 `: ping\n\n`；client 斷線（`r.Context().Done()`）或 channel 被關（伺服器關閉）就離開並從 `clients` 移除。
13. 瀏覽器 `EventSource` 收到 `onData`，更新畫面（前端邏輯見上層 UC-02）。

## 替代流程

- **A1 讀不到某個紀錄檔**（權限、讀取中被刪）：`track` 回錯誤，該檔記入 `failed`、tracker 移除，其餘照常；60 秒後重試。伺服器不會因此中止。
- **A2 紀錄沒有新內容但狀態該變**（例如 typing 靜默超過 45 秒）：5 秒 ticker 觸發 `push`，`view()` 的時效判定讓簽章改變，照常推送。
- **A3 多個 tick 連續有變更**：`changes` 緩衝只有 1，多次送出合併成一次通知；broadcastLoop 一次 `push` 就拿到最新狀態。
- **A4 某個 client 很慢**：它的 channel 塞滿後丟幀，其他 client 不受影響；它之後收到的是更新的快照，不會落後太久。
- **A5 瀏覽器斷線**：`r.Context().Done()` 觸發，handler 從 `clients` 移除並關閉 channel；`EventSource` 自動重連會重新走步驟 11。
- **A6 伺服器關閉**（UC-03、UC-04）：`closeClients` 關掉所有 channel，handler 收到 `!ok` 就 return，`httpServer.Shutdown` 得以完成。
- **A7 檔案被整個改寫變短**：丟掉 tracker，下一輪以新檔重開；session 狀態重建。

## 流程圖

```mermaid
flowchart TD
  subgraph tick["Watcher goroutine（每 1 秒）"]
    T0([Tick]) --> T1[取鎖；載入 Codex 索引、Gemini 專案表]
    T1 --> T2{距上次走訪 > 5 秒?}
    T2 -->|是| T3[走訪三家目錄 → candidates]
    T2 -->|否| T4
    T3 --> T4[對每個 candidate track]
    T4 --> T5{有 tracker?}
    T5 -->|否，且 mtime 在時間窗內| T6["open：整檔或頭+尾；feed → apply"]
    T5 -->|是，檔案變長| T7["readRange 增量；feed → apply"]
    T5 -->|是，檔案變短| T8[刪 tracker]
    T5 -->|是，無變化| T9[只更新 Mtime]
    T6 --> T10
    T7 --> T10
    T8 --> T10
    T9 --> T10[移除超出時間窗的 tracker；放鎖]
    T10 --> T11{有變更?}
    T11 -->|是| T12["changes ← struct{}（非阻塞）"]
    T11 -->|否| T0
    T12 --> T0
  end

  subgraph bl["broadcastLoop goroutine"]
    B0([select]) -->|changes 或 5 秒 ticker| B1["snapshot：Sessions 副本 → view → 排序 → lang"]
    B1 --> B2[去掉 now 後比對簽章]
    B2 --> B3{與上次相同?}
    B3 -->|是| B0
    B3 -->|否| B4["對每個 client chan 非阻塞送出 JSON"]
    B4 --> B0
  end

  subgraph ev["每個 /events 連線"]
    E0([GET /events]) --> E1[送初始快照；登記 chan 緩衝 8]
    E1 --> E2{select}
    E2 -->|收到 body| E3["寫 data: …；flush"] --> E2
    E2 -->|20 秒 ping| E4["寫 : ping；flush"] --> E2
    E2 -->|client 斷線或 chan 關閉| E5[移除 client；return]
  end

  T12 -.-> B0
  B4 -.-> E2
```

## 循序圖

```mermaid
sequenceDiagram
  participant FS as Session 紀錄
  participant W as Watcher（goroutine）
  participant P as parsers.go
  participant BL as broadcastLoop（goroutine）
  participant EV as serveEvents（goroutine）
  participant B as 瀏覽器

  B->>EV: GET /events
  EV->>BL: snapshot()（同一程序內呼叫）
  EV-->>B: data: 初始快照
  EV->>EV: 登記 client chan（緩衝 8）

  par 每 1 秒
    loop Tick
      W->>FS: ReadDir／Stat／ReadAt
      FS-->>W: 新增的行
      W->>P: applyClaude／applyCodex／applyGemini(session, line)
      P-->>W: 更新 Session 欄位
      opt 有變更
        W->>BL: changes ← struct{}（非阻塞）
      end
    end
  and 收到 changes 或每 5 秒
    loop push
      BL->>W: Sessions()
      W-->>BL: []Session 副本
      BL->>BL: view() 時效修正、排序、promptLanguage
      alt 簽章改變
        BL->>EV: client chan ← JSON（非阻塞，滿了丟幀）
        EV-->>B: data: 快照
      else 相同
        BL->>BL: 不送
      end
    end
  and 每 20 秒
    EV-->>B: ping
  end

  B->>B: onData：重新渲染
```

## 備註

- Watcher 不用 fsnotify 而用輪詢，理由寫在 `watcher.go` 檔頭：三個平台行為一致，幾百個檔案的 stat 迴圈很便宜。
- `Sessions()` 回傳的是值的切片，但在複製前會直接改寫 tracker 內 `*Session` 的 `Cwd`、`LastEventAt`、`Title`（Gemini、Codex 的補值）。這些補值是冪等的，而且在鎖內進行，不會造成 race。
- `track` 在「檔案變短」分支刪掉 tracker 後仍對同一個 `t.session.Mtime` 賦值；那個 session 已不在 map 內，下一輪重開會建新的，所以無害。
- `failed` 的項目在重試成功後不會被刪，但時間已過，`w.failed[c.file] > now` 不成立，之後每輪照常處理。
- 大檔只讀頭尾，中段的事件不會被套用；「頭」提供 cwd、title、model，「尾」提供現在的狀態，這是刻意的取捨（與 Node 版相同）。
