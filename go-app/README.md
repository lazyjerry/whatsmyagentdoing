# whatsmyagentdoing（Go 版）

與 `bin/cli.js` 功能一致的 Go 後端：同一份網頁、同一組 3D 模型、同樣的旗標與連接埠。只用 Go 標準函式庫，沒有第三方依賴。網頁、模型、場景目錄與版本號都以 `go:embed` 包在執行檔裡，**執行時不讀 go-app/ 以外的任何檔案**，執行檔可以單獨複製到任何地方使用。

## 需求

- Go 1.22 以上（`go version` 確認）。
- 執行時不需要 Node.js，也不需要這個 clone。

## 建置

```bash
cd <clone>/go-app
scripts/sync-static.sh          # 把上層 public/、assets/、scenes.json、版本號同步到 static/
go build -o whatsmyagentdoing .
```

`static/` 是內嵌副本，真源在上層；前端有改動（例如 `npm run build` 重打 3D bundle）就重跑同步再建置。`scripts/sync-static.sh --check` 只比對不改寫，有漂移以非零結束。產出的執行檔已列入 `.gitignore`。

## 啟動

```bash
./whatsmyagentdoing
```

會在 `127.0.0.1:4545` 架起儀表板並開啟瀏覽器；印出的那一行就是網址。已在執行時，同一道指令只會重新開瀏覽器分頁。連接埠被別的程式占住時，自動往後找最多 10 個埠。

| 旗標 | 作用 |
|---|---|
| `-p, --port <n>` | 改連接埠（預設 4545） |
| `--hours <n>` | 只顯示最近 n 小時有活動的 session（預設 24） |
| `--no-open` | 不開瀏覽器 |
| `--private` | 隱藏 prompt、回覆與標題，只留狀態 |
| `--json` | 印一次快照後結束，不開網頁 |
| `--demo` | 用虛構團隊取代真實 session |
| `--stop` | 停止執行中的儀表板 |
| `--update` | `git pull`、重新同步 `static/`、重新 `go build`，並停掉執行中的儀表板 |
| `--scenes` | 列出 3D 場景（已安裝與可下載） |
| `--scene <id>` | 需要時下載場景，再以該場景開啟 |
| `--remove-scene <id>` | 刪除已下載的場景 |

## 重啟

```bash
./whatsmyagentdoing --stop
./whatsmyagentdoing
```

`--stop` 會掃 `--port` 起算的 11 個埠，只停掉回應 `/api/ping` 為本程式的那些。

## 更新

```bash
./whatsmyagentdoing --update
```

流程：在 clone 根目錄 `git pull --ff-only`，跑 `scripts/sync-static.sh`，再對目前這個執行檔原地 `go build`，最後停掉舊的儀表板。網頁角落的「檢查更新」與「Update」按鈕走同一條路，更新完會自動以新執行檔重啟。

- 只有執行檔還放在 clone 裡的 `go-app/` 時才能更新（或用 `WMAD_SRC=<clone>/go-app` 指過去）；複製到別處的執行檔 `canUpdate` 為 false，頁面不顯示 Update 鈕，要更新就回 clone 重建一次再複製。
- 需要 `go` 在 PATH 上；沒有的話會印出手動重建的指令。
- Windows 無法覆寫正在執行的 exe：先 `--stop`，再跑 `--update`。
- 版本號來自同步時從 `package.json` 取出的 `static/VERSION`，與 Node 版相同。

## 搭配 skill 使用

`SKILL.md` 預設呼叫 Node 版。要讓 agent 改用 Go 版，把 skill 裡的 `node "<base directory>/bin/cli.js"` 換成 `"<base directory>/go-app/whatsmyagentdoing"` 即可，旗標完全相同。

## 環境變數

| 變數 | 作用 |
|---|---|
| `WMAD_SRC` | 指定 go-app/ 原始碼目錄，給 `--update` 用；未設定時看執行檔所在層與工作目錄 |
| `CLAUDE_CONFIG_DIR` / `CODEX_HOME` / `GEMINI_CLI_HOME` | 改 log 來源目錄，與 Node 版相同 |
| `WMAD_NO_UPDATE_CHECK=1` | 完全不連 GitHub 檢查版本 |
| `WMAD_SCENES_LOCAL=<dir>` | 從本機目錄讀場景包，測試安裝流程用 |
| `WMAD_LATEST_VERSION=<ver>` | 假裝遠端版本，測試更新提示用 |

下載的場景包仍放 `~/.whatsmyagentdoing/scenes/`，與 Node 版共用。

## 與 Node 版的已知差異

- 文字截斷以 Unicode 字元計長，Node 以 UTF-16 code unit 計長；含 emoji 的長句尾端可能多出一個字元。
- `Editing a, b` 這類多檔案摘要會先排序檔名，Node 依 JSON 原始順序。
- 開瀏覽器、重啟時不設 detached session；對 `open`／`xdg-open` 這種立即返回的指令沒有差別。

## 開發

```bash
scripts/sync-static.sh --check && gofmt -l . && go vet ./... && go test ./... && go build -o whatsmyagentdoing .
```

`go test ./...` 跑單元測試與功能測試；功能測試會先 `go build` 一份執行檔來驗 CLI 旗標、啟動與 `--stop`，`go test -short ./...` 跳過這段。測試不讀真實的 session 紀錄、不連網。

對照檢查：兩邊各跑一次 `--json --hours 72`，比對同一組 session 的欄位。前端的打包（`npm run build`）與瀏覽器測試（`npm run test:ui`）仍在上層以 Node 執行。
