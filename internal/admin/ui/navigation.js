import { el } from './core.js';

export function routeParams() {
  return new URLSearchParams(location.hash.split('?').slice(1).join('?'));
}
export function pageURL(page, values = {}) {
  const p = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) if (value !== '' && value !== null && value !== undefined) p.set(key, String(value));
  return '#' + page + (p.size ? '?' + p : '');
}
export function idLink(kind, id) {
  const a = el('a', `${kind} #${id}`, 'id-link');
  a.href = pageURL(kind === 'account' ? 'input' : 'output', { q: `${kind}:${id}` });
  return a;
}
// Construct text nodes and links, never HTML from provider names or event text.
export function linkedText(text) {
  const node = el('span', undefined, 'linked-text');
  let at = 0;
  for (const match of text.matchAll(/\b(account|stream)\s+#([0-9]+)/gi)) {
    node.append(document.createTextNode(text.slice(at, match.index)), idLink(match[1].toLowerCase(), match[2]));
    at = match.index + match[0].length;
  }
  node.append(document.createTextNode(text.slice(at)));
  return node;
}
