// The card table: shared piles, the other players, your own area, and the
// sheets of actions you get by tapping a card or a pile.
import { h, btn, sheet, closeSheet, field, select, numberInput, toast, confirmThen } from './ui.js';
import { cardEl, label } from './cards.js';
import { guideBar } from './guide.js';

let app;
let T;

export function renderTable(a) {
  app = a;
  T = app.state.table;
  for (const c of T.containers) c.cards = c.cards || [];
  const me = app.state.you;
  const shared = T.containers.filter((c) => !c.owner);
  const mine = T.containers.filter((c) => c.owner === me);
  const seatedMe = T.seats.includes(me);

  return h('main', { class: 'table' },
    banners(),
    playersStrip(),
    guideBar(app),
    h('section', { class: 'felt', 'aria-label': 'Shared table' }, shared.map(containerEl)),
    othersEl(),
    seatedMe ? h('section', { class: 'mine', 'aria-label': 'Your cards' }, mine.map(containerEl)) :
      h('p', { class: 'muted center-text' }, 'You are watching. The owner can seat you from the lobby.'),
    actionBar());
}

// ---------- helpers ----------

function name(id) { return id === app.state.you ? 'You' : app.playerName(id); }

export function containerName(c) {
  if (!c.owner) return c.label;
  if (c.owner === app.state.you) return 'Your ' + c.label.toLowerCase();
  return `${app.playerName(c.owner)}'s ${c.label.toLowerCase()}`;
}

function byID(id) { return T.containers.find((c) => c.id === id); }

function refOf(c, cv) { return cv.id ? { container: c.id, card: cv.id, index: cv.i } : { container: c.id, index: cv.i }; }

export function myHand() { return T.containers.find((c) => c.kind === 'hand' && c.owner === app.state.you); }

async function run(action, params, done = true) {
  const r = await app.act(action, params);
  if (r.ok && done) closeSheet();
  return r;
}

// ---------- banners ----------

function banners() {
  const me = app.state.you;
  const out = [];
  const o = T.cutOffer;
  if (o) {
    const pile = byID(o.container);
    if (o.to === me) {
      out.push(h('div', { class: 'banner attention', role: 'status' },
        h('span', { class: 'grow' }, `${name(o.by)} offers you the cut of ${pile ? containerName(pile) : 'the deck'}.`),
        btn('Cut', () => cutSheet(pile, true), 'small'),
        btn('Decline', () => run('cut.answer', { count: 0 }), 'small secondary')));
    } else {
      out.push(h('div', { class: 'banner', role: 'status' },
        h('span', { class: 'grow' }, `Waiting for ${name(o.to)} to cut or decline.`),
        o.by === me || app.isOwner() ? btn('Withdraw', () => run('cut.cancel'), 'small secondary') : null));
    }
  }
  const d = T.dealing;
  if (d) {
    const next = d.nextTo ? byID(d.nextTo) : null;
    if (d.by === me) {
      out.push(h('div', { class: 'banner attention', role: 'status' },
        h('span', { class: 'grow' }, `Dealing ${d.dealt}/${d.total}` + (next ? ` · next to ${containerName(next)}` : '')),
        btn('Next packet', () => run('deal.step', { all: false }), 'small'),
        btn('Deal the rest', () => run('deal.step', { all: true }), 'small secondary'),
        btn('Stop', () => run('deal.cancel'), 'small danger')));
    } else {
      out.push(h('div', { class: 'banner', role: 'status' }, `${name(d.by)} is dealing: ${d.dealt}/${d.total}.`));
    }
  }
  if (app.state.undo) {
    out.push(h('div', { class: 'banner' },
      h('span', { class: 'grow small' }, 'Last: ' + app.state.undo),
      btn('Undo', () => run('action.undo'), 'small secondary')));
  }
  return out;
}

// ---------- players ----------

function playersStrip() {
  return h('ol', { class: 'strip', 'aria-label': 'Seats, clockwise' }, T.seats.map((id) => {
    const p = app.player(id);
    const badges = [];
    if (T.dealer === id) badges.push(h('span', { class: 'badge dealer', title: 'Dealer' }, 'D'));
    if (T.turn === id) badges.push(h('span', { class: 'badge turn', title: 'Their turn' }, '▶'));
    return h('li', { class: 'strip-p' + (id === app.state.you ? ' me' : '') + (T.turn === id ? ' turn' : '') },
      h('span', { class: 'dot ' + (p && p.connected ? 'on' : 'off') }),
      h('span', { class: 'strip-name' }, p ? p.name : 'gone'),
      p && p.team ? h('span', { class: 'team-tag' }, p.team) : null,
      badges);
  }));
}

function othersEl() {
  const me = app.state.you;
  const idx = T.seats.indexOf(me);
  const order = idx < 0 ? T.seats : [...T.seats.slice(idx + 1), ...T.seats.slice(0, idx)];
  return h('section', { class: 'others', 'aria-label': 'Other players' }, order.map((pid) => {
    const cs = T.containers.filter((c) => c.owner === pid);
    if (!cs.length) return null;
    return h('div', { class: 'other' },
      h('div', { class: 'other-name' }, app.playerName(pid), T.dealer === pid ? h('span', { class: 'badge dealer' }, 'D') : null),
      cs.map(containerEl));
  }));
}

// ---------- containers ----------

function containerEl(c) {
  const mine = c.owner === app.state.you;
  const head = h('div', { class: 'c-head' },
    h('button', { type: 'button', class: 'c-label', onclick: () => pileSheet(c) },
      containerName(c), h('span', { class: 'count' }, ` ${c.count}`)),
    mine && c.kind === 'hand' ? h('span', { class: 'muted small' }, 'only you see these') : null);

  let body;
  if (c.kind === 'stack' && !c.spread) {
    const top = c.cards[0];
    body = h('div', { class: 'pile' },
      top ? cardEl(top, { onclick: () => cardSheet(c, top) }) : h('button', { type: 'button', class: 'card empty', onclick: () => pileSheet(c), 'aria-label': 'empty pile' }),
      c.count > 1 ? h('span', { class: 'depth', 'aria-hidden': 'true' }) : null);
  } else if (c.kind === 'grid') {
    body = h('div', { class: 'grid', style: `--cols:${c.cols || 3}` },
      c.cards.map((cv) => cardEl(cv, { onclick: () => (cv.empty ? pileSheet(c, cv.i) : cardSheet(c, cv)) })));
  } else if (c.kind === 'hand' && !mine) {
    body = h('div', { class: 'row-cards compact' },
      c.cards.map((cv) => cardEl(cv, { small: true, onclick: () => (cv.def ? cardSheet(c, cv) : blindSheet(c)) })));
  } else {
    body = h('div', { class: 'row-cards' + (c.kind === 'hand' ? ' hand' : '') },
      c.cards.length ? c.cards.map((cv) => cardEl(cv, { onclick: () => cardSheet(c, cv) })) :
        h('button', { type: 'button', class: 'card empty', onclick: () => pileSheet(c), 'aria-label': 'empty' }));
  }
  return h('div', { class: `container k-${c.kind}` + (mine ? ' own' : '') }, head, body);
}

// ---------- card actions ----------

function cardSheet(c, cv) {
  const ref = refOf(c, cv);
  const items = [];
  if (c.can.take) {
    const quick = T.containers.filter((d) => d.id !== c.id && !d.owner && d.can.place && d.kind !== 'grid');
    for (const d of quick) {
      const face = d.faceUp ? 'up' : 'down';
      items.push(btn(`${d.faceUp ? 'Play' : 'Put'} on ${d.label}` + (d.faceUp ? '' : ' (face down)'), () => run('card.move', { ref, to: d.id, face })));
    }
    const hand = myHand();
    if (hand && hand.id !== c.id) items.push(btn('Take into your hand', () => run('card.move', { ref, to: hand.id })));
    items.push(btn('Move to…', () => moveSheet(c, cv, ref), 'secondary'));
    if (c.kind !== 'stack' || c.spread || cv === c.cards[0]) {
      items.push(btn(cv.up ? 'Turn face down' : 'Turn face up', () => run('card.flip', { ref }), 'secondary'));
    }
    if (c.kind === 'hand' && c.owner === app.state.you && c.cards.length > 1) {
      const i = c.cards.indexOf(cv);
      items.push(h('div', { class: 'row' },
        btn('◀ Left', () => run('card.move', { ref, to: c.id, index: Math.max(0, i - 1) }), 'secondary'),
        btn('Right ▶', () => run('card.move', { ref, to: c.id, index: i + 1 }), 'secondary')));
    }
  }
  if (c.can.peek && !cv.def) items.push(btn('Peek (only you see it)', () => run('card.peek', { ref }), 'secondary'));
  if (!items.length) items.push(h('p', { class: 'muted' }, cv.def ? 'You can look, but this card isn\'t yours to move.' : 'This card is face down and not yours to touch.'));
  sheet(cv.def ? label(cv.def) + ' · ' + containerName(c) : 'Card in ' + containerName(c), h('div', { class: 'stack' }, items));
}

// moveSheet lists every place the card can go, grid slots included.
function moveSheet(c, cv, ref) {
  const face = select([['', 'Usual for that place'], ['up', 'Face up (everyone sees)'], ['down', 'Face down']], '');
  const dests = [];
  const discard = T.containers.find((d) => !d.owner && d.kind === 'stack' && d.faceUp && d.can.place);
  for (const d of T.containers) {
    if (!d.can.place) continue;
    if (d.kind === 'grid') {
      d.cards.forEach((slot, i) => {
        if (d.id === c.id && i === cv.i) return;
        if (slot.empty) {
          dests.push(btn(`${containerName(d)}: slot ${i + 1}`, () => run('card.move', { ref, to: d.id, index: i, face: face.value }), 'secondary'));
        } else if (d.owner === app.state.you && discard) {
          dests.push(btn(`${containerName(d)}: swap slot ${i + 1} (old card to ${discard.label})`,
            () => run('card.move', { ref, to: d.id, index: i, face: face.value || 'up', displaceTo: discard.id }), 'secondary'));
        }
      });
      continue;
    }
    if (d.id === c.id && d.kind !== 'stack') continue;
    const what = d.kind === 'hand' && d.owner && d.owner !== app.state.you ? `Give to ${app.playerName(d.owner)}` : containerName(d);
    dests.push(btn(what + (d.id === c.id ? ' (to the top)' : ''), () => run('card.move', { ref, to: d.id, face: face.value }), 'secondary'));
    if (d.kind === 'stack' && d.count > 0 && d.id !== c.id) {
      dests.push(btn(`${containerName(d)} (to the bottom)`, () => run('card.move', { ref, to: d.id, index: 0, face: face.value }), 'secondary small'));
    }
  }
  sheet('Move to…', h('div', { class: 'stack' }, field('Face', face), dests));
}

function blindSheet(c) {
  if (!T.blindDraw) {
    sheet(containerName(c), h('p', {}, `${app.playerName(c.owner)} has ${c.count} card${c.count === 1 ? '' : 's'}. Only they can see them.`));
    return;
  }
  sheet(`Draw blind from ${app.playerName(c.owner)}`,
    h('p', { class: 'muted' }, 'Pick a card back. Nobody sees which card it is except you, once it\'s in your hand.'),
    h('div', { class: 'row-cards' }, c.cards.map((cv) => cardEl(cv, { onclick: () => run('card.blind_draw', { from: c.id, index: cv.i }) }))),
    btn('Pick one at random', () => run('card.blind_draw', { from: c.id, index: -1 }), 'secondary'));
}

// ---------- pile actions ----------

function pileSheet(c) {
  const items = [];
  const hand = myHand();
  const top = c.cards.length ? c.cards[c.cards.length - 1] : null;
  if (c.count > 0 && c.can.take && c.kind !== 'hand') {
    if (hand) items.push(btn('Draw the top card', () => run('card.draw', { from: c.id, count: 1 })));
    items.push(btn('Draw several…', () => drawSheet(c), 'secondary'));
    if (c.kind === 'stack' && !c.spread) items.push(btn(top && top.up ? 'Turn the top card down' : 'Turn the top card up', () => run('card.flip', { ref: { container: c.id, index: -1 } }), 'secondary'));
  }
  if (c.count > 0 && c.can.peek && c.kind === 'stack' && top && !top.def) {
    items.push(btn('Peek at the top card', () => run('card.peek', { ref: { container: c.id, index: -1 } }), 'secondary'));
  }
  if (c.kind === 'hand' && c.owner === app.state.you && c.count > 0) {
    items.push(h('div', { class: 'row' },
      btn('Sort by suit', () => run('hand.sort', { by: 'suit' }), 'secondary'),
      btn('Sort by rank', () => run('hand.sort', { by: 'rank' }), 'secondary')));
    const shown = c.cards.every((cv) => cv.up);
    items.push(btn(shown ? 'Pick your hand back up' : 'Show your hand to everyone', () => {
      if (shown) run('hand.reveal', { up: false });
      else confirmThen('Turn every card in your hand face up for everyone?', () => app.act('hand.reveal', { up: true }));
    }, 'secondary'));
  }
  if (c.can.manage && c.kind === 'stack') {
    if (c.count > 1) {
      items.push(btn('Shuffle', () => run('pile.shuffle', { container: c.id })));
      items.push(btn('Cut…', () => cutSheet(c, false), 'secondary'));
      items.push(btn('Offer the cut to…', () => offerCutSheet(c), 'secondary'));
    }
    if (c.count > 0) items.push(btn('Deal…', () => dealSheet(c)));
  }
  if (c.count > 0 && c.can.take && c.kind !== 'hand') items.push(btn('Move all these cards to…', () => collectSheet(c), 'secondary'));
  if (c.kind === 'stack' && !c.owner && (app.isOwner() || T.dealer === app.state.you || !T.dealer)) {
    items.push(btn(`Gather every card on the table into ${c.label}`, () => confirmThen(`Gather all cards from everywhere, hands included, into ${c.label}, face down?`, () => app.act('pile.collect_all', { to: c.id })), 'secondary'));
  }
  if (c.id.startsWith('pile:') && c.count === 0) items.push(btn('Remove this pile', () => run('pile.remove', { container: c.id }), 'danger'));
  if (!items.length) items.push(h('p', { class: 'muted' }, c.count ? 'Nothing you can do with this right now.' : 'Empty.'));
  sheet(`${containerName(c)} · ${c.count} card${c.count === 1 ? '' : 's'}`, h('div', { class: 'stack' }, items));
}

function drawSheet(c) {
  const n = numberInput(2, 1, c.count);
  const dests = T.containers.filter((d) => d.id !== c.id && d.can.place);
  const to = select(dests.map((d) => [d.id, containerName(d)]), myHand() ? myHand().id : '');
  const face = select([['', 'Usual for that place'], ['up', 'Face up'], ['down', 'Face down']], '');
  sheet(`Draw from ${containerName(c)}`, h('div', { class: 'stack' },
    field('How many', n), field('To', to), field('Face', face),
    btn('Draw', () => run('card.draw', { from: c.id, to: to.value, count: Number(n.value), face: face.value }))));
}

function collectSheet(c) {
  const dests = T.containers.filter((d) => d.id !== c.id && d.can.place && d.kind !== 'grid');
  sheet(`Move all of ${containerName(c)} to…`, h('div', { class: 'stack' },
    dests.map((d) => btn(containerName(d), () => run('pile.collect', { from: c.id, to: d.id }), 'secondary'))));
}

function cutSheet(c, offered) {
  const n = c.count;
  const k = h('input', { type: 'range', min: 1, max: n - 1, value: Math.floor(n / 2), 'aria-label': 'Cards to lift' });
  const out = h('strong', {}, String(k.value));
  k.addEventListener('input', () => { out.textContent = k.value; });
  sheet(offered ? 'Your cut' : `Cut ${containerName(c)}`, h('div', { class: 'stack' },
    h('p', {}, 'Lift ', out, ` of ${n} cards off the top; they go underneath.`), k,
    btn('Cut here', () => run(offered ? 'cut.answer' : 'pile.cut', offered ? { count: Number(k.value) } : { container: c.id, count: Number(k.value) })),
    offered ? btn('Decline (knock)', () => run('cut.answer', { count: 0 }), 'secondary') : null));
}

function offerCutSheet(c) {
  const me = app.state.you;
  const right = T.seats.length ? T.seats[(T.seats.indexOf(T.dealer || me) - 1 + T.seats.length) % T.seats.length] : null;
  sheet('Offer the cut to…', h('div', { class: 'stack' },
    T.seats.filter((p) => p !== me).map((p) => btn(app.playerName(p) + (p === right ? ' (dealer\'s right)' : ''), () => run('cut.offer', { container: c.id, to: p }), p === right ? '' : 'secondary'))));
}

// ---------- dealing ----------

// dealOrder lists seated players starting at the dealer's left (dir 1) or right (-1).
function dealOrder(dir) {
  const n = T.seats.length;
  const d = T.seats.indexOf(T.dealer);
  const start = d < 0 ? 0 : d + dir;
  const out = [];
  for (let i = 0; i < n; i++) out.push(T.seats[(((start + dir * i) % n) + n) % n]);
  return out;
}

function dealSheet(c) {
  const dir = select([['1', 'Clockwise, from the dealer\'s left'], ['-1', 'Counterclockwise, from the dealer\'s right']], '1');
  const list = h('div', { class: 'stack' });
  const checks = [];
  function fill() {
    list.replaceChildren();
    checks.length = 0;
    for (const pid of dealOrder(Number(dir.value))) {
      for (const d of T.containers.filter((x) => x.owner === pid && x.can.place)) {
        const cb = h('input', { type: 'checkbox', checked: d.kind === 'hand' });
        checks.push([cb, d]);
        list.append(h('label', { class: 'check' }, cb, ' ', containerName(d)));
      }
    }
    for (const d of T.containers.filter((x) => !x.owner && x.id !== c.id && x.can.place)) {
      const cb = h('input', { type: 'checkbox' });
      checks.push([cb, d]);
      list.append(h('label', { class: 'check' }, cb, ' ', d.label));
    }
  }
  dir.addEventListener('change', fill);
  fill();
  const each = numberInput(5, 1, 52);
  const oneAtATime = h('input', { type: 'checkbox', checked: true });
  const packets = h('input', { placeholder: 'e.g. 2 3 / 3 2', autocomplete: 'off' });
  const face = select([['', 'Usual for each place'], ['down', 'Face down'], ['up', 'Face up']], '');
  const step = select([['0', 'All at once'], ['1', 'One packet at a time']], '0');
  sheet(`Deal from ${containerName(c)} (${c.count})`, h('div', { class: 'stack' },
    field('Direction', dir),
    h('fieldset', {}, h('legend', {}, 'Deal to, in order'), list),
    field('Cards each', each),
    h('label', { class: 'check' }, oneAtATime, ' One card at a time (else all of them in one packet)'),
    field('Or packets per pass', packets, 'One group per pass, separated by /. A single number goes to everyone; or give one number per place in order. "2 3 / 3 2" deals 2 then 3 to alternate places, then the reverse.'),
    field('Face', face),
    field('Pace', step),
    btn('Deal', () => {
      const to = checks.filter(([cb]) => cb.checked).map(([, d]) => d.id);
      if (!to.length) { toast('Choose at least one place to deal to.', 'error'); return; }
      let passes;
      if (packets.value.trim()) {
        passes = packets.value.split('/').map((p) => p.trim().split(/[\s,]+/).filter(Boolean).map(Number));
        passes = passes.map((p) => (p.length > 1 && p.length !== to.length ? expand(p, to.length) : p));
      } else {
        const n = Number(each.value);
        passes = oneAtATime.checked ? Array.from({ length: n }, () => [1]) : [[n]];
      }
      if (passes.some((p) => p.some((x) => !Number.isInteger(x) || x < 0))) { toast('Packets must be whole numbers.', 'error'); return; }
      run('deal.start', { from: c.id, to, passes, face: face.value, step: step.value === '1' });
    })));
}

// expand repeats a short pattern across all places: "2 3" for 4 places is 2 3 2 3.
function expand(p, n) { return Array.from({ length: n }, (_, i) => p[i % p.length]); }

// ---------- bottom bar ----------

function actionBar() {
  const me = app.state.you;
  const items = [];
  const isDealer = T.dealer === me;
  if (isDealer || app.isOwner()) items.push(btn('Pass the deal', () => run('dealer.pass'), 'small secondary'));
  if (!T.dealer || isDealer || app.isOwner()) items.push(btn('Choose dealer', () => choosePlayer('Who deals?', (p) => run('dealer.set', { player: p })), 'small secondary'));
  if (T.turn) items.push(btn('Next turn', () => run('turn.advance', { dir: 1 }), 'small secondary'));
  else items.push(btn('Show turns', () => choosePlayer('Whose turn is it?', (p) => run('turn.set', { player: p })), 'small secondary'));
  if (isDealer || app.isOwner() || T.policy === 'shared') items.push(btn('Add a pile', addPileSheet, 'small secondary'));
  if (app.isOwner()) items.push(btn('Fresh table', () => confirmThen('Start over with fresh cards and the same seats? Scores are kept.', () => app.act('table.reset')), 'small secondary'));
  return h('nav', { class: 'actionbar', 'aria-label': 'Table actions' }, items);
}

function choosePlayer(title, fn) {
  sheet(title, h('div', { class: 'stack' }, T.seats.map((p) => btn(app.playerName(p), () => fn(p), 'secondary'))));
}

function addPileSheet() {
  const name = h('input', { maxlength: 24, placeholder: 'e.g. Kitty' });
  const kind = select([['stack', 'A pile'], ['grid', 'A row of slots']], 'stack');
  const slots = numberInput(5, 1, 48);
  const face = select([['down', 'Cards land face down'], ['up', 'Cards land face up']], 'down');
  const spread = h('input', { type: 'checkbox' });
  sheet('Add a pile', h('div', { class: 'stack' },
    field('Name', name), field('Kind', kind), field('Slots (for a row)', slots), field('Face', face),
    h('label', { class: 'check' }, spread, ' Spread the cards out (show them all)'),
    btn('Add', () => run('pile.create', { label: name.value.trim(), kind: kind.value, slots: Number(slots.value), cols: Math.min(Number(slots.value), 6), faceUp: face.value === 'up', spread: spread.checked }))));
}
