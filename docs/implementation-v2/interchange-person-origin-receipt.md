# V30 portable person-mask origin receipt

`workers/photo_worker/v2/interchange_person_origin.py` defines a bounded,
canonical, domain-separated Ed25519 receipt for one original `mask.person@1`
producer. The claim binds issuer, original VersionBinding, exact archived
EditingRecipeV3/Override/MaskRef/MaskPixels refs, producer task/attempt/fence,
model lock and qualification report. It contains no person identity or consent.
The verifier takes a process-owned `(issuer_id, SHA256(public key)) → 32-byte
Ed25519 public key` trust map and independently supplied expected entry/ref scope. Archive
inventory, parsed JSON and HTTP payload may not add trusted keys or expected
values. Signature verification alone never grants import admission.
`backend/internal/interchange/import_person_origin_receipt.go` provides the
matching Go verifier for the import service. Both decoders reject unknown or
noncanonical fields, changed expected refs and untrusted signatures.
The eventual verifier deployment must include the existing `signer-key` worker
extra, which supplies `cryptography`; a missing crypto runtime fails closed.

The remaining issuer path must read the current original producer graph in a
tenant-scoped SQL transaction: source asset/version, canonical mask.person
Params and report, exact result roles and artifact versions, task attempt and
fence, model qualification, recipe/Override occurrence, MaskRef and NPY pixel
ref/SHA. It must repeat live authority after physical object reads, then sign
the exact claim through a provisioned key handle. The receipt must be included
in the bundle inventory and mapped explicitly to the same recipe occurrence.
Import must verify the receipt against configured issuer trust, archive bytes
and target source remap; store its exact hash and issuer/key in a new scoped
forward-only table, and require that live receipt **and** the separate current
user grant/epoch before and after IO and at activation. Revocation of either
authority must fail closed. The existing SQL grant remains only the consent
half; it cannot be reused as producer evidence.

The bundle schema now has an optional `person_origins` mapping. Its entry ID,
recipe, Override, MaskRef, MaskPixels and receipt path/SHA must all resolve to
the declared inventory and selected entry. Python archive verification and Go
parsed-import validation enforce this structural mapping; the later typed
recipe/Override traversal must still prove actual parent-child occurrence.
The claim itself
also includes the entry ID. The Go export validators currently reject any
nonempty `person_origins` until a qualified issuer and source-inventory
receipt path are installed; this prevents an untrusted file from being
published as provenance by the existing exporter.

The Python and Go pure contracts and negative unit scenarios are authored but
not executed.
No exporter signer, qualified receipt publisher, SQL receipt authority, process trust
bootstrap or end-to-end verification has been installed yet. Production
person-mask import remains unavailable.
