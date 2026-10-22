# Relight deployment configuration

Implementation status: written, NOT RUN. Per user instruction, compilation, unit/integration execution and deep acceptance are deferred until implementation of the remaining scope. This document does not certify working deployment.

API startup optionally reads `PHOTO_V28_RELIGHT_CONFIG` (regular JSON file, <=8 MiB) and requires `V28_RELIGHT_SCHEDULER_DATABASE_URL`. The connection explicitly assumes `photo_scheduler`; a login without permission to assume it fails startup. File configured but invalid, database unavailable or missing01012/01013 functions fails startup. With no deployment file and a configured `V28_RELIGHT_SCHEDULER_DATABASE_URL`, startup installs a receipt-only router using02420; it recovers historical acknowledgements with persisted pins and cannot publish or admit tasks. With neither file nor scheduler DSN the endpoint remains unavailable503. Invalid configured files still fail startup; they never silently select receipt-only mode. Dedicated pool is owned by the installation and closed on shutdown.

Top-level exact object: `schema_version:1`, `projects:[...]` (1..1024 entries). Each entry requires all fields below; extra fields are rejected. Duplicate `(tenant_id,project_id)` entries are rejected.

| Field | Meaning |
| --- | --- |
| tenant_id, project_id | Nonzero UUIDs selecting one server-owned service |
| qualification | Exact RelightQualificationPin: policy_id, qualification_id, policy_sha, report/calibration artifact refs, model_lock_sha, runtime_lock_sha, device_profile_sha, dataset_sha, issuer_id, backend=cpu |
| required_checks | 1..256 unique nonempty check names, each <=128 characters |
| test_fixture_shas, evidence_shas | 1..256 unique lowercase SHA256 digests from independently approved qualification inputs |
| calibration_shas | 0..256 unique SHA256 digests; empty uses existing verifier semantics, not a new bypass |
| license_cleared | Must be true; operator assertion does not replace live qualification checks |
| capacity | pixel_count 1..200000000; positive ram_bytes, scratch_bytes, output_bytes <=2^50; vram_bytes=0 |
| raster_limits, batch_limits | Exact point_checks, edge_evaluations, payload_bytes; positive, point/edge <=2^40, payload <=2^30; batch each >=single |

Capacity values represent measured deployment inputs, not client estimates. The constructor does not measure hardware. Runtime Params/source resource calculations and admission gates still apply; operator values cannot activate an unqualified operation. Configure original versioned qualification/calibration objects and approved digest inventories, not synthetic fixture reports.

`RelightProjectServices.SubmitProjectKeyed` routes by exact tenant/project tuple and refuses an unconfigured scope. `RelightSubmissionService.SubmitProjectKeyed` independently checks bound scope before SQL. Authenticated identity remains separate. Every submission rechecks live source, session, qualification and original objects. Registry activation remains outside this configuration.

Public POST `/api/v1/tenants/{t}/projects/{id}/relight-intents` accepts only schema_version, snapshot_id, ordinal, subject_kind and params; Idempotency-Key is required. Server derives run/workflow IDs. Repeating an admitted exact request returns its historical queued acknowledgement only while all receipt gates still hold. Current processing state must be read from workflow status.

Metal remains supported without an artificial 10 GB limit; the dedicated RTX deployment is qualified separately against its actual capacity. This CPU relight service must not be relabeled MPS to bypass backend pins. GPU operations will be tested separately on MPS where supported. Tests and qualification for this new deployment wiring have not yet run.
