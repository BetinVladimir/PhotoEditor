# V30 preset metadata policy editing

Status: authored, not qualified. Tests, builds and schema validators have not run.

`PATCH /api/v1/tenants/{t}/export-presets/{preset}/metadata-policy` requires user authentication, editor rights, web CSRF and Idempotency-Key. Body: `{ "revision": "1", "metadata_policy": "copyright" }`. Closed modes: strip, copyright, approved_exif. Revision is canonical decimal 1..9007199254740990.

Service `ChangeMetadataPolicy` takes the tenant editor transaction, enters `ExportPreset.MetadataPolicy` idempotency scope with preset ID and full change, locks the active tenant preset, rejects a stale revision with REVISION_CONFLICT/409, changes only metadata_policy in raw config, validates the resulting config, recomputes canonical hash and increments revision. No new migration or storage object is required. Existing frozen preset documents and active deliveries retain their config/hash. Metadata approval fields, particularly GPS, remain per-delivery explicit consent.

Success is 200 ExportPresetResource. Same-key exact replay returns the persisted response with Idempotency-Replayed=true, even if the preset later changes. A changed request under the same key follows the existing idempotency conflict behavior. Missing or archived preset returns 404; tenant access denial returns 403; invalid mode/revision returns 422. Transaction rollback prevents partial revision/config/hash updates.

OwnerAPI `changePresetMetadataPolicy` accepts an explicit key and AbortSignal. Mounted editor freezes policy/key on first attempt; unknown-result retry sends the same request. It locks mode edits after first attempt, treats 403/404/409 as terminal for that editor, aborts on unmount and ignores late results. Closing after a conflict requires refreshing the preset list before a new revision can be edited.

Outstanding qualification: positive tenant-isolated SQL fixture, concurrent edits, replay after unknown commit, stale/revoked scope, preserved sharpen/size/watermark/matte fields, current rendered UI and full V30 lifecycle. Installation/source presence is not completion evidence.
