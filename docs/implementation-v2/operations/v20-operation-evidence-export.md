# V20 operational evidence export

This operator tool observes existing authorities. It does not admit tasks, qualify models, activate providers or change availability. Keep technical qualification and photographer acceptance separate.

## Authority and privilege

Availability/status/reason/backend/model are copied from the authenticated tenant capabilities API, including current API-installation masks. Stored qualification references, report expiry/pins and worker freshness are separate observations and cannot promote or demote API readiness. All117 scope identities remain accounted:109 product tasks,6 foundation-only,1 bulk control and1 removed historical. Absent API identity is not_reported with null availability, not synthetic unsupported/ready.

Capture requires an existing authorized API token and existing DB observer/operator connection. The CLI checks the explicitly requested current_user and starts a bounded REPEATABLE READ READ ONLY transaction. It does not SET ROLE, grant privileges, create credentials, refresh/retry auth or fall back to admin. The existing role's privileges are NOT made read-only merely by naming it observer. A READ ONLY transaction is a per-capture guarantee, not proof that the LOGIN is least-privileged.

Current MAIN2659 role inventory found only authored_goose_owner and rtx_snapshot_admin able to read the complete required metadata with existing permissions; both are superuser/bypass principals. An explicitly root-authorized rtx_snapshot_admin capture can supply privileged diagnostic evidence, but must be labelled as such in its execution receipt. It is not restricted-principal/least-privilege qualification or a service deployment acceptance. A broad grant or new SECURITY DEFINER API solely to avoid that label is not justified by this exporter. Least-privilege continuous monitoring remains a separate deployment/design gap if later required. No SQL/role change is part of this tool.

## Inputs and invocation

Run from the repository with the existing dependencies. Supply existing raw bearer token on FD3 and raw observer DSN on FD4 (not the session JSON or infra.env). Do not put either credential in argv, shell tracing, output files or exported reports. Owner session validity must be checked separately under its approved flow; this tool refuses401 and does not invoke OTP/refresh.

```text
.venv-platform/bin/python -B tools/v20_operation_evidence.py   --scope docs/implementation-v2/current-operation-scope-2026-10-23.json   --tenant ACTUAL_TENANT_UUID --project ACTUAL_PROJECT_UUID   --api-origin http://localhost:58080   --token-fd 3 --observer-dsn-fd 4 --observer-login EXPLICIT_ACTUAL_LOGIN   --output /ABSOLUTE/TRUSTED/NEW/export.json
```

FDs must already exist and have finite raw bytes/end-of-file. Existing private owner session and scoped DSN may be read only by the authorized caller into memory/pipe. This guide does not generate a new auth session or provision a role. HTTPS or loopback HTTP only; GET-only, no redirects/proxies,5s per connection,30s overall deadline covering input FDs/HTTP body/DB connect/query/assembly. Existing active alarm is refused unchanged. Output0600 is exclusive/no-follow under trusted parents, fsynced; failure prints only a closed refusal status. No automatic retries or object downloads.

Optional `--annotations PATH --annotations-sha256 SHA` attaches operator provider-mode and receipt-reference metadata only after qualification ID and report SHA match the exact scoped stored record. Each reference is work-relative path+SHA, not URL/key/token. Its integrity/acceptance is not reverified here. Annotations never alter capability readiness. Generic qualification rows without mode remain explicitly not_recorded_by_generic_qualification.

## Output limits and interpretation

API and DB reads are not jointly atomic. Report records their provenance/digests and database time/head; a ready observation can expire after capture and cannot be reused for admission. Up to4096 scoped qualification rows/256 workers; input/output<=8MiB. Raw worker qualification payloads, object keys, bucket names, SQL and credentials are excluded; worker inventory is represented by SHA. Full stored qualification version domain is a string of1..256 characters (including legacy/untrusted punctuation/Unicode), matching model_qualifications SQL, NOT the stricter import-policy20-character grammar. The version is metadata, not an exception message or execution command.

Stored passed/trusted-import/report-ready/expiry is not fresh qualification verification. NULL expiry means no expiry recorded here, not permanent validity. Actual native/model/license/quality reports and dedicated provider admission remain their existing authorities. Missing project reference is an explicit evidence gap even when API says ready; an expired reference cannot rewrite API state.

## Verification

Run `PYTEST_DISABLE_PLUGIN_AUTOLOAD=1 .venv-platform/bin/python -B -m pytest -q -p no:cacheprovider tests/platform/test_v20_operation_evidence.py` after reviewed promotion. Tests mock all HTTP/DB actions; they do not claim current credentials/schema/live capture qualification. Actual operator captures and their privilege provenance require separately retained receipts.


## Versioned authority prerequisite (R3)

The source-pinned current denominator is117identities/109product; old114 inventory is preservedhistorical. Versioned API rows require additive operation_version; installed photo.tenant_capabilities_versioned() must be observed by the DB metadata snapshot. OldAPI/versionless answers are refused until the coordinated SQL/API/client source is qualified and installed. This tool does not install2660 or switchAPI.

Output operations are identities; each has versions[] with independent operation_version/availability/evidence/backends. There is deliberately no identity-level availability boolean/status. API rows and qualification refs match exact(operation,version), and duplicate exactpairs/unknown identities refuse. render.compose/1 unavailable plus /2ready stays two different observations, never OR/first-row. Unmatched stored qualification versions are shown with null API availability, not inherited readiness.
