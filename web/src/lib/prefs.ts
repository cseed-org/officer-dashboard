/**
 * Theme and density, saved in localStorage and applied to <html> before paint by the inline
 * script in AppLayout (PREPAINT below). Light is the default.
 */
export type ThemePref = 'light' | 'dark' | 'system';
export type Density = 'comfortable' | 'compact';
export const THEME_KEY = 'cseed-theme';
export const DENSITY_KEY = 'cseed-density';

const read = (k: string) => {
  try {
    return localStorage.getItem(k);
  } catch {
    return null;
  }
};
const write = (k: string, v: string) => {
  try {
    localStorage.setItem(k, v);
  } catch {
    /* private mode: applies for this page only */
  }
};

export const getTheme = (): ThemePref => {
  const v = read(THEME_KEY);
  return v === 'dark' || v === 'system' ? v : 'light';
};
export const getDensity = (): Density => (read(DENSITY_KEY) === 'compact' ? 'compact' : 'comfortable');

const mq = () => matchMedia('(prefers-color-scheme: dark)');
export function applyTheme(pref: ThemePref) {
  const root = document.documentElement;
  root.dataset.themePref = pref;
  root.dataset.theme = pref === 'system' ? (mq().matches ? 'dark' : 'light') : pref;
  root.style.colorScheme = root.dataset.theme;
}
export function setTheme(pref: ThemePref) {
  write(THEME_KEY, pref);
  applyTheme(pref);
}
export function setDensity(d: Density) {
  write(DENSITY_KEY, d);
  document.documentElement.dataset.density = d;
}

/** Follow the OS while the preference is "system". */
export function watchSystemTheme() {
  mq().addEventListener('change', () => {
    if (getTheme() === 'system') applyTheme('system');
  });
}
