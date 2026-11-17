import { theme } from '../stores/theme';

// Custom cursor: a glowing accent arrow built from the live --accent-rgb var,
// so it recolours with the active theme. A brighter "hot" variant is used
// over clickable elements (buttons, links, etc). Ported from asicpool's
// customCursor IIFE — same SVG shape/approach, rebuilt here against the
// theme store instead of a DOM MutationObserver since Svelte already gives
// us that signal directly.
const ARROW = 'M3 3 L3 16.5 L6.8 13 L8.9 17.2 L10.6 16.5 L8.6 12.5 L13 12.5 Z'; // compact, short tail

function svg(accent: string, blur: number, glowOp: number, fillOp: number, edge: string): string {
  return (
    '<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32">' +
    '<defs>' +
      '<filter id="s" x="-70%" y="-70%" width="240%" height="240%">' +
        '<feGaussianBlur in="SourceAlpha" stdDeviation="' + blur + '" result="b"/>' +
        '<feFlood flood-color="' + accent + '" flood-opacity="' + glowOp + '"/>' +
        '<feComposite in2="b" operator="in" result="glow"/>' +
        '<feGaussianBlur in="SourceGraphic" stdDeviation="0.55" result="soft"/>' +
        '<feMerge><feMergeNode in="glow"/><feMergeNode in="soft"/></feMerge>' +
      '</filter>' +
      '<linearGradient id="f" x1="0" y1="0" x2="0.85" y2="1">' +
        '<stop offset="0" stop-color="' + accent + '" stop-opacity="' + fillOp + '"/>' +
        '<stop offset="1" stop-color="' + accent + '" stop-opacity="' + (fillOp * 0.5).toFixed(2) + '"/>' +
      '</linearGradient>' +
    '</defs>' +
    '<path d="' + ARROW + '" fill="url(#f)" stroke="' + edge + '" stroke-opacity="0.5" stroke-width="0.9" ' +
    'stroke-linejoin="round" stroke-linecap="round" filter="url(#s)"/></svg>'
  );
}

function cursorUrl(s: string): string {
  return 'url("data:image/svg+xml,' + encodeURIComponent(s) + '") 3 3';
}

const CLICKABLE_SELECTOR =
  'a, button, select, label, summary, [role="button"], [onclick], .toggle-switch, ' +
  'input[type="checkbox"], input[type="radio"], input[type="submit"], input[type="button"]';
const TEXT_SELECTOR =
  'input[type="text"], input[type="search"], input[type="number"], input[type="email"], ' +
  'input[type="password"], input[type="url"], textarea, [contenteditable="true"]';

let styleEl: HTMLStyleElement | null = null;

function build() {
  if (!styleEl) return;
  const rgb = getComputedStyle(document.documentElement).getPropertyValue('--accent-rgb').trim() || '245,166,35';
  const accent = 'rgb(' + rgb + ')';
  const arrow = cursorUrl(svg(accent, 0.8, 0.75, 0.92, accent));                  // soft accent edge, feathered
  const hot = cursorUrl(svg(accent, 1.6, 1.0, 1.0, 'rgba(255,255,255,0.85)'));    // brighter, soft white edge
  styleEl.textContent =
    '* { cursor: ' + arrow + ', auto !important; }' +
    CLICKABLE_SELECTOR + ' { cursor: ' + hot + ', pointer !important; }' +
    TEXT_SELECTOR + ' { cursor: text !important; }';
}

let unsubTheme: (() => void) | null = null;

// Installs the themed custom cursor. No-op (and reversible) on touch-only
// devices where there's no real pointer to skin. Safe to call once; repeat
// calls are idempotent.
export function initCustomCursor(): () => void {
  if (window.matchMedia('(hover: none)').matches) return () => {};
  if (!styleEl) {
    styleEl = document.getElementById('custom-cursor-style') as HTMLStyleElement | null;
    if (!styleEl) {
      styleEl = document.createElement('style');
      styleEl.id = 'custom-cursor-style';
      document.head.appendChild(styleEl);
    }
  }
  build();
  if (!unsubTheme) {
    // Theme CSS vars are applied synchronously inside the theme store's own
    // subscribe callback (see stores/theme.ts); our subscription below runs
    // after that, so getComputedStyle already reflects the new theme.
    unsubTheme = theme.subscribe(() => build());
  }
  return () => {
    if (unsubTheme) { unsubTheme(); unsubTheme = null; }
  };
}
