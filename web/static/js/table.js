// The card table. Filled in with the table engine.
import { h } from './ui.js';

export function renderTable(app) {
  return h('main', { class: 'page' }, h('p', {}, 'The table is starting…'));
}
