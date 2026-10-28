package main

// Turns raw session-log lines from Claude Code, Codex and Gemini CLI into one
// shared Session shape. Port of src/parsers.js; the log formats are loose and
// evolve, so records are handled as map[string]any rather than typed structs.
//
// Status values:
//   thinking  – model is reasoning / waiting on the API
//   tool      – a tool call is in flight (Activity says which)
//   typing    – it has started writing its reply but the turn is not over
//   waiting   – it asked you something and is blocked on you
//   done      – the turn finished
//   interrupted, error, idle

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Session is the parser's view of one log file. Empty strings and zero
// timestamps stand for JavaScript's null.
type Session struct {
	Agent         string
	ID            string
	File          string
	SessionID     string
	Cwd           string
	Workspace     string // first cwd in the log: the folder the IDE window or CLI was opened in
	Title         string
	Model         string
	Subagent      bool
	ParentID      string
	Platform      string // vscode | cli | sdk | desktop; "" when the log says nothing
	Tool          string // "agy" for Antigravity rows; "" when the agent's default CLI resumes it
	PromptKind    string // command | notification; "" for a plain prompt
	Status        string
	Activity      string
	Prompt        string
	Reply         string
	Error         string
	TurnStartedAt int64 // ms since epoch
	LastEventAt   int64
	StartedAt     int64
	Mtime         int64 // file mtime, set by the watcher
	GeminiProject string
	titleLocked   bool // a custom (renamed) title beats any later ai-title

	// Big log whose tail holds no prompt: the turn began at or before this
	// (the tail's first event). Only used while TurnStartedAt is 0.
	TurnStartFloor int64
	ContextTokens  int64 // context size of the latest request; 0 when unknown
	ContextWindow  int64 // Codex's model_context_window
	CompactAt      int64 // where the CLI compacts on its own; set by the watcher (compact.go)
}

var waitingTools = map[string]bool{"AskUserQuestion": true, "ExitPlanMode": true}

func newSession(agent, id, file string) *Session {
	return &Session{Agent: agent, ID: id, File: file, Status: "idle"}
}

// ------------------------------------------------------------- map helpers

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%v", x)
	case bool, json.Number:
		return fmt.Sprintf("%v", x)
	}
	return ""
}

func str(m map[string]any, key string) string { return asString(m[key]) }

func num(m map[string]any, key string) int64 {
	f, _ := m[key].(float64)
	return int64(f)
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	}
	return true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// baseName is path.basename for both / and \ separated paths; logs written on
// Windows carry backslashes even when the dashboard runs elsewhere.
func baseName(p string) string {
	p = strings.TrimRight(p, "/\\")
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

// ------------------------------------------------------------- text clean-up

var (
	// Injected context blocks dropped whole, text included (one regex per tag:
	// RE2 has no backreference to pair the closing tag with the opening one).
	reContextBlocks = []*regexp.Regexp{
		regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`),
		regexp.MustCompile(`(?s)<ide_opened_file>.*?</ide_opened_file>`),
		regexp.MustCompile(`(?s)<ide_selection>.*?</ide_selection>`),
	}
	reCommandMessage = regexp.MustCompile(`(?s)<command-message>.*?</command-message>`)
	reCommandArgs    = regexp.MustCompile(`(?s)<command-args>.*?</command-args>`)
	reAnyTag         = regexp.MustCompile(`<[^>]+>`)
	reImage          = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reLink           = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reMarkdownNoise  = regexp.MustCompile("(?m)\\*\\*|__|`+|^#+\\s*")
	reSpaces         = regexp.MustCompile(`\s+`)
	reCommandName    = regexp.MustCompile(`<command-name>([^<]+)</command-name>`)
)

func isAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isWord(b byte) bool  { return isAlpha(b) || (b >= '0' && b <= '9') || b == '_' }
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

// stripPairedTags drops injected context blocks (<foo attr="…">…</foo>) and
// keeps the text of short inline tags. The JavaScript version is one regex
// with a backreference, which Go's RE2 engine has no equivalent for.
func stripPairedTags(t string) string {
	var b strings.Builder
	i := 0
	for i < len(t) {
		if t[i] != '<' || i+1 >= len(t) || !isAlpha(t[i+1]) {
			b.WriteByte(t[i])
			i++
			continue
		}
		k := i + 1
		for k < len(t) && (isWord(t[k]) || t[k] == '-') {
			k++
		}
		name := t[i+1 : k]
		m := k
		ok := false
		if m < len(t) && t[m] == '>' {
			ok = true
			m++
		} else if m < len(t) && isSpace(t[m]) {
			for m < len(t) && t[m] != '>' {
				m++
			}
			if m < len(t) {
				ok = true
				m++
			}
		}
		if ok {
			closing := "</" + name + ">"
			if idx := strings.Index(t[m:], closing); idx >= 0 {
				end := m + idx + len(closing)
				match := t[i:end]
				if utf8.RuneCountInString(match) > 300 {
					b.WriteByte(' ')
				} else {
					b.WriteString(reAnyTag.ReplaceAllString(match, " "))
				}
				i = end
				continue
			}
		}
		b.WriteByte(t[i])
		i++
	}
	return b.String()
}

func clean(text string, max int) string {
	if text == "" {
		return ""
	}
	t := text
	// Codex wraps the real prompt after injected context blocks.
	if marker := strings.LastIndex(t, "## My request:"); marker != -1 {
		t = t[marker+len("## My request:"):]
	}
	for _, re := range reContextBlocks {
		t = re.ReplaceAllString(t, " ")
	}
	t = reCommandMessage.ReplaceAllString(t, " ")
	t = reCommandArgs.ReplaceAllString(t, " ")
	t = stripPairedTags(t)
	// Markdown noise reads badly in a two-line preview.
	t = reImage.ReplaceAllString(t, " ")
	t = reLink.ReplaceAllString(t, "$1")
	t = reMarkdownNoise.ReplaceAllString(t, "")
	t = strings.TrimSpace(reSpaces.ReplaceAllString(t, " "))
	if t == "" {
		return ""
	}
	return truncate(t, max)
}

func short(s string, max int) string {
	s = strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
	return truncate(s, max)
}

// parseTime accepts the ISO 8601 stamps all three tools write; 0 means unparsable.
func parseTime(ts string) int64 {
	if ts == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999Z0700", "2006-01-02T15:04:05.999999999"} {
		if t, err := time.Parse(layout, ts); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

func stamp(s *Session, ts string) int64 {
	t := parseTime(ts)
	if t != 0 {
		if t > s.LastEventAt {
			s.LastEventAt = t
		}
		if s.StartedAt == 0 {
			s.StartedAt = t
		}
	}
	return t
}

// set changes the status and leaves Activity alone; setA also sets Activity
// ("" clears it). Matches set(s, status) vs set(s, status, null) in the JS.
func set(s *Session, status string) {
	s.Status = status
	if status != "error" {
		s.Error = ""
	}
}

func setA(s *Session, status, activity string) {
	s.Activity = activity
	set(s, status)
}

// ---------------------------------------------------------------- Claude Code

func describeClaudeTool(name string, input map[string]any) string {
	base := func(p string) string {
		if p == "" {
			return ""
		}
		return baseName(p)
	}
	if strings.HasPrefix(name, "mcp__") {
		parts := strings.Split(name, "__")
		server := ""
		if len(parts) > 1 {
			server = parts[1]
		}
		rest := ""
		if len(parts) > 2 {
			rest = strings.Join(parts[2:], "__")
		}
		return server + " · " + rest
	}
	switch name {
	case "Bash":
		return "Bash: " + short(firstNonEmpty(str(input, "description"), str(input, "command")), 90)
	case "Read", "Edit", "Write", "MultiEdit", "NotebookEdit":
		return name + " " + base(firstNonEmpty(str(input, "file_path"), str(input, "notebook_path")))
	case "Grep", "Glob":
		return name + " " + short(str(input, "pattern"), 60)
	case "WebFetch":
		if h := urlHost(str(input, "url")); h != "" {
			return "WebFetch " + h
		}
		return "WebFetch"
	case "WebSearch":
		return "WebSearch “" + short(str(input, "query"), 60) + "”"
	case "Agent", "Task":
		return "Subagent: " + short(firstNonEmpty(str(input, "description"), str(input, "subagent_type")), 60)
	case "Skill":
		return "Skill " + str(input, "skill")
	case "TodoWrite":
		return "Updating todo list"
	}
	return name
}

// Where a session runs: vscode | cli | sdk | desktop. Claude Code writes
// `entrypoint` on every record, Codex `originator` in session_meta; Gemini
// CLI logs carry nothing, so they count as cli.
func claudePlatform(entrypoint string) string {
	switch {
	case strings.Contains(entrypoint, "vscode"):
		return "vscode"
	case strings.HasPrefix(entrypoint, "sdk"):
		return "sdk"
	}
	return "cli"
}

func codexPlatform(originator string) string {
	o := strings.ToLower(originator)
	switch {
	case strings.Contains(o, "desktop"):
		return "desktop"
	case strings.Contains(o, "vscode"):
		return "vscode"
	case strings.Contains(o, "exec"):
		return "sdk"
	}
	return "cli"
}

func applyClaude(s *Session, v any) {
	d := asMap(v)
	if d == nil {
		return
	}
	ts := stamp(s, str(d, "timestamp"))
	if cwd := str(d, "cwd"); cwd != "" {
		s.Cwd = cwd
		if s.Workspace == "" {
			s.Workspace = cwd
		}
	}
	if ep := str(d, "entrypoint"); ep != "" {
		s.Platform = claudePlatform(ep)
	}

	switch str(d, "type") {
	case "custom-title":
		if t := str(d, "customTitle"); t != "" {
			s.Title = t
			s.titleLocked = true
		}
	case "ai-title":
		// Same title the VS Code extension's session list shows, unless renamed.
		if t := str(d, "aiTitle"); t != "" && !s.titleLocked {
			s.Title = t
		}
	case "agent-name":
		if n := str(d, "agentName"); s.Title == "" && n != "" {
			s.Title = n
		}
	case "system":
		switch str(d, "subtype") {
		case "stop_hook_summary":
			setA(s, "done", "")
		case "api_error":
			setA(s, "error", "")
			e := asMap(d["error"])
			s.Error = short(firstNonEmpty(str(e, "formatted"), str(e, "message"), "API error"), 120)
		case "compact_boundary":
			s.ContextTokens = num(asMap(d["compactMetadata"]), "postTokens")
		}
	case "user":
		// The summary written after a compaction is not a prompt: the turn
		// that ran out of context carries on.
		if truthy(d["isMeta"]) || truthy(d["isCompactSummary"]) {
			return
		}
		msg := asMap(d["message"])
		c := msg["content"]
		var text string
		switch cv := c.(type) {
		case string:
			text = cv
		case []any:
			var parts []string
			for _, x := range cv {
				xm := asMap(x)
				if str(xm, "type") == "tool_result" {
					// Tool finished (or you answered its question); model reads the result.
					set(s, "thinking")
					return
				}
				if str(xm, "type") == "text" {
					parts = append(parts, str(xm, "text"))
				}
			}
			text = strings.Join(parts, "\n")
		}
		if text == "" {
			return
		}
		if strings.HasPrefix(text, "[Request interrupted") {
			setA(s, "interrupted", "")
			return
		}
		if strings.Contains(text, "<local-command-stdout>") || strings.Contains(text, "<local-command-caveat>") {
			return
		}
		cmd := reCommandName.FindStringSubmatch(text)
		isNotification := strings.HasPrefix(strings.TrimLeft(text, " \t\r\n\f\v"), "<task-notification>")
		if !isNotification {
			if cmd != nil {
				s.Prompt = strings.TrimSpace(cmd[1])
			} else {
				s.Prompt = clean(text, 220)
			}
			if s.Title == "" && truthy(d["isSidechain"]) {
				s.Title = clean(text, 80)
			}
			s.Reply = ""
		}
		switch {
		case isNotification:
			s.PromptKind = "notification"
		case cmd != nil:
			s.PromptKind = "command"
		default:
			s.PromptKind = ""
		}
		s.TurnStartedAt = ts
		setA(s, "thinking", "")
	case "assistant":
		m := asMap(d["message"])
		if model := str(m, "model"); model != "" && model != "<synthetic>" {
			s.Model = model
		}
		// Every content block repeats the message's usage; the latest one wins.
		if u := asMap(m["usage"]); u != nil && !truthy(d["isSidechain"]) {
			if n := num(u, "input_tokens") + num(u, "cache_creation_input_tokens") + num(u, "cache_read_input_tokens"); n > 0 {
				s.ContextTokens = n
			}
		}
		for _, bv := range asSlice(m["content"]) {
			b := asMap(bv)
			switch str(b, "type") {
			case "thinking", "redacted_thinking":
				set(s, "thinking")
			case "text":
				if r := clean(str(b, "text"), 220); r != "" {
					s.Reply = r
				}
				stop := str(m, "stop_reason")
				if stop == "end_turn" || stop == "stop_sequence" {
					setA(s, "done", "")
				} else {
					setA(s, "typing", "")
				}
			case "tool_use":
				name := str(b, "name")
				status := "tool"
				if waitingTools[name] {
					status = "waiting"
				}
				setA(s, status, describeClaudeTool(name, asMap(b["input"])))
			}
		}
		if truthy(d["isApiErrorMessage"]) {
			setA(s, "error", "")
			s.Error = s.Reply
		}
	}
}

// ---------------------------------------------------------------------- Codex

func codexText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		parts := make([]string, 0, len(c))
		for _, x := range c {
			parts = append(parts, str(asMap(x), "text"))
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

var (
	reCodexCmd  = regexp.MustCompile(`(?:cmd|command)\s*:\s*"((?:[^"\\]|\\.)*)"`)
	reCodexTool = regexp.MustCompile(`tools\.([a-zA-Z_]+)\(`)
)

func lastOrString(v any) string {
	if arr, ok := v.([]any); ok {
		if len(arr) == 0 {
			return ""
		}
		return asString(arr[len(arr)-1])
	}
	return asString(v)
}

func describeCodexCall(name string, raw any) string {
	args := raw
	if rs, ok := raw.(string); ok {
		var parsed any
		if json.Unmarshal([]byte(rs), &parsed) == nil {
			args = parsed
		}
	}
	if name == "request_user_input" || name == "request_user_input_async" {
		return "Asking you a question"
	}
	if am, ok := args.(map[string]any); ok && am != nil {
		cmd := am["cmd"]
		if !truthy(cmd) {
			cmd = am["command"]
		}
		if truthy(cmd) {
			return "Shell: " + short(lastOrString(cmd), 90)
		}
		if q := str(am, "query"); q != "" {
			return name + " “" + short(q, 60) + "”"
		}
	}
	if as, ok := args.(string); ok {
		// The `exec` tool wraps other tools in a JS snippet; pull out the command.
		if m := reCodexCmd.FindStringSubmatch(as); m != nil {
			c := strings.ReplaceAll(strings.ReplaceAll(m[1], `\n`, " "), `\"`, `"`)
			return "Shell: " + short(c, 90)
		}
		if t := reCodexTool.FindStringSubmatch(as); t != nil {
			return t[1]
		}
	}
	return name
}

func applyCodex(s *Session, v any) {
	d := asMap(v)
	if d == nil {
		return
	}
	ts := stamp(s, str(d, "timestamp"))
	p := asMap(d["payload"])

	switch str(d, "type") {
	case "session_meta":
		if cwd := str(p, "cwd"); cwd != "" {
			s.Cwd = cwd
		}
		if o := str(p, "originator"); o != "" {
			s.Platform = codexPlatform(o)
		}
		if src := asMap(p["source"]); src != nil && truthy(src["subagent"]) {
			s.Subagent = true
		}
		if pt := str(p, "parent_thread_id"); pt != "" {
			s.ParentID = pt
			s.Subagent = true
		}
		if tsrc := str(p, "thread_source"); tsrc != "" && tsrc != "user" {
			s.Subagent = true
		}
	case "turn_context":
		if m := str(p, "model"); m != "" {
			s.Model = m
		}
		if cwd := str(p, "cwd"); cwd != "" {
			s.Cwd = cwd
		}
	case "compacted":
		s.ContextTokens = 0 // known again at the next token_count
	case "event_msg":
		switch str(p, "type") {
		case "task_started":
			s.TurnStartedAt = ts
			s.Reply = ""
			setA(s, "thinking", "")
		case "task_complete":
			if m := str(p, "last_agent_message"); m != "" {
				s.Reply = clean(m, 220)
			}
			setA(s, "done", "")
		case "turn_aborted":
			setA(s, "interrupted", "")
		case "error":
			setA(s, "error", "")
			s.Error = short(firstNonEmpty(str(p, "message"), "Error"), 120)
		case "user_message":
			s.Prompt = clean(str(p, "message"), 220)
			setA(s, "thinking", "")
		case "agent_message":
			if r := clean(str(p, "message"), 220); r != "" {
				s.Reply = r
			}
			setA(s, "typing", "")
		case "token_count":
			info := asMap(p["info"])
			if n := num(asMap(info["last_token_usage"]), "input_tokens"); n > 0 {
				s.ContextTokens = n
			}
			if w := num(info, "model_context_window"); w > 0 {
				s.ContextWindow = w
			}
		case "exec_approval_request", "apply_patch_approval_request", "request_user_input":
			setA(s, "waiting", "Needs your approval")
		case "item_started", "item_completed":
			it := asMap(p["item"])
			started := str(p, "type") == "item_started"
			running := "thinking"
			if started {
				running = "tool"
			}
			switch str(it, "type") {
			case "UserMessage":
				if t := clean(codexText(it["content"]), 220); t != "" {
					s.Prompt = t
				}
			case "AgentMessage":
				if r := clean(codexText(it["content"]), 220); r != "" {
					s.Reply = r
				}
				setA(s, "typing", "")
			case "Reasoning":
				set(s, "thinking")
			case "CommandExecution":
				setA(s, running, "Shell: "+short(lastOrString(it["command"]), 90))
			case "McpToolCall":
				setA(s, running, firstNonEmpty(str(it, "server"), "mcp")+" · "+str(it, "tool"))
			case "FileChange":
				changes := asMap(it["changes"])
				names := make([]string, 0, len(changes))
				for f := range changes {
					names = append(names, f)
				}
				sort.Strings(names)
				for i, f := range names {
					names[i] = baseName(f)
				}
				setA(s, running, "Editing "+strings.Join(names, ", "))
			case "Extension":
				setA(s, running, firstNonEmpty(str(it, "kind"), "tool")+" "+short(str(it, "query"), 50))
			}
		}
	case "response_item":
		switch str(p, "type") {
		case "reasoning":
			set(s, "thinking")
		case "function_call", "custom_tool_call", "local_shell_call", "web_search_call":
			name := firstNonEmpty(str(p, "name"), str(p, "type"))
			status := "tool"
			if strings.HasPrefix(name, "request_user_input") {
				status = "waiting"
			}
			var raw any
			for _, k := range []string{"arguments", "input", "action"} {
				if val, ok := p[k]; ok && val != nil {
					raw = val
					break
				}
			}
			setA(s, status, describeCodexCall(name, raw))
		case "function_call_output", "custom_tool_call_output":
			set(s, "thinking")
		case "message":
			if str(p, "role") == "assistant" {
				if r := clean(codexText(p["content"]), 220); r != "" {
					s.Reply = r
				}
				setA(s, "typing", "")
			}
		}
	}
}

// --------------------------------------------------------------- Gemini CLI
//
// ~/.gemini/tmp/<project>/chats/session-*.jsonl, append-only: a metadata
// line, then message records. A message is re-appended in full whenever it
// changes (tool calls finishing etc.), so the last line for an id wins.
// Older versions wrote a single .json file instead; see applyGeminiDocument.

var geminiPending = map[string]bool{"validating": true, "scheduled": true, "executing": true}

func geminiText(content any) string {
	switch c := content.(type) {
	case nil:
		return ""
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, x := range c {
			b.WriteString(geminiText(x))
		}
		return b.String()
	case map[string]any:
		if t, ok := c["text"].(string); ok {
			return t
		}
	}
	return ""
}

func describeGeminiTool(tc map[string]any) string {
	a := asMap(tc["args"])
	name := firstNonEmpty(str(tc, "name"), str(tc, "displayName"), "tool")
	display := firstNonEmpty(str(tc, "displayName"), name)
	file := firstNonEmpty(str(a, "absolute_path"), str(a, "file_path"), str(a, "path"))
	switch name {
	case "run_shell_command":
		return "Shell: " + short(firstNonEmpty(str(a, "description"), str(a, "command")), 90)
	case "read_file", "write_file", "replace", "edit":
		b := ""
		if file != "" {
			b = baseName(file)
		}
		return strings.TrimSpace(display + " " + b)
	case "glob", "search_file_content", "grep":
		return display + " " + short(str(a, "pattern"), 60)
	case "google_web_search":
		return "WebSearch “" + short(str(a, "query"), 60) + "”"
	case "web_fetch":
		return "WebFetch " + short(firstNonEmpty(str(a, "url"), str(a, "prompt")), 60)
	}
	return display
}

func applyGemini(s *Session, v any) {
	d := asMap(v)
	if d == nil {
		return
	}
	// metadata line / $set updates
	meta := asMap(d["$set"])
	if meta == nil && truthy(d["sessionId"]) && !truthy(d["type"]) {
		meta = d
	}
	if meta != nil {
		s.Platform = "cli"
		if id := str(meta, "sessionId"); id != "" {
			s.SessionID = id
		}
		if str(meta, "kind") == "subagent" {
			s.Subagent = true
		}
		if dirs := asSlice(meta["directories"]); len(dirs) > 0 && s.Cwd == "" {
			if first := asString(dirs[0]); first != "" {
				s.Cwd = first
			}
		}
		if sum := str(meta, "summary"); sum != "" {
			s.Title = clean(sum, 80)
		}
		if st := str(meta, "startTime"); st != "" {
			stamp(s, st)
		}
		for _, m := range asSlice(meta["messages"]) {
			applyGemini(s, m)
		}
		return
	}
	if truthy(d["$patch"]) || truthy(d["$rewindTo"]) || !truthy(d["type"]) {
		return
	}

	stamp(s, str(d, "timestamp"))
	switch str(d, "type") {
	case "user":
		content, ok := d["displayContent"]
		if !ok || content == nil {
			content = d["content"]
		}
		text := clean(geminiText(content), 220)
		if text == "" {
			return
		}
		if strings.HasPrefix(text, "/") {
			s.Prompt = strings.SplitN(text, " ", 2)[0]
			s.PromptKind = "command"
		} else {
			s.Prompt = text
			s.PromptKind = ""
		}
		s.Reply = ""
		if ts := str(d, "timestamp"); ts != "" {
			s.TurnStartedAt = parseTime(ts)
		}
		setA(s, "thinking", "")
	case "gemini":
		if m := str(d, "model"); m != "" {
			s.Model = m
		}
		calls := asSlice(d["toolCalls"])
		var waiting map[string]any
		var pending []map[string]any
		for _, cv := range calls {
			c := asMap(cv)
			if waiting == nil && str(c, "status") == "awaiting_approval" {
				waiting = c
			}
			if geminiPending[str(c, "status")] {
				pending = append(pending, c)
			}
		}
		text := clean(geminiText(d["content"]), 220)
		switch {
		case waiting != nil:
			setA(s, "waiting", describeGeminiTool(waiting))
		case len(pending) > 0:
			setA(s, "tool", describeGeminiTool(pending[len(pending)-1]))
		case len(calls) > 0 && text == "":
			setA(s, "thinking", "") // tools done, model continues
		case text != "":
			s.Reply = text
			setA(s, "typing", "")
		default:
			set(s, "thinking")
		}
	case "error":
		setA(s, "error", "")
		s.Error = short(firstNonEmpty(geminiText(d["content"]), "Error"), 120)
	case "info", "warning":
		t := strings.ToLower(geminiText(d["content"]))
		if strings.Contains(t, "cancel") || strings.Contains(t, "interrupt") {
			setA(s, "interrupted", "")
		}
	}
}

// Legacy whole-file .json sessions.
func applyGeminiDocument(s *Session, doc any) {
	applyGemini(s, map[string]any{"$set": doc})
}
