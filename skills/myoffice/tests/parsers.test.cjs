// Parser and snapshot checks with hand-written log lines (no private logs).
// Run: npm test
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { newSession, applyClaude, applyCodex, applyGemini, clean } = require('../src/parsers');
const { snapshot } = require('../src/server');

const now = () => new Date().toISOString();

function claudeUser(text, extra = {}) {
  return { type: 'user', timestamp: now(), cwd: '/home/dev/proj', message: { role: 'user', content: [{ type: 'text', text }] }, ...extra };
}

test('clean drops IDE context blocks and keeps the user text', () => {
  const text = '<ide_opened_file>The user opened the file /x/README.md in the IDE. This may or may not be related to the current task.</ide_opened_file>\n<ide_selection>const a = 1;</ide_selection>\n請評估這段程式';
  assert.equal(clean(text), '請評估這段程式');
});

test('clean keeps the text of short inline tags as before', () => {
  assert.equal(clean('see <b>this</b> now'), 'see this now');
});

test('claude platform comes from entrypoint', () => {
  const cases = [['claude-vscode', 'vscode'], ['cli', 'cli'], ['sdk-cli', 'sdk']];
  for (const [entrypoint, platform] of cases) {
    const s = newSession('claude', 'a', '/a.jsonl');
    applyClaude(s, claudeUser('hi', { entrypoint }));
    assert.equal(s.platform, platform, entrypoint);
  }
  const s = newSession('claude', 'a', '/a.jsonl');
  applyClaude(s, claudeUser('hi'));
  assert.equal(s.platform, null);
});

test('codex platform comes from session_meta originator', () => {
  const cases = [
    [{ originator: 'codex_vscode', source: 'vscode' }, 'vscode'],
    [{ originator: 'codex_exec', source: 'exec' }, 'sdk'],
    [{ originator: 'codex_cli_rs', source: 'cli' }, 'cli'],
    [{ originator: 'Codex Desktop', source: 'vscode' }, 'desktop'],
    [{ originator: 'codex_work_desktop', source: { subagent: { other: 'guardian' } } }, 'desktop'],
  ];
  for (const [payload, platform] of cases) {
    const s = newSession('codex', 'b', '/b.jsonl');
    applyCodex(s, { type: 'session_meta', timestamp: now(), payload: { cwd: '/p', ...payload } });
    assert.equal(s.platform, platform, payload.originator);
  }
});

test('gemini sessions are always on the cli', () => {
  const s = newSession('gemini', 'c', '/c.jsonl');
  applyGemini(s, { sessionId: 'c', projectHash: 'h', startTime: now(), lastUpdated: now(), kind: 'main' });
  assert.equal(s.platform, 'cli');
});

test('claude prompt kind: slash command, background notification, plain text', () => {
  const s = newSession('claude', 'd', '/d.jsonl');
  applyClaude(s, claudeUser('<command-message>myoffice</command-message>\n<command-name>/myoffice</command-name>'));
  assert.equal(s.prompt, '/myoffice');
  assert.equal(s.promptKind, 'command');
  applyClaude(s, claudeUser('<task-notification>\n<task-id>1</task-id>\n<status>completed</status>\n</task-notification>'));
  assert.equal(s.prompt, '/myoffice');
  assert.equal(s.promptKind, 'notification');
  applyClaude(s, claudeUser('修一下這個 bug'));
  assert.equal(s.prompt, '修一下這個 bug');
  assert.equal(s.promptKind, null);
});

test('gemini prompt kind: slash command', () => {
  const s = newSession('gemini', 'e', '/e.jsonl');
  applyGemini(s, { type: 'user', timestamp: now(), content: [{ text: '/chat list' }] });
  assert.equal(s.prompt, '/chat');
  assert.equal(s.promptKind, 'command');
  applyGemini(s, { type: 'user', timestamp: now(), content: [{ text: 'hello' }] });
  assert.equal(s.promptKind, null);
});

test('snapshot exposes platform and promptKind', () => {
  const s = newSession('claude', 'f', '/f.jsonl');
  applyClaude(s, claudeUser('<command-name>/myoffice</command-name>', { entrypoint: 'claude-vscode' }));
  const snap = snapshot({ sessions: () => [s] }, { privacy: false, demo: true });
  assert.equal(snap.sessions[0].platform, 'vscode');
  assert.equal(snap.sessions[0].promptKind, 'command');
});
