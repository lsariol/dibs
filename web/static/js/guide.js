// The game guide: where the hand is, and the preset's buttons for the next
// steps. Each button is an ordinary action, checked by the server like any other.
import { h, btn, confirmThen } from './ui.js';

export function guideBar(app) {
  const g = app.state.guide;
  if (!g) return null;
  const run = (s) => {
    const go = () => app.act(s.action, s.params || {});
    if (s.confirm) confirmThen(s.confirm, go);
    else go();
  };
  return h('section', { class: 'guide', 'aria-label': 'Game guide' },
    h('p', { class: 'guide-status' }, g.status),
    g.hint ? h('p', { class: 'guide-hint' }, g.hint) : null,
    (g.steps || []).length ? h('div', { class: 'guide-steps' },
      g.steps.map((s) => btn(s.label, () => run(s), 'small' + (s.primary ? '' : ' secondary')))) : null);
}
