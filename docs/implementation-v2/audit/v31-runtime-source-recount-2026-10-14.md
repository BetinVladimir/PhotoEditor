# V31 runtime source recount — 2026-10-14

This is a source inspection of the current checkout, not an executed acceptance result. Earlier audit entries that call the V31 API/main or scheduler entrypoint entirely absent are superseded for these exact paths.

- `backend/cmd/photo/main.go` calls `installAPISigning`, passes the returned tenant router to `httpapi.Server.ExportSigner` and Delivery policy checking, and closes the installation at shutdown.
- `backend/cmd/photo/signing_installation.go` installs a dedicated `photo_scheduler` pool for the API path, checks replay SQL ownership/privileges, and supplies `schedulerSigningFactory` for Delivery. It refuses a declared deployment without process-provided tenant trust/TSA handles.
- `backend/internal/httpapi/provenance.go` registers authenticated signing-policy discovery and `POST /exports/{id}/provenance` with an explicit unavailable response when the signer is not installed.
- `workers/photo_worker/runtime/signer_service.py` provides a dedicated CPU `provenance.sign` worker with exact SQL operation registration checks. `run_deployed_signer` requires pinned runtime files and independent native authority/inspector/binding handles.
- `workers/photo_worker/operations/provenance/sdk.py` stages through the C2PA Builder/Reader bridge and checks signed container, pixels, binding and output hashes before reporting success.

No V31 acceptance is inferred from these paths. The current checkout still needs a concrete qualified native SDK/key/trust/TSA deployment, installed tenant policy and operator evidence, physical migrations and role checks, real JPEG/PNG sign→verify/tamper and key-rotation scenarios, and current full API→scheduler→worker→adoption→Delivery evidence. The authored tests and migration files in this coding phase have not been run. Keep `provenance=required` unavailable when the qualified installation is absent.
