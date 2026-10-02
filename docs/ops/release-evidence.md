# Release evidence

`make release-evidence` creates `release/manifest.json` and `release/sbom.cdx.json` from repository-local lockfiles. The SBOM is CycloneDX 1.5 and contains normalized, sorted PURLs for Python, Go, portal npm and photographer npm dependencies.

`make release-check` rebuilds the evidence in memory and fails on drift. A release candidate must have `dirty: false`; the immutable source commit and every lockfile SHA-256 are recorded in the manifest. The SBOM digest covers its canonical JSON representation.

This gate proves dependency inventory and lockfile identity. It does not replace image/model lock qualification, container image signing, vulnerability policy, native signing, restore, performance, visual or hardware acceptance.
