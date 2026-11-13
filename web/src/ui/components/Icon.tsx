/** Line icons for the shell (24px grid, stroke = currentColor). */
const PATHS: Record<string, string> = {
  pen: 'm16 3 5 5-12 12-6 1 1-6zM14 5l5 5',
  rocket: 'M14 5c3-3 7-3 7-3s0 4-3 7l-7 7-5-5zM6 11H3l3-5h7M13 16v5l5-3v-7M5 16l-3 6 6-3M16 6h.01',
  blocks: 'M3 13h8v8H3zM13 13h8v8h-8zM8 3h8v8H8z',
  home: 'M3 10.5 12 3l9 7.5V20a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1z',
  calendar: 'M4 6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v13a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2zM4 10h16M8 2v4M16 2v4',
  wallet: 'M3 7a2 2 0 0 1 2-2h13v4M3 7v11a2 2 0 0 0 2 2h15V9H5a2 2 0 0 1-2-2zM16 14.5h.01',
  shield: 'M12 3 4 6v6c0 4.5 3.4 8.3 8 9 4.6-.7 8-4.5 8-9V6z',
  settings: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z',
  search: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM21 21l-4.3-4.3',
  menu: 'M4 6h16M4 12h16M4 18h16',
  close: 'M6 6l12 12M18 6 6 18',
  plus: 'M12 5v14M5 12h14',
  grip: 'M7.75 6a1.25 1.25 0 1 0 2.5 0a1.25 1.25 0 1 0-2.5 0M13.75 6a1.25 1.25 0 1 0 2.5 0a1.25 1.25 0 1 0-2.5 0M7.75 12a1.25 1.25 0 1 0 2.5 0a1.25 1.25 0 1 0-2.5 0M13.75 12a1.25 1.25 0 1 0 2.5 0a1.25 1.25 0 1 0-2.5 0M7.75 18a1.25 1.25 0 1 0 2.5 0a1.25 1.25 0 1 0-2.5 0M13.75 18a1.25 1.25 0 1 0 2.5 0a1.25 1.25 0 1 0-2.5 0',
  upload: 'M12 16V4M7 9l5-5 5 5M4 20h16',
  user: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM4 21a8 8 0 0 1 16 0',
  file: 'M14 3H6a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8zM14 3v5h5',
  check: 'M20 6 9 17l-5-5',
  message: 'M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12z',
  inbox: 'M3 13h5l1.5 3h5L16 13h5M5.5 5h13L21 13v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-6z',
  ledger: 'M4 4h16v16H4zM4 9h16M9 9v11',
  'chevron-down': 'm6 9 6 6 6-6',
  'chevrons-up-down': 'm7 9 5-5 5 5M7 15l5 5 5-5',
  mail: 'M4 5h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1zM3 7l9 6 9-6',
  'arrow-right': 'M5 12h14M13 6l6 6-6 6',
  clock: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 7v5l3 2',
  link: 'M10 14a4 4 0 0 0 5.66 0l3-3a4 4 0 0 0-5.66-5.66l-1 1M14 10a4 4 0 0 0-5.66 0l-3 3a4 4 0 0 0 5.66 5.66l1-1',
  flag: 'M5 21V4M5 4h11l-2 4 2 4H5',
  ticket: 'M4 5h16a1 1 0 0 1 1 1v3a3 3 0 0 0 0 6v3a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-3a3 3 0 0 0 0-6V6a1 1 0 0 1 1-1zM15 5v2M15 11v2M15 17v2',
  pie: 'M12 3a9 9 0 1 0 9 9h-9zM15 2.5a7 7 0 0 1 6.5 6.5H15z',
  activity: 'M3 12h4l3-8 4 16 3-8h4',
  globe: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18',
  gauge: 'M5.6 18.4a9 9 0 1 1 12.8 0M12 14l3.5-4.5M12 14h.01',
  database: 'M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3zM4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3',
  'folder-user': 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2zM12 17.5a3 3 0 0 1 6 0M15 14.5a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3z',
  badge: 'M12 14a5 5 0 1 0 0-10 5 5 0 0 0 0 10zM8.5 13 7 21l5-3 5 3-1.5-8',
  contact: 'M5 3h12a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5zM5 3v18M3 7h3M3 12h3M3 17h3M13 12a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5zM9 17a4 4 0 0 1 8 0',
  users: 'M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM2 21a7 7 0 0 1 14 0M16 3.5a4 4 0 0 1 0 7.5M18 14a6 6 0 0 1 4 7',
  'arrow-up-right': 'M7 17 17 7M8 7h9v9',
  'resize-corner': 'M20 12l-8 8M20 17l-3 3',
  // V0.10 Design board: request types and design-file buttons.
  folder: 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
  image: 'M5 3h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zM8.5 10a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3zM21 15l-5-5L5 21',
  social: 'M8 3h8a5 5 0 0 1 5 5v8a5 5 0 0 1-5 5H8a5 5 0 0 1-5-5V8a5 5 0 0 1 5-5zM12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7zM17 7h.01',
  slides: 'M3 4h18v12H3zM12 16v4M8 20h8',
  shirt: 'M8 4 3 7l2 4 2-1v10h10V10l2 1 2-4-5-3a4 4 0 0 1-8 0z',
  video: 'M3 6h13v12H3zM16 10l5-3v10l-5-3',
  palette: 'M12 3a9 9 0 0 0 0 18c1 0 1.7-.8 1.7-1.7 0-.5-.2-.9-.5-1.2-.3-.3-.5-.7-.5-1.2 0-.9.8-1.7 1.7-1.7H16a5 5 0 0 0 5-5c0-4-4-7.2-9-7.2zM7.5 12.5h.01M9.5 8h.01M14.5 7.5h.01M17.5 11h.01',
  sparkle: 'M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8zM19 16l.8 2.2L22 19l-2.2.8L19 22l-.8-2.2L16 19l2.2-.8z',
};

export type IconName = keyof typeof PATHS;

export function Icon({ name, size = 18, label, stroke = 1.75 }: { name: IconName; size?: number; label?: string; stroke?: number }) {
  return (
    <svg
      class="icon"
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="none"
      stroke="currentColor"
      stroke-width={stroke}
      stroke-linecap="round"
      stroke-linejoin="round"
      role={label ? 'img' : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : 'true'}
    >
      <path d={PATHS[name]} />
    </svg>
  );
}
