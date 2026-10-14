# V31: live signer possession boundary

`ChallengeSigningOperatorAuthority` implements the API-side SigningOperatorAuthority over an independently authenticated isolated signer transport. Installation supplies owned PEM certificate-chain bytes, a SigningChallengeSigner, mandatory SigningLiveTrustAuthority and a clock. Private key material never enters the API process.

For each check, the authority validates the complete canonical SigningPolicy and exact certificate-chain SHA, checks current validity of every installed certificate, and invokes independent live trust/runtime authority. It creates a cryptographically random 32-byte nonce and canonical payload with exactly domain `photoeditor.signing-operator-possession/v1`, policy_sha256 and hexadecimal nonce. The installed key signs this exact payload. The API verifies RSA-PSS SHA256 with digest-length salt, ECDSA SHA256/384/512 for P-256/384/521 respectively, or Ed25519 against the pinned leaf certificate. RSA keys are bounded to 2048–8192 bits; signatures to 1024 bytes. After key IO the full live trust/validity check repeats. Failure, revocation, expiry, cancellation, replay or a wrong signature never yields operator approval.

The signer-side NativeSigningDelegate.sign_operator_challenge accepts only the closed canonical envelope for the exact installed policy/key and compatible algorithm. It repeats actual native installation inspection before and after key access. RSA installations using PS384/PS512 may provide a PS256 possession proof using the same installed key; export signing algorithms remain unchanged. The proof does not sign an export, grant a task admission, verify a C2PA manifest, or replace native SDK qualification, certificate-chain trust, revocation or timestamp service policy.

This handler is deliberately not a public endpoint. Production requires authenticated isolated transport restricted to the API service, bounded requests/timeouts and current native trust provider. Main/API installation, transport deployment, hardware key execution and full native acceptance remain open. No private key or trust configuration is accepted from jobs or HTTP signing requests.

Tests authored, NOT RUN: five actual cryptographic key-family scenarios with callback ownership/fresh challenges; six replay/signature/revocation/expiry/pin/cancellation scenarios; eight worker envelope/policy/algorithm/key/native revocation scenarios. Certificates in unit tests are self-signed fixtures and do not establish production trust. No migrations or native runtime executed.

## API HTTPS transport

HTTPSigningChallengeSigner fixes the private route /internal/v1/operator-challenge on an operator-supplied HTTPS origin. It requires CA roots, API mTLS identity and exact signer leaf-certificate SHA; TLS1.3, verified-chain current validity, no redirects/proxy/compression, 1–30 second timeout, bounded connections/headers and a 4096-byte response are enforced. The closed canonical request contains schema_version, key_handle, algorithm and payload_hex; response contains schema_version and signature_hex. Noncanonical/unknown fields, encoding, oversized replies, non-200 status and invalid lowercase hex fail. Transport credentials identify the API service and are separate from the image signing key. Eleven real in-process mTLS/protocol scenarios and configuration refusal tests are authored, NOT RUN. Signer-side authenticated endpoint and process wiring remain open.

## Isolated signer endpoint

SigningChallengeApplication owns pinned API leaf hashes and installed policy bytes; only the exact policy SHA from the bounded inner challenge selects a policy. InstalledSigningOperator repeats full operator authority before/after the native delegate, which independently repeats native inspection. build_signer_challenge_server requires TLS1.3 server SSLContext with CERT_REQUIRED; production provisioning must load server chain/key and client CA before construction. It adds an exact-client leaf allowlist, 8KiB header/9KiB body bounds, 1–30s timeout and 1–16 concurrency bound; rejects ambiguous lengths/transfer encodings, closes connections and logs no request or native error details. The application alone is not TLS chain verification. Process lifecycle/configuration and live trust installation are still required; socket/hardware acceptance has not run. Protocol and installation tests are authored, NOT RUN.


### Deferred real TLS endpoint scenarios

`workers/tests/test_signer_challenge_mtls.py` authors eight loopback TLS1.3 scenarios with a synthetic CA and mandatory client certificate validation. A valid-chain client outside the exact leaf allowlist receives 403; missing identity fails TLS before signing. Closed HTTP routing/length/encoding/header/body limits refuse before the key callback. Server and thread are closed on all exits. These scenarios are NOT RUN and use a native signing double: they confer no SDK, production trust or deployment acceptance. Cross-language Go-to-Python acceptance and lifecycle installation remain required.


### Handshake admission

The socket accept loop performs no TLS handshake. Each accepted raw socket must acquire a bounded concurrency slot before its request thread performs mandatory mTLS; handshake failures close the connection and release the slot. Thus a stalled ClientHello consumes one slot, not the complete accept loop. Socket timeout applies during handshake. The deferred real-socket test keeps one peer silent and requires a second authenticated request to finish within a shorter deadline. NOT RUN. Native callbacks still require their own bounded/cancellable implementation; a socket timeout does not interrupt arbitrary native code.


### Shutdown ownership

Server shutdown marks the endpoint stopped before closing tracked raw and TLS connections. Admission and atomic raw-to-TLS descriptor ownership share a lock; request guards check stopped state around native signing. A signature returned after stop cannot become a successful challenge response. Native calls are not forcibly interrupted by Python socket closure and must retain their own timeouts/cancellation. Deferred socket scenario covers stop inside the key callback and idempotent server close; NOT RUN.


### Owned listener scope

`runtime/signer_challenge_lifecycle.py::running_signer_challenges` binds before yielding a live guard, closes active sockets/listener on scope exit, and joins the serving thread. Unexpected listener exit invalidates the guard. Process composition must pass that guard into native checks and keep the worker inside the scope. This primitive is not yet installed by `run_deployed_signer`; it does not substitute for shared operator identity or native/live trust authority. Five deferred lifecycle cases are authored, NOT RUN.


### Explicit deployed worker composition

`run_deployed_signer(..., challenge_listener=SigningChallengeListenerInstallation(...))` now installs the listener around the worker lifecycle. Executor construction hands off its exact InstalledSigningOperator only after SQL isolation, registration and native policy checks. The listener uses the deployment policy bytes and this same operator. Its guard composes with native authority and PKCS11 cancellation; stopped guard remains attached after exit. No endpoint is implicitly enabled by environment/task JSON. Existing signer-only composition remains available without a listener. Concrete live trust and API main provisioning plus end-to-end native acceptance remain open. Operator handoff tests authored, NOT RUN.


### Pinned X.509 authority

`NewCertificateSigningTrustAuthority` verifies actual leaf/intermediate chains against owned, SHA-pinned operator CA roots with no system-root fallback or network issuer discovery. Trusted installation supplies qualified EKUs; Any is forbidden and native SDK certificate-profile checks remain mandatory. Exact policy/chain/trust pins, leaf digital signature/non-CA constraints, certificate validity and context are rechecked after native, required revocation and required timestamp service calls. Missing required service refuses. Callback inputs are owned. Network revocation/TSA services, native qualification and API main installation remain open. Generated-chain tests authored, NOT RUN; unit certificates are not production trust evidence.


### Full signed CRL verification

`NewSigningCRLCheck` supplies a revocation callback from pinned issuer certificates and an operator-owned source returning complete DER CRLs. It verifies issuer/signature/CRLSign, current ThisUpdate/NextUpdate with configured maximum age (1 minute–24 hours), bounded bytes and revoked serials for all non-anchor chain certificates. Delta, partitioned, indirect or unsupported critical extensions refuse. Self-signed pinned CA anchors require independent live trust removal checks. No certificate/task distribution URL is fetched implicitly. Source transport, live native/root and timestamp checks remain required. Ten generated-CRL tests authored NOT RUN.


### HTTPS CRL source

`NewHTTPSigningCRLSource` maps exact issuer DER SHA to trusted installed HTTPS endpoints with explicit CA roots and exact server leaf pins. `ReadCRL` validates issuer identity before IO and returns at most2MiB of application/pkix-crl bytes over TLS1.3, with no redirects/proxy/compression and bounded timeout/header/connections. Compose its method as the source passed to `NewSigningCRLCheck`; transport alone never establishes revocation status. Close idle connections on lifecycle exit. Nine TLS transport scenarios authored NOT RUN. Source configuration/main installation, live root/native authority and TSA remain open.


### API authority and SQL service ownership

`NewInstalledChallengeSigningAuthority(SigningOperatorInstallation)` composes the pinned mTLS key challenge, actual X.509 trust, optional installed HTTPS CRL source/full-CRL validation, mandatory live native/root authority and explicit TSA check. Missing required revocation/TSA still refuses at policy use. Constructor failure closes created transports. Close is idempotent, cancels in-flight request contexts and prevents subsequent approvals. `NewInstalledSQLSigningService` installs this authority into the existing render/presentation SQL signing service and closes it on service-construction failure; callers own Close on success and must install SQLSignExportService in HTTP API. Main/provisioning and actual live native/root/TSA remain open. Lifecycle/refusal tests authored NOT RUN.


### Policy installation coverage

Before SQL service construction the owned installer validates the complete policy registry and exact chain/trust pins against operator bytes. Required TSA policies need an explicit TSA callback; required revocation policies need an installed CRL endpoint for every non-anchor issuer. Wrong/missing/duplicate issuer declarations refuse before service construction. This structural check performs no native/TSA/network IO and grants no live approval. Seven generated-certificate tests authored NOT RUN.


### CRL observation checkpoints

The CRL verifier retains synchronized per-issuer number/ThisUpdate/DER-SHA checkpoints, bounded to8192 issuers. It refuses lower number/time or differing bytes at an observed equal number. A structurally/cryptographically valid revoking CRL advances the checkpoint before certificate refusal, preventing a later older clean list from reversing observed revocation. This is process-local protection only; durable cross-restart checkpoints remain open. Four additional sequential observation tests authored NOT RUN.


### Durable CRL checkpoints

Forward-only01584 adds global operator trust metadata (not tenant/customer data): enabled issuer/trust-bundle allowlist and monotonic checkpoints. Provisioning installs issuers; API only executes the narrow observe function and cannot mutate tables directly. `SQLSigningCRLCheckpoint` is scoped to an installed trust bundle. `NewDurableSigningCRLCheck` records observations before revocation refusal; assembled API operator now requires a durable store whenever CRL endpoints are configured. PostgreSQL atomic upsert rejects rollback and equal-number equivocation across instances/restarts. No migration applied; SQL role/monotonic scenarios authored NOT RUN. Startup signature checks, provisioning and populated concurrency acceptance remain open.


### Durable installation startup validation

Assembled API CRL authority now requires `SigningCRLInstallationCheckpoint` and performs bounded startup validation. SQL implementation checks exact trust scope, effective API role and absence of direct table mutation; both narrow functions must have expected SECURITY DEFINER owner, pinned search_path, allowed API execution and no PUBLIC/unrelated service execution. Readiness checks all configured issuer pins are enabled and unique. Added startup/refusal/readiness scenarios NOT RUN;01584 remains unapplied. No live trust approval is inferred from readiness.


### Concurrent observation acceptance scenarios

SQL and Go bound CRL numbers to160 bits. Deferred isolated-DB test starts32 concurrent transactions assuming photo_api and verifies the maximum number/SHA survives, followed by lower-number and equal-number changed-SHA refusal and exact replay acceptance. Fixtures use a unique test trust pin; administrative cleanup removes only those test-created rows. Neither this test nor01584 has been executed.


### Absolute HTTP deadline

After TLS handshake, an absolute request timer starts before request-line/header parsing and closes the socket at timeout regardless of partial input. The task guard and final pre-response guard share this deadline. Handshake retains its own bounded timeout; native code still needs its own bounded cancellation. Two deferred real-TLS slow-drip tests cover incomplete headers/body while sending bytes every50ms; NOT RUN.


### Tenant-scoped API registration checks

01585 provides a narrow API-only probe of an explicitly provisioned tenant signer, exact SDK/certificate/trust/qualification pins, dedicated CPU operation isolation, enabled/unexpired qualification and30s heartbeat freshness. `SQLSigningRegistrationTrust.Check` binds tenant/worker from trusted provisioning, freezes canonical policy/chain and checks SQL registration before/after mandatory independent live native/root trust. It neither attests native behavior nor renews registration. The isolated endpoint still checks its actual native operator. Missing-dependency/cancel tests authored NOT RUN; migration NOT APPLIED. Positive SQL/native lifecycle and main remain open.


### Registration lifecycle scenarios

Deferred SQL scenarios create a provisioned signer in a rolled-back tenant fixture, assume photo_api for every probe, and cover exact readiness, absent/foreign scope, worker and pin substitution, disable/qualification expiry/heartbeat staleness and role ACL. Missing tenant context explicitly returns false. Tests NOT RUN;01585 remains unapplied and does not establish native/TSA acceptance.


### Mandatory tenant registration service binding

`NewInstalledSQLSigningService` requires explicit Registration on the exact API pool and validates01585 function role/owner/ACL/search_path at bounded startup. It composes mandatory Native with registration-before/after checks. Install the returned wrapper at HTTP boundary: its SignExport and AvailableSigningPolicies enforce the bound TenantID before downstream IO. Do not expose the embedded unscoped service. Multi-tenant router and main provisioning remain open. Tenant/cancel/startup tests authored NOT RUN.


### Multi-tenant API routing

`NewTenantSigningRouter` owns a copied catalogue of installed tenant-scoped services and implements existing HTTP ExportSigner/SigningPolicyLister. Unknown tenants never fall back to another operator. Duplicate tenants, shared authority instances and closed installations refuse; failed construction retains caller lifecycle ownership. Successful router Close cancels all owned authorities, and new routes refuse. Source/catalog SQL still performs caller authorization; routing is not an authorization grant. Install router into HTTP Server.ExportSigner and Close on process exit. Main/provisioning remains open. Ownership/isolation/lifecycle tests and interface assertions authored NOT RUN.


### Atomic tenant catalogue startup

`NewInstalledTenantSigningRouter(ctx, configs)` validates and copies tenant/worker registration declarations before resources, installs every full SQL/operator service, and returns a router only on complete success. Any failure, partial returned service, tenant mismatch or cancellation closes all created authorities. Context-aware constructors propagate startup cancellation into SQL readiness. Main must install only the returned complete router and close it on exit. Five atomic lifecycle modes and duplicate/cancel scenarios authored NOT RUN. Main/config/live native-root/TSA remain open.


### Startup declaration ownership

Before external startup callbacks the atomic catalogue bounds mutable provisioning bytes to128MiB and freezes reports/policy bodies, key/TSA lists, chain/trust bytes, EKUs/endpoints, TLS identity certificate/OCSP bytes, cloned root pools and registration values. DB/object/native/private-key handles remain explicitly trusted process interfaces. Callback mutation of later tenant declarations cannot alter the frozen catalogue. Ownership test authored NOT RUN.


### Tenant-bound policy/resource installation

Installed SQL service projects the policy registry to registration TenantID during construction and binds a copied measured resource qualifier to the same registry. Router rejects unbound/mismatched registries. The SQL service itself rejects a foreign bound tenant before historical ticket lookup, in addition to wrapper/API isolation. Rebinding to another tenant or mismatched resource registry refuses without mutation. Tests authored NOT RUN.


### Preserve negative processing ancestry

Service installation accepts explicit historical NegativePolicies. Configured entries select existing full optical.negative + render + presentation source, including independent qualification, pixel/mask/source, review and producer evidence. Explicit empty or invalid policies refuse rather than dropping to a narrower source. Atomic tenant catalogue owns lock slices and charges their declaration bytes. Constructor matrix and empty-refusal tests authored NOT RUN. Deployment catalogue/main remains open.


### Integrated installed operator scenarios

`operator_installation_crypto_test.go` connects the actual installed authority through real TLS1.3 mutual authentication to a synthetic Ed25519 external-key endpoint. Generated CodeSigning chain, policy SHA/domain/nonce and signature are verified by actual components. Eight modes cover success and signature/native-revocation/required-service/pin/closed/cancel refusals, including no-key-IO assertions. Native callback remains synthetic; no production qualification implied. Tests NOT RUN.


### Public API deployment schema

`DecodeSigningAPIDeploymentDocument` accepts only pinned canonical signing-api-deployment/v1 (8MiB,1–256 tenants). It describes public certificates/roots/pins, tenant/worker qualification, policy bodies/windows, resource report and optional negative policies. API TLS identity is referenced by installed ID; private keys, native callbacks or import hooks are not JSON fields. Decoder validates closed nested schemas/pins/URLs/uniqueness and certificate roots but grants no native trust or runtime admission. Thirteen deferred scenarios authored NOT RUN. Safe file loading, explicit live dependency resolution and main remain open.


### Pinned deployment file loader

`LoadSigningAPIDeploymentDocument` requires an absolute normalized path and externally installed SHA. It refuses nonregular/multiple-link/group-or-public-writable/out-of-envelope files; uses nofollow/nonblocking open, bounded read, and inode/size/mtime/path rechecks before closed canonical schema validation. File hash alone grants no native trust. Eleven file modes plus schema-after-pin scenario authored NOT RUN. Explicit runtime identity/native/TSA resolution and main remain open.


### Deployment runtime resolution

`InstallSigningAPIDeployment/File` resolves every tenant against process-installed worker/qualification/identity-ID and mTLS/native/TSA handles before atomic service creation. Shared Base supplies only API/scheduler pools, objects, bucket, working ICC and clock; competing policy/authority/report config refuses. It derives pinned policies/windows, resource installation, optional negative locks, transport roots, CRL source and durable SQL store plus tenant registration. Native/private-key callbacks never originate in JSON. Ten runtime resolution/ownership/cancel scenarios authored NOT RUN. Main and actual live dependency provisioning remain open.


### API process integration

API process assembly now accepts explicit trusted `apiProcessDependencies.SigningTenants`, validates PHOTO_SIGNING_DEPLOYMENT_FILE/SHA before API resources, opens a separate photo_scheduler pool with role validation, installs the pinned tenant catalogue and assigns its router to HTTP Server.ExportSigner. Scope exit/failure closes authorities and scheduler pool. Standard `run()` has no concrete native/TSA/identity bootstrap yet: declaring signing enabled there refuses startup. This is process wiring, not proof of deployed provenance.sign.execute. Eight declaration and disabled/cancel scenarios authored NOT RUN; native bootstrap/TSA and full deployment acceptance remain open.


### Concrete remote-native callback

`NewRemoteSigningNativeTrust` owns an exact installed policy/chain catalogue and verifies a fresh signed proof through the pinned mTLS signer endpoint, whose actual native operator inspects installation before/after key invocation. Mandatory independent chain/root/revocation/TSA authority must not call back into this adapter: recursive trust cycles are invalid provisioning. The callback is not hardware attestation and cannot replace independent trust. Close cancels current transport requests; unknown/closed policy inputs refuse before key IO. Seven real TLS/Ed25519 scenarios authored NOT RUN with synthetic independent-trust double. Default bootstrap and actual trust/TSA remain open.


### Remote trust invocation cycles

RemoteSigningNativeTrust carries a private context-local invocation stack. Re-entering the same authority, including A→B→A, or nesting beyond sixteen authorities refuses before key IO. Independent trust callbacks must propagate the supplied context; dropping it defeats cancellation and cycle tracking and is outside this installation contract. Concurrent requests have independent stacks. Unit scenarios authored, NOT RUN.


### Exact installed TSA routing

`NewSigningTimestampRouter` owns a closed required-timestamp policy catalogue and exact service-ID callback map. `Check` requires exact canonical installed policy bytes and certificate-chain SHA, invokes only the selected installed service with owned inputs, and repeats cancellation after callback. Missing/nil/unused services and duplicate policies refuse at construction. Runtime `TimestampServices` installs this router before any authority IO and is mutually exclusive with generic `Timestamp`. Service callbacks remain responsible for real qualified timestamp trust/token verification: routing is not RFC3161 validation or approval based on endpoint reachability. Tests authored NOT RUN; live TSA bootstrap and full acceptance remain open.
