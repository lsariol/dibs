// Card faces and backs. Only rendering: no game knowledge.

const suitSymbols = { S: '♠', H: '♥', D: '♦', C: '♣' };
const suitNames = { S: 'spades', H: 'hearts', D: 'diamonds', C: 'clubs' };
const colorRanks = { skip: 'Skip', reverse: 'Rev', draw2: '+2', wild: 'Wild', wild4: '+4' };

// label is a card's short text, e.g. "10♥".
export function label(def) {
  if (!def) return 'card';
  if (def.kind === 'standard') return def.rank + suitSymbols[def.suit];
  if (def.kind === 'joker') return 'Joker';
  return (def.color === 'wild' ? '' : cap(def.color) + ' ') + (colorRanks[def.rank] || def.rank);
}

// spoken is a card's accessible name, e.g. "10 of hearts".
export function spoken(def) {
  if (!def) return 'face-down card';
  if (def.kind === 'standard') return `${def.rank} of ${suitNames[def.suit]}`;
  return label(def);
}

function cap(s) { return s ? s[0].toUpperCase() + s.slice(1) : s; }

// cardEl draws one card view: a face if the viewer may know it, else a back.
// Private faces (only you can see) get a dashed edge; face-up ones are public.
export function cardEl(cv, { onclick, small, selected } = {}) {
  const el = document.createElement(onclick ? 'button' : 'div');
  if (onclick) {
    el.type = 'button';
    el.addEventListener('click', onclick);
  }
  let cls = 'card';
  if (small) cls += ' small';
  if (selected) cls += ' selected';
  if (cv.empty) {
    el.className = cls + ' empty';
    el.setAttribute('aria-label', 'empty slot');
    return el;
  }
  if (!cv.def) {
    el.className = cls + ' back';
    el.setAttribute('aria-label', 'face-down card');
    return el;
  }
  const d = cv.def;
  cls += ' face';
  if (d.kind === 'color') cls += ' c-' + d.color;
  else if (d.color === 'red') cls += ' red';
  if (cv.mine) cls += ' private';
  el.className = cls;
  const rank = d.kind === 'standard' ? d.rank : d.kind === 'joker' ? '★' : (colorRanks[d.rank] || d.rank);
  const suit = d.kind === 'standard' ? suitSymbols[d.suit] : d.kind === 'joker' ? 'Joker' : (d.color === 'wild' ? 'wild' : '');
  const r = document.createElement('span');
  r.className = 'rank';
  r.textContent = rank;
  const s = document.createElement('span');
  s.className = 'suit';
  s.textContent = suit;
  el.append(r, s);
  el.setAttribute('aria-label', spoken(d) + (cv.up ? ', face up' : cv.mine ? ', only you can see it' : ''));
  return el;
}
