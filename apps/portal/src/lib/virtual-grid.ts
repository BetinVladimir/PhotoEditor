export type VirtualWindow = { start: number; end: number; startRow: number; endRow: number; totalHeight: number };

export function virtualWindow(itemCount: number, columns: number, rowHeight: number, scrollTop: number, viewportHeight: number, overscan = 3): VirtualWindow {
  if (!Number.isInteger(itemCount) || itemCount < 0 || !Number.isInteger(columns) || columns < 1 || !Number.isFinite(rowHeight) || rowHeight <= 0) throw new Error("invalid virtual grid");
  const rows = Math.ceil(itemCount / columns);
  const first = Math.max(0, Math.floor(Math.max(0, scrollTop) / rowHeight) - overscan);
  const last = Math.min(rows, Math.ceil((Math.max(0, scrollTop) + Math.max(0, viewportHeight)) / rowHeight) + overscan);
  return { start: first * columns, end: Math.min(itemCount, last * columns), startRow: first, endRow: last, totalHeight: rows * rowHeight };
}
