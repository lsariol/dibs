// The realtime connection: one WebSocket, request IDs, resending after a
// reconnect, and a fresh snapshot from the server every time it changes.

function newID() {
  if (window.crypto && crypto.randomUUID) return crypto.randomUUID();
  const b = new Uint8Array(16);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
}

export class Net {
  constructor(code, { onState, onEnded, onStatus }) {
    this.code = code;
    this.onState = onState;
    this.onEnded = onEnded;
    this.onStatus = onStatus;
    this.rev = 0;
    this.pending = new Map(); // id -> {msg, resolve}
    this.ws = null;
    this.ended = false;
    this.failures = 0;
    this.timer = null;
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') this.reconnectNow();
    });
    window.addEventListener('online', () => this.reconnectNow());
    window.addEventListener('pageshow', (e) => { if (e.persisted) this.reconnectNow(); });
  }

  connect() {
    if (this.ended) return;
    clearTimeout(this.timer);
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${location.host}/l/${this.code}/ws`);
    this.ws = ws;
    this.onStatus('connecting');
    ws.onopen = () => {
      this.failures = 0;
      this.onStatus('online');
      for (const p of this.pending.values()) ws.send(p.msg);
    };
    ws.onmessage = (ev) => {
      let m;
      try { m = JSON.parse(ev.data); } catch { return; }
      if (m.type === 'state') {
        if (m.rev >= this.rev) {
          this.rev = m.rev;
          this.onState(m);
        }
      } else if (m.type === 'result') {
        const p = this.pending.get(m.id);
        if (p) {
          this.pending.delete(m.id);
          p.resolve(m);
        }
      } else if (m.type === 'ended') {
        this.end(m.reason);
      }
    };
    ws.onclose = () => {
      if (this.ws !== ws || this.ended) return;
      this.ws = null;
      this.onStatus('offline');
      this.failures++;
      if (this.failures >= 3) this.checkSession();
      const delay = Math.min(10000, 500 * 2 ** Math.min(this.failures, 5));
      this.timer = setTimeout(() => this.connect(), delay);
    };
  }

  reconnectNow() {
    if (this.ended) return;
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) return;
    this.connect();
  }

  // checkSession asks whether we still belong to the lobby, so a removed
  // player or an ended lobby stops retrying.
  async checkSession() {
    try {
      const r = await fetch(`/l/${this.code}/me`, { cache: 'no-store' });
      if (r.status === 404) this.end('This lobby has ended.');
      else if (r.status === 403) this.end('You are no longer in this lobby.');
    } catch { /* offline: keep retrying */ }
  }

  end(reason) {
    this.ended = true;
    clearTimeout(this.timer);
    for (const p of this.pending.values()) p.resolve({ ok: false, error: { code: 'ended', message: reason } });
    this.pending.clear();
    if (this.ws) this.ws.close();
    this.onEnded(reason || 'The lobby has ended.');
  }

  // send asks the server for an action; it resolves with the server's result.
  send(action, params = {}) {
    if (this.ended) return Promise.resolve({ ok: false, error: { code: 'ended', message: 'The lobby has ended.' } });
    const id = newID();
    const msg = JSON.stringify({ id, rev: this.rev, action, params });
    return new Promise((resolve) => {
      this.pending.set(id, { msg, resolve });
      if (this.ws && this.ws.readyState === WebSocket.OPEN) this.ws.send(msg);
    });
  }
}
