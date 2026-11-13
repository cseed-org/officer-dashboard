/**
 * Starts the Home guided tour from anywhere. On Home it opens at once; elsewhere (Settings'
 * "Replay dashboard tour") it opens Home with the tour on. Settings calls `startTour()`.
 */
export const TOUR_EVENT = 'cseed:start-tour';

export function startTour(): void {
  if (location.pathname === '/' || location.pathname === '/index.html') {
    window.dispatchEvent(new Event(TOUR_EVENT));
    return;
  }
  location.assign('/?tutorial=1');
}
