// Records the README GIFs from the --demo dashboard: Chrome's screencast
// gives PNG frames with timestamps, ffmpeg crops one element out of them,
// resamples to 10 fps and writes an 800 px wide GIF. Run via record-gifs.sh.
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn, spawnSync } = require('node:child_process');

const root = process.argv[2] || path.join(__dirname, '..');
const skill = path.join(root, 'skills', 'myoffice');
const { chromium } = require(require.resolve('playwright', { paths: [skill] }));
const binary = path.join(skill, 'bin', process.platform === 'win32' ? 'whatsmyagentdoing.exe' : 'whatsmyagentdoing');
const out = path.join(root, 'docs');
const FPS = 10, WIDTH = 800, SETTLE_MS = 6000;
// What to record: the page's ?quality, the element to crop, how long, and
// for the list a fixed aspect (the old GIF's 800 × 564).
const SHOTS = [
  { file: 'office-3d.gif', quality: 'high', el: '#office', seconds: 8 },
  { file: 'office-2d.gif', quality: 'mid', el: '#office', seconds: 8 },
  { file: 'list.gif', quality: 'mid', el: '#main', seconds: 6, aspect: 564 / 800, pad: 8 },
];

async function record(browser, base, shot, tmp) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 960 }, locale: 'en-US', colorScheme: 'light' });
  const page = await context.newPage();
  await page.goto(`${base}/?quality=${shot.quality}&lang=en`);
  await page.waitForFunction(() => typeof office !== 'undefined' && office?.framesShown > 4, null, { timeout: 30000 });
  if (shot.el === '#main') await page.locator('#main section').first().waitFor();
  await page.evaluate((sel) => document.querySelector(sel).scrollIntoView({ block: 'start' }), shot.el);
  await page.waitForTimeout(SETTLE_MS);
  const box = await page.evaluate(([sel, pad]) => {
    const r = document.querySelector(sel).getBoundingClientRect();
    const x = Math.max(0, r.left - pad), y = Math.max(0, r.top - pad);
    return { x, y, w: Math.min(r.right + pad, innerWidth) - x, h: Math.min(r.bottom + pad, innerHeight) - y };
  }, [shot.el, shot.pad || 0]);
  if (shot.aspect) box.h = Math.min(box.h, Math.round(box.w * shot.aspect));

  const dir = fs.mkdtempSync(path.join(tmp, shot.file.replace('.gif', '-')));
  const frames = [];
  const cdp = await context.newCDPSession(page);
  cdp.on('Page.screencastFrame', ({ data, metadata, sessionId }) => {
    const name = path.join(dir, `f${String(frames.length).padStart(5, '0')}.png`);
    fs.writeFileSync(name, Buffer.from(data, 'base64'));
    frames.push({ name, t: metadata.timestamp });
    cdp.send('Page.screencastFrameAck', { sessionId }).catch(() => {});
  });
  await cdp.send('Page.startScreencast', { format: 'png', everyNthFrame: 1 });
  await page.waitForTimeout(shot.seconds * 1000 + 500);
  await cdp.send('Page.stopScreencast');
  await context.close();
  if (frames.length < 2) throw new Error(`${shot.file}: no frames captured`);

  // Each frame lasts until the next one; the last gets one GIF frame.
  const list = frames.map((f, i) => {
    const d = i + 1 < frames.length ? frames[i + 1].t - f.t : 1 / FPS;
    return `file '${f.name}'\nduration ${d.toFixed(4)}`;
  }).join('\n') + `\nfile '${frames[frames.length - 1].name}'\n`;
  fs.writeFileSync(path.join(dir, 'frames.txt'), list);
  const crop = [box.w, box.h, box.x, box.y].map((v) => Math.round(v)).join(':');
  const filter = `fps=${FPS},crop=${crop},scale=${WIDTH}:-2:flags=lanczos,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle`;
  const r = spawnSync('ffmpeg', ['-v', 'error', '-y', '-f', 'concat', '-safe', '0', '-i', path.join(dir, 'frames.txt'),
    '-t', String(shot.seconds), '-vf', filter, '-loop', '0', path.join(out, shot.file)], { stdio: 'inherit' });
  if (r.status !== 0) throw new Error(`${shot.file}: ffmpeg failed`);
  const span = frames[frames.length - 1].t - frames[0].t;
  console.log(`${shot.file}: ${frames.length} frames over ${span.toFixed(1)} s (${(frames.length / span).toFixed(1)} fps captured), crop ${crop}`);
}

(async () => {
  if (!fs.existsSync(binary)) throw new Error(`missing ${binary}: run scripts/build.sh && scripts/installSkill.sh`);
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'wmad-gifs-'));
  const server = spawn(binary, ['--demo', '--no-open', '--port', String(40000 + Math.floor(Math.random() * 20000)), '--tv-dir', path.join(tmp, 'no-tv')],
    { stdio: ['ignore', 'pipe', 'inherit'], env: { ...process.env, WMAD_NO_UPDATE_CHECK: '1' } });
  let browser;
  try {
    const base = await new Promise((resolve, reject) => {
      let text = '';
      server.stdout.on('data', (d) => { text += d; const m = text.match(/http:\/\/localhost:(\d+)/); if (m) resolve(`http://127.0.0.1:${m[1]}`); });
      server.once('exit', (code) => reject(new Error(`dashboard exited before listening (${code})`)));
    });
    browser = await chromium.launch({ headless: process.env.WMAD_HEADED !== '1', channel: 'chrome', args: ['--window-size=1280,960'] });
    for (const shot of SHOTS) await record(browser, base, shot, tmp);
  } finally {
    await browser?.close();
    server.kill();
    fs.rmSync(tmp, { recursive: true, force: true });
  }
})().catch((e) => { console.error(e.message || e); process.exitCode = 1; });
