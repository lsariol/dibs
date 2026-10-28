// The scoreboard: scores, quick adjustments, history and corrections.
import { h, btn, sheet, field, select, numberInput, confirmThen, liveSheet } from './ui.js';

const modeText = {
  solo: 'Solo: everyone changes only their own score.',
  team: 'Team: each team\'s scorekeeper changes only that team\'s score.',
  owner: 'Owner: only the lobby owner changes scores.',
};

// entryName names a scoreboard line: a player, or a team with its members.
export function entryName(app, id) {
  if (id.startsWith('team:')) {
    const t = id.slice(5);
    const members = app.state.lobby.players.filter((p) => p.team === t).map((p) => p.name);
    return `Team ${t}` + (members.length ? ` (${members.join(' & ')})` : '');
  }
  return app.playerName(id);
}

// scoreStrip is the compact line of scores on the table.
export function scoreStrip(app) {
  const s = app.state.score;
  if (!s || !s.entries || !s.entries.length) return null;
  s.leaders = s.leaders || [];
  s.history = s.history || [];
  const parts = s.entries.map((e) => h('span', { class: 'score-chip' + (s.leaders.includes(e.id) ? ' lead' : '') },
    e.id.startsWith('team:') ? 'Team ' + e.id.slice(5) : app.playerName(e.id), ' ', h('strong', {}, String(e.score))));
  return h('button', { type: 'button', class: 'scores', onclick: () => openScoreboard(app), 'aria-label': 'Open the scoreboard' },
    parts, s.target ? h('span', { class: 'muted small' }, ` to ${s.target}`) : null);
}

export function openScoreboard(app) {
  const s = app.state.score;
  if (!s) return;
  s.leaders = s.leaders || [];
  s.history = s.history || [];
  const canEdit = new Set(s.canEdit);
  const rows = s.entries.map((e) => {
    const editable = canEdit.has(e.id);
    return h('li', { class: 'score-row' },
      h('span', { class: 'grow' }, entryName(app, e.id)),
      h('strong', { class: 'score-n' }, String(e.score)),
      editable ? h('span', { class: 'row' },
        btn('−1', () => app.act('score.add', { entry: e.id, delta: -1 }), 'small secondary'),
        btn('+1', () => app.act('score.add', { entry: e.id, delta: 1 }), 'small'),
        btn('…', () => customSheet(app, e.id), 'small secondary')) : null);
  });
  const leaders = s.leaders.length ? h('p', { class: 'win' }, `${entryName(app, s.leaders[0])} reached ${s.target}!`) : null;
  const history = [...s.history].reverse().map((c) => h('li', {},
    h('span', { class: 'grow' }, `${c.delta > 0 ? '+' : ''}${c.delta} ${entryName(app, c.entry)}`,
      c.reason ? ` · ${c.reason}` : '', ` · by ${app.playerName(c.by)}`,
      c.revertedBy ? h('span', { class: 'muted' }, ' (corrected)') : null),
    canEdit.has(c.entry) && !c.revertedBy && !c.reverts ? btn('Undo this', () => app.act('score.revert', { change: c.id }), 'small secondary') : null));
  const keepers = s.mode === 'team' ? h('p', { class: 'muted small' }, 'Scorekeepers: ',
    Object.entries(s.keepers).map(([t, p]) => `team ${t}: ${app.playerName(p)}`).join(', ') || 'none') : null;
  const owner = app.isOwner() ? h('details', {}, h('summary', {}, 'Owner controls'), ownerControls(app)) : null;
  sheet('Scoreboard', h('div', { class: 'stack' },
    h('p', { class: 'muted small' }, modeText[s.mode], s.target ? ` First to ${s.target}.` : ''),
    leaders,
    h('ul', { class: 'score-list' }, rows),
    keepers,
    owner,
    history.length ? h('div', {}, h('h3', {}, 'History'), h('ul', { class: 'log' }, history)) : null));
  liveSheet(() => openScoreboard(app));
}

function customSheet(app, entry) {
  const n = numberInput(1, -1000, 1000);
  const reason = h('input', { maxlength: 60, placeholder: 'optional, e.g. euchred' });
  sheet(`Change ${entryName(app, entry)}`, h('div', { class: 'stack' },
    field('Points (negative to take away)', n), field('Reason', reason),
    btn('Record', async () => {
      const r = await app.act('score.add', { entry, delta: Number(n.value), reason: reason.value });
      if (r.ok) openScoreboard(app);
    })));
}

function ownerControls(app) {
  const s = app.state.score;
  const target = numberInput(s.target || 0, 0, 100000);
  const items = [field('Target score (0 for none)', target), btn('Set target', () => app.act('score.target', { target: Number(target.value) }), 'secondary')];
  if (s.mode === 'team') {
    for (const e of s.entries) {
      const t = e.id.slice(5);
      const members = app.state.lobby.players.filter((p) => p.team === t);
      const sel = select(members.map((p) => [p.id, p.name]), s.keepers[t]);
      items.push(field(`Team ${t} scorekeeper`, sel), btn('Appoint', () => app.act('score.keeper', { team: t, player: sel.value }), 'secondary'));
    }
  }
  items.push(btn('Reset all scores to 0', () => confirmThen('Reset every score to 0 and clear the score history?', () => app.act('score.reset')), 'danger'));
  return h('div', { class: 'stack' }, items);
}
