/** A short burst for the Design board's Done moment (CSS only; hidden under reduced motion). */
export function Confetti() {
  return (
    <span class="confetti" aria-hidden="true" data-confetti>
      {Array.from({ length: 14 }, (_, i) => (
        <i key={i} style={{ '--a': `${(i / 14) * 360}deg`, '--d': `${26 + (i % 3) * 12}px`, '--c': `var(--c-${['green', 'blue', 'amber', 'coral', 'violet', 'teal', 'green'][i % 7]})` }} />
      ))}
    </span>
  );
}
