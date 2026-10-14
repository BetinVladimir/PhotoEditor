# V32 tenant purge bucket installation

The maintenance process always purges `S3_BUCKET`. If tenant objects also exist in other buckets on the same `S3_ENDPOINT`, set `S3_TENANT_PURGE_BUCKETS` to a comma-separated list of every additional bucket before starting maintenance. All listed buckets use the configured S3 credentials and region. Empty or duplicate names prevent startup; invalid bucket names fail object-store client construction.

For different endpoints or credentials, set `S3_TENANT_PURGE_TARGETS_FILE` to a regular file owned by the maintenance UID or root, with no group/world permissions. The final path must not be a symlink; the file must have exactly one hard link. An atomic replacement during reading causes startup refusal. Provision a standalone `0600` file, including when the secret manager ordinarily exposes a symlinked projection. The file is a JSON object with `schema_version: 1` and a nonempty `targets` array. Each target requires `endpoint`, `bucket`, `access_key`, `secret_key`, `region`, and `development` (normally `false`); unknown or duplicate JSON keys are rejected. Example: `{"schema_version":1,"targets":[{"endpoint":"https://archive.example","bucket":"photos-archive","access_key":"…","secret_key":"…","region":"eu-west-1","development":false}]}`. The primary target and same-endpoint bucket list remain included. Endpoint+bucket duplicates, including equivalent default ports and hostname letter case, more than 256 total targets, invalid S3 configuration, or an unavailable/unversioned target prevent startup. Credentials are used only by the corresponding storage client; no target can silently inherit primary credentials. Keep this file out of source control and provision it through the deployment secret manager.

The manifest path must be absolute. The loader resolves system directory aliases, then opens every canonical parent using `openat` without following symlinks and requires each parent to be owned by root or the maintenance UID and not writable by group or others. An untrusted writable parent, a relative path, or a symlinked final file prevents startup.

`TenantPurgeBuckets` divides the configured `TenantCleaner.Batch` across all buckets, checks every bucket on each call, and reports empty only after each store has completed its own two-pass version and multipart absence check. One bucket's error or cancellation prevents metadata `Finish`. The operator must inventory all actual writer destinations before enabling irreversible tenant deletion; the bucket list alone is configuration, not proof that every writer uses it.

Physical purge treats listed S3 object keys as opaque within the exact
`tenants/<tenant>/`, `staging/tenants/<tenant>/` or
`originals/tenants/<tenant>/` prefix. A legacy key containing spaces,
Unicode or noncanonical path segments is deleted by its exact version or
multipart ID; the grammar used when creating new PhotoEditor keys must not
leave such tenant-owned bytes behind. A key outside those exact prefixes is
always refused. The dedicated legacy-key and complete current key-generator
unit scenarios are authored but have not run.

For restore acceptance, `ObservePurgedTenantBuckets` composes the read-only
two-pass `Store.ObservePurgedTenant` across an explicit complete bucket
inventory. It probes at most 32 targets concurrently with a process-wide
slot limit and returns no partial proof. Every receipt must name the exact
tenant and configured bucket, all three ordered tenant prefixes, two passes,
and an observation time from the current call. The caller still has to supply
the authoritative writer-destination inventory and an independent fixture /
paused-writer guard; this helper does not infer either. Unit scenarios are
authored but not run. The composer checks that guard before any bucket IO and
again after the last result; an intervening revocation returns no proof. The
Python restore-acceptance dispatcher has its own complete-bucket MinIO purge
probe; this Go helper is not part of that Python call chain.

Before `finish_tenant_deletion`, maintenance records the independent requested-deletion history, renews the live deletion token through migration 01886, performs a first all-bucket purge/absence pass, settles archive generations under that token, renews it again, and performs a second all-bucket purge/absence pass. A remnant in either pass checkpoints the job; a failed or expired renewal refuses the next physical pass and metadata deletion. `TenantCleaner` has a bounded 120-second cycle to accommodate both 40-second scans and the two renewals, while other maintenance jobs retain 50 seconds. Deploy migration 01886 before this maintenance binary: startup checks the function's owner, `SECURITY DEFINER` mode, search path, and maintenance-only execution grant. The migration is authored but not applied; unit and physical acceptance are not run under the implementation-first plan.

Maintenance startup and `/health/ready` call the read-only versioning readiness probe on every configured purge bucket, with at most 32 concurrent probes under the shared request deadline. An unavailable or unversioned secondary bucket blocks readiness even when the primary bucket is healthy. The probe does not create or mutate buckets, and it does not replace the tenant-specific two-pass deletion evidence.

The 32-probe limit is shared across concurrent readiness requests in one maintenance process. A canceled request returns on its deadline even if an adapter ignores cancellation; a stalled adapter can hold one of the 32 slots until it returns. This bounds outstanding storage probes and causes subsequent readiness to fail closed while the target is unresponsive.

Each readiness request now creates at most 32 worker goroutines for its full inventory of up to 256 targets, instead of one waiting goroutine per target. The process-wide 32-slot network limit and the requirement to observe every target before success remain unchanged. A maximum-inventory unit scenario is authored but NOT RUN.

Code and unit/integration scenarios are authored but have not been executed under the current implementation-first plan. Migrations are not applied. Physical multi-bucket deletion and metadata purge acceptance remain open.

Migration 01775 adds an independent SQL `finish_tenant_deletion` fence for every open `v30_archive_writers` row of the tenant. A lost Archive Begin response therefore blocks irreversible metadata deletion even if a storage listing temporarily appears empty. The open writer needs explicit physical reconciliation; neither elapsed time nor an empty listing alone closes it. The migration and its SQL integration assertion are authored but not applied or run.
