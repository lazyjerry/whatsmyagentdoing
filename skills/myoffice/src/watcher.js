'use strict';

// Finds recent session logs on disk and keeps reading them as they grow.
// Polling (not fs.watch) because it behaves the same on macOS, Linux and
// Windows, and a 1s stat loop over a few hundred files is cheap.

const fs = require('fs');
const os = require('os');
const path = require('path');
const { EventEmitter } = require('events');
const crypto = require('crypto');
const { newSession, applyClaude, applyCodex, applyGemini, applyGeminiDocument } = require('./parsers');

const HEAD_BYTES = 128 * 1024;
const TAIL_BYTES = 768 * 1024;

const home = os.homedir();
const CLAUDE_DIR = path.join(process.env.CLAUDE_CONFIG_DIR || path.join(home, '.claude'), 'projects');
const CODEX_HOME = process.env.CODEX_HOME || path.join(home, '.codex');
const CODEX_DIR = path.join(CODEX_HOME, 'sessions');
const CODEX_INDEX = path.join(CODEX_HOME, 'session_index.jsonl');
// Gemini CLI: ~/.gemini (or $GEMINI_CLI_HOME/.gemini); sandboxed runs on
// macOS keep their state under ~/.cache/.gemini instead.
const GEMINI_HOMES = [
  path.join(process.env.GEMINI_CLI_HOME || home, '.gemini'),
  path.join(home, '.cache', '.gemini'),
];
const GEMINI_DIR = path.join(GEMINI_HOMES[0], 'tmp');

function ls(dir) {
  try { return fs.readdirSync(dir, { withFileTypes: true }); } catch { return []; }
}

function statSafe(f) {
  try { return fs.statSync(f); } catch { return null; }
}

function readRange(file, start, end) {
  const len = Math.max(0, end - start);
  if (!len) return '';
  const fd = fs.openSync(file, 'r');
  try {
    const buf = Buffer.alloc(len);
    fs.readSync(fd, buf, 0, len, start);
    return buf.toString('utf8');
  } finally {
    fs.closeSync(fd);
  }
}

function claudeFiles() {
  const out = [];
  for (const proj of ls(CLAUDE_DIR)) {
    if (!proj.isDirectory()) continue;
    const pdir = path.join(CLAUDE_DIR, proj.name);
    for (const e of ls(pdir)) {
      if (e.isFile() && e.name.endsWith('.jsonl')) {
        out.push({ agent: 'claude', file: path.join(pdir, e.name), id: e.name.slice(0, -6) });
      } else if (e.isDirectory()) {
        const sub = path.join(pdir, e.name, 'subagents');
        for (const s of ls(sub)) {
          if (s.isFile() && s.name.endsWith('.jsonl')) {
            out.push({ agent: 'claude', file: path.join(sub, s.name), id: s.name.slice(0, -6), parentId: e.name });
          }
        }
      }
    }
  }
  return out;
}

function geminiFiles() {
  // <root>/tmp/<project>/chats/session-*.jsonl (+ legacy .json), and
  // subagents in <root>/tmp/<project>/chats/<parentSessionId>/<id>.jsonl
  const out = [];
  for (const root of GEMINI_HOMES) {
    const tmp = path.join(root, 'tmp');
    for (const proj of ls(tmp)) {
      if (!proj.isDirectory()) continue;
      const chats = path.join(tmp, proj.name, 'chats');
      for (const e of ls(chats)) {
        if (e.isFile() && /^session-.*\.jsonl?$/.test(e.name)) {
          out.push({ agent: 'gemini', file: path.join(chats, e.name), id: e.name.replace(/\.jsonl?$/, ''), project: proj.name, root });
        } else if (e.isDirectory()) {
          for (const f of ls(path.join(chats, e.name))) {
            if (f.isFile() && /\.jsonl$/.test(f.name)) {
              out.push({ agent: 'gemini', file: path.join(chats, e.name, f.name), id: f.name.slice(0, -6), project: proj.name, root, parentId: e.name });
            }
          }
        }
      }
    }
  }
  return out;
}

function codexFiles(sinceMs) {
  // sessions/YYYY/MM/DD/rollout-*.jsonl — only walk days inside the window.
  const out = [];
  const cutoff = new Date(sinceMs - 86400000);
  const key = (y, m, d) => `${y}-${m}-${d}`;
  const minKey = key(cutoff.getFullYear(), String(cutoff.getMonth() + 1).padStart(2, '0'), String(cutoff.getDate()).padStart(2, '0'));
  for (const y of ls(CODEX_DIR)) {
    if (!y.isDirectory()) continue;
    for (const m of ls(path.join(CODEX_DIR, y.name))) {
      if (!m.isDirectory()) continue;
      for (const d of ls(path.join(CODEX_DIR, y.name, m.name))) {
        if (!d.isDirectory() || key(y.name, m.name, d.name) < minKey) continue;
        const dir = path.join(CODEX_DIR, y.name, m.name, d.name);
        for (const f of ls(dir)) {
          if (!f.isFile() || !f.name.endsWith('.jsonl')) continue;
          const id = (f.name.match(/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$/) || [])[1] || f.name;
          out.push({ agent: 'codex', file: path.join(dir, f.name), id });
        }
      }
    }
  }
  return out;
}

class Watcher extends EventEmitter {
  constructor({ hours = 24, interval = 1000 } = {}) {
    super();
    this.windowMs = hours * 3600 * 1000;
    this.interval = interval;
    this.trackers = new Map(); // file -> { session, offset, partial, apply }
    this.failed = new Map();   // file -> retry-after time, for logs we couldn't read
    this.codexNames = new Map();
    this.geminiProjects = new Map(); // tmp dir name -> project path
    this.geminiRegistryMtime = 0;
    this.codexIndexMtime = 0;
    this.lastScan = 0;
    this.candidates = [];
  }

  start() {
    this.tick();
    this.timer = setInterval(() => this.tick(), this.interval);
    return this;
  }

  stop() { clearInterval(this.timer); }

  loadCodexIndex() {
    const st = statSafe(CODEX_INDEX);
    if (!st || st.mtimeMs === this.codexIndexMtime) return false;
    this.codexIndexMtime = st.mtimeMs;
    const start = Math.max(0, st.size - 2 * 1024 * 1024);
    for (const line of readRange(CODEX_INDEX, start, st.size).split('\n')) {
      try {
        const d = JSON.parse(line);
        if (d.id && d.thread_name) this.codexNames.set(d.id, d.thread_name);
      } catch {}
    }
    return true;
  }

  // ~/.gemini/projects.json maps project paths to the short slugs used as
  // tmp dir names; older versions used sha256(path) instead.
  loadGeminiProjects() {
    let mt = 0;
    const files = GEMINI_HOMES.map((r) => path.join(r, 'projects.json'));
    for (const f of files) mt += (statSafe(f) || {}).mtimeMs || 0;
    if (mt === this.geminiRegistryMtime) return false;
    this.geminiRegistryMtime = mt;
    for (const f of files) {
      try {
        const reg = JSON.parse(fs.readFileSync(f, 'utf8'));
        for (const [dir, slug] of Object.entries(reg.projects || {})) {
          this.geminiProjects.set(slug, dir);
          this.geminiProjects.set(crypto.createHash('sha256').update(dir).digest('hex'), dir);
        }
      } catch {}
    }
    return true;
  }

  tick() {
    const now = Date.now();
    const since = now - this.windowMs;
    let changed = this.loadCodexIndex();
    if (this.loadGeminiProjects()) changed = true;

    // Directory walk every 5s; stat known files every tick.
    if (now - this.lastScan > 5000) {
      this.lastScan = now;
      this.candidates = [...claudeFiles(), ...codexFiles(since), ...geminiFiles()];
    }

    for (const c of this.candidates) {
      // A log we can't read (permissions, deleted mid-read…) is skipped and
      // retried a minute later; it must never take the server down.
      if ((this.failed.get(c.file) || 0) > now) continue;
      try {
        if (this.track(c, since)) changed = true;
      } catch {
        this.failed.set(c.file, now + 60000);
        this.trackers.delete(c.file);
      }
    }

    for (const [file, t] of this.trackers) {
      if ((t.session.lastEventAt || t.session.mtime || 0) < since) { this.trackers.delete(file); changed = true; }
    }

    if (changed) this.emit('change');
  }

  // Returns true when this file's session changed.
  track(c, since) {
    const st = statSafe(c.file);
    if (!st) return false;
    const t = this.trackers.get(c.file);
    if (!t) {
      if (st.mtimeMs < since) return false;
      this.open(c, st);
      return true;
    }
    let changed = false;
    if (t.whole && st.mtimeMs !== t.mtime) {
      // legacy single-document session: re-read it whole
      this.trackers.delete(c.file);
      this.open(c, st);
      return true;
    } else if (st.size > t.offset) {
      this.feed(t, readRange(c.file, t.offset, st.size), false);
      t.offset = st.size;
      changed = true;
    } else if (st.size < t.offset) {
      // File was rewritten; start over.
      this.trackers.delete(c.file);
    }
    t.session.mtime = st.mtimeMs;
    return changed;
  }

  open(c, st) {
    const session = newSession(c.agent, c.id, c.file);
    if (c.parentId) { session.parentId = c.parentId; session.subagent = true; }
    session.mtime = st.mtimeMs;
    if (c.agent === 'gemini') session.geminiProject = c.project;
    const apply = { claude: applyClaude, codex: applyCodex, gemini: applyGemini }[c.agent];
    const t = { session, offset: st.size, partial: '', apply, mtime: st.mtimeMs };
    if (c.agent === 'gemini' && c.file.endsWith('.json')) {
      t.whole = true;
      try { applyGeminiDocument(session, JSON.parse(readRange(c.file, 0, st.size))); } catch {}
      this.trackers.set(c.file, t);
      return t;
    }
    if (st.size <= HEAD_BYTES + TAIL_BYTES) {
      this.feed(t, readRange(c.file, 0, st.size), false);
    } else {
      // Big log: head for metadata (cwd, title, model), tail for current state.
      this.feed(t, readRange(c.file, 0, HEAD_BYTES), true);
      t.partial = '';
      this.feed(t, readRange(c.file, st.size - TAIL_BYTES, st.size), false, true);
    }
    this.trackers.set(c.file, t);
    return t;
  }

  feed(t, chunk, dropLastPartial, dropFirst = false) {
    const lines = (t.partial + chunk).split('\n');
    t.partial = lines.pop();
    if (dropFirst) lines.shift();
    if (dropLastPartial) t.partial = '';
    for (const line of lines) {
      if (!line) continue;
      let d;
      try { d = JSON.parse(line); } catch { continue; }
      try { t.apply(t.session, d); } catch {}
    }
  }

  sessions() {
    const out = [];
    for (const t of this.trackers.values()) {
      const s = t.session;
      if (s.agent === 'gemini' && !s.cwd && this.geminiProjects.has(s.geminiProject)) {
        s.cwd = this.geminiProjects.get(s.geminiProject);
      }
      if (s.agent === 'gemini' && !s.cwd) s.cwd = s.geminiProject;
      // Gemini re-appends whole messages with their original timestamps, so
      // the file's own write time is the best "last activity" signal.
      if (s.agent === 'gemini') s.lastEventAt = Math.max(s.lastEventAt || 0, s.mtime || 0);
      if (s.agent === 'codex' && !s.title) {
        const name = this.codexNames.get(s.id);
        if (name) s.title = name;
      }
      out.push(s);
    }
    return out;
  }
}

module.exports = { Watcher, CLAUDE_DIR, CODEX_DIR, GEMINI_DIR };
