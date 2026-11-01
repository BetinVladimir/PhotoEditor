import {useCallback, useRef} from 'react';

export type QuotaThreshold = 80 | 95 | 100;

/**
 * Deduplicates quota threshold notifications within a component's lifecycle.
 * Returns a check-and-mark function: true the first time a (resource, period, threshold)
 * triple is seen, false thereafter — preventing re-notification on every render.
 * No PII is included in the dedup key.
 */
export function useQuotaNotify(): (resource: string, period: string, threshold: QuotaThreshold) => boolean {
  const shown = useRef(new Set<string>());
  return useCallback((resource: string, period: string, threshold: QuotaThreshold): boolean => {
    const key = `${resource}:${period}:${threshold}`;
    if (shown.current.has(key)) return false;
    shown.current.add(key);
    return true;
  }, []);
}
