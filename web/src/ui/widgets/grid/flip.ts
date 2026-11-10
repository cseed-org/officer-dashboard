import type { RefObject } from 'preact';
import { useFlip } from '../../components/motion';
export function useGridFlip(root: RefObject<HTMLElement | null>, revision: unknown, dragId?: string, resizeId?: string) {
  useFlip(root, 'home', { revision, size: true, skip: id => id === dragId || id === resizeId });
}
