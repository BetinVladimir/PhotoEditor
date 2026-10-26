---
id: P-7a
title: Adaptive upload concurrency policy (pure function)
area: photographer
size: S
depends: []
edit:
  - apps/photographer/src/upload-concurrency.ts (new)
  - apps/photographer/src/upload-concurrency.test.ts (new)
context:
  - apps/photographer/src/upload-resume-policy.ts
  - apps/photographer/src/upload-part-transport.ts
verify:
  - cd apps/photographer && npx vitest run src/upload-concurrency.test.ts
---
## Goal
Today the import runs "two files x two parts". Provide the pure decision logic for adapting it; wiring into `uploads.ts` is a later task.

## Spec
```ts
export type Sample = {ok: boolean; ms: number; bytes: number};
export type Limits = {files: number; parts: number};
export function nextLimits(current: Limits, recent: Sample[], max?: Limits, min?: Limits): Limits
```
Defaults `min={files:1,parts:1}`, `max={files:4,parts:4}`. Rules over the last up to 8 samples (ignore if fewer than 4): error rate >= 25% -> decrease parts by 1 (if parts is already min then files by 1); error rate 0 and median per-part latency < 2000 ms and median throughput >= 1 MiB/s -> increase: first parts up to `max.parts`, then files up to `max.files`; otherwise unchanged. Never leave `[min,max]`. Pure and deterministic, no timers.
## Tests
Table-driven: not enough samples; all fast -> grows parts then files; 3 of 8 failures -> shrinks; stays within bounds at both ends; slow-but-ok -> unchanged.
