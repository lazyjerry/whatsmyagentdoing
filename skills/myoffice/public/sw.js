// Service worker: one clock offset shared by every dashboard window and tab.
// A page posts {type: 'time'} and gets {type: 'time', offset, now} back, where
// offset is the host's clock (/api/time) minus this device's, measured with
// half the round trip removed and refreshed at most every five minutes.
// There is no fetch handler: SSE, the API and the page go straight to the
// server, and the worker never serves anything stale.
const REFRESH_MS = 5 * 60 * 1000;
let offset = 0;
let syncedAt = 0;

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()));

async function sync() {
  const t0 = Date.now();
  const r = await fetch('/api/time', { cache: 'no-store' });
  if (!r.ok) throw new Error(String(r.status));
  const { now } = await r.json();
  const t1 = Date.now();
  offset = now - (t0 + (t1 - t0) / 2);
  syncedAt = t1;
}

self.addEventListener('message', (e) => {
  if (e.data?.type !== 'time' || !e.source) return;
  e.waitUntil((async () => {
    if (Date.now() - syncedAt > REFRESH_MS) {
      try { await sync(); } catch { /* keep the last offset */ }
    }
    e.source.postMessage({ type: 'time', offset, now: Date.now() + offset });
  })());
});
