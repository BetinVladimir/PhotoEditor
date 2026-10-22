# Reviewer access instructions — operator completion required

INTERNAL TESTING ONLY. This draft contains no credentials and must not be pasted as completed reviewer access. No account was created, no agreement accepted, no store submission performed.

Before inviting testers: operator must supply exact signed build/version, iOS bundle ID/Android applicationId, stable HTTPS API environment, enabled services/processing allowlist, approved test workspace, actual reviewer mailbox access and secure contact channel. Current app.json does not establish bundleIdentifier/package or a signing/build pipeline. app.config.js supplies Framevela name/icons, not release readiness.

Authentication is email plus one-time code (app/login.tsx), not a fabricated password. Provide a real permitted mailbox whose OTP the reviewer can retrieve through an approved process. Do not publish an OTP, refresh token, production password or private client link in store metadata. If unattended review is required, operator must establish a genuine compliant access flow; no authentication bypass is proposed.

Proposed steps once provisioned: launch → choose EN/RU/BG → enter assigned email → retrieve genuine code → open assigned workspace → use only seeded permission-cleared project/photos → follow beta-testing.md. Provide actual photographer gallery link separately only if that workflow is enabled. Disclose unavailable worker operations and known defects for this exact build.

Account deletion: tenant/workspace deletion is not automatically account deletion. Operator must confirm in-app account-deletion capability and external request URL, retention exceptions and support identity before completing store forms. This draft cannot assert those flows exist.

Local V20–V32 diagnostics and source/component tests are not TestFlight/Play acceptance. Latest runtime status must be checked from the current audit, not treated as production deployment. No video-editing claim is authorized by the camera/play/AI logo.
