// The waiting room: players, seats, teams, settings and owner controls.
import { h, btn, sheet, closeSheet, field, select } from './ui.js';

const presetNames = { blank: 'Blank table', euchre: 'Euchre', holdem: "Texas Hold'em", golf: 'Six-card Golf', blackjack: 'Blackjack' };
const scoreNames = { solo: 'everyone keeps their own score', team: 'a scorekeeper per team', owner: 'the owner keeps score' };

export function settingsSummary(s) {
  return `${presetNames[s.preset] || s.preset} · up to ${s.maxPlayers} players · ${scoreNames[s.scoreMode]}` +
    (s.policy === 'shared' ? ' · shared piles open to all' : '');
}

export function renderLobby(app) {
  const s = app.state;
  const L = s.lobby;
  const me = app.me();
  const owner = app.isOwner();

  const seats = [];
  for (let i = 0; i < L.settings.maxPlayers; i++) {
    const p = L.players.find((x) => x.seat === i);
    seats.push(seatButton(app, i, p, me, owner));
  }
  const unseated = L.players.filter((p) => p.seat < 0);

  return h('main', { class: 'lobby' },
    h('section', { class: 'panel' },
      h('h1', {}, presetNames[L.settings.preset] || 'Table'),
      h('p', { class: 'muted' }, settingsSummary(L.settings)),
      h('p', {}, 'Others join with code ', h('strong', { class: 'code' }, L.code), ' or the link from the lobby chip above.')),
    h('section', { class: 'panel' },
      h('h2', {}, 'Seats'),
      h('p', { class: 'muted small' }, 'Seats go clockwise. Tap a free seat to sit there.'),
      h('div', { class: 'seats' }, seats),
      unseated.length ? h('p', { class: 'muted' }, 'Not seated: ', unseated.map((p) => p.name).join(', ')) : null,
      me.seat >= 0 ? btn('Stand up', () => app.act('lobby.seat', { seat: -1 }), 'small secondary') : null),
    h('section', { class: 'panel' },
      h('h2', {}, 'Players'),
      h('ul', { class: 'players' }, L.players.map((p) => playerRow(app, p, owner))),
      h('div', { class: 'row' },
        btn(me.ready ? 'Not ready' : "I'm ready", () => app.act('lobby.ready', { ready: !me.ready }), me.ready ? 'secondary' : ''),
        btn('Change name', () => renameSheet(app), 'secondary'))),
    owner ? h('section', { class: 'panel' },
      h('h2', {}, 'Owner'),
      h('div', { class: 'stack' },
        btn('Start the table', () => app.act('table.start'), 'big'),
        btn('Lobby settings', () => settingsSheet(app), 'secondary'))) :
      h('p', { class: 'muted center-text' }, `Waiting for ${app.playerName(L.owner)} to start the table.`));
}

function seatButton(app, i, p, me, owner) {
  const label = p ? p.name + (p.id === me.id ? ' (you)' : '') : 'Empty';
  const cls = 'seat' + (p ? ' taken' : '') + (p && p.id === me.id ? ' mine' : '');
  return h('button', {
    type: 'button', class: cls,
    onclick: () => {
      if (!p) app.act('lobby.seat', { seat: i });
      else if (owner) playerSheet(app, p);
    },
  }, h('span', { class: 'seat-n' }, `Seat ${i + 1}`), h('span', {}, label), p && p.team ? h('span', { class: 'team-tag' }, `Team ${p.team}`) : null);
}

function playerRow(app, p, owner) {
  const L = app.state.lobby;
  return h('li', {},
    h('span', { class: 'dot ' + (p.connected ? 'on' : 'off'), title: p.connected ? 'Connected' : 'Disconnected' }),
    h('span', { class: 'grow' }, p.name, p.id === app.state.you ? ' (you)' : '', p.id === L.owner ? ' · owner' : ''),
    p.team ? h('span', { class: 'team-tag' }, `Team ${p.team}`) : null,
    p.ready ? h('span', { class: 'ready' }, 'ready') : null,
    owner ? btn('Manage', () => playerSheet(app, p), 'small secondary') : null);
}

function playerSheet(app, p) {
  const L = app.state.lobby;
  const seatOpts = [[-1, 'Not seated']];
  for (let i = 0; i < L.settings.maxPlayers; i++) {
    const holder = L.players.find((x) => x.seat === i);
    seatOpts.push([i, `Seat ${i + 1}` + (holder && holder.id !== p.id ? ` (swap with ${holder.name})` : '')]);
  }
  const seatSel = select(seatOpts, p.seat);
  const teamSel = select([['', 'No team'], ...L.teams.map((t) => [t, `Team ${t}`])], p.team);
  const items = [
    field('Seat', seatSel),
    btn('Move', async () => { if ((await app.act('lobby.seat', { player: p.id, seat: Number(seatSel.value) })).ok) closeSheet(); }),
    field('Team', teamSel),
    btn('Set team', async () => { if ((await app.act('lobby.team', { player: p.id, team: teamSel.value })).ok) closeSheet(); }),
  ];
  if (p.id !== app.state.you) {
    items.push(btn('Make lobby owner', async () => { if ((await app.act('lobby.owner', { player: p.id })).ok) closeSheet(); }, 'secondary'));
    items.push(btn('Remove from lobby', async () => { if ((await app.act('lobby.kick', { player: p.id })).ok) closeSheet(); }, 'danger'));
  }
  sheet(p.name, h('div', { class: 'stack' }, items));
}

function renameSheet(app) {
  const input = h('input', { value: app.me().name, maxlength: 20 });
  sheet('Your name', h('div', { class: 'stack' }, field('Name', input),
    btn('Save', async () => { if ((await app.act('lobby.rename', { name: input.value })).ok) closeSheet(); })));
}

function settingsSheet(app) {
  const s = app.state.lobby.settings;
  const preset = select(Object.entries(presetNames), s.preset);
  const max = select([2, 3, 4, 5, 6, 7, 8].map((n) => [n, String(n)]), s.maxPlayers);
  const score = select([['team', 'Team scorekeepers'], ['owner', 'Owner only'], ['solo', 'Everyone their own']], s.scoreMode);
  const policy = select([['strict', 'Dealer runs the deck'], ['shared', 'Anyone manages shared piles']], s.policy);
  const deck = select([['standard52', 'Standard 52'], ['standard54', 'Standard 54 (jokers)'], ['euchre24', 'Euchre 24'], ['color108', 'Color deck (108)']], s.deckType || 'standard52');
  const decks = select([1, 2, 3, 4, 5, 6, 7, 8].map((n) => [n, String(n)]), s.deckCount);
  const rows = select([0, 1, 2, 3, 4].map((n) => [n, n ? String(n) : 'None']), s.gridRows);
  const cols = select([0, 1, 2, 3, 4, 5, 6].map((n) => [n, n ? String(n) : 'None']), s.gridCols);
  const blind = h('input', { type: 'checkbox', checked: s.blindDraw });
  sheet('Lobby settings', h('div', { class: 'stack' },
    field('Game', preset),
    field('Most players', max, 'Euchre is always 4.'),
    field('Who changes scores', score),
    field('Shared piles', policy),
    field('Deck (blank table, blackjack)', deck),
    field('Number of decks', decks),
    h('div', { class: 'row' }, field('Grid rows', rows), field('Grid columns', cols)),
    h('label', { class: 'check' }, blind, ' Allow blind draws from other hands'),
    btn('Save', async () => {
      const r = await app.act('lobby.settings', {
        preset: preset.value, maxPlayers: Number(max.value), scoreMode: score.value, policy: policy.value,
        deckType: deck.value, deckCount: Number(decks.value), gridRows: Number(rows.value), gridCols: Number(cols.value),
        blindDraw: blind.checked, custom: s.custom,
      });
      if (r.ok) closeSheet();
    })));
}
