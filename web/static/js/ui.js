// DOM helpers: element building (never innerHTML with user text), sheets, toasts.

// h builds an element: h('button', {class: 'btn', onclick: fn}, 'Text', child).
export function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2), v);
    else if (k === 'class') el.className = v;
    else if (k === 'dataset') Object.assign(el.dataset, v);
    else if (v === true) el.setAttribute(k, '');
    else el.setAttribute(k, v);
  }
  append(el, children);
  return el;
}

function append(el, children) {
  for (const c of children) {
    if (c === undefined || c === null || c === false) continue;
    if (Array.isArray(c)) append(el, c);
    else el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
}

export function btn(label, onclick, cls = '') {
  return h('button', { type: 'button', class: 'btn ' + cls, onclick }, label);
}

let sheetEl = null;

// sheet opens a bottom sheet with a title and content; it returns a close function.
export function sheet(title, ...content) {
  const refreshing = refreshingLive;
  const scroll = refreshing && sheetEl ? sheetEl.querySelector('.sheet').scrollTop : 0;
  closeSheet();
  const close = () => closeSheet();
  sheetEl = h('div', { class: 'sheet-wrap', onclick: (e) => { if (e.target === sheetEl) close(); } },
    h('div', { class: 'sheet', role: 'dialog', 'aria-modal': 'true', 'aria-label': title },
      h('div', { class: 'sheet-head' }, h('h2', {}, title), btn('Close', close, 'small secondary')),
      h('div', { class: 'sheet-body' }, ...content)));
  document.body.append(sheetEl);
  document.body.classList.add('noscroll');
  if (refreshing) {
    sheetEl.querySelector('.sheet').scrollTop = scroll;
  } else {
    const first = sheetEl.querySelector('.sheet-body button, .sheet-body input, .sheet-body select');
    if (first) first.focus({ preventScroll: true });
  }
  return close;
}

export function closeSheet() {
  if (sheetEl) sheetEl.remove();
  sheetEl = null;
  live = null;
  document.body.classList.remove('noscroll');
}

let live = null;
let refreshingLive = false;

// liveSheet makes the open sheet redraw with every new state (the scoreboard);
// call it right after opening the sheet.
export function liveSheet(fn) { live = fn; }

// refreshLiveSheet redraws a live sheet, keeping its scroll position.
export function refreshLiveSheet() {
  if (!live || !sheetEl) return;
  const fn = live;
  refreshingLive = true;
  try { fn(); } finally { refreshingLive = false; }
  live = fn;
}

export function sheetOpen() { return sheetEl !== null; }

let toastTimer = null;

// toast shows a short message at the bottom; kind is '', 'error' or 'ok'.
export function toast(msg, kind = '') {
  let el = document.getElementById('toast');
  if (!el) {
    el = h('div', { id: 'toast', role: 'status', 'aria-live': 'polite' });
    document.body.append(el);
  }
  el.textContent = msg;
  el.className = 'show ' + kind;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.className = ''; }, kind === 'error' ? 4500 : 2500);
}

// field wraps a labelled input or select.
export function field(label, input, hint) {
  return h('label', { class: 'field' }, label, input, hint ? h('small', {}, hint) : null);
}

export function select(options, value) {
  const s = h('select', {});
  for (const [v, label] of options) {
    s.append(h('option', { value: v, selected: String(v) === String(value) }, label));
  }
  return s;
}

export function numberInput(value, min, max) {
  return h('input', { type: 'number', inputmode: 'numeric', value, min, max });
}

// confirmThen asks a yes/no question and runs fn on yes.
export function confirmThen(question, fn) {
  sheet('Are you sure?', h('p', {}, question), h('div', { class: 'row' },
    btn('Yes', () => { closeSheet(); fn(); }), btn('Cancel', closeSheet, 'secondary')));
}
