# Qualification operator CLI

`import` imports an existing ready report artifact; `publish` uses the existing guarded report publisher and importer. Both require deployment-owned locks independently of report bytes. `configure-render` invokes the maintenance-only installation binding with explicit `--enabled true|false`. None of these commands creates a trusted policy or invents report checks.

## Read-only lineage capture

```
DATABASE_URL=... photo-v2-qualifications observe-lineage --output /absolute/new-observation.json
```

Use an operator-selected read-only catalog-capable database identity. This path does not require or assume the maintenance role. It sets default_transaction_read_only, uses one pool connection, bounded connection/query deadlines, and the core PostgreSQL18 read-only repeatable-read observer. It executes no Goose migration, installation or activation operation.

The output must not already exist. A canonical observation is created exclusively with mode0600; existing files and symlinks are rejected. Stdout contains only digest/count/classification metadata and always `installation_authorized:false`.

Without references this is **capture only**, not recognized lineage and not creation of an allowlist.

## Independent reference comparison

Optional paired flags:

```
--references /absolute/trusted-reference-manifest.json --references-sha256 INDEPENDENT_SHA256
```

The canonical manifest contains exactly schema_version=1, coverage equal to the core observer's current versioned coverage string, and snapshots with exactly identity and semantic_sha256. Identities and hashes must both be unique. References are bounded to1MiB/1024 snapshots and are validated before database access.

Trusted references must come from an independent reviewed source. Do not derive an approval manifest from the observation being assessed. The command never generates reference manifests. Missing, partial, unknown or ambiguous observations fail the core classifier. A mismatch returns failure and writes no observation output; run a separate explicit capture without references when diagnostic evidence is needed.

A recognized semantic catalog snapshot is not full fresh/upgrade/migration acceptance. The semantic ledger contains ordered version/applied history; an exact physical ledger witness digest is separate. External non-photo/non-extension dependencies, extension binaries and user/seed data are outside this observation's validation closure. The external dependency validation requirement remains true, and classification never grants installation authority.
