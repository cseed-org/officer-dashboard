import type { RefObject } from 'preact';
import { useFlip } from '../../components/motion';
/** The dragged card rides the pointer (not part of the layout); a resized card lands at its new size at once. */
export function useGridFlip(root: RefObject<HTMLElement | null>, revision: unknown, dragId?: string, resizeId?: string) {
  useFlip(root, 'home', { revision, size: true, collisionFree: true, skip: id => id === dragId || id === resizeId, float: id => id === dragId });
}
