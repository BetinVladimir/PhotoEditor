# Очередь задач для кодогенерации (порядок выполнения)

Файл сгенерирован `tools/make_index.py` (порядок внутри этапа = топологический по `depends`). Всего карточек: **312**.

Размер: **XS** ≈ 1 правка / ≤ 60 строк, **S** ≈ 1–3 файла / ≤ 200 строк. Карточки с инструментом (`move_routes.py`, `move_methods.py`, `extract_stmts.py`, `cards_from_plan.py`) модель не переписывает код, а запускает команду. Что остаётся человеку - в [NOT-CODEGEN.md](NOT-CODEGEN.md).

Карточка запускается, когда выполнены все `depends`; после каждой зелёной - `git commit -am "agent(<id>): <title>"`.

## Этап A — Проверить то, что уже внесено в волне 1 (тесты и рефакторинг без изменения поведения)

| Карточка | Размер | Что | После |
|---|---|---|---|
| `B-0a` | XS | Config tests for TRUSTED_PROXY_CIDRS | — |
| `P-1b` | XS | mvp-profile - isRouteEnabled helper | — |
| `P-1c` | XS | Hide the interchange-import button on the project screen in the MVP profile | `P-1b` |
| `P-1d` | XS | Hide the style-training button in the MVP profile | `P-1b` |
| `P-1e` | XS | Hide the editor "export interchange bundle" button in the MVP profile | `P-1b` |
| `P-1f` | XS | MVP guard for the non-localized routes in app/_layout.tsx | `P-1b` |
| `P-4b` | S | Guard test - every clear*Attempts store is cleared at logout | — |
| `PT-1t` | XS | Tests - no production domain in LocaleLinks/metadata, share page has no alternates | — |
| `PT-3t` | XS | Tests - caller-supplied Idempotency-Key for download intents | — |
| `W-1a` | S | Extract the task heartbeat loop into runtime/heartbeat.py (no behaviour change) | — |
| `W-1b` | S | Unit tests for heartbeat tolerance (run_heartbeat_loop) | `W-1a` |
| `W-3a` | XS | Tests for Executor._commit_with_retry | — |

## Этап B — Надёжность worker по VPN

| Карточка | Размер | Что | После |
|---|---|---|---|
| `W-12a` | XS | Clock skew guard module and TaskDatabase.server_now | — |
| `W-12b` | XS | Run the clock skew guard at worker start | `W-12a` |
| `W-2a` | S | Transport.paused - stop taking new tasks without dropping the active one | — |
| `W-2b` | S | CapacityReporter degraded/recovered callbacks | — |
| `W-2c` | XS | Wire reporter degradation to Transport.paused in _run_built_worker | `W-2a` `W-2b` |
| `W-3b` | S | Durable commit spool module (runtime/commit_spool.py) | — |
| `W-3c` | S | Spool the result before commit in Executor.process | `W-3b` |
| `W-3d` | S | Deliver spooled results on worker start | `W-3b` `W-3c` |
| `W-4a` | S | TaskDatabase._connect - env timeouts and TCP keepalive for VPN | — |
| `W-4b` | XS | TaskDatabase._query - retry connection failures only | `W-4a` |
| `W-5a` | XS | Deterministic Idempotency-Key for output uploads | — |
| `W-5b1` | S | Gateway.open_range - validated ranged GET (block <= 16 MiB) | — |
| `W-5b2` | S | ArtifactStream resumes a broken download via open_range | `W-5b1` |
| `W-6a` | S | AMQP connection parameters for VPN (timeouts, keepalive, client TLS) | — |
| `W-6b` | S | RabbitMQ over TLS (5671) - config file and compose wiring | — |
| `W-6c` | S | provision.py - broker certificate and amqps URL | `W-6b` |

## Этап C — Backend: лимиты, метрики, профиль, аудит

| Карточка | Размер | Что | После |
|---|---|---|---|
| `B-1a` | S | Rate limiter core (token bucket, in memory) | — |
| `B-1b` | S | Server.limit middleware and portal session limit | `B-1a` |
| `B-1c` | XS | Apply limit classes to portal download/upload routes | `B-1b` |
| `B-2a` | S | Minimal Prometheus-text metrics registry (stdlib only) | — |
| `B-2b` | S | HTTP request metrics middleware | `B-2a` |
| `B-2c` | XS | Private metrics listener (METRICS_LISTEN) | `B-2a` |
| `B-4a` | S | Server-side MVP profile guard for deferred API groups | `B-0a` |
| `B-4b` | XS | Enable PHOTO_PROFILE=mvp and TRUSTED_PROXY_CIDRS in compose/env examples | `B-4a` |
| `B-5a` | S | Golden test of the HTTP route table (safety net for splitting catalogRoutes) | — |
| `B-8a` | S | Audit script for context.Background() and panic( in backend code | — |
| `O-2a` | XS | /health/version endpoint (build identity) | — |
| `X-7a` | S | Audit SECURITY DEFINER functions (search_path and owner) in migrations | — |

## Этап C2 — Backend: механическое разрезание `catalogRoutes` (B-5, инструмент `move_routes.py`, цепочка снизу вверх)

| Карточка | Размер | Что | После |
|---|---|---|---|
| `B-5m-Admin` | XS | Move catalogRoutes block "admin dead letters and legacy import reconcile" into catalogAdminRoutes | `B-5a` |
| `B-5m-Assets` | XS | Move catalogRoutes block "assets and media links" into catalogAssetsRoutes | `B-5a` |
| `B-5m-Capture` | XS | Move catalogRoutes block "capture receipts" into catalogCaptureRoutes | `B-5a` |
| `B-5m-Collections` | XS | Move catalogRoutes block "collections CRUD" into catalogCollectionsRoutes | `B-5a` |
| `B-5m-ContactSheet` | XS | Move catalogRoutes block "contact sheets" into catalogContactSheetRoutes | `B-5a` |
| `B-5m-Editing` | XS | Move catalogRoutes block "save-edits" into catalogEditingRoutes | `B-5a` |
| `B-5m-Exports` | XS | Move catalogRoutes block "exports" into catalogExportsRoutes | `B-5a` |
| `B-5m-Items` | XS | Move catalogRoutes block "collection items" into catalogItemsRoutes | `B-5a` |
| `B-5m-Me` | XS | Move catalogRoutes block "/me profile and sessions" into catalogMeRoutes | `B-5a` |
| `B-5m-Members` | XS | Move catalogRoutes block "tenant members" into catalogMembersRoutes | `B-5a` |
| `B-5m-Misc` | XS | Move catalogRoutes block "events" into catalogMiscRoutes | `B-5a` |
| `B-5m-Persons` | XS | Move catalogRoutes block "project persons" into catalogPersonsRoutes | `B-5a` |
| `B-5m-Projects` | XS | Move catalogRoutes block "projects and project ACL" into catalogProjectsRoutes | `B-5a` |
| `B-5m-Reviews` | XS | Move catalogRoutes block "reviews and result selections" into catalogReviewsRoutes | `B-5a` |
| `B-5m-Runs` | XS | Move catalogRoutes block "runs" into catalogRunsRoutes | `B-5a` |
| `B-5m-Sections` | XS | Move catalogRoutes block "guest uploads" into catalogSectionsRoutes | `B-5a` |
| `B-5m-SessionAnalysis` | XS | Move catalogRoutes block "session analyses and scene board" into catalogSessionAnalysisRoutes | `B-5a` |
| `B-5m-Shares` | XS | Move catalogRoutes block "shares" into catalogSharesRoutes | `B-5a` |
| `B-5m-ShootSession` | XS | Move catalogRoutes block "shoot-sessions CRUD" into catalogShootSessionRoutes | `B-5a` |
| `B-5m-Styles` | XS | Move catalogRoutes block "styles and style versions" into catalogStylesRoutes | `B-5a` |
| `B-5m-Tenants` | XS | Move catalogRoutes block "tenants and invites" into catalogTenantsRoutes | `B-5a` |
| `B-5m-Training` | XS | Move catalogRoutes block "training sets" into catalogTrainingRoutes | `B-5a` |
| `B-5m-Uploads` | XS | Move catalogRoutes block "uploads" into catalogUploadsRoutes | `B-5a` |
| `B-5m-V2Draft` | XS | Move catalogRoutes block "v2 layers/documents/drafts" into catalogV2DraftRoutes | `B-5a` |
| `B-5p` | XS | Hoist the draftsV2 Media configuration next to its declaration | `B-5a` |
| `B-5m-V2Workflow` | XS | Move catalogRoutes block "v2 workflows" into catalogV2WorkflowRoutes | `B-5a` `B-5p` |

## Этап D — Портал

| Карточка | Размер | Что | После |
|---|---|---|---|
| `PT-2a` | S | Portal security headers in next.config.ts (+ cache split) | — |
| `PT-4a` | S | Analysis - state map of Portal.tsx (input for its decomposition) | — |
| `PT-4t` | S | Characterization test for Portal (jsdom, stubbed fetch) before decomposition | `PT-4a` |
| `PT-6a` | XS | APIError.retryAfterSeconds from the Retry-After header | — |
| `PT-6b` | S | Rate-limit notice - pure helper and i18n keys | `PT-6a` |
| `PT-6c` | XS | Use noticeFor in Portal download paths | `PT-6b` |
| `PT-4b` | S | Portal: extract useAbortScope from Portal.tsx | `PT-4t` |
| `PT-4c` | S | Portal: extract usePortalSession from Portal.tsx | `PT-4b` |
| `PT-4d` | S | Portal: extract useMediaLinks from Portal.tsx | `PT-4c` |
| `PT-4e` | S | Portal: extract useGallery from Portal.tsx | `PT-4d` |
| `PT-4f` | S | Portal: extract useSelection from Portal.tsx | `PT-4e` |
| `PT-4g` | S | Portal: extract useDownloadJob from Portal.tsx | `PT-4f` |
| `PT-4h` | S | Portal: extract useGuestUpload from Portal.tsx | `PT-4g` |
| `PT-4i` | XS | Portal: final size check and import cleanup | `PT-4h` |

## Этап E — Фотограф: инструменты, темы, журнал мутаций, `OwnerAPI`

| Карточка | Размер | Что | После |
|---|---|---|---|
| `P-3a` | XS | Prettier configuration and scripts (no reformatting yet) | — |
| `P-4a-catalog-create` | S | Analysis - catalog-create attempt store (input for the mutation journal) | — |
| `P-4a-collection-create` | S | Analysis - collection-create attempt store (input for the mutation journal) | — |
| `P-4a-export-batch` | S | Analysis - export-batch attempt store (input for the mutation journal) | — |
| `P-4c` | S | Analysis - common contract of the three pilot stores (mutation journal spec) | `P-4a-catalog-create` `P-4a-collection-create` `P-4a-export-batch` |
| `P-4d` | S | Mutation journal core module (src/mutation-journal.ts) from the approved contract | `P-4c` |
| `P-4e-catalog-create` | S | Migrate catalog-create attempt store onto mutation-journal (pilot) | `P-4d` |
| `P-4e-collection-create` | S | Migrate collection-create attempt store onto mutation-journal (pilot) | `P-4d` `P-4e-catalog-create` |
| `P-4e-export-batch` | S | Migrate export-batch attempt store onto mutation-journal (pilot) | `P-4d` `P-4e-collection-create` |
| `P-4h` | S | Analysis - migration recipe from the three pilot diffs | `P-4e-catalog-create` `P-4e-collection-create` `P-4e-export-batch` |
| `P-5p` | XS | OwnerAPI: make members public and move requireArchiveUUID to api/shared.ts (mechanical) | — |
| `P-6a` | S | WCAG contrast checker for theme tokens | — |
| `P-6b` | S | Analysis - map every hard-coded colour literal to a theme token | — |
| `P-6c` | S | Theme mode: system / light / dark with a persisted choice | — |
| `P-6d` | S | Settings: theme mode selector on the profile screen | `P-6c` |
| `P-6f-screens-collections-id-apply` | XS | Theme tokens in screens/collections/[id]/apply.tsx | `P-6b` `P-6c` |
| `P-6f-screens-collections-id-index` | XS | Theme tokens in screens/collections/[id]/index.tsx | `P-6b` `P-6c` |
| `P-6f-screens-exports-index` | XS | Theme tokens in screens/exports/index.tsx | `P-6b` `P-6c` |
| `P-6f-screens-imports-new` | XS | Theme tokens in screens/imports/new.tsx | `P-6b` `P-6c` |
| `P-6f-screens-login` | XS | Theme tokens in screens/login.tsx | `P-6b` `P-6c` |
| `P-6f-screens-runs-id` | XS | Theme tokens in screens/runs/[id].tsx | `P-6b` `P-6c` |
| `P-6f-screens-settings-members` | XS | Theme tokens in screens/settings/members.tsx | `P-6b` `P-6c` |
| `P-6f-screens-settings-profile` | XS | Theme tokens in screens/settings/profile.tsx | `P-6b` `P-6c` |
| `P-6f-screens-settings-retention` | XS | Theme tokens in screens/settings/retention.tsx | `P-6b` `P-6c` |
| `P-6f-screens-styles-id-training` | XS | Theme tokens in screens/styles/[id]/training.tsx | `P-6b` `P-6c` |
| `P-6f-screens-workspaces` | XS | Theme tokens in screens/workspaces.tsx | `P-6b` `P-6c` |
| `P-6f-src-auth-context` | XS | Theme tokens in src/auth-context.tsx | `P-6b` `P-6c` |
| `P-6f-src-compare-canvas` | XS | Theme tokens in src/compare-canvas.tsx | `P-6b` `P-6c` |
| `P-6f-src-compare-frames-view` | XS | Theme tokens in src/compare-frames-view.tsx | `P-6b` `P-6c` |
| `P-6f-src-interchange-person-mask-screen` | XS | Theme tokens in src/interchange-person-mask-screen.tsx | `P-6b` `P-6c` |
| `P-6f-src-interchange-prepared-review-screen` | XS | Theme tokens in src/interchange-prepared-review-screen.tsx | `P-6b` `P-6c` |
| `P-6f-src-lens-preview-view` | XS | Theme tokens in src/lens-preview-view.tsx | `P-6b` `P-6c` |
| `P-6f-src-mask-canvas` | XS | Theme tokens in src/mask-canvas.tsx | `P-6b` `P-6c` |
| `P-6f-src-multi-preview-view` | XS | Theme tokens in src/multi-preview-view.tsx | `P-6b` `P-6c` |
| `P-6f-src-perspective-canvas` | XS | Theme tokens in src/perspective-canvas.tsx | `P-6b` `P-6c` |
| `P-6f-src-perspective-lines-canvas` | XS | Theme tokens in src/perspective-lines-canvas.tsx | `P-6b` `P-6c` |
| `P-6f-src-perspective-preview-view` | XS | Theme tokens in src/perspective-preview-view.tsx | `P-6b` `P-6c` |
| `P-6f-src-read-resource-state` | XS | Theme tokens in src/read-resource-state.tsx | `P-6b` `P-6c` |
| `P-6f-src-region-loupe` | XS | Theme tokens in src/region-loupe.tsx | `P-6b` `P-6c` |
| `P-7a` | S | Adaptive upload concurrency policy (pure function) | — |
| `P-8a` | XS | Reproducible web build script and wrangler path | — |
| `P-4f` | S | Journal registry - logout clears every registered journal (and a test proves it) | `P-4h` |
| `P-4g-access-link` | S | Migrate access-link attempt store onto mutation-journal | `P-4f` |
| `P-4g-capture-create` | S | Migrate capture-create attempt store onto mutation-journal | `P-4f` |
| `P-4g-contact-sheet` | S | Migrate contact-sheet attempt store onto mutation-journal | `P-4f` |
| `P-4g-culling-decision` | S | Migrate culling-decision attempt store onto mutation-journal | `P-4f` |
| `P-4g-delivery-create` | S | Migrate delivery-create attempt store onto mutation-journal | `P-4f` |
| `P-4g-delivery-publish` | S | Migrate delivery-publish attempt store onto mutation-journal | `P-4f` |
| `P-4g-glare-donor` | S | Migrate glare-donor attempt store onto mutation-journal | `P-4f` |
| `P-4g-import-mask` | S | Migrate import-mask attempt store onto mutation-journal | `P-4f` |
| `P-4g-interchange-archive` | S | Migrate interchange-archive attempt store onto mutation-journal | `P-4f` |
| `P-4g-interchange-create` | S | Migrate interchange-create attempt store onto mutation-journal | `P-4f` |
| `P-4g-interchange-review` | S | Migrate interchange-review attempt store onto mutation-journal | `P-4f` |
| `P-4g-interchange-snapshot` | S | Migrate interchange-snapshot attempt store onto mutation-journal | `P-4f` |
| `P-4g-invitation` | S | Migrate invitation attempt store onto mutation-journal | `P-4f` |
| `P-4g-local-portrait` | S | Migrate local-portrait attempt store onto mutation-journal | `P-4f` |
| `P-4g-multi` | S | Migrate multi attempt store onto mutation-journal | `P-4f` |
| `P-4g-negative` | S | Migrate negative attempt store onto mutation-journal | `P-4f` |
| `P-4g-open-eyes` | S | Migrate open-eyes attempt store onto mutation-journal | `P-4f` |
| `P-4g-perspective` | S | Migrate perspective attempt store onto mutation-journal | `P-4f` |
| `P-4g-perspective-lines` | S | Migrate perspective-lines attempt store onto mutation-journal | `P-4f` |
| `P-4g-prepared-review` | S | Migrate prepared-review attempt store onto mutation-journal | `P-4f` |
| `P-4g-preset-create` | S | Migrate preset-create attempt store onto mutation-journal | `P-4f` |
| `P-4g-preset-metadata` | S | Migrate preset-metadata attempt store onto mutation-journal | `P-4f` |
| `P-4g-reflection` | S | Migrate reflection attempt store onto mutation-journal | `P-4f` |
| `P-4g-relight` | S | Migrate relight attempt store onto mutation-journal | `P-4f` |
| `P-4g-relight-analysis` | S | Migrate relight-analysis attempt store onto mutation-journal | `P-4f` |
| `P-4g-relight-review-decision` | S | Migrate relight-review-decision attempt store onto mutation-journal | `P-4f` |
| `P-4g-review-decision` | S | Migrate review-decision attempt store onto mutation-journal | `P-4f` |
| `P-4g-run-cancel` | S | Migrate run-cancel attempt store onto mutation-journal | `P-4f` |
| `P-4g-run-retry` | S | Migrate run-retry attempt store onto mutation-journal | `P-4f` |
| `P-4g-scene-apply` | S | Migrate scene-apply attempt store onto mutation-journal | `P-4f` |
| `P-4g-scene-reference` | S | Migrate scene-reference attempt store onto mutation-journal | `P-4f` |
| `P-4g-section-create` | S | Migrate section-create attempt store onto mutation-journal | `P-4f` |
| `P-4g-semantic` | S | Migrate semantic attempt store onto mutation-journal | `P-4f` |
| `P-4g-semantic-apply` | S | Migrate semantic-apply attempt store onto mutation-journal | `P-4f` |
| `P-4g-session-analysis` | S | Migrate session-analysis attempt store onto mutation-journal | `P-4f` |
| `P-4g-signing` | S | Migrate signing attempt store onto mutation-journal | `P-4f` |
| `P-4g-training` | S | Migrate training attempt store onto mutation-journal | `P-4f` |
| `P-4g-workspace-create` | S | Migrate workspace-create attempt store onto mutation-journal | `P-4f` |
| `P-5m-projects` | XS | OwnerAPI: move 9 "projects" methods into src/api/projects.ts (mechanical) | `P-5p` |
| `P-5m-delivery` | XS | OwnerAPI: move 10 "delivery" methods into src/api/delivery.ts (mechanical) | `P-5m-projects` |
| `P-5m-interchange` | XS | OwnerAPI: move 24 "interchange" methods into src/api/interchange.ts (mechanical) | `P-5m-delivery` |
| `P-5m-collectionversion` | XS | OwnerAPI: move 2 "collectionversion" methods into src/api/collectionversion.ts (mechanical) | `P-5m-interchange` |
| `P-5m-perspective` | XS | OwnerAPI: move 10 "perspective" methods into src/api/perspective.ts (mechanical) | `P-5m-collectionversion` |
| `P-5m-multi` | XS | OwnerAPI: move 9 "multi" methods into src/api/multi.ts (mechanical) | `P-5m-perspective` |
| `P-5m-negative` | XS | OwnerAPI: move 4 "negative" methods into src/api/negative.ts (mechanical) | `P-5m-multi` |
| `P-5m-relight` | XS | OwnerAPI: move 7 "relight" methods into src/api/relight.ts (mechanical) | `P-5m-negative` |
| `P-5m-glare` | XS | OwnerAPI: move 9 "glare" methods into src/api/glare.ts (mechanical) | `P-5m-relight` |
| `P-5m-openeyes` | XS | OwnerAPI: move 6 "openeyes" methods into src/api/openeyes.ts (mechanical) | `P-5m-glare` |
| `P-5m-reflection` | XS | OwnerAPI: move 4 "reflection" methods into src/api/reflection.ts (mechanical) | `P-5m-openeyes` |
| `P-5m-localportrait` | XS | OwnerAPI: move 8 "localportrait" methods into src/api/localportrait.ts (mechanical) | `P-5m-reflection` |
| `P-5m-render` | XS | OwnerAPI: move 13 "render" methods into src/api/render.ts (mechanical) | `P-5m-localportrait` |
| `P-5m-sessionanalysis` | XS | OwnerAPI: move 5 "sessionanalysis" methods into src/api/sessionanalysis.ts (mechanical) | `P-5m-render` |
| `P-5m-capture` | XS | OwnerAPI: move 11 "capture" methods into src/api/capture.ts (mechanical) | `P-5m-sessionanalysis` |
| `P-5m-runs` | XS | OwnerAPI: move 5 "runs" methods into src/api/runs.ts (mechanical) | `P-5m-capture` |
| `P-5m-styles` | XS | OwnerAPI: move 9 "styles" methods into src/api/styles.ts (mechanical) | `P-5m-runs` |
| `P-5m-reviews` | XS | OwnerAPI: move 7 "reviews" methods into src/api/reviews.ts (mechanical) | `P-5m-styles` |
| `P-5m-exports` | XS | OwnerAPI: move 10 "exports" methods into src/api/exports.ts (mechanical) | `P-5m-reviews` |
| `P-5m-editing` | XS | OwnerAPI: move 19 "editing" methods into src/api/editing.ts (mechanical) | `P-5m-exports` |
| `P-5m-v2draft` | XS | OwnerAPI: move 9 "v2draft" methods into src/api/v2draft.ts (mechanical) | `P-5m-editing` |
| `P-5m-tenant` | XS | OwnerAPI: move 11 "tenant" methods into src/api/tenant.ts (mechanical) | `P-5m-v2draft` |

## Этап F — Worker: `build_worker` (W-9, инструмент `extract_stmts.py`) и control-plane (W-7/W-8)

| Карточка | Размер | Что | После |
|---|---|---|---|
| `W-7a` | S | Worker RPC: allowlist generator + drift test (SQL surface of TaskDatabase) | — |
| `W-7b` | S | Worker RPC: Python value codec + shared test vectors | `W-7a` |
| `W-7c` | S | Worker RPC: Go value codec validated against the shared vectors | `W-7b` |
| `W-7d` | S | Worker RPC: Go handler with allowlist, transaction batching and fake-querier tests | `W-7c` `W-7a` |
| `W-7e` | S | Worker RPC: pgx adapter (Beginner/Tx) and database-role guard | `W-7d` |
| `W-7f` | S | Worker RPC: Python HTTP client implementing the TaskDatabase query seam | `W-7b` |
| `W-8a` | S | Worker identity: per-worker certificate identity extraction (Go) | `W-7d` |
| `W-8b` | S | Worker identity: issue per-worker certificates (provisioning tool) | — |
| `W-8c` | XS | Worker identity: runbook for enrolment, rotation and revocation | `W-8a` `W-8b` |
| `W-9m01` | XS | Extract build_worker step 1: _select_restoration_and_cuda_backends | — |
| `W-9m02` | XS | Extract build_worker step 2: _validate_learning_interchange_delivery | `W-9m01` |
| `W-9m03` | XS | Extract build_worker step 3: _validate_render_region_semantic | `W-9m02` |
| `W-9m04` | XS | Extract build_worker step 4: _validate_cpu_preview_installations | `W-9m03` |
| `W-9m05` | XS | Extract build_worker step 5: _validate_runtime_locks_and_multi | `W-9m04` |
| `W-9m06` | XS | Extract build_worker step 6: _prepare_model_pools_and_gpu_operations | `W-9m05` |
| `W-9m07` | XS | Extract build_worker step 7: _prepare_cpu_lens_and_removal_selection | `W-9m06` |
| `W-9m08` | XS | Extract build_worker step 8: _open_database_and_check_readiness | `W-9m07` |
| `W-9m09` | XS | Extract build_worker step 9: _check_glare_reflection_readiness | `W-9m08` |
| `W-9m10` | XS | Extract build_worker step 10: _check_training_and_learning_readiness | `W-9m09` |
| `W-9m11` | XS | Extract build_worker step 11: _register_operations | `W-9m10` |
| `W-7g` | S | Worker RPC: select the control plane by configuration (SQL stays the default) | `W-7f` `W-9m08` |
| `W-7h` | S | Worker RPC: Go "worker-api" role (mTLS listener) wired into the process entry point | `W-7e` `W-8a` |
| `W-7i` | S | Worker RPC: compose service, secrets and example env | `W-7h` |

## Этап G — Эксплуатация: сети, мониторинг, бэкапы, runbooks, секреты, baseline миграций

| Карточка | Размер | Что | После |
|---|---|---|---|
| `B-3a` | S | Baseline builder: schema-dump normalizer (pure function + tests) | — |
| `B-3b` | S | Baseline equivalence test (skipped without a database) | `B-3a` |
| `O-1a` | S | docs/ops/network-topology.md (servers <-> GPU workers over VPN) | — |
| `O-1b` | S | deploy/vpn - WireGuard hub/worker templates and nftables policy | `O-1a` |
| `O-4a` | S | Prometheus scrape config and alert rules | `B-2a` |
| `O-5a` | S | Backup/restore runbook, pgBackRest example, drill template | — |
| `O-7a` | S | Scanner for private identifiers committed to the repository | — |
| `O-7b` | XS | provision.py - root and volume paths from environment | — |
| `O-9b` | S | Apple Silicon worker package (launchd + installer + env template) | — |
| `T-1a` | S | tests/e2e scaffold (Playwright project, config, env helper) | — |
| `T-1b` | S | e2e helpers - Mailpit OTP reader and API client | `T-1a` |
| `X-8a` | XS | Runbook - worker-unavailable | — |
| `X-8b` | XS | Runbook - scratch-disk-full | — |
| `X-8c` | XS | Runbook - queue-growth | — |
| `X-8d` | XS | Runbook - broker-failure | — |

## Этап H — E2E: каркас и сценарии (recon → spec)

| Карточка | Размер | Что | После |
|---|---|---|---|
| `T-1c` | S | Analysis - what compose.e2e.yaml must contain | — |
| `T-1d` | S | e2e helper - assert-bytes (SHA-256 and JPEG inspection) | `T-1a` |
| `T-1dt` | S | e2e helper - assert-bytes TIFF/PNG inspection | `T-1d` |
| `T-1e` | S | e2e fixtures - deterministic photo generator and manifest | `T-1a` |
| `T-2a` | S | e2e spec E-16 - invalid portal secret shows an invalid state without data leakage | `T-1a` |
| `T-2e01r` | S | Recon for e2e E-01 - sign-in by OTP, repeat sign-in, sign-out | `T-1b` |
| `T-2e01s` | S | e2e spec E-01 - sign-in by OTP, repeat sign-in, sign-out | `T-2e01r` |
| `T-2e02r` | S | Recon for e2e E-02 - create workspace, project and collection (idempotent double click) | `T-1b` |
| `T-2e03r` | S | Recon for e2e E-03 - invite an editor, accept, revoke access | `T-1b` |
| `T-2e04r` | S | Recon for e2e E-04 - import 10 files (JPEG/TIFF/PNG/DNG/corrupt/duplicate) | `T-1b` `T-1e` `T-1d` |
| `T-2e05r` | S | Recon for e2e E-05 - pause / resume / reload during upload | `T-1b` `T-1e` |
| `T-2e06r` | S | Recon for e2e E-06 - culling and compare decisions with concurrent change | `T-1b` `T-1e` |
| `T-2e07r` | S | Recon for e2e E-07 - editor: edit -> preview -> cancel is non-destructive | `T-1b` `T-1e` |
| `T-2e08r` | S | Recon for e2e E-08 - apply a style/profile to a series (CPU-compatible operations) | `T-1b` `T-1e` |
| `T-2e09r` | S | Recon for e2e E-09 - operation without qualification/model is unavailable | `T-1b` |
| `T-2e10r` | S | Recon for e2e E-10 - review a result: accept / reject | `T-1b` |
| `T-2e11r` | S | Recon for e2e E-11 - export JPEG and TIFF16 by preset | `T-1b` `T-1d` `T-1dt` `T-1e` |
| `T-2e12r` | S | Recon for e2e E-12 - create a delivery and publish a link | `T-1b` `T-1e` |
| `T-2e13r` | S | Recon for e2e E-13 - tenant isolation by direct URL and API | `T-1b` |
| `T-2e14r` | S | Recon for e2e E-14 - quotas and limits | `T-1b` |
| `T-2e15r` | S | Recon for e2e E-15 - open a link whose secret is in the URL fragment | `T-1b` |
| `T-2e17r` | S | Recon for e2e E-17 - viewing, lightbox from the keyboard, focus trap | `T-1b` |
| `T-2e18r` | S | Recon for e2e E-18 - download one file and a volume archive | `T-1b` `T-1d` |
| `T-2e19r` | S | Recon for e2e E-19 - guest upload with resume | `T-1b` `T-1e` |
| `T-2e20r` | S | Recon for e2e E-20 - contrast and font size on all released screens (both themes) | `T-1b` |
| `T-2e22r` | S | Recon for e2e E-22 - revoke access during a download | `T-1b` |
| `T-2e23r` | S | Recon for e2e E-23 - 429 and Retry-After behaviour | `T-1b` |
| `T-2e24r` | S | Recon for e2e E-24 - purge a tenant / delete data | `T-1b` |
| `T-2e25r` | S | Recon for e2e E-25 - GPU worker unavailable | `T-1b` `T-1c` |
| `T-3a` | XS | Makefile - lint, typecheck, e2e targets and verify | `T-1a` |
| `T-5a` | S | Coverage targets (Go, worker, portal) | — |
| `T-1f` | S | e2e helper - seed (user session, tenant, project, collection) | `T-1b` `T-2e02r` |
| `T-2e02s` | S | e2e spec E-02 - create workspace, project and collection (idempotent double click) | `T-2e02r` `T-1f` |
| `T-2e03s` | S | e2e spec E-03 - invite an editor, accept, revoke access | `T-2e03r` `T-1f` |
| `T-2e04s` | S | e2e spec E-04 - import 10 files (JPEG/TIFF/PNG/DNG/corrupt/duplicate) | `T-2e04r` `T-1e` `T-1d` `T-1f` |
| `T-2e05s` | S | e2e spec E-05 - pause / resume / reload during upload | `T-2e05r` `T-1e` `T-1f` |
| `T-2e06s` | S | e2e spec E-06 - culling and compare decisions with concurrent change | `T-2e06r` `T-1e` `T-1f` |
| `T-2e07s` | S | e2e spec E-07 - editor: edit -> preview -> cancel is non-destructive | `T-2e07r` `T-1e` `T-1f` |
| `T-2e08s` | S | e2e spec E-08 - apply a style/profile to a series (CPU-compatible operations) | `T-2e08r` `T-1e` `T-1f` |
| `T-2e09s` | S | e2e spec E-09 - operation without qualification/model is unavailable | `T-2e09r` `T-1f` |
| `T-2e10s` | S | e2e spec E-10 - review a result: accept / reject | `T-2e10r` `T-2e08s` `T-1f` |
| `T-2e11s` | S | e2e spec E-11 - export JPEG and TIFF16 by preset | `T-2e11r` `T-1d` `T-1dt` `T-1e` `T-1f` |
| `T-2e12s` | S | e2e spec E-12 - create a delivery and publish a link | `T-2e12r` `T-1e` `T-1f` |
| `T-2e13s` | S | e2e spec E-13 - tenant isolation by direct URL and API | `T-2e13r` `T-1f` |
| `T-2e14s` | S | e2e spec E-14 - quotas and limits | `T-2e14r` `T-1f` |
| `T-2e15s` | S | e2e spec E-15 - open a link whose secret is in the URL fragment | `T-2e15r` `T-2e12s` `T-1f` |
| `T-2e17s` | S | e2e spec E-17 - viewing, lightbox from the keyboard, focus trap | `T-2e17r` `T-2e12s` `T-1f` |
| `T-2e18s` | S | e2e spec E-18 - download one file and a volume archive | `T-2e18r` `T-1d` `T-2e12s` `T-1f` |
| `T-2e19s` | S | e2e spec E-19 - guest upload with resume | `T-2e19r` `T-1e` `T-2e12s` `T-1f` |
| `T-2e20s` | S | e2e spec E-20 - contrast and font size on all released screens (both themes) | `T-2e20r` `P-6a` `T-1f` |
| `T-2e22s` | S | e2e spec E-22 - revoke access during a download | `T-2e22r` `T-2e18s` `T-1f` |
| `T-2e23s` | S | e2e spec E-23 - 429 and Retry-After behaviour | `T-2e23r` `PT-6c` `B-1c` `T-1f` |
| `T-2e24s` | S | e2e spec E-24 - purge a tenant / delete data | `T-2e24r` `T-1f` |
| `T-2e25s` | S | e2e spec E-25 - GPU worker unavailable | `T-2e25r` `T-1c` `T-1f` |

## Этап I — Chaos-тесты сети (нужен стенд для реального прогона, иначе skip)

| Карточка | Размер | Что | После |
|---|---|---|---|
| `W-11a` | S | Chaos scaffold: toxiproxy HTTP client, fault marker, stand guard | — |
| `W-11b` | S | Chaos invariants: no lost and no duplicated task result | `W-11a` |
| `W-11c` | S | Chaos scenario: Connection loss of 5 / 20 / 70 s while the task computes | `W-11b` |
| `W-11d` | S | Chaos scenario: Connection loss during artifact upload | `W-11b` |
| `W-11e` | S | Chaos scenario: Connection loss at commit | `W-11b` |
| `W-11f` | S | Chaos scenario: AMQP connection drop with an active task | `W-11b` |
| `W-11g` | S | Chaos scenario: Worker restart with a non-empty spool | `W-11b` |
| `W-11h` | S | Chaos scenario: Latency 300 ms and 2 percent loss on all channels | `W-11b` |
| `W-11i` | S | Chaos scenario: Two workers never execute the same task (lease exclusivity) | `W-11b` |
| `W-11j` | S | Chaos scenario: Worker death after a completed stage | `W-11b` |
| `W-11k` | S | Chaos scenario: OOM retry with tile 256 | `W-11b` |

## Этап J — Крупные функции по плану (анализ кусками → план → карточки генерирует `cards_from_plan.py`)

| Карточка | Размер | Что | После |
|---|---|---|---|
| `B-6-an01` | S | B-6 analysis: lines 834-1073 | — |
| `B-6-an02` | S | B-6 analysis: lines 1074-1313 | — |
| `B-6-an03` | S | B-6 analysis: lines 1314-1553 | — |
| `B-6-an04` | S | B-6 analysis: lines 1554-1793 | — |
| `B-6-an05` | S | B-6 analysis: lines 1794-2033 | — |
| `B-6-an06` | S | B-6 analysis: lines 2034-2273 | — |
| `B-6-an07` | S | B-6 analysis: lines 2274-2513 | — |
| `B-6-an08` | S | B-6 analysis: lines 2514-2753 | — |
| `B-6-an09` | S | B-6 analysis: lines 2754-2993 | — |
| `B-6-an10` | S | B-6 analysis: lines 2994-3151 | — |
| `B-6-plan` | S | B-6 - merge chunk notes into an extraction plan | `B-6-an01` `B-6-an02` `B-6-an03` `B-6-an04` `B-6-an05` `B-6-an06` `B-6-an07` `B-6-an08` `B-6-an09` `B-6-an10` |
| `B-6t` | S | Characterization test for ApplyCoordinator.build (before the split) | — |
| `B-7-an01` | S | B-7 analysis: lines 60-219 | — |
| `B-7-an02` | S | B-7 analysis: lines 220-379 | — |
| `B-7-an03` | S | B-7 analysis: lines 380-539 | — |
| `B-7-an04` | S | B-7 analysis: lines 540-698 | — |
| `B-7-plan` | S | B-7 - merge chunk notes into an extraction plan | `B-7-an01` `B-7-an02` `B-7-an03` `B-7-an04` |
| `P-9-an01` | S | P-9 analysis: lines 1-100 | — |
| `P-9-an02` | S | P-9 analysis: lines 101-200 | — |
| `P-9-an03` | S | P-9 analysis: lines 201-300 | — |
| `P-9-an04` | S | P-9 analysis: lines 301-400 | — |
| `P-9-an05` | S | P-9 analysis: lines 401-500 | — |
| `P-9-an06` | S | P-9 analysis: lines 501-600 | — |
| `P-9-an07` | S | P-9 analysis: lines 601-700 | — |
| `P-9-an08` | S | P-9 analysis: lines 701-800 | — |
| `P-9-an09` | S | P-9 analysis: lines 801-894 | — |
| `P-9-plan` | S | P-9 - merge chunk notes into an extraction plan | `P-9-an01` `P-9-an02` `P-9-an03` `P-9-an04` `P-9-an05` `P-9-an06` `P-9-an07` `P-9-an08` `P-9-an09` |

## Ворота для человека внутри очереди

- `P-4d` не стартует, пока в `notes/P-4-contract.md` нет строки `APPROVED-BY: <имя>` (чек-лист H-P4).
- `P-9-plan`, `B-6-plan`, `B-7-plan` создают план; человек проверяет его и запускает `cards_from_plan.py` (чек-листы H-PLAN) - только после этого появляются карточки `P-9x-*`, `B-6x-*`, `B-7x-*`.
- Карточки W-11c…k и T-2 (spec) без стенда завершаются как «skip»/«UNKNOWN»: это нормальный результат, стенд поднимает человек (H-W11, H-T1).
