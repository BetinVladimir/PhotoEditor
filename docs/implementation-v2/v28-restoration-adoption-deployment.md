# V28 restoration adoption runtime

The scheduler installs glare/reflection result adoption only when
`PHOTO_V28_RESTORATION_ADOPTION_ENABLED=true`. Set
`PHOTO_V28_RESTORATION_CATALOG_DATABASE_URL` to a separate connection permitted
to assume `photo_api`; the scheduler's normal database connection assumes
`photo_scheduler`. Both roles are checked at startup. The catalog pool is
closed during scheduler shutdown.

Startup requires the scheduler's dedicated candidate, manual-origin, result
document/array, atomic adoption and review-creation functions to exist as `SECURITY DEFINER`
functions owned by `photo_fn_owner`, with execute grants for `photo_scheduler`.
The editor pool must see the current `v2_draft_artifact_scope` function. A
missing dependency rejects installation rather than running a partial loop.

The adoption loop discovers only succeeded, generic-adopted tasks with a
canonical result and no V28 receipt. For each candidate it replays the
original manual-mask authority, reads immutable result objects, verifies
physical image/mask content, and commits a receipt and pending review in one
database transaction. Temporary failures retain
the candidate in the durable SQL queue and are retried after cursor wrap.
Metrics expose completed receipts and failures; enabled-loop failure affects
scheduler readiness. This installation does not approve the result or Apply it.

The migrations `01895`–`01907`, worker model installation, qualified weights,
dedicated human review/Apply and physical end-to-end acceptance remain
separate prerequisites for making the operations available to users. The new
runtime and unit scenarios are authored but have not been executed under the
current code-first phase.
