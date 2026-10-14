# Import upscale qualification deployment

Load an operator-controlled canonical JSON file with `LoadImportUpscaleQualifications(ctx, absolutePath, expectedSHA256)` and assign its result to `ImportRecipeCodecDependencies.UpscaleQualifications`. The file SHA must come from deployment configuration. An import request or archive cannot supply this policy.

The exact root fields are `schema_version: "interchange-upscale-qualification/v1"` and `scales`. The latter contains one or both string JSON keys `"2"`, `"4"`. Each value contains exactly `qualification_report` (artifact UUID/SHA) and complete `v2qualification.DeploymentLocks` under `locks`. Every lock policy must specify `image.upscale/1`, an actual backend, model/runtime/device/dataset hashes, trusted issuer, required quality checks, held-out fixtures/evidence and cleared license. Both scale configurations are explicit; missing 4× never borrows 2× qualification.

The loader enforces exact SHA/JCS, a regular non-symlink absolute file, 2 bytes–1 MiB, no group/other writes, and stable file/path identity, mode, size and mtime. Returned scale maps and lock arrays are owned.

Apply forward-only migration `01580_v30_import_upscale_qualification.sql` during the later migration/test phase. The scheduler-only lookup uses the complete current import workflow/source gate, then matches a trusted passed report to an enabled admission policy, exact locks/check/fixture/calibration/evidence sets and import audit linkage. It requires an immutable ready verified unexpired report. Go reads its actual bytes, runs `v2qualification.Verify`, and repeats SQL authority after object IO.

Entry composition derives per-scale model pins from this configuration, refuses conflicting configured pins or duplicate operation authority, and wraps the scale runtime router with canonical source/master/color/dtype checks and native Params schema validation. Params retain authored scale, model, adapter and tile/overlap. Go and worker reject output above 200 million pixels before allocation. This does not estimate RAM/VRAM or qualify a final resized WorkingImage: real execution, output transform-chain qualification and measured admission remain separate requirements. Metal checks must retain the agreed 10 GiB VRAM cap.

Current status: loader, SQL/Go policy composition and deferred unit/integration scenarios authored. No tests or migrations executed. Complete main-server installation and populated SQL/native lifecycle remain unfinished.

Startup composition: `ImportRestorationDeploymentFiles.Install(ctx, baseFactory)` loads both optional pinned files before returning an updated `SQLImportEntryRecipeAuthorityFactory`. A failed file or conflicting existing policy returns no partial factory. This helper must still be called by the future complete main-server import installation.
