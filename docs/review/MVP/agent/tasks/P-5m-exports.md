---
id: P-5m-exports
title: OwnerAPI: move 10 "exports" methods into src/api/exports.ts (mechanical)
area: photographer
size: XS
depends: [P-5m-reviews]
edit:
  - apps/photographer/src/owner-api.ts
  - apps/photographer/src/api/exports.ts (new)
context:
  - apps/photographer/src/owner-api.ts:118-124
verify:
  - cd apps/photographer && npx tsc --noEmit
  - cd apps/photographer && npx vitest run src/owner-api
  - test -f apps/photographer/src/api/exports.ts
---
## Goal
Move these `OwnerAPI` methods into the module-level functions of `apps/photographer/src/api/exports.ts`; the class keeps one-line facades that delegate, so no caller changes. A deterministic script does the edit (it copies the method text, replaces `this.` by `self.`, prunes imports); you only run it. DO NOT retype or rewrite methods by hand.
Methods (10): `presets,changePresetMetadataPolicy,watermarkAssets,exportBatchStatus,createExportBatch,createPreset,startDownload,downloadJob,cancelDownloadJob,downloadJobVolume`

## Steps
1. Dry run (writes nothing):
```
python3 docs/review/MVP/agent/tools/move_methods.py --domain exports --methods presets,changePresetMetadataPolicy,watermarkAssets,exportBatchStatus,createExportBatch,createPreset,startDownload,downloadJob,cancelDownloadJob,downloadJobVolume --dry-run
```
If it prints `REFUSED: ...` stop and answer `BLOCKED: <message>`.
2. Run the same command without `--dry-run`.
3. `cd apps/photographer && npx tsc --noEmit`. Allowed fixes, and ONLY these: a missing/unused import in `api/exports.ts` (copy the import line from `owner-api.ts`), or an `implicit any` caused by a moved callback parameter (copy the type annotation from the facade).
4. `cd apps/photographer && npx vitest run src/owner-api` must pass.

## Do not
- Do not edit method bodies, signatures or the order of facades. Do not run a formatter on `owner-api.ts`.
- Do not create files other than `api/exports.ts`.
