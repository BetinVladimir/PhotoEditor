# Operator configuration for imported SCUNet Params

The optional `ImportRecipeCodecDependencies.DenoiseQualification` installation enables the exact `image.denoise/1` validator. Load it with `LoadImportDenoiseQualification(ctx, absolutePath, expectedFileSHA256)`. The SHA must come from deployment configuration, never an import request or archive.

The canonical JSON document has exactly three fields:

- `schema_version`: `interchange-denoise-qualification/v1`.
- `qualification_report`: exact `{artifact_id, sha256}` of a trusted imported qualification report in the importing tenant/project.
- `locks`: complete `v2qualification.DeploymentLocks` with operation `image.denoise`, version `1`, backend `cpu`, `cuda` or `mps`, model/runtime/device/dataset hashes, issuer UUID, required checks, calibration/test/evidence hashes and cleared license.

The loader requires a regular non-symlink file, an absolute path, a size from 2 bytes to 1 MiB, and no group/other write permission. It rechecks opened-file identity, size, mode, modification time and current path before decoding exact canonical bytes and checking the deployment SHA. Policy arrays are owned by the returned installation.

Provision the qualification policy and physically verified report using the existing maintenance qualification import. Do not invent hashes or use an archive's quality claims. Migration `01579_v30_import_denoise_qualification.sql` adds scheduler-only report discovery after the complete current import/source gate. The lookup requires enabled admission and license, trusted passed qualification, matching operator locks/check/evidence sets, import audit linkage, and a ready immutable unexpired report. Go reads the actual report object and runs `v2qualification.Verify`, then repeats the SQL lookup.

Entry composition installs this runtime policy, binds it to the canonical master and exact model lock, and adds native operation Params schema validation when native Style deployment is configured. Imported Params retain authored strength, tile, overlap and model lock; only the image ref changes to the qualified canonical ingest target. A different canvas is explicitly unsupported. No zero-strength bypass exists.

This policy observation is not execution admission or a resource measurement. Final task admission must still recheck current policy, physical inputs, source authorization, measured resources and capacity. The model runtime hash and native schema-probe hash describe different components and must not be substituted for one another. For the planned Metal verification, the operator must provision the real MPS device policy and enforce the agreed 10 GiB VRAM limit.

Current status: code and unit scenarios authored; tests and migration not executed. The main server does not yet install the complete import semantic graph. This document does not claim a deployed denoise import feature.

Startup composition: `ImportRestorationDeploymentFiles.Install(ctx, baseFactory)` loads both optional pinned files before returning an updated `SQLImportEntryRecipeAuthorityFactory`. A failed file or conflicting existing policy returns no partial factory. This helper must still be called by the future complete main-server import installation.
