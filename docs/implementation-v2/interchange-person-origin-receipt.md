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
`SQLExportDocumentCatalog.LookupPortablePersonOrigin` now provides a narrow
source-side metadata locator for the exact source version, person MaskRef root
and MaskPixels. It requires the current editor/project/source gate, canonical
mask.person task result, attempt/fence, unique root/report roles and immutable
object versions. It does **not** read or qualify root/Params/report bytes, prove
model runtime qualification, bind the root to a selected recipe Override or
sign anything. The issuer must perform those checks and repeat the locator
after object IO before any signature can be published.
`PortablePersonOriginIssuer` composes this locator with the existing
`ReadQualifiedPersonEvidence` physical/model reader, a mandatory exact
recipe/Override occurrence authority and a process-owned Ed25519 signer. It
checks pinned object metadata, typed MaskRef/canvas/pixels/model lock, repeats
the occurrence and locator after evidence IO and again after signing, verifies
its own signature against the configured public key and emits canonical bytes.
`ProcessPersonOriginSigner` now accepts only a process-provisioned Ed25519
`crypto.Signer` whose public key matches the issuer configuration. It signs
owned, bounded bytes and verifies the returned signature against that pinned
public key. Provisioning and startup installation remain outstanding.
The occurrence authority and production signer installation are not yet wired;
the issuer is not reachable from the export API.
The current `mask.person` worker emits `selector: "person", protected: false`,
while `editor.ValidateOverride` admits that selector neither as a protected
binding (which requires `protected: true`) nor for the existing unprotected
purposes. The SQL override provenance gate also admits only `mask.rasterize`
and `mask.prompt` producers. Therefore a purported person-mask Override
occurrence is currently impossible through the editor API. The source contract
must gain an explicit person-purpose admission and exact `mask.person` producer
lineage before an occurrence authority can sign any such receipt. An occurrence
checker that merely finds the mask SHA in a recipe would be unsound.
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

`VerifyImportedPersonOriginFile` now joins the validated parsed manifest to
the exact staged `PersonOriginReceipt` file and its SHA, requires one mapping
for the caller's expected entry/refs, then invokes the process-trust Ed25519
verifier over the actual receipt bytes. It returns the staged file ref for
later version-pinned object and SQL checks. A parsed mapping or valid signature
alone remains insufficient for origin acceptance or user consent.

The Python and Go pure contracts and negative unit scenarios are authored but
not executed.
No exporter signer, qualified receipt publisher, SQL receipt authority, process trust
bootstrap or end-to-end verification has been installed yet. Production
person-mask import remains unavailable.
