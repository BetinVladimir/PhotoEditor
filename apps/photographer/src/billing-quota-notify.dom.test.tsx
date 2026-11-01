// @vitest-environment jsdom
import {act, cleanup, renderHook} from '@testing-library/react';
import {afterEach, expect, it} from 'vitest';
import {useQuotaNotify} from './billing-quota-notify';

afterEach(()=>cleanup());

it('returns true the first time a threshold is seen (criterion D)', () => {
  const {result} = renderHook(() => useQuotaNotify());
  let shown: boolean;
  act(() => { shown = result.current('storage', '2026-11', 80); });
  expect(shown!).toBe(true);
});

it('returns false on the same resource/period/threshold (dedup, criterion D)', () => {
  const {result} = renderHook(() => useQuotaNotify());
  act(() => { result.current('storage', '2026-11', 95); });
  let shown: boolean;
  act(() => { shown = result.current('storage', '2026-11', 95); });
  expect(shown!).toBe(false);
});

it('treats different thresholds as distinct notifications (criterion D)', () => {
  const {result} = renderHook(() => useQuotaNotify());
  let a: boolean, b: boolean;
  act(() => { a = result.current('storage', '2026-11', 80); });
  act(() => { b = result.current('storage', '2026-11', 95); });
  expect(a!).toBe(true);
  expect(b!).toBe(true);
});

it('treats different resources as distinct (criterion D)', () => {
  const {result} = renderHook(() => useQuotaNotify());
  let a: boolean, b: boolean;
  act(() => { a = result.current('storage', '2026-11', 100); });
  act(() => { b = result.current('compute', '2026-11', 100); });
  expect(a!).toBe(true);
  expect(b!).toBe(true);
});

it('treats different periods as distinct (criterion D)', () => {
  const {result} = renderHook(() => useQuotaNotify());
  let a: boolean, b: boolean;
  act(() => { a = result.current('storage', '2026-10', 80); });
  act(() => { b = result.current('storage', '2026-11', 80); });
  expect(a!).toBe(true);
  expect(b!).toBe(true);
});

it('dedup is stable across re-renders (no re-notification on render, criterion D)', () => {
  const {result, rerender} = renderHook(() => useQuotaNotify());
  act(() => { result.current('storage', '2026-11', 80); });
  rerender();
  let shown: boolean;
  act(() => { shown = result.current('storage', '2026-11', 80); });
  expect(shown!).toBe(false);
});
