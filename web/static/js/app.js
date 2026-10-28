// Entry point: connects, keeps the newest snapshot and redraws on change.
import { Net } from './net.js';
import { h, btn, toast, sheet, closeSheet, confirmThen } from './ui.js';
import { renderLobby, settingsSummary } from './lobby.js';
import { renderTable } from './table.js';

const root = document.getElementById('app');

export const app = {
  code: root.dataset.code,
  state: null,
  status: 'connecting',
  ended: null,
  net: null,
  // act sends an action and shows its error, if any; it resolves with the result.
  async act(action, params = {}) {
    const r = await app.net.send(action, params);
    if (!r.ok && r.error && r.error.code !== 'ended') toast(r.error.message, 'error');
    return r;
  },
  me() { return app.state && app.state.lobby.players.find((p) => p.id === app.state.you); },
  isOwner() { return app.state && app.state.lobby.owner === app.state.you; },
  player(id) { return app.state && app.state.lobby.players.find((p) => p.id === id); },
  playerName(id) {
    const p = app.player(id);
    return p ? p.name : 'someone';
  },
  render,
};

app.net = new Net(app.code, {
  onState(s) { app.state = s; render(); },
  onEnded(reason) { app.ended = reason; closeSheet(); render(); },
  onStatus(st) { app.status = st; renderStatus(); },
});
app.net.connect();

function render() {
  if (app.ended) {
    root.replaceChildren(h('main', { class: 'page' },
      h('a', { class: 'home', href: '/' }, 'dibs'),
      h('h1', {}, 'Session over'),
      h('p', {}, app.ended),
      h('a', { class: 'btn big', href: '/' }, 'Back to start')));
    return;
  }
  const s = app.state;
  if (!s) return;
  const body = s.lobby.state === 'playing' && s.table ? renderTable(app) : renderLobby(app);
  root.replaceChildren(header(s), body);
  renderStatus();
}

function header(s) {
  const players = s.lobby.players;
  const online = players.filter((p) => p.connected).length;
  return h('header', { class: 'top' },
    h('button', { type: 'button', class: 'code-chip', onclick: share, 'aria-label': 'Share lobby link' },
      h('span', { class: 'muted' }, 'Lobby '), h('strong', {}, s.lobby.code)),
    h('span', { class: 'top-info' }, `${online}/${players.length} online`),
    h('span', { id: 'conn', class: 'conn', title: '' }),
    btn('Menu', openMenu, 'small secondary'));
}

function renderStatus() {
  const el = document.getElementById('conn');
  if (!el) return;
  el.className = 'conn ' + app.status;
  el.title = app.status === 'online' ? 'Connected' : app.status === 'connecting' ? 'Connecting…' : 'Reconnecting…';
  el.textContent = app.status === 'online' ? '' : 'Reconnecting…';
}

async function share() {
  const url = app.state.lobby.url;
  if (navigator.share) {
    try { await navigator.share({ title: 'dibs', text: `Join my table: ${app.state.lobby.code}`, url }); return; } catch { /* cancelled */ }
  }
  try {
    await navigator.clipboard.writeText(url);
    toast('Link copied');
  } catch {
    sheet('Share this lobby', h('p', {}, 'Code: ', h('strong', { class: 'code' }, app.state.lobby.code)), h('p', { class: 'wrap' }, url));
  }
}

function openMenu() {
  const s = app.state;
  const items = [
    h('p', { class: 'muted' }, settingsSummary(s.lobby.settings)),
    btn('Share link', () => { closeSheet(); share(); }, 'secondary'),
    btn('History', () => openLog(), 'secondary'),
  ];
  if (s.score) items.push(btn('Scoreboard', () => import('./score.js').then((m) => m.openScoreboard(app)), 'secondary'));
  if (app.isOwner() && s.lobby.state === 'playing') {
    items.push(btn('Back to the lobby', () => confirmThen('Return everyone to the lobby? The table is cleared; scores are kept.', () => app.act('table.stop')), 'secondary'));
  }
  items.push(btn('Leave lobby', () => confirmThen('Leave this lobby? Your seat and cards are given up.', () => app.act('lobby.leave')), 'danger'));
  if (app.isOwner()) items.push(btn('End lobby for everyone', () => confirmThen('End the lobby for everyone?', () => app.act('lobby.end')), 'danger'));
  sheet('Menu', h('div', { class: 'stack' }, items));
}

function openLog() {
  const log = [...app.state.log].reverse();
  sheet('History', h('ol', { class: 'log' }, log.map((e) =>
    h('li', {}, h('time', {}, new Date(e.time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })), ' ', e.text))));
}
