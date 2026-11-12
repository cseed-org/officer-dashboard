import { useRef } from 'preact/hooks';
import { blocks } from './layout';
import type { BlockSize } from '../registry';

/** Pointer jitter within a snap cell must not create another layout commit. */
export function useGridResize() {
  const previewSize = useRef<BlockSize | null>(null);
  return previewSize;
}
export function snappedSize(sizes: BlockSize[], size: BlockSize, axis: string, dx: number, dy: number, width: number, height: number) {
  const b = blocks(size);
  const wantW = b.w + dx / (width / b.w);
  const wantH = b.h + dy / (height / b.h);
  return sizes.filter(s => axis === 'x' ? blocks(s).h === b.h : axis === 'y' ? blocks(s).w === b.w : true)
    .sort((a, c) => {
      const distance = (s: BlockSize) => (blocks(s).w - wantW) ** 2 + (blocks(s).h - wantH) ** 2;
      return distance(a) - distance(c);
    })[0] ?? size;
}
