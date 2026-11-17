import { writable } from 'svelte/store';

export type ThemeName = 'vault-tec' | 'nuclear' | 'tron' | 'synthwave' | 'mono' | 'light';

const VALID_THEMES: ThemeName[] = ['vault-tec', 'nuclear', 'tron', 'synthwave', 'mono', 'light'];
const STORAGE_KEY = 'govault-theme';

function getInitialTheme(): ThemeName {
  try {
    const stored = localStorage.getItem(STORAGE_KEY) as ThemeName;
    if (VALID_THEMES.includes(stored)) return stored;
  } catch {}
  return 'vault-tec';
}

export const theme = writable<ThemeName>(getInitialTheme());

// Subscribe to apply theme class on <html> and persist
theme.subscribe((t) => {
  if (typeof document !== 'undefined') {
    const html = document.documentElement;
    VALID_THEMES.forEach(name => html.classList.remove(`theme-${name}`));
    html.classList.add(`theme-${t}`);
  }
  try {
    localStorage.setItem(STORAGE_KEY, t);
  } catch {}
});
