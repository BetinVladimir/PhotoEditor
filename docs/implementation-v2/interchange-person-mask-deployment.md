# V30: imported whole-person mask policy

`ImportPersonMaskQualificationInstallation` provides report-backed runtime admission for selector `person`. Configuration contains a pinned report and complete deployment locks for mask.person/1 plus a mandatory independent `Consent` authority. No default consent implementation or automatic approval is installed.

The SQL entry factory builds this policy with the actual entry's scheduler store and immutable object reader, adds it to an owned mask-selector registry, and refuses an existing competing person policy or broad MaskSemantics authority. It does not enable manual masks implicitly. Workflow binding owns deployment lock arrays. The report's model lock becomes the exact person selector lock and physical array allowlist entry.

Migration 01583 introduces scheduler-only `v30_import_person_mask_qualification_object`. It retains the complete current import-source gate and matches enabled trusted policy/report evidence for exact operation/version, model/runtime/backend/device/dataset/issuer/checks/fixtures/calibrations. Actual immutable report bytes are verified and SQL metadata repeated. The process must provision real qualification evidence; a JSON declaration of a lock is insufficient.

The policy accepts whole-person masks with no object_id and exact configured model lock. Object-scoped masks require a different explicit identity/consent policy. Independent consent is called before report IO and after verification, with fresh owned MaskRef documents. Failure or cancellation returns no approval. The outer BoundImportMaskSemantics retains canonical source/geometry/current editor authority; physical NPY and warp qualification remain independent.

A qualification report proves installed runtime qualification. It does not establish that archived pixels were produced by that model, prove numerical segmentation quality on the imported image, carry original user consent into the new asset, or replace archive-origin evidence. The independent acceptance/consent authority must enforce the applicable import policy; full origin/consent installation is still required before production enablement.

Deferred coverage includes exact selector/lock/object rejection, consent before/after IO, cancellation, callback ownership, actual report hash/model/check/version/MIME verification, and SQL ACL/absent scope. Tests NOT RUN; migration NOT APPLIED. Concrete origin/consent authority, main installation, populated SQL/native lifecycle and module acceptance remain open.


## Process configuration loader

`DecodeImportPersonMaskQualification(ctx, raw, pin, consent)` and `LoadImportPersonMaskQualification(ctx, absolutePath, pin, consent)` accept canonical `interchange-person-mask-qualification/v1` containing exactly `schema_version`, `qualification_report` and complete `locks` for mask.person/1. The separate acceptance/consent service is mandatory and cannot be supplied by the JSON document. An extra JSON consent claim is rejected. The loader enforces SHA, regular non-symlink file, 1 MiB bound, no group/other writes and repeated opened/path identity, size, mode and modification-time checks.

`ImportPersonMaskDeploymentFile.Install` attaches this configuration without invoking consent or object IO. It refuses existing report authority, broad MaskSemantics and an explicit person selector policy; it leaves other selectors and explicit manual configuration intact. Failure returns no partially configured factory. An empty path/SHA pair means no installation; an incomplete pair fails. This is composition support, not a deployed main-server route.

Eleven file/configuration refusal cases and separate-consent/conflicting-authority scenarios are authored, NOT RUN. No migration or runtime execution was performed.

`ImportRecipeDeploymentFiles.Install` composes restoration, Stats and person qualification files atomically. All path/SHA pairs and required independent person acceptance are validated before file IO. Any later failure returns no partial factory and leaves the input configuration intact. This helper does not replace the still-required main installation or actual acceptance service. Eight combined-file scenarios and a pre-IO declaration refusal test are authored, NOT RUN.
