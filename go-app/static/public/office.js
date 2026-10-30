// Isometric office: agents that are working sit at cubicle desks, those
// waiting on you queue at the hotline desk and those with a warning argue in
// the meeting room. Finished ones take the lounge seats first and watch TV once those are full,
// idle ones wander the pantry and lounge. Plain canvas 2D with local provider logos, no dependencies.
(() => {
  'use strict';

  const TW = 64; // tile width px
  const TH = 32; // tile height px; one z unit is TH px too
  const WALL_H = 2.4;
  const WORKING = new Set(['thinking', 'tool', 'typing', 'waiting', 'error']);
  const MAX_WANDERERS = 7;
  const SPEED = 2.1; // tiles / second

  const SKIN = ['#f3cfb0', '#e5b38f', '#c98d66', '#9a6646', '#6e4630', '#f7dcc7'];
  const HAIR = ['#2a2019', '#4b3020', '#8a4b2a', '#c99a5b', '#1c1c1c', '#8c8c8c', '#a33a2c', '#3d3550'];
  const SHIRT = {
    claude: ['#c96442', '#d9825f', '#b4553a', '#e09a72'],
    codex: ['#10a37f', '#2bb38f', '#0d8a6b', '#4cc3a0'],
    gemini: ['#4f7cf0', '#6b8ff5', '#3d66d6', '#8aa6f8'],
    opencode: ['#4a4745', '#5a5858', '#3a3736', '#6e6b69'],
  };
  // Official product icons, shared by both renderers and the page UI.
  // Load once per page from the same origin so video capture stays readable.
  // Initials remain a fallback if an image cannot be loaded.
  const AGENT_BADGES = {
    claude: { label: 'Claude', short: 'Cl', color: '#c96442', logo: '/assets/agents/claude.png' },
    codex: { label: 'Codex', short: 'Cx', color: '#10a37f', logo: '/assets/agents/codex.png' },
    gemini: { label: 'Gemini', short: 'G', color: '#4f7cf0', logo: '/assets/agents/gemini.png' },
    opencode: { label: 'OpenCode', short: 'Oc', color: '#5a5858', logo: '/assets/agents/opencode.png' },
  };
  for (const badge of Object.values(AGENT_BADGES)) {
    badge.image = new Image();
    badge.image.src = badge.logo;
  }
  const PANTS = ['#34405a', '#2c2c34', '#4a3f35', '#3b4a3f'];
  // Hair colour says where the session runs (shirt stays the agent's brand);
  // sessions without a platform keep a random hair colour.
  const PLATFORM_HAIR = {
    vscode: ['#c99a5b', '#e0b56a'], cli: ['#2a2019', '#1c1c1c'], sdk: ['#a33a2c', '#c4553f'], desktop: ['#3d3550', '#5b4e7d'],
  };
  const PANEL = ['#7d93c4', '#d98b6a', '#7fb08a', '#c9a24c'];

  const STATUS_COLOR = {
    thinking: '#8b6be6', tool: '#3b8ee0', typing: '#22a874', waiting: '#e09412',
    done: '#5f9a4c', error: '#de4b40', interrupted: '#9b9a94', idle: '#9b9a94',
  };

  // Warnings beside the status (server: config.go): the context is close to
  // where the CLI compacts on its own, or this turn has been working for long.
  // Drawn as monospace chips with a magenta ring, apart from the status bubble.
  const ALERT_RING = '#c2418f';
  const ALERT_LEGEND = { compact: 'ctx %', longTurn: 'min' };
  function alertText(k, s, now = Date.now()) {
    if (!s) return ALERT_LEGEND[k];
    if (k === 'compact') return s.compactAt ? `ctx ${Math.round((100 * (s.contextTokens || 0)) / s.compactAt)}%` : 'ctx';
    if (k === 'longTurn') {
      const m = s.turnStartedAt ? Math.max(0, Math.floor((now - s.turnStartedAt) / 60000)) : 0;
      return m > 99 ? `${Math.floor(m / 60)}h${String(m % 60).padStart(2, '0')}m` : `${m}m`;
    }
    return '';
  }

  const ACT_EMOJI = {
    coffee: '☕', water: '💧', fridge: '🍎', sink: '🫧', stool: '💬', couch: '💬', beanbag: '🎧',
    read: '📖', arcade: '🎮', scroll: '📱', tv: '📺',
  };

  // The two whiteboards. Each edition is drawn from a seed, so the boss can
  // erase one and write the next; a BoardSurface keeps the current picture in
  // a canvas that the 2D wall and the 3D texture both use.
  function seeded(n) {
    let a = (n * 2654435761) >>> 0 || 1;
    return () => {
      a = (a + 0x6d2b79f5) | 0;
      let t = Math.imul(a ^ (a >>> 15), 1 | a);
      t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }
  const marker = (c, col, px, w = 700) => {
    c.fillStyle = col;
    c.font = `${w} ${px}px "Marker Felt", "Comic Sans MS", ui-sans-serif, system-ui, sans-serif`;
  };
  const NOTES = ['SHIP IT', 'TESTS ✓', 'LGTM', 'BUG BASH', 'NO MTGS', 'DEPLOY?', 'v2 🚀', 'COFFEE ☕', 'PR #', 'OKR %'];
  const TOPICS = ['Q4 roadmap', 'Fix flaky tests', 'Cut token costs', 'Ship v2?', 'Hire 2 devs', 'Pizza Friday',
    'Refactor auth', 'On-call rota', 'Kill legacy API', 'Docs sprint', 'Faster CI', 'Pricing page'];

  // The sales board, H = 384 px tall and as wide as the wall board: a title,
  // then as many panels as fit (KPIs, bars, trend, leaderboard, pie), sticky notes last.
  function drawSalesBoard(c, W, H, ed) {
    const r = seeded(ed);
    const int = (a, b) => Math.round(a + r() * (b - a));
    c.textBaseline = 'alphabetic';
    c.textAlign = 'left';
    marker(c, '#1f4fa8', 42, 800); c.fillText('Q4 SALES  ★  KPI', 28, 56);
    marker(c, '#9a9a96', 26, 700); c.textAlign = 'right'; c.fillText(`WEEK ${1 + (ed % 52)}`, W - 28, 52); c.textAlign = 'left';
    c.strokeStyle = '#1f4fa8'; c.lineWidth = 4;
    c.beginPath(); c.moveTo(28, 68); c.lineTo(Math.min(420, W - 28), 68); c.stroke();
    const panels = {
      kpi(x, w) {
        const up = int(-8, 140), churn = (r() * 6).toFixed(1);
        const rows = [['Revenue', `${up >= 0 ? '▲' : '▼'} ${Math.abs(up)}%`, up >= 0 ? '#2e8b57' : '#c0392b'], ['Deals won', String(int(120, 480)), '#1c1c1a'],
          ['MRR', `$${(20 + r() * 60).toFixed(1)}k`, '#1c1c1a'], ['Churn', `▼ ${churn}%`, '#c0392b'], ['Tokens', `${(2 + r() * 15).toFixed(1)}M`, '#6a4bc4']];
        rows.forEach(([k, v, col], i) => {
          marker(c, '#3a3a38', 26, 600); c.fillText(k, x, 112 + i * 48);
          marker(c, col, 30, 800); c.fillText(v, x + Math.min(178, w * 0.55), 112 + i * 48);
        });
      },
      bars(x, w) {
        const bars = Array.from({ length: 6 }, () => int(30, 120));
        const top = bars.indexOf(Math.max(...bars)), by = 330, bw = (w - 30) / 6;
        c.strokeStyle = '#3a3a38'; c.lineWidth = 3;
        c.beginPath(); c.moveTo(x, 90); c.lineTo(x, by); c.lineTo(x + w - 10, by); c.stroke();
        bars.forEach((v, i) => {
          c.fillStyle = i === top ? '#c96442' : '#3b8ee0';
          c.fillRect(x + 12 + i * bw, by - v * 1.9, bw * 0.65, v * 1.9);
          marker(c, '#3a3a38', 18, 600); c.fillText('JASOND'[i], x + 14 + i * bw, by + 24);
        });
        const tx = x + 12 + top * bw + bw * 0.32, ty = by - bars[top] * 1.9 - 4;
        c.strokeStyle = '#c0392b'; c.lineWidth = 4;
        c.beginPath(); c.ellipse(tx, ty, 30, 22, 0, 0, Math.PI * 2); c.stroke();
        marker(c, '#c0392b', 24, 800); c.textAlign = 'center'; c.fillText('record!', tx, Math.max(100, ty - 30)); c.textAlign = 'left';
      },
      trend(x, w) {
        const ly = 270, n = 8, sx = (w - 30) / (n - 1);
        const pts = []; let v = int(0, 20);
        for (let i = 0; i < n; i++) { pts.push(v); v = Math.max(0, Math.min(150, v + int(-18, 34))); }
        c.setLineDash([10, 8]); c.strokeStyle = '#9a9a96'; c.lineWidth = 3;
        c.beginPath(); c.moveTo(x, ly - 120); c.lineTo(x + w - 20, ly - 120); c.stroke(); c.setLineDash([]);
        marker(c, '#9a9a96', 18, 600); c.fillText('target', x + w - 80, ly - 128);
        c.strokeStyle = '#2e8b57'; c.lineWidth = 5;
        c.beginPath();
        pts.forEach((p, i) => (i ? c.lineTo(x + i * sx, ly - p) : c.moveTo(x, ly - p)));
        c.stroke();
        const qoq = int(-12, 60);
        marker(c, qoq >= 0 ? '#2e8b57' : '#c0392b', 26, 800); c.fillText(`${qoq >= 0 ? '↗ +' : '↘ '}${qoq}% QoQ`, x, ly + 52);
      },
      leader(x, w) {
        const names = ['Claude', 'Codex', 'Gemini', 'OpenCode'].map((n) => [n, int(8, 60)]).sort((a, b) => b[1] - a[1]);
        marker(c, '#1c1c1a', 26, 800); c.fillText('TOP AGENTS', x, 112);
        names.forEach(([n, v], i) => {
          marker(c, i ? '#3a3a38' : '#c96442', 26, i ? 600 : 800);
          c.fillText(`${i + 1}. ${n}`, x, 162 + i * 48);
          c.textAlign = 'right'; c.fillText(`${v}${i ? '' : ' ★'}`, x + w - 24, 162 + i * 48); c.textAlign = 'left';
        });
      },
      pie(x, w) {
        const cx = x + w / 2 - 10, cy = 220, rad = Math.min(95, w / 2 - 30);
        const parts = [r() + 0.4, r() + 0.3, r() + 0.2, r() + 0.1], sum = parts.reduce((a, b) => a + b);
        let a0 = -Math.PI / 2;
        parts.forEach((p, i) => {
          const a1 = a0 + (p / sum) * Math.PI * 2;
          c.fillStyle = ['#c96442', '#3b8ee0', '#5f9a4c', '#e2b03b'][i];
          c.beginPath(); c.moveTo(cx, cy); c.arc(cx, cy, rad, a0, a1); c.closePath(); c.fill();
          a0 = a1;
        });
        marker(c, '#3a3a38', 22, 700); c.textAlign = 'center'; c.fillText('token mix', cx, cy + rad + 34); c.textAlign = 'left';
      },
      notes(x, w) {
        const pick = NOTES.slice().sort(() => r() - 0.5).slice(0, 4);
        const cols = ['#ffe27a', '#a8e6a1', '#ffb4c8', '#9fd3ff'];
        pick.forEach((t, i) => {
          const nx = x + 60 + (i % 2) * Math.min(120, w - 120), ny = 130 + Math.floor(i / 2) * 150;
          const text = t.includes('#') ? `PR ${int(40, 400)}` : t.includes('%') ? `OKR ${int(50, 99)}%` : t;
          c.save(); c.translate(nx, ny); c.rotate((r() - 0.5) * 0.14);
          c.fillStyle = cols[i]; c.fillRect(-56, -40, 112, 80);
          marker(c, '#1c1c1a', 22, 800); c.textAlign = 'center'; c.fillText(text, 0, 8);
          c.restore();
        });
      },
    };
    const order = ['kpi', 'bars', 'trend', 'leader', 'pie'];
    const n = Math.max(3, Math.min(6, Math.round((W - 40) / 330)));
    const use = [...order.slice(0, n - 1), 'notes'];
    const pw = (W - 40) / use.length;
    use.forEach((k, i) => panels[k](28 + i * pw, pw));
  }

  // The meeting room board, H = 256 px: a title and one of four pages
  // (agenda, flow chart, burndown, vote), turned every few seconds.
  const MEET_PAGES = 4, MEET_PAGE_MS = 6000;
  const BOSS_EVERY = 60_000, BOSS_ERASE = 3500, BOSS_WRITE = 6000;
  const FRIDGE_PUFF_EVERY = 24, FRIDGE_PUFF_FOR = 4, FRIDGE_PUFFS = 10; // seconds, seconds, puffs
  function drawMeetingBoard(c, W, H, ed, page) {
    const r = seeded(ed * 7 + 3);
    const topics = TOPICS.slice().sort(() => r() - 0.5).slice(0, 4);
    c.textBaseline = 'alphabetic';
    c.textAlign = 'left';
    marker(c, '#1f4fa8', 28, 800); c.fillText(`SPRINT ${10 + (ed % 90)} · ${topics[0]}`, 18, 36);
    c.strokeStyle = '#1f4fa8'; c.lineWidth = 3;
    c.beginPath(); c.moveTo(18, 46); c.lineTo(W * 0.7, 46); c.stroke();
    if (page === 0) {
      topics.forEach((t, i) => {
        const done = r() < 0.4;
        c.strokeStyle = '#3a3a38'; c.lineWidth = 3; c.strokeRect(22, 66 + i * 44, 22, 22);
        if (done) { c.strokeStyle = '#2e8b57'; c.beginPath(); c.moveTo(25, 78 + i * 44); c.lineTo(32, 86 + i * 44); c.lineTo(44, 64 + i * 44); c.stroke(); }
        marker(c, '#1c1c1a', 24, 700); c.fillText(t, 58, 86 + i * 44);
      });
    } else if (page === 1) {
      const boxes = topics.slice(0, 3).map((t, i) => ({ t, x: 24 + i * (W - 48) / 3, y: 90 + (i % 2) * 70 }));
      c.lineWidth = 3;
      boxes.forEach((b, i) => {
        const bw = (W - 48) / 3 - 30;
        c.strokeStyle = '#c96442'; c.strokeRect(b.x, b.y, bw, 54);
        marker(c, '#1c1c1a', 18, 700); c.fillText(b.t, b.x + 8, b.y + 34, bw - 14);
        if (i) { c.strokeStyle = '#3a3a38'; c.beginPath(); c.moveTo(boxes[i - 1].x + bw, boxes[i - 1].y + 27); c.lineTo(b.x, b.y + 27); c.stroke(); }
      });
      marker(c, '#c0392b', 22, 800); c.fillText('?!', W - 50, 230);
    } else if (page === 2) {
      const n = 8, sx = (W - 80) / (n - 1);
      c.strokeStyle = '#3a3a38'; c.lineWidth = 3;
      c.beginPath(); c.moveTo(30, 66); c.lineTo(30, 226); c.lineTo(W - 30, 226); c.stroke();
      c.setLineDash([8, 7]); c.strokeStyle = '#9a9a96';
      c.beginPath(); c.moveTo(30, 76); c.lineTo(W - 40, 222); c.stroke(); c.setLineDash([]);
      c.strokeStyle = '#5f9a4c'; c.lineWidth = 4;
      c.beginPath();
      let v = 150;
      for (let i = 0; i < n; i++) { const y = 226 - v; i ? c.lineTo(30 + i * sx, y) : c.moveTo(30, y); v = Math.max(5, v - r() * 34); }
      c.stroke();
      marker(c, '#3a3a38', 20, 700); c.fillText('burndown', W - 140, 70);
    } else {
      topics.forEach((t, i) => {
        const votes = Math.round(r() * 7);
        marker(c, '#1c1c1a', 22, 700); c.fillText(t, 22, 86 + i * 44);
        c.strokeStyle = '#c96442'; c.lineWidth = 3;
        for (let k = 0; k < votes; k++) {
          const vx = W * 0.55 + k * 14 + Math.floor(k / 5) * 10;
          c.beginPath();
          if (k % 5 === 4) { c.moveTo(vx - 66, 70 + i * 44); c.lineTo(vx + 2, 86 + i * 44); } else { c.moveTo(vx, 66 + i * 44); c.lineTo(vx, 90 + i * 44); }
          c.stroke();
        }
      });
    }
  }

  class BoardSurface {
    constructor(draw, ed) {
      this.draw = draw;
      this.canvas = document.createElement('canvas');
      this.ed = ed;      // the edition on the board
      this.old = ed;     // the one being erased
      this.prog = null;  // null, or 0–1 erasing the old edition right to left, 1–2 writing the new one left to right
      this.page = 0;
      this.key = '';
      this.version = 0;  // bumps on every redraw
    }

    render(W, H) {
      const prog = this.prog == null ? '' : Math.round(this.prog * 60);
      const key = `${W}x${H}:${this.ed}:${this.old}:${prog}:${this.page}`;
      if (key === this.key) return;
      this.key = key;
      const cv = this.canvas;
      if (cv.width !== W || cv.height !== H) { cv.width = W; cv.height = H; }
      const c = cv.getContext('2d');
      c.fillStyle = '#fbfbf8'; c.fillRect(0, 0, W, H);
      const part = (ed, x1) => {
        c.save();
        c.beginPath(); c.rect(0, 0, x1, H); c.clip();
        this.draw(c, W, H, ed, this.page);
        c.restore();
      };
      if (this.prog == null) part(this.ed, W);
      else if (this.prog < 1) {
        const x = W * (1 - this.prog);
        part(this.old, x);
        // eraser smudge right behind the eraser
        c.fillStyle = 'rgba(150,150,150,.12)';
        c.fillRect(x, H * 0.08, Math.min(70, W - x), H * 0.84);
      } else part(this.ed, W * (this.prog - 1));
      this.version++;
    }
  }

  function hash(str) {
    let h = 2166136261;
    for (let i = 0; i < str.length; i++) { h ^= str.charCodeAt(i); h = Math.imul(h, 16777619); }
    return h >>> 0;
  }
  const pick = (arr, h, salt) => arr[(Math.imul(h ^ Math.imul(salt, 2654435761), 2246822519) >>> 0) % arr.length];
  const rand = (a, b) => a + Math.random() * (b - a);
  const clamp = (v, a, b) => Math.max(a, Math.min(b, v));

  function palette() {
    const dark = matchMedia('(prefers-color-scheme: dark)').matches;
    return dark ? {
      dark: true,
      wood1: '#4a3f35', wood2: '#453a31', woodLine: 'rgba(0,0,0,.22)',
      tileA: '#3d4044', tileB: '#35383c', carpet: '#33404d', carpetLine: 'rgba(255,255,255,.04)',
      wallL: '#4a4640', wallR: '#3f3b36', wallTop: '#5a554e', trim: '#2b2926',
      deskTop: '#8a6a4c', deskL: '#6e533b', deskR: '#5c4431',
      monitor: '#18181a', screenOff: '#26282c', chair: '#2d2d32',
      rug: '#47544f', rugEdge: '#3a4541', couch: '#5b6b8c', couchD: '#4a587a',
      counter: '#d9d4c9', counterL: '#a9a397', counterR: '#958f84', metal: '#8d939a',
      glass: 'rgba(160,200,230,.10)', glassEdge: 'rgba(190,220,240,.35)', mat: '#2f4a3a',
      text: '#ecebe6', tagBg: 'rgba(20,20,19,.78)', bubble: '#f4f3ee', bubbleInk: '#1c1c1a',
      plant: '#4f8a52', pot: '#9a5b3c', shadow: 'rgba(0,0,0,.28)', book: ['#c96442', '#3b8ee0', '#5f9a4c', '#c9a24c', '#8b6be6'],
    } : {
      dark: false,
      wood1: '#e6d3b6', wood2: '#dfcaab', woodLine: 'rgba(120,90,50,.12)',
      tileA: '#f4f4f1', tileB: '#e6e7e3', carpet: '#c9d3dc', carpetLine: 'rgba(0,0,0,.035)',
      wallL: '#f4efe6', wallR: '#e8e1d4', wallTop: '#fbf8f2', trim: '#cfc5b3',
      deskTop: '#c89f72', deskL: '#a8825a', deskR: '#946f4b',
      monitor: '#2a2a2e', screenOff: '#3a3d44', chair: '#4a4a52',
      rug: '#c4d4cb', rugEdge: '#a9bcb1', couch: '#7d93c4', couchD: '#6a80b0',
      counter: '#fbfaf7', counterL: '#d9d4c9', counterR: '#c9c3b6', metal: '#b5bcc4',
      glass: 'rgba(170,210,235,.16)', glassEdge: 'rgba(120,160,190,.55)', mat: '#bfd8c4',
      text: '#1c1c1a', tagBg: 'rgba(255,255,255,.9)', bubble: '#ffffff', bubbleInk: '#1c1c1a',
      plant: '#5a9b5d', pot: '#b86f4b', shadow: 'rgba(60,40,20,.18)', book: ['#c96442', '#3b8ee0', '#5f9a4c', '#c9a24c', '#8b6be6'],
    };
  }

  function skyColor(d = new Date()) {
    const h = d.getHours() + d.getMinutes() / 60;
    if (h >= 7 && h < 17) return { sky: '#a9d4f2', night: false };
    if (h >= 5 && h < 7) return { sky: '#f3c9a8', night: false };
    if (h >= 17 && h < 19) return { sky: '#f0a679', night: false };
    return { sky: '#1d2747', night: true };
  }

  // ---------------------------------------------------------------- A* grid

  class Grid {
    constructor(W, D, res = 4) {
      this.res = res;
      this.cw = Math.ceil(W * res);
      this.ch = Math.ceil(D * res);
      this.cost = new Uint8Array(this.cw * this.ch);
    }
    block(x, y, w, d, pad = 0.12) {
      const R = this.res;
      const i0 = clamp(Math.floor((x - pad) * R), 0, this.cw - 1);
      const i1 = clamp(Math.ceil((x + w + pad) * R) - 1, 0, this.cw - 1);
      const j0 = clamp(Math.floor((y - pad) * R), 0, this.ch - 1);
      const j1 = clamp(Math.ceil((y + d + pad) * R) - 1, 0, this.ch - 1);
      for (let j = j0; j <= j1; j++) for (let i = i0; i <= i1; i++) this.cost[j * this.cw + i] = 1;
    }
    blocked(x, y) {
      const i = Math.floor(x * this.res), j = Math.floor(y * this.res);
      if (i < 0 || j < 0 || i >= this.cw || j >= this.ch) return true;
      return !!this.cost[j * this.cw + i];
    }
    path(ax, ay, bx, by) {
      const R = this.res, cw = this.cw, ch = this.ch;
      const si = clamp(Math.floor(ax * R), 0, cw - 1), sj = clamp(Math.floor(ay * R), 0, ch - 1);
      const gi = clamp(Math.floor(bx * R), 0, cw - 1), gj = clamp(Math.floor(by * R), 0, ch - 1);
      const N = cw * ch;
      const g = new Float32Array(N).fill(Infinity);
      const came = new Int32Array(N).fill(-1);
      const closed = new Uint8Array(N);
      const heap = [];
      const push = (f, n) => {
        heap.push([f, n]);
        let k = heap.length - 1;
        while (k > 0) { const p = (k - 1) >> 1; if (heap[p][0] <= heap[k][0]) break; [heap[p], heap[k]] = [heap[k], heap[p]]; k = p; }
      };
      const pop = () => {
        const top = heap[0], last = heap.pop();
        if (heap.length) {
          heap[0] = last;
          let k = 0;
          for (;;) {
            const l = 2 * k + 1, r = l + 1;
            let m = k;
            if (l < heap.length && heap[l][0] < heap[m][0]) m = l;
            if (r < heap.length && heap[r][0] < heap[m][0]) m = r;
            if (m === k) break;
            [heap[m], heap[k]] = [heap[k], heap[m]]; k = m;
          }
        }
        return top;
      };
      const hfn = (i, j) => { const dx = Math.abs(i - gi), dy = Math.abs(j - gj); return Math.max(dx, dy) + 0.41 * Math.min(dx, dy); };
      // Blocked cells are expensive, not impossible, so a path always exists
      // (someone standing right against furniture can still step away).
      const stepCost = (n) => (this.cost[n] ? 25 : 1);
      const start = sj * cw + si, goal = gj * cw + gi;
      g[start] = 0;
      push(hfn(si, sj), start);
      const dirs = [[1, 0, 1], [-1, 0, 1], [0, 1, 1], [0, -1, 1], [1, 1, 1.41], [1, -1, 1.41], [-1, 1, 1.41], [-1, -1, 1.41]];
      while (heap.length) {
        const [, n] = pop();
        if (n === goal) break;
        if (closed[n]) continue;
        closed[n] = 1;
        const i = n % cw, j = (n / cw) | 0;
        for (const [di, dj, c] of dirs) {
          const ni = i + di, nj = j + dj;
          if (ni < 0 || nj < 0 || ni >= cw || nj >= ch) continue;
          if (di && dj && (this.cost[j * cw + ni] || this.cost[nj * cw + i])) continue;
          const nn = nj * cw + ni;
          const ng = g[n] + c * stepCost(nn);
          if (ng < g[nn]) { g[nn] = ng; came[nn] = n; push(ng + hfn(ni, nj), nn); }
        }
      }
      const cells = [];
      for (let n = goal; n !== -1 && n !== start; n = came[n]) cells.push(n);
      cells.reverse();
      const pts = [];
      let lastDir = null;
      for (let k = 0; k < cells.length; k++) {
        const n = cells[k];
        const dir = n - (k ? cells[k - 1] : start);
        const pt = { x: ((n % cw) + 0.5) / R, y: (((n / cw) | 0) + 0.5) / R };
        if (dir === lastDir && pts.length) pts[pts.length - 1] = pt;
        else pts.push(pt);
        lastDir = dir;
      }
      if (pts.length) pts[pts.length - 1] = { x: bx, y: by };
      else pts.push({ x: bx, y: by });
      return pts;
    }
  }

  // ---------------------------------------------------------------- office

  // The TV in the sofa corner: GIFs listed by /api/tv (the TV folder, or the
  // built-in public-domain clips) decoded frame by frame into one small
  // canvas that the 2D and 3D views both draw. With several clips, each plays
  // at least TV_MIN_PLAY and switches at the end of a loop, or after
  // TV_MAX_PLAY. Without ImageDecoder (Safari) the screen keeps its colour cycle.
  const TV_MIN_PLAY = 15_000, TV_MAX_PLAY = 30_000, TV_RELIST = 60_000;
  class TvPlayer {
    constructor() {
      this.canvas = document.createElement('canvas');
      this.canvas.width = 296; this.canvas.height = 160; // the screen's 1.85:1
      this.ctx = this.canvas.getContext('2d');
      this.ready = false; // a frame is on the canvas
      this.version = 0;   // bumps with every new frame
      this.clips = [];
      this.i = -1;
      this.gen = 0;
      this.dec = null;
      this.busy = false;
      if (typeof ImageDecoder === 'function') this.list();
    }

    async list() {
      try {
        const { clips } = await (await fetch('/api/tv', { cache: 'no-store' })).json();
        if (clips.join('\n') !== this.clips.join('\n')) {
          this.clips = clips;
          this.i = -1;
          this.next();
        }
      } catch {}
      setTimeout(() => this.list(), TV_RELIST);
    }

    // Load the clip after the current one; a clip that fails is skipped.
    async next(tries = 0) {
      const gen = ++this.gen;
      if (this.dec) { this.dec.close(); this.dec = null; }
      if (tries >= this.clips.length) { this.ready = false; return; }
      this.i = (this.i + 1) % this.clips.length;
      try {
        const r = await fetch(this.clips[this.i]);
        if (!r.ok) throw new Error(r.status);
        const dec = new ImageDecoder({ data: await r.arrayBuffer(), type: 'image/gif' });
        await dec.tracks.ready;
        await dec.completed;
        if (gen !== this.gen) { dec.close(); return; }
        if (!dec.tracks.selectedTrack) { dec.close(); throw new Error('no track'); }
        this.dec = dec;
        this.frame = 0;
        this.start = performance.now();
        this.due = 0;
      } catch {
        if (gen === this.gen) this.next(tries + 1);
      }
    }

    // Called by the renderers every frame: decode the next GIF frame when due.
    tick(now) {
      if (!this.dec || this.busy || now < this.due) return;
      const many = this.clips.length > 1;
      if (this.frame >= this.dec.tracks.selectedTrack.frameCount) {
        this.frame = 0;
        if (many && now - this.start >= TV_MIN_PLAY) return void this.next();
      }
      if (many && now - this.start >= TV_MAX_PLAY) return void this.next();
      const gen = this.gen;
      this.busy = true;
      this.dec.decode({ frameIndex: this.frame }).then(({ image }) => {
        if (gen === this.gen) {
          this.draw(image);
          this.frame++;
          // Browsers play GIF delays under 20 ms as 100 ms.
          const ms = (image.duration || 0) / 1000;
          this.due = performance.now() + (ms < 20 ? 100 : ms);
        }
        image.close();
      }).catch(() => {
        if (gen === this.gen) this.next();
      }).finally(() => { this.busy = false; });
    }

    // Fit the frame inside the screen, black bars around it.
    draw(img) {
      const c = this.ctx, W = this.canvas.width, H = this.canvas.height;
      const w = img.displayWidth, h = img.displayHeight, k = Math.min(W / w, H / h);
      c.fillStyle = '#000';
      c.fillRect(0, 0, W, H);
      c.drawImage(img, (W - w * k) / 2, (H - h * k) / 2, w * k, h * k);
      this.ready = true;
      this.version++;
    }
  }
  let tvPlayer = null;

  class Office {
    constructor(root, { onHover, onZone, label, sublabel, onFrame, showAgent = true, compact = false } = {}) {
      this.root = root;
      this.onHover = onHover || (() => {});
      this.onZone = onZone || (() => {});
      this.label = label || ((s) => s.project || s.agent);
      this.sublabel = sublabel || (() => '');
      this.onFrame = onFrame;
      this.showAgent = showAgent;
      this.compact = compact;
      this.offDutyLabel = 'went home';
      this.canvas = document.createElement('canvas');
      this.canvas.className = 'office-canvas';
      root.appendChild(this.canvas);
      this.ctx = this.canvas.getContext('2d');
      this.people = new Map();
      this.deskOf = new Map();
      this.queue = [];   // keys waiting on you, in line for the hotline desk
      this.meeting = []; // keys with a warning, in the meeting room
      this.offDuty = 0;
      this.first = true;
      this.visible = true;
      this.hit = [];
      this.hoverKey = null;
      this.t = performance.now();
      this.pal = palette();
      this.plan = this.buildPlan(0);
      this.cat = { x: this.plan.W - 3, y: this.plan.D - 2, path: [], until: 0, face: 1 };
      const ed = Math.floor(Date.now() / 60_000);
      this.boards = { sales: new BoardSurface(drawSalesBoard, ed), meet: new BoardSurface(drawMeetingBoard, ed) };
      this.renderBoards();
      const home = this.bossHome();
      // The boss: first round a few seconds after the page opens, then once a minute.
      this.boss = { x: home.x, y: home.y, path: [], walkFace: 'front', tasks: [], task: null, next: Date.now() + 5000 };
      this.theme = matchMedia('(prefers-color-scheme: dark)');
      this.onTheme = () => (this.pal = palette());
      this.theme.addEventListener('change', this.onTheme);

      this.ro = new ResizeObserver(() => this.resize());
      this.ro.observe(root);
      this.canvas.addEventListener('mousemove', (e) => this.pointer(e));
      this.canvas.addEventListener('click', (e) => this.pointer(e, true));
      this.canvas.addEventListener('mouseleave', () => { this.hoverKey = null; this.onHover(null); });
      this.resize();
      this.last = performance.now();
      // Resolves true once a few frames have been drawn with data in place,
      // false if the renderer gave up (3D load failure).
      this.ready = new Promise((ok) => (this.markReady = ok));
      this.framesShown = 0;
      const loop = (t) => { if (this.dead) return; this.frame(t); this.raf = requestAnimationFrame(loop); };
      this.raf = requestAnimationFrame(loop);
    }

    destroy() {
      this.dead = true;
      cancelAnimationFrame(this.raf);
      this.ro.disconnect();
      this.theme.removeEventListener('change', this.onTheme);
      this.canvas.remove();
    }

    // ------------------------------------------------------------ floor plan

    buildPlan(nWorking) {
      const pal = this.pal;
      const pods = Math.max(4, Math.ceil((nWorking + 2) / 4));
      const podCols = pods <= 4 ? 2 : 3;
      const podRows = Math.ceil(pods / podCols);
      const OW = 1.3 + (podCols - 1) * 4.2 + 5.1; // open office width
      const OD = podRows * 4.7 + 0.8;             // open office depth (pods)
      const D = Math.max(OD + 5.5, 14.5);
      const RW = 6.4;                              // right column width
      const W = OW + RW;
      const MR = 5.2;                              // meeting room depth
      const PY = 9.9;                              // pantry / lounge boundary
      const grid = new Grid(W, D);
      const items = [];
      const pois = [];
      const desks = [];
      const add = (depth, draw) => items.push({ depth, draw });
      // Semantic list of what is where, for renderers that don't use the 2D
      // draw closures (the 3D view).
      const props = [];
      const prop = (t, o) => props.push({ t, ...o });
      const solid = (x, y, w, d, pad) => grid.block(x, y, w, d, pad);
      // Spots people pick while off duty, tagged with the area they belong to
      // (lounge seats, then sofa: finished; pantry and lounge: idle).
      let zone = null;
      const poi = (x, y, act, face, opt = {}) => pois.push({ x, y, act, face, sit: 0, zone, ...opt });
      // Areas that explain themselves when clicked: a box on the floor plan.
      const zones = [];

      // Long boxes are split into ~1 tile pieces so depth sorting against
      // people stays right along their length. `extra(x0, x1)` draws things on
      // the front face of each piece (books on a shelf, etc).
      const longBox = (x, y, z, w, d, h, top, left, right, opts = {}) => {
        const n = Math.max(1, Math.ceil(Math.max(w, d) / 0.9));
        for (let k = 0; k < n; k++) {
          const sx = w >= d ? x + (w / n) * k : x, sy = w >= d ? y : y + (d / n) * k;
          const sw = w >= d ? w / n : w, sd = w >= d ? d : d / n;
          add(sx + sw / 2 + sy + sd / 2 + (opts.bias || 0), () => {
            this.box(sx, sy, z, sw, sd, h, top, left, right);
            if (opts.extra) opts.extra(sx, sx + sw);
          });
        }
      };

      // ---- cubicle pods: 4 desks back to back with a divider
      for (let p = 0; p < pods; p++) {
        const c = p % podCols, r = Math.floor(p / podCols);
        const px = 1.3 + c * 4.2, py = 2.0 + r * 4.7;
        const panel = PANEL[p % PANEL.length];
        for (let k = 0; k < 2; k++) {
          const x = px + k * 1.5;
          desks.push({ x, y: py, side: 'n', seat: { x: x + 0.7, y: py - 0.45 } });
          desks.push({ x, y: py + 0.92, side: 's', seat: { x: x + 0.7, y: py + 0.92 + 1.25 } });
          solid(x, py, 1.4, 0.8, 0.08);
          solid(x, py + 0.92, 1.4, 0.8, 0.08);
        }
        longBox(px - 0.1, py + 0.82, 0, 3.15, 0.08, 1.15, this.shade(panel, 25), panel, this.shade(panel, -15), { bias: 0.05 });
        prop('panel', { x: px - 0.1, y: py + 0.82, w: 3.15, d: 0.08, h: 1.15, color: panel });
        prop('panel', { x: px - 0.18, y: py - 0.05, w: 0.08, d: 1.82, h: 1.15, color: panel });
        add(px - 0.1 + py + 0.86, () => this.box(px - 0.18, py - 0.05, 0, 0.08, 1.82, 1.15, this.shade(panel, 25), panel, this.shade(panel, -15)));
        solid(px - 0.18, py + 0.8, 3.25, 0.12, 0.05);
      }

      // ---- open office extras
      const rx = OW - 2.35;
      add(rx + 0.35 + 0.5, () => this.serverRack(rx, 0.2));
      prop('rack', { x: rx, y: 0.2 });
      prop('printer', { x: OW - 3.5, y: 0.25 });
      solid(rx, 0.2, 0.7, 0.6);
      add(OW - 3.5 + 0.3 + 0.5, () => this.printer(OW - 3.5, 0.25));
      solid(OW - 3.5, 0.25, 0.75, 0.55);
      // big sales board on the left wall, across the two pods nearest it
      // (with one row of pods, down to just past it); the plant stands after it
      const board = { y: 1.45, z: 0.75, h: 1.5 };
      board.w = (podRows >= 2 ? 8.6 : OD + 0.5) - board.y;
      for (const [x, y] of [[0.45, 0.45], [0.45, board.y + board.w + 0.4]]) { add(x + y, () => this.plant(x, y)); prop('plant', { x, y }); solid(x - 0.2, y - 0.2, 0.4, 0.4); }
      const windows = [];
      for (let x = 1.0; x < OW - 3.8; x += 2.4) windows.push(x);
      prop('board', board);
      zones.push({ id: 'whiteboard', x: 0, y: board.y, z: board.z, w: 0.1, d: board.w, h: board.h });

      // ---- sofa corner (finished agents once the lounge seats are full): one
      // sofa facing the TV on the left wall, one to scroll a phone on
      zone = 'sofa';
      // A small office leaves this front strip deep: centre the corner in it.
      const fy = OD + Math.max(0, (D - OD - 5.5) / 2);
      const sy = fy + 0.5;
      const s1x = 1.6;
      add(s1x + sy, () => this.couch(s1x, sy));
      prop('couch', { x: s1x, y: sy, w: 2.8, d: 0.8 });
      solid(s1x, sy, 2.8, 0.8);
      for (let k = 0; k < 3; k++) poi(s1x + 0.5 + k * 0.9, sy + 0.6, 'scroll', 'front', { sit: 0.36 });
      // The TV sofa runs along y with its back to the right, facing the wall.
      const s2x = s1x + 2.95, s2y = sy + 0.95, s2len = 2.6;
      this.couchY(add, s2x, s2y, s2len);
      prop('couch', { x: s2x, y: s2y, w: 0.8, d: s2len, rot: -Math.PI / 2 });
      solid(s2x, s2y, 0.8, s2len);
      for (let k = 0; k < 3; k++) poi(s2x + 0.35, s2y + (k + 0.5) * s2len / 3, 'tv', 'back', { sit: 0.36, yaw: -Math.PI / 2 });
      const tv = { y: s2y + s2len / 2 - 0.8, w: 1.6 };
      add(0.25 + tv.y + tv.w / 2, () => this.box(0.05, tv.y + 0.1, 0, 0.45, tv.w - 0.2, 0.45, this.shade(pal.deskTop, -10), pal.deskL, pal.deskR));
      prop('tv', tv);
      solid(0, tv.y + 0.1, 0.5, tv.w - 0.2);
      zones.push({ id: 'tv', x: 0, y: tv.y, z: 0, w: 0.5, d: tv.w, h: 1.9 });
      const tx = s1x + 0.7, ty = tv.y + 0.45;
      add(tx + 0.8 + ty + 0.35, () => this.coffeeTable(tx, ty));
      prop('coffeeTable', { x: tx, y: ty, w: 1.6, d: 0.7 });
      solid(tx, ty, 1.6, 0.7);
      const sofaRug = { x: s1x - 0.4, y: sy - 0.2, w: s2x + 1.4 - s1x, d: s2y + s2len + 0.45 - sy };
      prop('rug', sofaRug);
      zone = null;

      // ---- hotline desk beside the server rack (waiting on you): one sits at
      // the computer, the rest queue in the aisle in front of it
      const solo = { x: OW - 1.5, y: 0.2, side: 's', seat: { x: OW - 0.8, y: 1.45 } };
      solid(solo.x, solo.y, 1.4, 0.8, 0.08);
      zones.push({ id: 'booth', x: solo.x, y: solo.y, z: 0, w: 1.4, d: 0.8, h: 1.3 });
      const booth = [{ x: solo.seat.x, y: solo.seat.y, act: 'type', face: 'back', yaw: Math.PI, sit: 0.34 }];
      for (let qy = solo.seat.y + 0.75; qy < D - 0.45 && booth.length < 7; qy += 0.65) booth.push({ x: solo.seat.x, y: qy, act: 'queue', face: 'back', yaw: Math.PI });
      add(OW - 0.45 + D - 0.6, () => this.plant(OW - 0.45, D - 0.6));
      prop('plant', { x: OW - 0.45, y: D - 0.6 });

      // ---- meeting room behind glass, door at y 3.6–4.6
      const mx = OW;
      for (const [y, d] of [[0, 1], [1, 1], [2, 1], [3, 0.6], [4.6, MR - 4.6]]) this.glassX(add, mx, y, d);
      for (let x = mx; x < W - 0.01; x += 1) this.glassY(add, x, MR, Math.min(1, W - x));
      prop('glass', { x: mx, y: 0, w: 0, d: 3.6 });
      prop('glass', { x: mx, y: 4.6, w: 0, d: MR - 4.6 });
      prop('glass', { x: mx, y: MR, w: RW, d: 0 });
      solid(mx - 0.05, 0, 0.1, 3.6, 0.05);
      solid(mx - 0.05, 4.6, 0.1, MR - 4.6, 0.05);
      solid(mx, MR - 0.05, RW, 0.1, 0.05);
      zones.push({ id: 'meeting', x: mx + 2.4, y: 0, z: 0.8, w: 2.6, d: 0.1, h: 1.15 });
      const mtx = mx + 1.6, mty = 1.7;
      add(mtx + 1.5 + mty + 0.7, () => this.meetingTable(mtx, mty));
      prop('meetingTable', { x: mtx, y: mty, w: 3.0, d: 1.4 });
      solid(mtx, mty, 3.0, 1.4);
      for (let k = 0; k < 3; k++) {
        const cx = mtx + 0.5 + k * 1.0;
        add(cx + mty - 0.45, () => this.stool(cx, mty - 0.4, 0.4, '#555c66'));
        add(cx + mty + 1.78, () => this.stool(cx, mty + 1.8, 0.4, '#555c66'));
        prop('chair', { x: cx, y: mty - 0.4, face: 'front' });
        prop('chair', { x: cx, y: mty + 1.8, face: 'back' });
      }
      // Warned agents (near compact, long turn): one alone sits at the table;
      // two or more stand between the chairs and argue across it.
      const meetSeat = [{ x: mtx + 1.5, y: mty - 0.4, act: 'meeting', face: 'front', yaw: 0, sit: 0.42 }];
      const argue = [
        [mtx + 1.0, mty - 0.45, 'front', 0], [mtx + 2.0, mty + 1.85, 'back', Math.PI],
        [mtx - 0.45, mty + 0.7, 'front', Math.PI / 2], [mtx + 3.45, mty + 0.7, 'back', -Math.PI / 2],
        [mtx + 2.0, mty - 0.45, 'front', 0], [mtx + 1.0, mty + 1.85, 'back', Math.PI],
      ].map(([x, y, face, yaw]) => ({ x, y, act: 'argue', face, yaw }));
      add(W - 0.45 + 0.45, () => this.plant(W - 0.45, 0.45));
      prop('plant', { x: W - 0.45, y: 0.45 });
      solid(W - 0.65, 0.25, 0.4, 0.4);

      // ---- pantry
      zone = 'pantry';
      const cy = MR + 0.3;
      longBox(mx + 1.0, cy, 0, RW - 2.3, 0.7, 0.9, pal.counter, pal.counterL, pal.counterR, { bias: -0.5 });
      solid(mx + 1.0, cy, RW - 2.3, 0.7);
      prop('counter', { x: mx + 1.0, y: cy, w: RW - 2.3, d: 0.7 });
      prop('coffee', { x: mx + 2.4, y: cy + 0.12 });
      prop('sink', { x: mx + 3.9, y: cy + 0.12 });
      prop('fridge', { x: W - 1.15, y: cy });
      prop('cooler', { x: mx + 0.35, y: cy + 0.05 });
      add(mx + 2.65 + cy + 0.35, () => this.coffeeMachine(mx + 2.4, cy + 0.12));
      add(mx + 4.15 + cy + 0.35, () => this.sink(mx + 3.9, cy + 0.12));
      add(W - 0.75 + cy + 0.4, () => this.fridge(W - 1.15, cy));
      solid(W - 1.15, cy, 0.85, 0.75);
      zones.push({ id: 'fridge', x: W - 1.15, y: cy, z: 0, w: 0.85, d: 0.75, h: 1.95 });
      add(mx + 0.55 + cy + 0.25, () => this.waterCooler(mx + 0.35, cy + 0.05));
      solid(mx + 0.3, cy, 0.5, 0.5);
      poi(mx + 2.65, cy + 1.2, 'coffee', 'back');
      poi(mx + 4.15, cy + 1.2, 'sink', 'back');
      poi(W - 0.75, cy + 1.3, 'fridge', 'back');
      poi(mx + 0.6, cy + 0.95, 'water', 'back');
      const rtx = mx + 3.6, rty = MR + 2.9;
      add(rtx + rty, () => this.roundTable(rtx, rty));
      prop('roundTable', { x: rtx, y: rty });
      solid(rtx - 0.45, rty - 0.45, 0.9, 0.9);
      for (const [sx, sy, face] of [[rtx - 1.0, rty, 'front'], [rtx + 1.0, rty, 'front'], [rtx, rty + 1.0, 'back'], [rtx, rty - 0.95, 'front']]) {
        add(sx + sy - 0.02, () => this.stool(sx, sy, 0.45, '#c9a24c'));
        poi(sx, sy, 'stool', face, { sit: 0.47 });
        prop('barStool', { x: sx, y: sy });
      }

      // ---- lounge
      zone = 'lounge';
      const lx = mx + 0.6, ly = PY + 0.4;
      const shelfX = mx + 3.4, shelfW = RW - 3.6;
      longBox(shelfX, PY - 0.25, 0, shelfW, 0.45, 1.45, this.shade(pal.deskTop, -5), pal.deskL, pal.deskR, {
        extra: (x0, x1) => this.books(x0, x1, PY + 0.2),
      });
      solid(shelfX, PY - 0.25, shelfW, 0.45);
      prop('shelf', { x: shelfX, y: PY - 0.25, w: shelfW, d: 0.45 });
      poi(mx + 4.6, PY + 0.75, 'read', 'back');
      poi(mx + 5.8, PY + 0.75, 'read', 'back');
      add(lx + ly + 0.6, () => this.couch(lx, ly + 0.6));
      prop('couch', { x: lx, y: ly + 0.6, w: 2.8, d: 0.8 });
      prop('coffeeTable', { x: lx + 0.6, y: ly + 2.0, w: 1.6, d: 0.7 });
      prop('beanbag', { x: lx + 0.4, y: ly + 3.6, color: '#c96442' });
      prop('beanbag', { x: lx + 2.6, y: ly + 3.6, color: '#7d93c4' });
      solid(lx, ly + 0.6, 2.8, 0.8);
      for (let k = 0; k < 3; k++) poi(lx + 0.5 + k * 0.9, ly + 1.2, 'couch', 'front', { sit: 0.36 });
      add(lx + 1.4 + ly + 2.35, () => this.coffeeTable(lx + 0.6, ly + 2.0));
      solid(lx + 0.6, ly + 2.0, 1.6, 0.7);
      add(lx + 0.4 + ly + 3.6, () => this.beanbag(lx + 0.4, ly + 3.6, '#c96442'));
      add(lx + 2.6 + ly + 3.6, () => this.beanbag(lx + 2.6, ly + 3.6, '#7d93c4'));
      poi(lx + 0.4, ly + 3.6, 'beanbag', 'front', { sit: 0.18 });
      poi(lx + 2.6, ly + 3.6, 'beanbag', 'front', { sit: 0.18 });
      const ax = W - 1.2, ay = ly + 1.0;
      add(ax + 0.35 + ay + 0.35, () => this.arcade(ax, ay));
      prop('arcade', { x: ax, y: ay });
      prop('plant', { x: W - 0.5, y: D - 0.5 });
      prop('lamp', { x: lx - 0.2, y: D - 0.6 });
      prop('rug', { x: OW + 0.3, y: PY + 0.6, w: 4.0, d: D - PY - 1.0 });
      solid(ax, ay, 0.7, 0.7);
      poi(ax + 0.35, ay + 1.15, 'arcade', 'back');
      add(W - 0.5 + D - 0.5, () => this.plant(W - 0.5, D - 0.5));
      add(lx - 0.2 + D - 0.6, () => this.floorLamp(lx - 0.2, D - 0.6));

      return { W, D, OW, OD, MR, PY, RW, grid, items, props, pois, desks, solo, windows, board, tv, sofaRug, booth, meetSeat, argue, zones, door: { x: 0.4, y: D - 1.2 } };
    }

    glassX(add, x, y, d) {
      add(x + y + d / 2 + 0.02, () => {
        const P = (a, b, c) => this.iso(a, b, c);
        this.poly([P(x, y, 0), P(x, y + d, 0), P(x, y + d, 0.22), P(x, y, 0.22)], this.pal.trim);
        this.poly([P(x, y, 0.22), P(x, y + d, 0.22), P(x, y + d, 2.1), P(x, y, 2.1)], this.pal.glass, this.pal.glassEdge);
      });
    }

    glassY(add, x, y, w) {
      add(x + w / 2 + y + 0.02, () => {
        const P = (a, b, c) => this.iso(a, b, c);
        this.poly([P(x, y, 0), P(x + w, y, 0), P(x + w, y, 0.22), P(x, y, 0.22)], this.pal.trim);
        this.poly([P(x, y, 0.22), P(x + w, y, 0.22), P(x + w, y, 2.1), P(x, y, 2.1)], this.pal.glass, this.pal.glassEdge);
      });
    }

    // ------------------------------------------------------------ sessions → people

    update(sessions) {
      const working = sessions.filter((s) => WORKING.has(s.status));
      const off = sessions
        .filter((s) => !WORKING.has(s.status))
        .sort((a, b) => (b.lastEventAt || 0) - (a.lastEventAt || 0));
      const wanderers = off.slice(0, MAX_WANDERERS);
      this.offDuty = off.length - wanderers.length;

      // Waiting on you: in line for the hotline desk, first come first served.
      // Busy with a warning (near compact, long turn): to the meeting room.
      // Both keep their order across updates; whoever doesn't fit stays at a desk.
      const keep = (list, ss) => {
        const want = new Set(ss.map((s) => s.key));
        const out = list.filter((k) => want.has(k));
        for (const s of ss) if (!out.includes(s.key)) out.push(s.key);
        return out;
      };
      const byTime = (f) => (a, b) => (a[f] || 0) - (b[f] || 0);
      this.queue = keep(this.queue, working.filter((s) => s.status === 'waiting').sort(byTime('lastEventAt')));
      this.meeting = keep(this.meeting, working.filter((s) => s.status !== 'waiting' && s.alerts && s.alerts.length).sort(byTime('startedAt')));
      const away = () => {
        const m = new Map();
        const { booth, meetSeat, argue } = this.plan;
        this.queue.slice(0, booth.length).forEach((k, i) => m.set(k, booth[i]));
        const room = this.meeting.length > 1 ? argue : meetSeat;
        this.meeting.slice(0, room.length).forEach((k, i) => m.set(k, room[i]));
        return m;
      };
      let spots = away();

      const cap = this.plan.desks.length;
      const atDesk = working.length - spots.size;
      // The office grows with the team (16 desks at the smallest).
      if (atDesk > cap - 1 || (cap > 16 && atDesk < cap * 0.35)) {
        this.plan = this.buildPlan(atDesk);
        this.deskOf.clear();
        for (const p of this.people.values()) { p.poi = null; p.desk = null; p.spot = null; p.until = 0; }
        spots = away();
      }

      const workKeys = new Set(working.filter((s) => !spots.has(s.key)).map((s) => s.key));
      for (const k of [...this.deskOf.keys()]) if (!workKeys.has(k)) this.deskOf.delete(k);
      const used = new Set(this.deskOf.values());
      // Fill the desks that face us first so more faces and screens show.
      const desks = this.plan.desks;
      const order = desks.map((d, i) => i).sort((a, b) => (desks[a].side === 's') - (desks[b].side === 's') || a - b);
      for (const s of working.filter((s) => workKeys.has(s.key)).sort(byTime('startedAt'))) {
        if (this.deskOf.has(s.key)) continue;
        const i = order.find((n) => !used.has(n));
        if (i == null) continue;
        this.deskOf.set(s.key, i);
        used.add(i);
      }

      const shown = new Set();
      for (const s of [...working, ...wanderers]) {
        const spot = spots.get(s.key);
        const isWorker = this.deskOf.has(s.key);
        if (!spot && !isWorker && WORKING.has(s.status)) continue; // no desk left
        shown.add(s.key);
        let p = this.people.get(s.key);
        if (!p) { p = this.newPerson(s); this.people.set(s.key, p); }
        p.s = s;
        p.leaving = false;
        if (spot) {
          this.release(p);
          p.desk = null;
          const mode = s.status === 'waiting' ? 'booth' : 'meet';
          if (p.spot !== spot || p.mode !== mode) { p.spot = spot; this.goTo(p, spot); }
          p.mode = mode;
        } else if (isWorker) {
          this.release(p);
          p.spot = null;
          const desk = desks[this.deskOf.get(s.key)];
          if (p.desk !== desk || p.mode !== 'work') { p.desk = desk; this.goTo(p, desk.seat); }
          p.mode = 'work';
        } else if (p.mode !== 'wander') {
          p.mode = 'wander'; p.desk = null; p.spot = null; p.poi = null; p.until = 0;
        }
      }
      for (const [k, p] of this.people) {
        if (!shown.has(k) && !p.leaving) {
          p.leaving = true;
          p.mode = 'leave';
          this.release(p);
          p.desk = null;
          p.spot = null;
          this.goTo(p, this.plan.door);
        }
      }
      this.first = false;
    }

    release(p) {
      if (p.poi) { p.poi.taken = null; p.poi = null; }
    }

    newPerson(s) {
      const h = hash(s.key);
      const door = this.plan.door;
      return {
        key: s.key, h, s,
        skin: pick(SKIN, h, 3), hair: pick(PLATFORM_HAIR[s.platform] || HAIR, h, 7), pants: pick(PANTS, h, 13),
        shirt: pick(SHIRT[s.agent] || SHIRT.claude, h, 11),
        hairStyle: h % 4,
        phase: (h % 1000) / 1000 * Math.PI * 2,
        x: door.x, y: door.y,
        path: [], walkFace: 'front', until: 0, mode: null,
        // On the first snapshot everyone is placed directly, no stampede.
        placeOnArrive: this.first,
      };
    }

    goTo(p, target) {
      if (p.placeOnArrive) {
        p.x = target.x; p.y = target.y; p.path = []; p.placeOnArrive = false;
        return;
      }
      p.path = this.plan.grid.path(p.x, p.y, target.x, target.y);
    }

    wander(p, now) {
      if (p.path.length) return;
      if (p.poi && now < p.until) return;
      const s = p.s;
      const done = s.status === 'done';
      // Finished and sitting in the sofa corner: stay put until a lounge seat frees up.
      if (done && p.poi && p.poi.zone === 'sofa' && !this.plan.pois.some((q) => !q.taken && this.isRest(q))) {
        p.until = now + rand(20, 40) * 1000;
        return;
      }
      this.release(p);
      // Finished: the lounge sofa and beanbags at the bottom right first, then
      // the TV sofa, then the phone sofa. Idle (or every seat taken): the
      // pantry, sometimes the lounge, leaving its seats to finished agents
      // still looking for one.
      const free = this.plan.pois.filter((q) => !q.taken);
      let pool = [];
      if (done) {
        for (const tier of [(q) => this.isRest(q), (q) => q.act === 'tv', (q) => q.zone === 'sofa']) {
          pool = free.filter(tier);
          if (pool.length) break;
        }
      }
      if (!pool.length) {
        const seeking = [...this.people.values()].some((o) => o.mode === 'wander' && o.s.status === 'done' && !(o.poi && this.isRest(o.poi)));
        pool = free.filter((q) => (q.zone === 'pantry' || q.zone === 'lounge') && !(seeking && this.isRest(q)));
      }
      const sleepy = s.status === 'idle';
      const weighted = pool.map((q) => {
        if (q.act === 'couch' || q.act === 'beanbag') return [q, sleepy ? 2 : 0.7];
        return [q, q.zone === 'pantry' ? 2 : 0.7];
      });
      if (!weighted.length) return;
      let r = Math.random() * weighted.reduce((a, [, w]) => a + w, 0);
      let choice = weighted[0][0];
      for (const [q, w] of weighted) { r -= w; if (r <= 0) { choice = q; break; } }
      choice.taken = p.key;
      p.poi = choice;
      p.until = Infinity; // set on arrival
      p.stay = (done && this.isRest(choice) ? rand(120, 300) : choice.zone === 'sofa' ? rand(20, 40) : rand(8, 20)) * 1000;
      this.goTo(p, choice);
    }

    // The lounge sofa and beanbags at the bottom right, which finished agents take first.
    isRest(q) {
      return q.zone === 'lounge' && (q.act === 'couch' || q.act === 'beanbag');
    }

    // The TV picture, shared by every office on the page (2D, 3D and the PiP window).
    get tvPlayer() {
      return tvPlayer || (tvPlayer = new TvPlayer());
    }

    // ------------------------------------------------------------ whiteboards, the boss, the fridge

    // Pixel sizes follow the boards' world sizes: the sales board is 384 px
    // tall, the meeting room one (2.6 × 1.15, on the back wall) 256 px.
    renderBoards() {
      const { board } = this.plan;
      this.boards.sales.render(Math.round(384 * board.w / board.h), 384);
      this.boards.meet.render(Math.round(256 * 2.6 / 1.15), 256);
    }

    bossHome() {
      return { x: this.plan.OW - 0.6, y: 0.9 };
    }

    // Where the boss stands to reach point u (0 = left edge, 1 = right edge as
    // drawn) of a board, and which way he then faces.
    boardSpot(kind, u) {
      const { board, OW } = this.plan;
      if (kind === 'sales') return { x: 0.55, y: board.y + board.w * (1 - u), yaw: -Math.PI / 2 };
      return { x: OW + 2.4 + 2.6 * u, y: 0.55, yaw: Math.PI };
    }

    // Once a minute the boss walks to the sales board, erases it right to left
    // and writes the next edition left to right, does the same in the meeting
    // room, then goes back to his corner.
    moveBoss(dt, wall) {
      const b = this.boss;
      if (!b.task && !b.tasks.length && wall >= b.next) {
        b.next = wall + BOSS_EVERY;
        b.tasks = [{ board: 'sales' }, { board: 'meet' }, { home: true }];
      }
      if (!b.task && b.tasks.length) {
        b.task = b.tasks.shift();
        const at = b.task.home ? this.bossHome() : this.boardSpot(b.task.board, 1);
        b.path = this.plan.grid.path(b.x, b.y, at.x, at.y);
      }
      if (b.path.length) {
        const n = b.path[0];
        const dx = n.x - b.x, dy = n.y - b.y, d = Math.hypot(dx, dy), v = SPEED * 0.85 * dt;
        if (d > 0.001) b.walkFace = dx + dy >= 0 ? 'front' : 'back';
        if (d <= v) { b.x = n.x; b.y = n.y; b.path.shift(); } else { b.x += (dx / d) * v; b.y += (dy / d) * v; }
        return;
      }
      const t = b.task;
      if (!t) return;
      if (t.home) { b.task = null; return; }
      const sb = this.boards[t.board];
      if (!t.t0) { t.t0 = wall; sb.old = sb.ed; sb.ed++; }
      const e = wall - t.t0;
      const prog = e < BOSS_ERASE ? e / BOSS_ERASE : 1 + Math.min(1, (e - BOSS_ERASE) / BOSS_WRITE);
      const at = this.boardSpot(t.board, prog < 1 ? 1 - prog : prog - 1);
      b.x = at.x; b.y = at.y;
      if (prog >= 2) { sb.prog = null; b.task = null; } else sb.prog = prog;
    }

    // What the boss is doing right now: walking, erasing, writing or standing.
    bossAct() {
      const b = this.boss;
      if (b.path.length) return 'walk';
      if (!b.task || b.task.home || !b.task.t0) return 'stand';
      return this.boards[b.task.board].prog < 1 ? 'erase' : 'write';
    }

    // A plump bald boss in a dark suit: seen from the back at a board.
    drawBoss() {
      const b = this.boss, c = this.ctx, t = this.t / 1000;
      const act = this.bossAct();
      const walking = act === 'walk';
      const front = walking ? b.walkFace !== 'back' : act === 'stand';
      const step = walking ? Math.sin(t * 10) : 0;
      const fb = this.iso(b.x, b.y, 0);
      c.fillStyle = this.pal.shadow;
      c.beginPath(); c.ellipse(fb.x, fb.y, 13, 6, 0, 0, Math.PI * 2); c.fill();
      c.save();
      c.translate(fb.x, fb.y - Math.abs(step) * 1.2);
      c.fillStyle = '#2b2f3a';
      c.fillRect(-6, -10, 5, 10 + step * 2);
      c.fillRect(1, -10, 5, 10 - step * 2);
      c.fillStyle = '#151515';
      c.fillRect(-6, -1 + step, 5, 2);
      c.fillRect(1, -1 - step, 5, 2);
      // round belly in a suit; shirt and red tie from the front
      c.fillStyle = '#3a4150';
      c.beginPath(); c.ellipse(0, -20, 11.5, 11, 0, 0, Math.PI * 2); c.fill();
      if (front) {
        c.fillStyle = '#f4f1ea';
        c.beginPath(); c.moveTo(-4, -30); c.lineTo(4, -30); c.lineTo(0, -19); c.closePath(); c.fill();
        c.fillStyle = '#c0392b';
        c.beginPath(); c.moveTo(-1.5, -28); c.lineTo(1.5, -28); c.lineTo(2.2, -17); c.lineTo(0, -15); c.lineTo(-2.2, -17); c.closePath(); c.fill();
      }
      // arms: the right one up at the board, scrubbing or writing
      c.fillStyle = '#323846';
      c.fillRect(-13, -27, 3.5, 12);
      if (act === 'erase' || act === 'write') {
        const wig = act === 'erase' ? Math.sin(t * 16) * 2.5 : Math.sin(t * 9) * 1.2;
        c.fillRect(9.5 + wig * 0.4, -42 + wig, 3.5, 15);
        c.fillStyle = act === 'erase' ? '#d8d2c4' : '#1f4fa8';
        c.fillRect(8.5 + wig * 0.4, -46 + wig, 5.5, 4);
      } else {
        c.fillRect(9.5, -27, 3.5, 12);
      }
      // bald head: a grey fringe round the back and sides, a shine on top
      const hy = -36;
      c.fillStyle = '#e8b994';
      c.beginPath(); c.arc(0, hy, 7.5, 0, Math.PI * 2); c.fill();
      c.fillStyle = '#8f8a84';
      c.fillRect(-7.6, hy - 0.5, 2.6, 4.5);
      c.fillRect(5, hy - 0.5, 2.6, 4.5);
      if (!front) c.fillRect(-6.5, hy + 1.5, 13, 3.2);
      c.fillStyle = 'rgba(255,255,255,.55)';
      c.beginPath(); c.ellipse(-2.4, hy - 4, 2.4, 1.3, -0.4, 0, Math.PI * 2); c.fill();
      if (front) {
        c.fillStyle = '#2a2420';
        c.fillRect(-3.4, hy - 0.5, 1.6, 1.8);
        c.fillRect(1.8, hy - 0.5, 1.6, 1.8);
        c.fillStyle = '#6b6560';
        c.fillRect(-3.6, hy + 2.6, 7.2, 1.8);
      }
      c.restore();
    }

    // Now and then the fridge breathes out a puff of cold air: 0–1 through a
    // puff, null between them.
    fridgePuff(wall = Date.now()) {
      const k = (wall / 1000) % FRIDGE_PUFF_EVERY;
      return k < FRIDGE_PUFF_FOR ? k / FRIDGE_PUFF_FOR : null;
    }

    // The puffs of one fridge as [x, y, z, radius, alpha], shared by 2D and 3D:
    // they billow out of the door seam, drift forward and rise as they fade.
    fridgeSmoke(x, y, f) {
      const out = [];
      for (let i = 0; i < FRIDGE_PUFFS; i++) {
        const g = f * 1.4 - i * 0.04;
        if (g <= 0 || g >= 1) continue;
        const side = ((i * 37) % 10) / 9 - 0.5;
        out.push([x + 0.425 + side * 0.6 + side * g * 0.5, y + 0.8 + g * 0.9, 0.9 + (i % 4) * 0.3 + g * 0.5, 0.14 + g * 0.3, 0.85 * Math.min(1, (1 - g) * 1.6)]);
      }
      return out;
    }

    setVisible(v) { this.visible = v; if (v) this.resize(); }

    // ------------------------------------------------------------ camera / input

    resize() {
      const r = this.root.getBoundingClientRect();
      const dpr = Math.min(2, window.devicePixelRatio || 1);
      this.cw = Math.max(this.compact ? 1 : 200, r.width);
      this.ch = Math.max(this.compact ? 1 : 240, r.height);
      this.canvas.width = Math.round(this.cw * dpr);
      this.canvas.height = Math.round(this.ch * dpr);
      this.canvas.style.width = this.cw + 'px';
      this.canvas.style.height = this.ch + 'px';
      this.dpr = dpr;
    }

    fitCamera() {
      const { W, D } = this.plan;
      const left = this.iso(0, D, 0).x, right = this.iso(W, 0, 0).x;
      const top = this.iso(0, 0, WALL_H).y - 20, bottom = this.iso(W, D, 0).y + 10;
      const zoom = Math.min(this.cw / (right - left + 30), this.ch / (bottom - top + 30), 2);
      const want = { zoom, ox: this.cw / 2 - ((left + right) / 2) * zoom, oy: this.ch / 2 - ((top + bottom) / 2) * zoom };
      if (!this.cam) this.cam = want;
      else for (const k of ['zoom', 'ox', 'oy']) this.cam[k] += (want[k] - this.cam[k]) * 0.1;
    }

    pointer(e, click) {
      const r = this.canvas.getBoundingClientRect();
      const mx = e.clientX - r.left, my = e.clientY - r.top;
      let found = null;
      for (let i = this.hit.length - 1; i >= 0; i--) {
        const h = this.hit[i];
        if (mx >= h.x0 && mx <= h.x1 && my >= h.y0 && my <= h.y1) { found = h; break; }
      }
      const key = found ? found.key : null;
      this.canvas.style.cursor = key ? 'pointer' : 'default';
      if (key !== this.hoverKey || click) {
        this.hoverKey = key;
        const at = { x: e.clientX, y: e.clientY };
        if (key && key.startsWith('zone:')) {
          if (click) this.onZone(key.slice(5), at);
          else this.onHover(null, at, false);
          return;
        }
        const p = key && this.people.get(key);
        this.onHover(p ? p.s : null, at, !!click);
      }
    }

    // Hit boxes for the clickable areas, pushed before the people so a
    // person standing in front still wins. `proj` maps a floor-plan point
    // (x, y, height) to the coordinates the hit list uses.
    zoneHits(proj) {
      for (const z of this.plan.zones) {
        let x0 = Infinity, x1 = -Infinity, y0 = Infinity, y1 = -Infinity;
        for (const dx of [0, z.w]) for (const dy of [0, z.d]) for (const dz of [0, z.h]) {
          const q = proj(z.x + dx, z.y + dy, z.z + dz);
          x0 = Math.min(x0, q.x); x1 = Math.max(x1, q.x); y0 = Math.min(y0, q.y); y1 = Math.max(y1, q.y);
        }
        this.hit.push({ key: 'zone:' + z.id, x0, x1, y0, y1 });
      }
    }

    iso(x, y, z = 0) {
      return { x: (x - y) * TW / 2, y: (x + y) * TH / 2 - z * TH };
    }

    // ------------------------------------------------------------ primitives

    poly(pts, fill, stroke) {
      const c = this.ctx;
      c.beginPath();
      pts.forEach((p, i) => (i ? c.lineTo(p.x, p.y) : c.moveTo(p.x, p.y)));
      c.closePath();
      if (fill) { c.fillStyle = fill; c.fill(); }
      if (stroke) { c.strokeStyle = stroke; c.lineWidth = 1; c.stroke(); }
    }

    box(x, y, z, w, d, h, top, left, right) {
      const P = (a, b, cc) => this.iso(a, b, cc);
      this.poly([P(x, y + d, z), P(x + w, y + d, z), P(x + w, y + d, z + h), P(x, y + d, z + h)], left);
      this.poly([P(x + w, y, z), P(x + w, y + d, z), P(x + w, y + d, z + h), P(x + w, y, z + h)], right);
      this.poly([P(x, y, z + h), P(x + w, y, z + h), P(x + w, y + d, z + h), P(x, y + d, z + h)], top);
    }

    shade(color, amt) {
      if (color[0] !== '#') return color;
      const n = parseInt(color.slice(1), 16);
      const f = (v) => clamp(Math.round(v + amt), 0, 255);
      return `rgb(${f(n >> 16)},${f((n >> 8) & 255)},${f(n & 255)})`;
    }

    roundRect(x, y, w, h, r, fill, stroke, lw = 1.5) {
      const c = this.ctx;
      c.beginPath();
      c.moveTo(x + r, y);
      c.arcTo(x + w, y, x + w, y + h, r);
      c.arcTo(x + w, y + h, x, y + h, r);
      c.arcTo(x, y + h, x, y, r);
      c.arcTo(x, y, x + w, y, r);
      c.closePath();
      if (fill) { c.fillStyle = fill; c.fill(); }
      if (stroke) { c.strokeStyle = stroke; c.lineWidth = lw; c.stroke(); }
    }

    // A monospace label on a dark chip with a coloured ring (the warnings),
    // so it reads as UI and not as part of the office.
    chip(text, x, y, ring, align = 'center') {
      const c = this.ctx;
      c.save();
      c.font = '700 10px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace';
      const w = Math.max(16, c.measureText(text).width + 9);
      const x0 = align === 'right' ? x - w : align === 'left' ? x : x - w / 2;
      this.roundRect(x0, y - 8, w, 16, 4, 'rgba(22,24,30,.92)', ring, 1.6);
      c.fillStyle = '#f4f4f0';
      c.textAlign = 'center';
      c.textBaseline = 'middle';
      c.fillText(text, x0 + w / 2, y + 0.5);
      c.restore();
      return w;
    }

    // Draw flat things on a wall: 'y0' = back wall (runs along x),
    // 'x0' = left wall (runs along y).
    onWall(dir, a, z, fn) {
      const c = this.ctx;
      const o = dir === 'y0' ? this.iso(a, 0, z) : this.iso(0, a, z);
      c.save();
      c.translate(o.x, o.y);
      c.transform(1, dir === 'y0' ? 0.5 : -0.5, 0, 1, 0, 0);
      fn(c);
      c.restore();
    }

    // ------------------------------------------------------------ room shell

    drawRoom() {
      const { W, D, OW, MR, PY, board, tv, sofaRug: rg } = this.plan;
      const pal = this.pal;
      const P = (x, y, z) => this.iso(x, y, z);
      const t = this.t / 1000;

      // floors: wood office, carpet meeting room, tiled pantry, wood lounge
      for (let x = 0; x < Math.ceil(OW); x++) {
        for (let y = 0; y < Math.ceil(D); y++) {
          const x1 = Math.min(OW, x + 1), y1 = Math.min(D, y + 1);
          this.poly([P(x, y), P(x1, y), P(x1, y1), P(x, y1)], (x + y) % 2 ? pal.wood1 : pal.wood2);
        }
      }
      for (let y = 0.5; y < D; y += 0.5) this.poly([P(0, y), P(OW, y), P(OW, y + 0.012), P(0, y + 0.012)], pal.woodLine);
      this.poly([P(OW, 0), P(W, 0), P(W, MR), P(OW, MR)], pal.carpet);
      for (let x = OW + 0.5; x < W; x += 0.5) this.poly([P(x, 0), P(x + 0.015, 0), P(x + 0.015, MR), P(x, MR)], pal.carpetLine);
      for (let i = 0; i < Math.ceil((W - OW) * 2); i++) {
        for (let j = 0; j < Math.ceil((PY - MR) * 2); j++) {
          const x = OW + i / 2, y = MR + j / 2;
          const x1 = Math.min(W, x + 0.5), y1 = Math.min(PY, y + 0.5);
          this.poly([P(x, y), P(x1, y), P(x1, y1), P(x, y1)], (i + j) % 2 ? pal.tileA : pal.tileB);
        }
      }
      for (let x = Math.floor(OW); x < W; x++) {
        for (let y = PY; y < D; y++) {
          const x0 = Math.max(OW, x), x1 = Math.min(W, x + 1), y1 = Math.min(D, y + 1);
          this.poly([P(x0, y), P(x1, y), P(x1, y1), P(x0, y1)], (x + Math.floor(y)) % 2 ? pal.wood1 : pal.wood2);
        }
      }
      this.poly([P(OW + 0.3, PY + 0.6), P(OW + 4.3, PY + 0.6), P(OW + 4.3, D - 0.4), P(OW + 0.3, D - 0.4)], pal.rug);
      this.poly([P(OW + 0.5, PY + 0.8), P(OW + 4.1, PY + 0.8), P(OW + 4.1, D - 0.6), P(OW + 0.5, D - 0.6)], null, pal.rugEdge);
      this.poly([P(rg.x, rg.y), P(rg.x + rg.w, rg.y), P(rg.x + rg.w, rg.y + rg.d), P(rg.x, rg.y + rg.d)], pal.rug);
      this.poly([P(rg.x + 0.2, rg.y + 0.2), P(rg.x + rg.w - 0.2, rg.y + 0.2), P(rg.x + rg.w - 0.2, rg.y + rg.d - 0.2), P(rg.x + 0.2, rg.y + rg.d - 0.2)], null, pal.rugEdge);

      // walls
      this.poly([P(0, 0, 0), P(W, 0, 0), P(W, 0, WALL_H), P(0, 0, WALL_H)], pal.wallL);
      this.poly([P(0, 0, 0), P(0, D, 0), P(0, D, WALL_H), P(0, 0, WALL_H)], pal.wallR);
      this.poly([P(0, 0, WALL_H), P(W, 0, WALL_H), P(W, -0.15, WALL_H), P(-0.15, -0.15, WALL_H), P(-0.15, D, WALL_H), P(0, D, WALL_H)], pal.wallTop);
      this.poly([P(0, 0, 0), P(W, 0, 0), P(W, 0, 0.12), P(0, 0, 0.12)], pal.trim);
      this.poly([P(0, 0, 0), P(0, D, 0), P(0, D, 0.12), P(0, 0, 0.12)], pal.trim);

      // windows with the real sky outside
      const sky = skyColor();
      for (const x of this.plan.windows) {
        this.poly([P(x, 0, 0.85), P(x + 1.5, 0, 0.85), P(x + 1.5, 0, 2.0), P(x, 0, 2.0)], sky.sky);
        if (sky.night) {
          this.onWall('y0', x + 0.3, 1.75, (c) => { c.fillStyle = '#f4f1d0'; c.beginPath(); c.arc(0, 0, 3, 0, Math.PI * 2); c.fill(); });
          this.onWall('y0', x + 1.1, 1.55, (c) => { c.fillStyle = 'rgba(255,255,255,.7)'; c.fillRect(0, 0, 1.5, 1.5); });
        } else {
          const cx = x + 0.25 + ((t * 0.02 + x * 0.37) % 1) * 1.0;
          this.onWall('y0', cx, 1.65, (c) => {
            c.fillStyle = 'rgba(255,255,255,.85)';
            c.beginPath(); c.ellipse(0, 0, 7, 3, 0, 0, Math.PI * 2); c.ellipse(5, -2, 5, 3, 0, 0, Math.PI * 2); c.fill();
          });
        }
        this.poly([P(x + 0.74, 0, 0.85), P(x + 0.76, 0, 0.85), P(x + 0.76, 0, 2.0), P(x + 0.74, 0, 2.0)], pal.trim);
        this.poly([P(x, 0, 0.85), P(x + 1.5, 0, 0.85), P(x + 1.5, 0, 0.9), P(x, 0, 0.9)], pal.trim);
        this.poly([P(x, 0.05), P(x + 1.5, 0.05), P(x + 1.95, 1.2), P(x + 0.45, 1.2)], sky.night ? 'rgba(120,140,200,.06)' : 'rgba(255,250,230,.16)');
      }
      // meeting room whiteboard + sign
      const wx = OW + 2.4;
      this.poly([P(wx, 0, 0.8), P(wx + 2.6, 0, 0.8), P(wx + 2.6, 0, 1.95), P(wx, 0, 1.95)], '#fdfdfb', pal.trim);
      const meet = this.boards.meet.canvas;
      this.onWall('y0', wx, 1.95, (c) => {
        c.scale((2.6 * TW / 2) / meet.width, (1.15 * TH) / meet.height);
        c.drawImage(meet, 0, 0);
      });
      this.onWall('y0', OW + 0.5, 2.2, (c) => {
        c.font = '700 9px ui-sans-serif, system-ui, sans-serif'; c.fillStyle = this.shade(pal.trim, -60); c.fillText('MEETING', 0, 0);
      });
      // left wall: sales board, TV, clock, door
      this.poly([P(0, board.y - 0.06, board.z - 0.06), P(0, board.y + board.w + 0.06, board.z - 0.06), P(0, board.y + board.w + 0.06, board.z + board.h + 0.06), P(0, board.y - 0.06, board.z + board.h + 0.06)], '#b9bec5');
      // onWall's x runs toward the window wall, one tile = TW / 2 px.
      const sales = this.boards.sales.canvas;
      this.onWall('x0', board.y + board.w, board.z + board.h, (c) => {
        c.scale((board.w * TW / 2) / sales.width, (board.h * TH) / sales.height);
        c.drawImage(sales, 0, 0);
      });
      this.poly([P(0, tv.y, 0.95), P(0, tv.y + tv.w, 0.95), P(0, tv.y + tv.w, 1.85), P(0, tv.y, 1.85)], '#111214');
      this.poly([P(0, tv.y + 0.06, 1.0), P(0, tv.y + tv.w - 0.06, 1.0), P(0, tv.y + tv.w - 0.06, 1.8), P(0, tv.y + 0.06, 1.8)], `hsl(${(t * 12) % 360},55%,${pal.dark ? 26 : 34}%)`);
      const tvp = this.tvPlayer;
      tvp.tick(performance.now());
      if (tvp.ready) {
        this.onWall('x0', tv.y + tv.w - 0.06, 1.8, (c) => {
          c.scale(((tv.w - 0.12) * TW / 2) / tvp.canvas.width, (0.8 * TH) / tvp.canvas.height);
          c.drawImage(tvp.canvas, 0, 0);
        });
      }
      this.onWall('x0', 1.0, 1.85, (c) => {
        c.beginPath(); c.arc(0, 0, 11, 0, Math.PI * 2); c.fillStyle = pal.bubble; c.fill();
        c.lineWidth = 2; c.strokeStyle = pal.trim; c.stroke();
        const d = new Date();
        const hand = (ang, len, w) => { c.beginPath(); c.moveTo(0, 0); c.lineTo(Math.sin(ang) * len, -Math.cos(ang) * len); c.lineWidth = w; c.strokeStyle = pal.bubbleInk; c.stroke(); };
        hand(((d.getHours() % 12) + d.getMinutes() / 60) / 12 * Math.PI * 2, 5.5, 2);
        hand((d.getMinutes() / 60) * Math.PI * 2, 8, 1.4);
      });
      const dy = this.plan.door.y;
      this.poly([P(0, dy - 0.6, 0), P(0, dy + 0.6, 0), P(0, dy + 0.6, 1.8), P(0, dy - 0.6, 1.8)], pal.trim);
      this.poly([P(0, dy - 0.52, 0), P(0, dy + 0.52, 0), P(0, dy + 0.52, 1.72), P(0, dy - 0.52, 1.72)], this.shade(pal.deskL, -10));
      this.onWall('x0', dy - 0.25, 0.85, (c) => { c.fillStyle = '#d8c48a'; c.beginPath(); c.arc(0, 0, 1.8, 0, Math.PI * 2); c.fill(); });
      this.poly([P(0.05, dy - 0.6), P(0.9, dy - 0.6), P(0.9, dy + 0.6), P(0.05, dy + 0.6)], pal.dark ? '#5a3b2e' : '#b9654a');
    }

    // ------------------------------------------------------------ furniture

    plant(x, y) {
      const pal = this.pal;
      this.box(x - 0.18, y - 0.18, 0, 0.36, 0.36, 0.3, this.shade(pal.pot, 10), pal.pot, this.shade(pal.pot, -15));
      const c = this.ctx;
      const b = this.iso(x, y, 0.3);
      const sway = Math.sin(this.t / 1400 + x) * 0.05;
      c.fillStyle = pal.plant;
      for (let i = 0; i < 6; i++) {
        const a = (i / 6) * Math.PI * 2;
        c.beginPath();
        c.ellipse(b.x + Math.cos(a) * 6, b.y - 13 + Math.sin(a) * 3, 5.5, 13, a / 3 + sway, 0, Math.PI * 2);
        c.fill();
      }
      c.fillStyle = this.shade(pal.plant, 22);
      c.beginPath(); c.ellipse(b.x, b.y - 20, 5, 10, sway, 0, Math.PI * 2); c.fill();
    }

    serverRack(x, y) {
      this.box(x, y, 0, 0.7, 0.6, 1.9, '#3a3d44', '#24262b', '#1b1d21');
      const c = this.ctx;
      const busy = this.deskOf.size;
      for (let r = 0; r < 7; r++) {
        for (let k = 0; k < 4; k++) {
          const on = Math.sin(this.t / (180 + r * 37 + k * 53) + r * k) > 0.2 && (r * 4 + k) < Math.max(4, busy * 3);
          const p = this.iso(x + 0.12 + k * 0.13, y + 0.6, 1.7 - r * 0.22);
          c.fillStyle = on ? (k % 3 === 0 ? '#ffb347' : '#5cf08a') : '#2f3a33';
          c.fillRect(p.x - 1, p.y - 1, 2.5, 2);
        }
      }
    }

    printer(x, y) {
      const pal = this.pal;
      this.box(x, y, 0, 0.75, 0.55, 0.55, pal.deskTop, pal.deskL, pal.deskR);
      this.box(x + 0.05, y + 0.05, 0.55, 0.65, 0.45, 0.28, '#e8e8e8', '#c8c8c8', '#b3b3b3');
      this.box(x + 0.15, y + 0.35, 0.83, 0.45, 0.15, 0.02, '#ffffff', '#eee', '#ddd');
    }

    meetingTable(x, y) {
      const pal = this.pal;
      this.box(x + 0.3, y + 0.5, 0, 0.12, 0.4, 0.7, pal.deskL, pal.deskL, pal.deskR);
      this.box(x + 2.6, y + 0.5, 0, 0.12, 0.4, 0.7, pal.deskL, pal.deskL, pal.deskR);
      this.box(x, y, 0.7, 3.0, 1.4, 0.08, this.shade(pal.deskTop, 12), pal.deskL, pal.deskR);
      this.box(x + 1.2, y + 0.45, 0.78, 0.5, 0.35, 0.03, '#2a2a2e', '#222', '#1a1a1a');
      this.box(x + 0.4, y + 0.3, 0.78, 0.2, 0.28, 0.02, '#fff', '#eee', '#ddd');
      this.box(x + 2.2, y + 0.8, 0.78, 0.12, 0.12, 0.12, '#f2f2f2', '#d0d0d0', '#bbb');
    }

    stool(x, y, h, color) {
      this.box(x - 0.04, y - 0.04, 0, 0.08, 0.08, h, '#555', '#444', '#333');
      this.box(x - 0.2, y - 0.2, h, 0.4, 0.4, 0.07, this.shade(color, 18), color, this.shade(color, -18));
    }

    roundTable(x, y) {
      const c = this.ctx;
      this.box(x - 0.05, y - 0.05, 0, 0.1, 0.1, 0.72, '#555', '#444', '#333');
      const p = this.iso(x, y, 0.75);
      c.fillStyle = this.shade(this.pal.deskTop, -15);
      c.beginPath(); c.ellipse(p.x, p.y + 3, 30, 15, 0, 0, Math.PI * 2); c.fill();
      c.fillStyle = this.shade(this.pal.deskTop, 15);
      c.beginPath(); c.ellipse(p.x, p.y, 30, 15, 0, 0, Math.PI * 2); c.fill();
      c.fillStyle = '#fff';
      c.beginPath(); c.ellipse(p.x + 8, p.y - 2, 5, 2.5, 0, 0, Math.PI * 2); c.fill();
      c.fillStyle = '#e6b35c';
      c.beginPath(); c.arc(p.x + 8, p.y - 3, 2, 0, Math.PI * 2); c.fill();
    }

    coffeeMachine(x, y) {
      this.box(x, y, 0.9, 0.5, 0.45, 0.6, '#555b63', '#3a3f45', '#2e3237');
      this.box(x + 0.15, y + 0.42, 1.0, 0.2, 0.05, 0.15, '#222', '#111', '#111');
      const p = this.iso(x + 0.4, y + 0.45, 1.35);
      this.ctx.fillStyle = Math.sin(this.t / 400) > 0 ? '#ff6b4a' : '#7a3a2a';
      this.ctx.fillRect(p.x, p.y, 2.5, 2.5);
      this.box(x + 0.6, y + 0.1, 0.9, 0.14, 0.14, 0.14, '#fff', '#ddd', '#ccc');
    }

    sink(x, y) {
      this.box(x, y + 0.05, 0.9, 0.55, 0.4, 0.02, '#9aa3ad', '#7d858e', '#6c737b');
      this.box(x + 0.22, y + 0.02, 0.92, 0.06, 0.06, 0.3, this.pal.metal, this.pal.metal, '#888');
      this.box(x + 0.22, y + 0.02, 1.2, 0.06, 0.2, 0.04, this.pal.metal, this.pal.metal, '#888');
    }

    fridge(x, y) {
      this.box(x, y, 0, 0.85, 0.75, 1.95, '#eef1f4', '#cfd5db', '#bcc3ca');
      const P = (a, b, c) => this.iso(a, b, c);
      this.poly([P(x, y + 0.75, 1.25), P(x + 0.85, y + 0.75, 1.25), P(x + 0.85, y + 0.75, 1.27), P(x, y + 0.75, 1.27)], '#9aa3ad');
      this.poly([P(x + 0.7, y + 0.75, 1.4), P(x + 0.73, y + 0.75, 1.4), P(x + 0.73, y + 0.75, 1.75), P(x + 0.7, y + 0.75, 1.75)], '#888');
      for (const [mx, mz, col] of [[0.2, 1.6, '#c96442'], [0.4, 1.45, '#3b8ee0'], [0.3, 0.9, '#5f9a4c']]) {
        const p = P(x + mx, y + 0.75, mz);
        this.ctx.fillStyle = col; this.ctx.fillRect(p.x, p.y, 4, 4);
      }
      const f = this.fridgePuff();
      if (f != null) {
        for (const [sx, sy, sz, r, a] of this.fridgeSmoke(x, y, f)) {
          const p = P(sx, sy, sz);
          this.ctx.fillStyle = `rgba(214,228,240,${a})`;
          this.ctx.strokeStyle = `rgba(150,170,190,${a * 0.5})`;
          this.ctx.lineWidth = 1;
          this.ctx.beginPath(); this.ctx.arc(p.x, p.y, r * TW / 2, 0, Math.PI * 2); this.ctx.fill(); this.ctx.stroke();
        }
      }
    }

    waterCooler(x, y) {
      this.box(x, y, 0, 0.42, 0.42, 0.95, '#e8ecef', '#cfd5db', '#bcc3ca');
      const c = this.ctx;
      const p = this.iso(x + 0.21, y + 0.21, 0.95);
      c.fillStyle = 'rgba(120,180,240,.6)';
      c.beginPath(); c.ellipse(p.x, p.y - 9, 8, 11, 0, 0, Math.PI * 2); c.fill();
      c.fillStyle = 'rgba(255,255,255,.5)';
      c.fillRect(p.x - 4, p.y - 16, 2, 10);
      const k = (this.t / 1800) % 1;
      c.fillStyle = 'rgba(255,255,255,.85)';
      c.beginPath(); c.arc(p.x + 2, p.y - 2 - k * 16, 1.2, 0, Math.PI * 2); c.fill();
      const tap = this.iso(x + 0.21, y + 0.42, 0.65);
      c.fillStyle = '#3b8ee0'; c.fillRect(tap.x - 1.5, tap.y, 3, 3);
    }

    couch(x, y) {
      const pal = this.pal;
      this.box(x, y - 0.05, 0.32, 2.8, 0.25, 0.42, this.shade(pal.couch, 10), pal.couchD, this.shade(pal.couchD, -12));
      this.box(x, y, 0, 2.8, 0.8, 0.32, pal.couch, pal.couchD, this.shade(pal.couchD, -12));
      this.box(x - 0.15, y - 0.05, 0, 0.18, 0.85, 0.52, this.shade(pal.couch, 6), pal.couchD, this.shade(pal.couchD, -12));
      this.box(x + 2.8, y - 0.05, 0, 0.18, 0.85, 0.52, this.shade(pal.couch, 6), pal.couchD, this.shade(pal.couchD, -12));
      this.box(x + 2.15, y + 0.12, 0.32, 0.35, 0.16, 0.28, '#e9c46a', '#d4ae52', '#c19c43');
    }

    // A sofa along y, back to the right (x high), seat facing -x. The back
    // goes in three pieces, one per seat, so each sitter sorts behind it.
    couchY(add, x, y, len) {
      const pal = this.pal;
      const top = this.shade(pal.couch, 10), side = this.shade(pal.couchD, -12), arm = this.shade(pal.couch, 6);
      add(x + y + 0.2, () => this.box(x, y, 0, 0.8, len, 0.32, pal.couch, pal.couchD, side));
      add(x + 0.4 + y - 0.06, () => this.box(x - 0.05, y - 0.15, 0, 0.85, 0.18, 0.52, arm, pal.couchD, side));
      for (let k = 0; k < 3; k++) {
        const py = y + (k * len) / 3;
        add(x + 0.675 + py + len / 6, () => this.box(x + 0.55, py, 0.32, 0.25, len / 3, 0.42, top, pal.couchD, side));
      }
      add(x + 0.4 + y + len + 0.09, () => this.box(x - 0.05, y + len, 0, 0.85, 0.18, 0.52, arm, pal.couchD, side));
    }

    coffeeTable(x, y) {
      const pal = this.pal;
      this.box(x, y, 0, 1.6, 0.7, 0.3, this.shade(pal.deskTop, -10), pal.deskL, pal.deskR);
      this.box(x + 0.2, y + 0.15, 0.3, 0.35, 0.25, 0.06, '#c96442', '#a85336', '#8f462e');
      this.box(x + 1.1, y + 0.3, 0.3, 0.12, 0.12, 0.12, '#fff', '#ddd', '#ccc');
    }

    beanbag(x, y, color) {
      const c = this.ctx;
      const p = this.iso(x, y, 0);
      c.fillStyle = this.shade(color, -25);
      c.beginPath(); c.ellipse(p.x, p.y - 4, 20, 11, 0, 0, Math.PI * 2); c.fill();
      c.fillStyle = color;
      c.beginPath(); c.ellipse(p.x, p.y - 8, 18, 10, 0, 0, Math.PI * 2); c.fill();
      c.fillStyle = this.shade(color, 20);
      c.beginPath(); c.ellipse(p.x - 3, p.y - 14, 10, 6, -0.2, 0, Math.PI * 2); c.fill();
    }

    books(x0, x1, y) {
      const P = (a, b, c) => this.iso(a, b, c);
      const cols = this.pal.book;
      for (let shelf = 0; shelf < 3; shelf++) {
        const z = 0.12 + shelf * 0.45;
        let bx = x0 + 0.05;
        let i = Math.round(x0 * 7) + shelf * 5;
        while (bx < x1 - 0.1) {
          const bw = 0.07 + ((i * 13) % 5) * 0.015;
          const bh = 0.25 + ((i * 7) % 4) * 0.03;
          if (i % 7 !== 3) this.poly([P(bx, y, z), P(bx + bw, y, z), P(bx + bw, y, z + bh), P(bx, y, z + bh)], cols[i % cols.length]);
          bx += bw + 0.015;
          i++;
        }
        this.poly([P(x0, y, z - 0.02), P(x1, y, z - 0.02), P(x1, y, z), P(x0, y, z)], this.pal.deskR);
      }
    }

    arcade(x, y) {
      this.box(x, y, 0, 0.7, 0.7, 1.75, '#3a2a6a', '#2b1f52', '#221843');
      const P = (a, b, c) => this.iso(a, b, c);
      const hue = (this.t / 20) % 360;
      this.poly([P(x + 0.1, y + 0.7, 1.0), P(x + 0.6, y + 0.7, 1.0), P(x + 0.6, y + 0.7, 1.45), P(x + 0.1, y + 0.7, 1.45)], `hsl(${hue},70%,55%)`);
      const blip = this.iso(x + 0.25 + 0.2 * Math.sin(this.t / 300), y + 0.7, 1.2);
      this.ctx.fillStyle = '#fff'; this.ctx.fillRect(blip.x, blip.y, 3, 3);
      this.poly([P(x, y + 0.7, 1.55), P(x + 0.7, y + 0.7, 1.55), P(x + 0.7, y + 0.7, 1.72), P(x, y + 0.7, 1.72)], '#ffcf4a');
      this.box(x, y + 0.7, 0.8, 0.7, 0.2, 0.08, '#4a3a8a', '#3a2a6a', '#2b1f52');
      const j = this.iso(x + 0.25, y + 0.8, 0.88);
      this.ctx.fillStyle = '#e04a4a'; this.ctx.beginPath(); this.ctx.arc(j.x, j.y - 3, 2.5, 0, Math.PI * 2); this.ctx.fill();
    }

    floorLamp(x, y) {
      const c = this.ctx;
      const b = this.iso(x, y, 0), t = this.iso(x, y, 1.7);
      if (skyColor().night) {
        c.fillStyle = 'rgba(255,220,140,.14)';
        c.beginPath(); c.ellipse(b.x, b.y, 50, 25, 0, 0, Math.PI * 2); c.fill();
      }
      c.strokeStyle = '#333'; c.lineWidth = 2;
      c.beginPath(); c.moveTo(b.x, b.y); c.lineTo(t.x, t.y); c.stroke();
      c.fillStyle = '#333'; c.beginPath(); c.ellipse(b.x, b.y, 7, 3.5, 0, 0, Math.PI * 2); c.fill();
      c.fillStyle = '#f3e3b5';
      c.beginPath(); c.moveTo(t.x - 9, t.y + 4); c.lineTo(t.x + 9, t.y + 4); c.lineTo(t.x + 5, t.y - 8); c.lineTo(t.x - 5, t.y - 8); c.closePath(); c.fill();
    }

    desk(dk, p) {
      const pal = this.pal;
      const { x, y } = dk;
      for (const [lx, ly] of [[0.05, 0.05], [1.27, 0.05], [0.05, 0.67], [1.27, 0.67]]) {
        this.box(x + lx, y + ly, 0, 0.08, 0.08, 0.72, pal.deskL, pal.deskL, pal.deskR);
      }
      this.box(x, y, 0.72, 1.4, 0.8, 0.07, pal.deskTop, pal.deskL, pal.deskR);
      const worker = p && (p.mode === 'work' || p.mode === 'booth') ? p : null;
      if (dk.side === 's') {
        const mx = x + 0.42, my = y + 0.1, mz = 0.79;
        this.box(mx + 0.22, my + 0.04, mz, 0.1, 0.1, 0.18, pal.monitor, pal.monitor, pal.monitor);
        this.box(mx, my, mz + 0.18, 0.56, 0.08, 0.42, pal.monitor, pal.monitor, this.shade(pal.monitor, 10));
        this.screen(mx + 0.04, my + 0.08, mz + 0.22, 0.48, 0.34, worker);
        this.box(x + 0.48, y + 0.48, 0.79, 0.44, 0.16, 0.025, '#d8d8d8', '#aaa', '#999');
      } else {
        // We see this monitor from behind; a status LED on its back.
        const mx = x + 0.42, my = y + 0.6, mz = 0.79;
        this.box(x + 0.48, y + 0.16, 0.79, 0.44, 0.16, 0.025, '#d8d8d8', '#aaa', '#999');
        this.box(mx + 0.22, my - 0.02, mz, 0.1, 0.1, 0.18, pal.monitor, pal.monitor, pal.monitor);
        this.box(mx, my, mz + 0.18, 0.56, 0.08, 0.42, this.shade(pal.monitor, 15), this.shade(pal.monitor, 8), this.shade(pal.monitor, 12));
        if (worker) {
          const g = this.iso(mx + 0.28, my + 0.08, mz + 0.5);
          this.ctx.fillStyle = STATUS_COLOR[worker.s.status] || '#888';
          this.ctx.globalAlpha = 0.6 + 0.4 * Math.sin(this.t / 400 + worker.phase);
          this.ctx.beginPath(); this.ctx.arc(g.x, g.y, 1.8, 0, Math.PI * 2); this.ctx.fill();
          this.ctx.globalAlpha = 1;
        }
      }
      // personal touches, stable per desk
      const h = hash(`${x.toFixed(1)},${y.toFixed(1)}`);
      if (h % 3 === 0) this.box(x + 1.12, y + 0.3, 0.79, 0.12, 0.12, 0.13, '#f2f2f2', '#d0d0d0', '#bbb');
      if (h % 5 === 1) {
        this.box(x + 0.14, y + 0.24, 0.79, 0.12, 0.12, 0.08, pal.pot, pal.pot, this.shade(pal.pot, -15));
        const q = this.iso(x + 0.2, y + 0.3, 0.87);
        this.ctx.fillStyle = pal.plant;
        this.ctx.beginPath(); this.ctx.ellipse(q.x, q.y - 4, 4, 6, 0, 0, Math.PI * 2); this.ctx.fill();
      }
      if (h % 4 === 2) this.box(x + 0.12, y + 0.45, 0.79, 0.22, 0.15, 0.05, '#f2d16b', '#d9b84f', '#c4a33f');
    }

    screen(x, y, z, w, h, p) {
      const P = (a, b, cc) => this.iso(a, b, cc);
      const st = p ? p.s.status : 'off';
      const base = { thinking: '#3a3160', tool: '#14324f', typing: '#123d2e', waiting: '#4d3510', error: '#4a1612' }[st] || this.pal.screenOff;
      const corners = [P(x, y, z), P(x + w, y, z), P(x + w, y, z + h), P(x, y, z + h)];
      this.poly(corners, base);
      if (!p) return;
      const c = this.ctx;
      c.save();
      c.beginPath();
      corners.forEach((q, i) => (i ? c.lineTo(q.x, q.y) : c.moveTo(q.x, q.y)));
      c.closePath();
      c.clip();
      const t = this.t / 1000;
      const col = STATUS_COLOR[st];
      if (st === 'tool' || st === 'typing') {
        const scroll = (t * (st === 'tool' ? 2.2 : 1.2) + p.phase) % 1;
        for (let i = -1; i < 6; i++) {
          const zz = z + h - 0.05 - ((i + scroll) * h) / 5;
          const len = 0.12 + ((p.h >> ((i + 8) & 15)) % 7) / 20;
          const a = P(x + 0.05, y, zz), b = P(x + 0.05 + Math.min(len, w - 0.1), y, zz);
          c.strokeStyle = i % 3 === 0 ? col : 'rgba(255,255,255,.55)';
          c.lineWidth = 1.5;
          c.beginPath(); c.moveTo(a.x, a.y); c.lineTo(b.x, b.y); c.stroke();
        }
      } else {
        const m = P(x + w / 2, y, z + h / 2);
        c.fillStyle = col;
        c.globalAlpha = st === 'thinking' ? 0.5 + 0.4 * Math.sin(t * 3 + p.phase) : 0.9;
        c.font = '700 10px ui-sans-serif, system-ui, sans-serif';
        c.textAlign = 'center';
        c.textBaseline = 'middle';
        c.fillText({ thinking: '…', waiting: '?', error: '!' }[st] || '', m.x, m.y);
      }
      c.restore();
      c.save();
      c.globalAlpha = 0.18;
      const g = P(x + w / 2, y + 0.35, 0.8);
      c.fillStyle = col;
      c.beginPath(); c.ellipse(g.x, g.y, 18, 7, 0, 0, Math.PI * 2); c.fill();
      c.restore();
    }

    chair(dk, p) {
      const pal = this.pal;
      const sx = dk.seat.x - 0.25, sy = dk.seat.y - 0.22;
      const seated = p && !p.path.length && p.pose === 'sit' && Math.hypot(p.x - dk.seat.x, p.y - dk.seat.y) < 0.05;
      const ox = seated ? 0 : 0.12, oy = seated ? 0 : dk.side === 's' ? 0.18 : -0.18;
      this.box(sx + 0.22 + ox, sy + 0.19 + oy, 0, 0.06, 0.06, 0.32, '#222', '#222', '#111');
      if (dk.side === 'n') this.box(sx + ox, sy + oy - 0.05, 0.4, 0.5, 0.07, 0.55, this.shade(pal.chair, 15), pal.chair, this.shade(pal.chair, -10));
      this.box(sx + ox, sy + oy, 0.32, 0.5, 0.45, 0.08, this.shade(pal.chair, 15), pal.chair, this.shade(pal.chair, -10));
      if (dk.side === 's' && !seated) this.box(sx + ox, sy + oy + 0.4, 0.4, 0.5, 0.07, 0.5, this.shade(pal.chair, 15), pal.chair, this.shade(pal.chair, -10));
    }

    // ------------------------------------------------------------ people

    stateOf(p) {
      const s = p.s || {};
      if (p.path.length) return { pose: 'walk', face: p.walkFace, act: null, sitZ: 0 };
      if (p.mode === 'work' && p.desk) {
        const face = p.desk.side === 'n' ? 'front' : 'back';
        if (s.status === 'waiting' || s.status === 'error') return { pose: 'stand', face, act: 'wave', sitZ: 0 };
        return { pose: 'sit', face, act: s.status === 'tool' || s.status === 'typing' ? 'type' : 'think', sitZ: 0.34 };
      }
      const q = (p.mode === 'booth' || p.mode === 'meet') ? p.spot : p.mode === 'wander' ? p.poi : null;
      // Finished agents on the lounge sofa scroll their phones.
      const act = q && q.act === 'couch' && s.status === 'done' ? 'scroll' : q && q.act;
      if (q) return { pose: q.sit ? 'sit' : 'stand', face: q.face, act, sitZ: q.sit || 0 };
      return { pose: 'stand', face: 'front', act: null, sitZ: 0 };
    }

    person(p) {
      const c = this.ctx;
      const t = this.t / 1000;
      const s = p.s || {};
      const st = this.stateOf(p);
      p.pose = st.pose;
      const moving = st.pose === 'walk';
      const seated = st.pose === 'sit';
      const front = st.face !== 'back';
      const b = this.iso(p.x, p.y, st.sitZ);
      const step = moving ? Math.sin(t * 12 + p.phase) : 0;
      const bob = moving ? Math.abs(step) * 1.5 : 0;

      const fb = this.iso(p.x, p.y, 0);
      c.fillStyle = this.pal.shadow;
      c.beginPath(); c.ellipse(fb.x, fb.y, 10, 5, 0, 0, Math.PI * 2); c.fill();
      if (this.hoverKey === p.key) {
        c.strokeStyle = STATUS_COLOR[s.status] || '#888';
        c.lineWidth = 2;
        c.beginPath(); c.ellipse(fb.x, fb.y, 14, 7, 0, 0, Math.PI * 2); c.stroke();
      }

      let x = b.x;
      const y = b.y - bob;
      if (st.act === 'argue') x += Math.sin(t * 14 + p.phase) * 1.2;
      const sc = s.subagent ? 0.8 : 1;
      c.save();
      c.translate(x, y);
      c.scale(sc, sc);

      c.fillStyle = p.pants;
      if (seated) {
        c.fillRect(-5, -4, 4, 5);
        c.fillRect(1, -4, 4, 5);
      } else {
        c.fillRect(-5, -10, 4, 10 + step * 2);
        c.fillRect(1, -10, 4, 10 - step * 2);
        c.fillStyle = '#2a2a2a';
        c.fillRect(-5, -1 + step, 4, 2);
        c.fillRect(1, -1 - step, 4, 2);
      }
      const hip = seated ? -4 : -10;
      this.roundRect(-7, hip - 13, 14, 14, 3, p.shirt);
      const arm = this.shade(p.shirt, -18);
      const hand = (hx, hy) => { c.fillStyle = p.skin; c.beginPath(); c.arc(hx, hy, 2.2, 0, Math.PI * 2); c.fill(); };
      c.fillStyle = arm;
      const sip = Math.sin(t * 0.9 + p.phase) > 0.6;
      switch (st.act) {
        case 'wave': {
          const wave = Math.sin(t * 8 + p.phase) * 2;
          c.fillRect(6, hip - 26, 3, 14);
          hand(7.5 + wave * 0.5, hip - 27);
          c.fillStyle = arm; c.fillRect(-9, hip - 11, 3, 10);
          break;
        }
        case 'type': {
          const k1 = Math.sin(t * 18 + p.phase) > 0 ? 1 : 0;
          c.fillRect(-9, hip - 11 - k1, 3, 8);
          c.fillRect(6, hip - 11 - (1 - k1), 3, 8);
          break;
        }
        case 'coffee':
        case 'water': {
          c.fillRect(-9, hip - 11, 3, 10);
          c.fillRect(6, sip ? hip - 15 : hip - 11, 3, sip ? 7 : 10);
          const cupY = sip && front ? hip - 19 : hip - 4;
          c.fillStyle = st.act === 'water' ? 'rgba(170,215,255,.95)' : '#fafafa';
          c.fillRect(6, cupY, 5, 6);
          if (st.act === 'coffee') { c.fillStyle = '#6b3f22'; c.fillRect(7, cupY, 3, 1.5); }
          break;
        }
        case 'read': {
          c.fillRect(-8, hip - 11, 3, 7);
          c.fillRect(5, hip - 11, 3, 7);
          if (front) { c.fillStyle = pick(this.pal.book, p.h, 5); c.fillRect(-7, hip - 12, 14, 9); c.fillStyle = '#fff'; c.fillRect(-0.5, hip - 12, 1, 9); }
          break;
        }
        case 'arcade': {
          const k = Math.sin(t * 14 + p.phase);
          c.fillRect(-9, hip - 11, 3, 10);
          c.fillRect(6, hip - 20 + k * 2, 3, 12);
          hand(7.5, hip - 21 + k * 2);
          break;
        }
        case 'phone': {
          c.fillRect(-9, hip - 11, 3, 10);
          c.fillRect(5, hip - 22, 3, 12);
          c.fillStyle = '#222'; c.fillRect(5, hip - 25, 3, 6);
          break;
        }
        case 'scroll': {
          c.fillRect(-6, hip - 11, 3, 7);
          c.fillRect(3, hip - 11, 3, 7);
          if (front) {
            c.fillStyle = '#222'; c.fillRect(-3, hip - 15, 6, 8);
            c.fillStyle = '#8fd3ff'; c.fillRect(-2, hip - 14, 4, 5.5);
          }
          break;
        }
        case 'argue': {
          const k = Math.sin(t * 9 + p.phase) * 3;
          c.fillRect(-9, hip - 23 + k, 3, 13);
          c.fillRect(6, hip - 23 - k, 3, 13);
          hand(-7.5, hip - 24 + k);
          hand(7.5, hip - 24 - k);
          break;
        }
        default: {
          const sw = moving ? step * 2 : 0;
          c.fillRect(-9, hip - 11 + sw, 3, 10);
          c.fillRect(6, hip - 11 - sw, 3, 10);
        }
      }

      const hy = hip - 20 + (st.act === 'scroll' ? 1.5 : 0) + (st.act === 'think' ? Math.sin(t * 1.5 + p.phase) * 0.6 : 0);
      c.fillStyle = p.skin;
      c.beginPath(); c.arc(0, hy, 7, 0, Math.PI * 2); c.fill();
      c.fillStyle = p.hair;
      if (front) {
        c.beginPath(); c.arc(0, hy - 1.5, 7.3, Math.PI * 1.05, Math.PI * 1.95); c.fill();
        if (p.hairStyle === 1) { c.fillRect(-7.3, hy - 2, 2.5, 8); c.fillRect(4.8, hy - 2, 2.5, 8); }
        if (p.hairStyle === 2) { c.beginPath(); c.arc(0, hy - 8, 3.2, 0, Math.PI * 2); c.fill(); }
        c.fillStyle = '#2a2018';
        const sleepy = s.status === 'idle';
        const blink = Math.sin(t * 0.7 + p.phase * 3) > 0.985;
        c.fillRect(-3, hy - 0.5, 1.6, blink || sleepy ? 0.7 : 2);
        c.fillRect(1.6, hy - 0.5, 1.6, blink || sleepy ? 0.7 : 2);
        c.strokeStyle = '#2a2018'; c.lineWidth = 1;
        if (p.mode === 'wander' && !sleepy) {
          c.beginPath(); c.arc(0, hy + 2.2, 2, 0.15 * Math.PI, 0.85 * Math.PI); c.stroke();
        } else if ((s.status === 'error' && p.mode === 'work') || st.act === 'argue') {
          c.beginPath(); c.arc(0, hy + 4.5, 2, 1.15 * Math.PI, 1.85 * Math.PI); c.stroke();
        }
      } else {
        c.beginPath(); c.arc(0, hy - 0.5, 7.2, 0, Math.PI * 2); c.fill();
        if (p.hairStyle === 2) { c.beginPath(); c.arc(0, hy - 7, 3.2, 0, Math.PI * 2); c.fill(); }
        if (p.hairStyle === 1) c.fillRect(-6, hy, 12, 8);
      }
      if (st.act === 'arcade' || st.act === 'beanbag') {
        c.strokeStyle = '#222'; c.lineWidth = 2;
        c.beginPath(); c.arc(0, hy, 8, Math.PI * 1.1, Math.PI * 1.9); c.stroke();
      }
      c.restore();

      const headTop = y + (hy - 8) * sc;
      this.hit.push({ key: p.key, x0: x - 13, x1: x + 13, y0: headTop - 22, y1: fb.y + 4 });
      return { x, y: headTop, act: st.act };
    }

    bubble(p, at) {
      const c = this.ctx;
      const t = this.t / 1000;
      const s = p.s || {};
      const st = s.status;
      const pal = this.pal;
      const x = at.x, y = at.y - 6;
      c.save();
      c.textAlign = 'center';
      c.textBaseline = 'middle';
      const font = (w, px) => `${w} ${px}px ui-sans-serif, system-ui, -apple-system, "PingFang TC", "Apple Color Emoji", sans-serif`;
      c.font = font(600, 10);

      const badge = this.showAgent && AGENT_BADGES[s.agent];
      const pill = (text, fg, bg, yy, alpha = 1, identify = false) => {
        c.globalAlpha = alpha;
        let tx = text;
        while (c.measureText(tx).width > 118 && tx.length > 2) tx = tx.slice(0, -2) + '…';
        const extra = badge && identify ? 26 : 0;
        const w = c.measureText(tx).width + 12 + extra;
        const height = extra ? 22 : 16;
        this.roundRect(x - w / 2, yy - height / 2, w, height, 8, bg);
        c.fillStyle = fg;
        c.fillText(tx, x + extra / 2, yy + 0.5);
        if (extra) this.agentBadge(badge, x - w / 2 + 13, yy);
        c.globalAlpha = 1;
        return yy - 8;
      };
      // Name tag: the label, plus the platform on a smaller second line when
      // the session says where it runs. Grows upward so the bottom edge stays
      // where the one-line pill would sit.
      const tag = (text, sub, yy, alpha) => {
        if (!sub) return pill(text, pal.text, pal.tagBg, yy, alpha, true);
        c.globalAlpha = alpha;
        let tx = text;
        while (c.measureText(tx).width > 118 && tx.length > 2) tx = tx.slice(0, -2) + '…';
        const extra = badge ? 26 : 0;
        const w1 = c.measureText(tx).width;
        c.font = font(500, 8.5);
        const w2 = c.measureText(sub).width;
        c.font = font(600, 10);
        const w = Math.max(w1, w2) + 12 + extra;
        const height = 27, bottom = yy + 8;
        this.roundRect(x - w / 2, bottom - height, w, height, 8, pal.tagBg);
        c.fillStyle = pal.text;
        c.fillText(tx, x + extra / 2, bottom - height + 9.5);
        c.font = font(500, 8.5);
        c.globalAlpha = alpha * 0.75;
        c.fillText(sub, x + extra / 2, bottom - 7);
        c.font = font(600, 10);
        c.globalAlpha = alpha;
        if (extra) this.agentBadge(badge, x - w / 2 + 13, bottom - height / 2);
        c.globalAlpha = 1;
        return bottom - height;
      };

      let top = y;
      const busy = p.mode === 'work' || p.mode === 'booth' || p.mode === 'meet';
      if (at.act === 'argue' && !p.path.length) {
        // Arguing across the table: an anger mark flickers over each head.
        const on = (t * 1.6 + p.phase) % 2 < 1.3;
        c.globalAlpha = on ? 1 : 0.25;
        c.font = font(400, 14);
        c.fillText(Math.sin(t * 0.8 + p.phase) > 0 ? '💢' : '🗯️', x + Math.sin(t * 20) * 1.5, y - 11);
        c.font = font(600, 10);
        c.globalAlpha = 1;
        top = y - 20;
      } else if (busy && !p.path.length) {
        const col = STATUS_COLOR[st] || '#888';
        if (st === 'thinking') {
          c.fillStyle = pal.bubble;
          c.beginPath(); c.arc(x + 6, y - 2, 2, 0, Math.PI * 2); c.fill();
          c.beginPath(); c.arc(x + 10, y - 7, 3, 0, Math.PI * 2); c.fill();
          this.roundRect(x - 2, y - 26, 32, 16, 8, pal.bubble);
          for (let i = 0; i < 3; i++) {
            c.fillStyle = col;
            c.globalAlpha = 0.3 + 0.7 * Math.max(0, Math.sin(t * 5 - i * 0.7));
            c.beginPath(); c.arc(x + 6 + i * 8, y - 18, 2.4, 0, Math.PI * 2); c.fill();
          }
          c.globalAlpha = 1;
          top = y - 26;
        } else if (st === 'typing') {
          this.roundRect(x - 2, y - 24, 34, 16, 5, col);
          c.beginPath(); c.moveTo(x + 2, y - 9); c.lineTo(x - 2, y - 3); c.lineTo(x + 8, y - 9); c.fillStyle = col; c.fill();
          const n = Math.floor(t * 4) % 4;
          c.fillStyle = '#fff';
          for (let i = 0; i < n; i++) { c.beginPath(); c.arc(x + 7 + i * 8, y - 16, 2, 0, Math.PI * 2); c.fill(); }
          top = y - 24;
        } else if (st === 'tool') {
          const tool = (s.activity || 'tool').split(/[:\s]/)[0];
          top = pill('⚙ ' + tool, '#fff', col, y - 12);
        } else if (st === 'waiting' || st === 'error') {
          const jump = Math.abs(Math.sin(t * 4)) * 4;
          c.fillStyle = col;
          c.beginPath(); c.arc(x, y - 12 - jump, 9, 0, Math.PI * 2); c.fill();
          c.fillStyle = '#fff';
          c.font = font(800, 12);
          c.fillText('!', x, y - 11.5 - jump);
          c.font = font(600, 10);
          top = y - 22 - jump;
        }
      } else if (p.mode === 'wander') {
        const justDone = st === 'done' && Date.now() - (s.lastEventAt || 0) < 3 * 60_000;
        const dozing = (st === 'idle' && (at.act === 'couch' || at.act === 'beanbag' || (p.poi && p.poi.sleepy)));
        if (justDone) {
          c.fillStyle = STATUS_COLOR.done;
          c.beginPath(); c.arc(x, y - 10, 7, 0, Math.PI * 2); c.fill();
          c.fillStyle = '#fff'; c.font = font(800, 9); c.fillText('✓', x, y - 9.5); c.font = font(600, 10);
          top = y - 18;
        } else if (dozing && !p.path.length) {
          c.fillStyle = STATUS_COLOR.idle;
          c.font = font(700, 10);
          for (let i = 0; i < 3; i++) {
            const k = (t * 0.6 + i / 3) % 1;
            c.globalAlpha = 1 - k;
            c.fillText('z', x + 6 + k * 10, y - 4 - k * 18);
          }
          c.globalAlpha = 1;
          c.font = font(600, 10);
          top = y - 20;
        } else if (at.act && (ACT_EMOJI[at.act] || (p.poi && p.poi.emoji)) && !p.path.length) {
          const cycle = (t + p.phase * 3) % 9;
          if (cycle < 2.2) {
            c.globalAlpha = Math.min(1, cycle * 3, (2.2 - cycle) * 3);
            c.font = font(400, 13);
            c.fillText((p.poi && p.poi.emoji) || ACT_EMOJI[at.act], x, y - 10);
            c.globalAlpha = 1;
            c.font = font(600, 10);
            top = y - 18;
          }
        }
      }

      const hover = this.hoverKey === p.key;
      if (!s.subagent || hover) {
        tag(this.label(s), this.sublabel(s), top - 11, busy || hover ? 1 : 0.7);
      } else if (badge) {
        this.agentBadge(badge, x, top - 11);
      }
      // Warning chips sit left of the head and never move, apart from the
      // status bubble (above, right of the head).
      let right = x - 12;
      for (const a of s.alerts || []) {
        const text = alertText(a, s);
        if (!text) continue;
        right -= this.chip(text, right, at.y + 2, ALERT_RING, 'right') + 3;
      }
      c.restore();
    }

    agentBadge(badge, x, y) {
      const c = this.ctx;
      c.save();
      c.globalAlpha = 1;
      if (badge.image.complete && badge.image.naturalWidth > 0) {
        c.imageSmoothingEnabled = true;
        c.imageSmoothingQuality = 'high';
        c.drawImage(badge.image, x - 9, y - 9, 18, 18);
      } else {
        c.fillStyle = this.pal.text;
        c.font = '800 10px ui-sans-serif, system-ui, sans-serif';
        c.textAlign = 'center';
        c.textBaseline = 'middle';
        c.fillText(badge.short, x, y + 0.5);
      }
      c.restore();
    }

    moveCat(dt, now) {
      const k = this.cat;
      if (!k.path.length && now > k.until) {
        const { W, D, grid } = this.plan;
        let tx, ty, tries = 0;
        do { tx = rand(0.6, W - 0.6); ty = rand(1, D - 0.6); tries++; } while (grid.blocked(tx, ty) && tries < 30);
        k.path = grid.path(k.x, k.y, tx, ty);
        k.until = Infinity;
      }
      if (k.path.length) {
        const n = k.path[0];
        const dx = n.x - k.x, dy = n.y - k.y, d = Math.hypot(dx, dy), v = 1.2 * dt;
        if (d > 0.001) k.face = dx - dy >= 0 ? 1 : -1;
        if (d <= v) { k.x = n.x; k.y = n.y; k.path.shift(); if (!k.path.length) k.until = now + rand(4000, 12000); }
        else { k.x += (dx / d) * v; k.y += (dy / d) * v; }
      }
    }

    drawCat() {
      const k = this.cat;
      const c = this.ctx;
      const p = this.iso(k.x, k.y, 0);
      const t = this.t / 1000;
      const walking = k.path.length > 0;
      c.save();
      c.translate(p.x, p.y);
      c.scale(k.face, 1);
      c.fillStyle = this.pal.shadow;
      c.beginPath(); c.ellipse(0, 0, 8, 3, 0, 0, Math.PI * 2); c.fill();
      const col = '#e09a4a';
      c.fillStyle = col;
      if (walking) {
        const s = Math.sin(t * 14) * 1.5;
        c.fillRect(-5, -4, 2, 4 + s); c.fillRect(3, -4, 2, 4 - s);
      }
      c.beginPath(); c.ellipse(0, walking ? -6 : -4, 7, walking ? 4 : 5, 0, 0, Math.PI * 2); c.fill();
      c.beginPath(); c.arc(6, walking ? -10 : -9, 3.6, 0, Math.PI * 2); c.fill();
      c.beginPath(); c.moveTo(4, -12); c.lineTo(5, -16); c.lineTo(7, -12.5); c.fill();
      c.beginPath(); c.moveTo(7, -12.5); c.lineTo(9, -15.5); c.lineTo(9.5, -11); c.fill();
      c.strokeStyle = col; c.lineWidth = 2; c.lineCap = 'round';
      c.beginPath(); c.moveTo(-6, -6); c.quadraticCurveTo(-12, -8 + Math.sin(t * 3) * 3, -10, -14); c.stroke();
      c.fillStyle = '#2a2018';
      c.fillRect(7, -10.5, 1.2, walking ? 1.2 : 0.6);
      c.restore();
    }

    // ------------------------------------------------------------ frame

    frame(now) {
      const dt = Math.min(0.05, (now - this.last) / 1000);
      this.last = now;
      this.t = now;
      if (!this.visible) return;
      this.step(dt);
      if (this.draw() !== false) {
        if (!this.first && ++this.framesShown === 3) this.markReady(true);
        // Capture immediately: a WebGL drawing buffer may be cleared after
        // this task. The overlay and the 3D canvas must be copied together.
        if (this.onFrame) this.onFrame(this);
      }
    }

    // Advance people and the cat; shared by the 2D and 3D renderers.
    step(dt) {
      const wall = Date.now();
      for (const [k, p] of this.people) {
        if (p.mode === 'wander') {
          if (p.poi && !p.path.length && p.until === Infinity) p.until = wall + p.stay;
          this.wander(p, wall);
        }
        if (p.path.length) {
          const n = p.path[0];
          const dx = n.x - p.x, dy = n.y - p.y, d = Math.hypot(dx, dy), v = SPEED * dt;
          if (d > 0.001) p.walkFace = dx + dy >= 0 ? 'front' : 'back';
          if (d <= v) { p.x = n.x; p.y = n.y; p.path.shift(); }
          else { p.x += (dx / d) * v; p.y += (dy / d) * v; }
        } else if (p.leaving) {
          this.people.delete(k);
        }
      }
      this.moveCat(dt, wall);
      this.moveBoss(dt, wall);
      if (this.boards.meet.prog == null) this.boards.meet.page = Math.floor(wall / MEET_PAGE_MS) % MEET_PAGES;
      this.renderBoards();
    }

    draw() {
      const c = this.ctx;
      c.setTransform(this.dpr, 0, 0, this.dpr, 0, 0);
      c.clearRect(0, 0, this.cw, this.ch);
      this.fitCamera();
      c.translate(this.cam.ox, this.cam.oy);
      c.scale(this.cam.zoom, this.cam.zoom);

      this.drawRoom();
      const occupant = new Map();
      for (const p of this.people.values()) if (p.mode === 'work' && p.desk) occupant.set(p.desk, p);
      // The one at the hotline desk lights its screen like a worker does.
      for (const p of this.people.values()) if (p.mode === 'booth' && p.spot === this.plan.booth[0] && !p.path.length) occupant.set(this.plan.solo, p);
      const items = [...this.plan.items];
      for (const dk of [...this.plan.desks, this.plan.solo]) {
        const p = occupant.get(dk);
        items.push({ depth: dk.x + 0.7 + dk.y + 0.4, draw: () => this.desk(dk, p) });
        items.push({ depth: dk.seat.x + dk.seat.y - 0.06, draw: () => this.chair(dk, p) });
      }
      for (const p of this.people.values()) items.push({ depth: p.x + p.y + 0.01, person: p });
      items.push({ depth: this.cat.x + this.cat.y, draw: () => this.drawCat() });
      items.push({ depth: this.boss.x + this.boss.y + 0.01, draw: () => this.drawBoss() });
      items.sort((a, b) => a.depth - b.depth);

      this.hit = [];
      this.zoneHits((x, y, z) => this.iso(x, y, z));
      const heads = [];
      for (const it of items) {
        if (it.person) heads.push([it.person, this.person(it.person)]);
        else it.draw();
      }
      if (this.compact) {
        // Keep labels readable when the room is scaled down for PiP.
        c.save();
        c.setTransform(this.dpr, 0, 0, this.dpr, 0, 0);
        for (const [p, at] of heads) this.bubble(p, {
          ...at, x: at.x * this.cam.zoom + this.cam.ox, y: at.y * this.cam.zoom + this.cam.oy,
        });
        c.restore();
      } else {
        for (const [p, at] of heads) this.bubble(p, at);
      }

      if (this.offDuty > 0) {
        const d = this.iso(0.2, this.plan.door.y + 0.9, 0);
        c.font = '600 10px ui-sans-serif, system-ui, -apple-system, "PingFang TC", sans-serif';
        c.textAlign = 'center';
        c.fillStyle = this.pal.text;
        c.globalAlpha = 0.65;
        c.fillText(`+${this.offDuty} ${this.offDutyLabel}`, d.x - 10, d.y + 14);
        c.globalAlpha = 1;
      }

      const z = this.cam.zoom;
      this.hit = this.hit.map((h) => ({
        key: h.key,
        x0: h.x0 * z + this.cam.ox, x1: h.x1 * z + this.cam.ox,
        y0: h.y0 * z + this.cam.oy, y1: h.y1 * z + this.cam.oy,
      }));
    }
  }

  window.Office = Office;
  window.OFFICE_AGENT_BADGES = AGENT_BADGES;
  window.OFFICE_PLATFORM_HAIR = PLATFORM_HAIR;
  window.OFFICE_ALERT_TEXT = alertText;
  window.OfficeGrid = Grid;
})();
