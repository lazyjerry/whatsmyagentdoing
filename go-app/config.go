package main

// Build-time settings in one place: every external host this program reaches
// out to, and every tunable clock. Change a value, then rebuild
// (scripts/start.sh rebuilds, reinstalls and restarts the dashboard). The
// README's「設定」section documents each one.

import "time"

const (
	Repo   = "lazyjerry/whatsmyagentdoing"
	Branch = "main"

	// false: nothing reaches GitHub until the "Check for updates" button on the
	// page is pressed. true: the page checks on load and every 30 minutes, and
	// the CLI checks on start-up. WMAD_NO_UPDATE_CHECK=1 turns checks off either way.
	AutoUpdateCheck = false

	RepoURL   = "https://github.com/" + Repo
	LatestURL = "https://raw.githubusercontent.com/" + Repo + "/" + Branch + "/package.json"
)

// 狀態時鐘（server.go 的 view 用）。Log 只記完成的區塊，所以一個突然沒動靜的
// session 要靠時間判定它怎麼了：距最後一個事件超過下列時間，就改判成下一個狀態。
// 改完要重建（scripts/start.sh）才生效。
const (
	// 撰寫回覆：回覆區塊落地後這麼久沒有後續，視為這一輪結束 → 已完成。
	TypingDoneAfter = 45 * time.Second
	// 思考中：這麼久沒動靜，視為中途被關掉 → 閒置（人物下班）。
	ThinkingIdleAfter = 15 * time.Minute
	// 執行中：工具呼叫這麼久沒回報 → 閒置。
	ToolIdleAfter = 45 * time.Minute
	// 等你回應／錯誤：人物留在辦公室揮手這麼久，之後退出「需要你」清單 → 閒置。
	AttentionIdleAfter = 5 * time.Hour
)

// 掃描與更新的時間設定。
const (
	// --hours 的預設值：超過這段時間沒活動的 session 不顯示。
	DefaultWindowHours = 24.0
	// 多久 stat 一次已知的 log 檔與 SQLite 資料庫（檔案有變才真的讀）。
	PollInterval = time.Second
	// 多久重新走一次 log 目錄找新檔案。
	RescanInterval = 5 * time.Second
	// 讀不到的 log 檔（權限不足、讀到一半被刪）多久後再試。
	UnreadableRetry = time.Minute
	// SQLite 讀取失敗（例如撞上 WAL checkpoint）多久後再試。
	DBRetry = 10 * time.Second
	// 一次版本檢查的結果沿用多久，才再向 GitHub 查一次；
	// 頁面自己的自動檢查間隔另寫在 index.html（也是 30 分鐘）。
	UpdateCheckEvery = 30 * time.Minute
)

// 警告標記（server.go 的 view 用）：跟狀態分開顯示，同一個人可以同時有兩種。
// 設 0 關閉該項。
const (
	// 快要 compact：最新一次請求的 context 達到自動 compact 門檻的這個比例就標記。
	CompactWarnRatio = 0.85
	// 本輪太久：思考／執行／撰寫中，這一輪已經跑超過這麼久就標記（等你回應不算）。
	LongTurnAfter = 60 * time.Minute
)

// 自動 compact 門檻（compact.go 用）。設定檔讀得到就照設定，以下是讀不到時的預設。
const (
	// Claude Code：~/.claude/settings.json 的 modelSettings.<model>.autoCompactWindow 沒設時的 window；
	// context 已經超過它算出的門檻，就表示是 1M 模型，改用 ClaudeLargeWindow。
	ClaudeDefaultWindow = 200_000
	ClaudeLargeWindow   = 1_000_000
	// Claude Code 的門檻 = window − 這個保留量（20k 留給摘要輸出、13k 緩衝；本機實測吻合）。
	ClaudeCompactReserve = 33_000
	// Codex：~/.codex/config.toml 沒設 model_auto_compact_token_limit 時，
	// 門檻 = log 裡的 model_context_window × 這個比例。
	CodexCompactRatio = 0.9
)
