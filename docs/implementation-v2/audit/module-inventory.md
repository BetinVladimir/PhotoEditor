# Инвентарь функциональных модулей

Источник: текущие файлы; SHA каждого экрана/компонента в module-inventory.json. Статические совпадения не подтверждают реализацию или приёмку.

Найдено экранов: 28; файлов с экспортируемыми компонентами: 72; объявлений маршрутов API: 317; кандидатов состояний: 638.

## Все обнаруженные экраны

- `capture` — `apps/photographer/app/capture/[id].tsx` — требует текущей сквозной проверки
- `capture` — `apps/photographer/app/capture/index.tsx` — требует текущей сквозной проверки
- `collections` — `apps/photographer/app/collections/[id]/apply.tsx` — требует текущей сквозной проверки
- `collections` — `apps/photographer/app/collections/[id]/index.tsx` — требует текущей сквозной проверки
- `editor` — `apps/photographer/app/editor/[version].tsx` — требует текущей сквозной проверки
- `exports` — `apps/photographer/app/exports/index.tsx` — требует текущей сквозной проверки
- `imports` — `apps/photographer/app/imports/new.tsx` — требует текущей сквозной проверки
- `shell_auth` — `apps/photographer/app/index.tsx` — требует текущей сквозной проверки
- `shell_auth` — `apps/photographer/app/login.tsx` — требует текущей сквозной проверки
- `projects` — `apps/photographer/app/projects/[id]/index.tsx` — требует текущей сквозной проверки
- `projects` — `apps/photographer/app/projects/index.tsx` — требует текущей сквозной проверки
- `reviews` — `apps/photographer/app/reviews/[id].tsx` — требует текущей сквозной проверки
- `reviews` — `apps/photographer/app/reviews/index.tsx` — требует текущей сквозной проверки
- `shell_auth` — `apps/photographer/app/runs/[id].tsx` — требует текущей сквозной проверки
- `semantic` — `apps/photographer/app/semantic/[id].tsx` — требует текущей сквозной проверки
- `sessions` — `apps/photographer/app/sessions/[id]/index.tsx` — требует текущей сквозной проверки
- `sessions` — `apps/photographer/app/sessions/[id]/scenes.tsx` — требует текущей сквозной проверки
- `settings` — `apps/photographer/app/settings/members.tsx` — требует текущей сквозной проверки
- `settings` — `apps/photographer/app/settings/profile.tsx` — требует текущей сквозной проверки
- `settings` — `apps/photographer/app/settings/quotas.tsx` — требует текущей сквозной проверки
- `settings` — `apps/photographer/app/settings/retention.tsx` — требует текущей сквозной проверки
- `styles` — `apps/photographer/app/styles/[id]/index.tsx` — требует текущей сквозной проверки
- `styles` — `apps/photographer/app/styles/[id]/training.tsx` — требует текущей сквозной проверки
- `styles` — `apps/photographer/app/styles/index.tsx` — требует текущей сквозной проверки
- `shell_auth` — `apps/photographer/app/workspaces.tsx` — требует текущей сквозной проверки
- `portal` — `apps/portal/src/app/[locale]/page.tsx` — требует текущей сквозной проверки
- `portal` — `apps/portal/src/app/[locale]/s/[share_id]/page.tsx` — требует текущей сквозной проверки
- `portal` — `apps/portal/src/app/page.tsx` — требует текущей сквозной проверки

## Охват модулей V20–V32

### V20

актуализировать Pxx по трассировке; model bundle/doctor/capacity; tests model qualification

Приёмка: каждый existing operation имеет ready/unqualified/unsupported и evidence; legacy hashes сохранены; own registry/output policy, GC/purge integration and denied-role tests

Статус: текущий полный цикл не доказан.

### V21

workflow foundation, versioned documents, draft persistence and approval, GC and RLS

Приёмка: cross-tenant, fence, cancel-before-commit, quota/replay, GC race; own registry/output policy, GC/purge integration and denied-role tests

Статус: текущий полный цикл не доказан.

### V22

sessionbrain + session operations 

Приёмка: 5000 frames bounded memory; deterministic groups; manual refs; unknown time; no skin homogenization; own registry/output policy, GC/purge integration and denied-role tests

Статус: текущий полный цикл не доказан.

### V23

learning, CorrectionEvent/fit/evaluate, immutable safety review/activation, identity.compare primitive

Приёмка: leakage-free sessions; small data reject; explicit activation; rollback; revoke consent; own registry/output policy, GC/purge integration and denied-role tests

Статус: текущий полный цикл не доказан.

### V24

capture , studio-agent, QA

Приёмка: crash/restart dedup, rotated credentials, queue lag, real camera/watch-folder fixture; own registry/output policy, GC/purge integration and denied-role tests

Статус: текущий полный цикл не доказан.

### V25

RAW/lens/metadata/export qualification

Приёмка: supported camera matrix, ICC RGB16, alpha, HDR limits, ZIP64 >4GiB real IO; own registry/output policy, GC/purge integration and denied-role tests

Статус: текущий полный цикл не доказан.

### V26

editor loupe/layers/scene board/keyboard

Приёмка: real browser+native, stale response, offline CAS, RU/BG/EN, 5000 gallery; own registry/output policy, GC/purge integration and denied-role tests

Статус: текущий полный цикл не доказан.

### V27

semanticedit , transfer/review

Приёмка: parallax/occlusion fallback, per-target decisions, no extra target mutation; own registry/output policy, GC/purge integration and denied-role tests

Статус: текущий полный цикл не доказан.

### V28

portrait.open_eyes/glare/hair/clothing/reflection/relight

Приёмка: real model and donor corpus, protected unchanged, human approval, unavailable state; own registry/output policy, GC/purge integration and denied-role tests

Статус: текущий полный цикл не доказан.

### V29

panorama/focus_stack/negative/perspective

Приёмка: multi-source lineage, moving objects,200MP cap, color/geometry QA; own registry/output policy, GC/purge integration and denied-role tests

Статус: текущий полный цикл не доказан.

### V30

delivery slot matrix, interchange parse/ingest/resolve/export and batch review

Приёмка: full required bundle atomic, revoked share, XMP hostile input, unsupported fields visible; own registry/output policy, GC/purge integration and denied-role tests

Статус: текущий полный цикл не доказан.

### V31

provenance signer 

Приёмка: valid/invalid signature, key rotation, privacy redaction, actual SDK export verification; own registry/output policy, GC/purge integration and denied-role tests

Статус: текущий полный цикл не доказан.

### V32

lifecycle reconciliation, deployment, capacity and restore acceptance; no deferred registry registration

Приёмка: mixed workloads on3060, restore, role matrix, actual end-to-end acceptance; own registry/output policy, GC/purge integration and denied-role tests

Статус: текущий полный цикл не доказан.

## Ограничения

- Static inventory is not implementation proof
- Nested router prefixes need manual resolution
- Runtime-generated components and states need manual trace
- Paper-only named contract not found; normative module criteria are implementation-v2 per user clarification
- Historical evidence not counted as current acceptance

## Incremental V29 screen coverage — 2026-10-10, NOT RUN

Added `/collections/[id]/multi` for both panorama and focus stack, plus collection navigation. Components: MultiRoute, MultiScope, MultiPhoto. Covers capability/collection loading, strict version selection, ordered/reference/options editing, frozen snapshot/admission, unknown retry, acknowledged run/open/new selection, unavailable authority, route cancellation, preview pending and RU/BG/EN labels. Source SHA recorded for this route and changed collection screen in module-inventory.json. Earlier total counts are historical inventory counts, not a fresh exhaustive scan. Unit/DOM scenarios authored; no execution evidence. Result review/Apply and wider V20–V32 coverage remain open.


## Incremental V30 delivery screen — 2026-10-10, NOT RUN

Added Expo route `/deliveries/[id]?tenant={tenant}&project={project}` and DeliveryPackageScreen/Scope components. States authored: initial/loading/read failure/refresh, building/review/ready/failed/cancelled, manifest unavailable, publication in flight, unknown outcome/retry with same CAS+key, access lost, observed published, route cancellation/late response ignored. EN/RU/BG localized. Review publication is deliberately disabled until full frozen-manifest inspection exists; a SHA alone is not human review evidence. Ready publication is wired to OwnerAPI. Route is deep-linkable; list/CreateDelivery navigation remains open. DOM tests authored, NOT RUN. Inventory totals above are historical and are not updated as acceptance evidence.


V30 increment: DeliveryPackageScreen now includes item-inspection loading, all-page failure, complete metadata list with FlatList virtualization, refresh reset and inspection access-loss cleanup. delivery-items.ts decoder/collector and OwnerAPI.deliveryItems added. Unit/DOM authored, NOT RUN. Full frozen-manifest semantic inspection and review publication still open.


V30 review increment: screen now includes inspection pending/failed/verified and explicit review-consent checkbox states. Complete items plus matching semantic proof enable review publication; refresh/reload reset consent. Earlier statements that review publication is disabled are superseded by this authored integration. Unit/DOM tests NOT RUN; no current runtime proof.


V31: available SigningPolicyChoice projection exposes only policy UUID/revision, filters installed validity/MIME and live operator authority and returns sorted owned entries. SQLSignExportService.AvailableSigningPolicies repeats current authorized ready export pins before/after operator probe. Registered auxiliary GET /api/v1/tenants/{t}/exports/{id}/signing-policies, authenticated through existing tenantRequest and catalogue editor/download checks; unconfigured provider503, unavailable policies produce empty list. Authored redaction/validity/denial/cancellation/mutation unit tests NOT RUN. Endpoint advisory only; SignExport repeats all authority. Auxiliary route added to module inventory; original90 normative-entrypoint accounting unchanged. Public signing screen, policy tenant publication and production installation remain open.


V31: photographer SigningScreen and /exports/sign route restore origin/user/tenant/export-scoped journal after current me(), load server-only policies, freeze choice after pending submission, reuse original retry key, persist acknowledgement before navigation, open saved job without re-querying live operator, block access-loss retries and abort/ignore unmounted requests. Ready delivery item gains signing navigation. Added English/Russian/Bulgarian messages and DOM unit cases success-after-ambiguity/exact key/saved job/empty catalogue/access loss/unmount. Tests authored NOT RUN. New UI inventory: /exports/sign; states loading, available selection, empty/unavailable, pending retry, busy, saved ticket, denied/error; route loading/auth failure. Production signer/policy publication and full SQL/native/UI acceptance remain open; no whole-module credit.


V31 UI: saved-ticket Open action now goes through submitSigning/journal authority instead of navigating from rendered cached ticket, so expired/logout-cleared mounted journals refuse navigation. Policy refresh immediately clears prior choices to avoid presenting stale policies after failure. Added DOM expired-journal/open refusal, failed-refresh stale-choice removal and rotated-policy pending retry preserving original policy/key cases; authored NOT RUN. Current public UI/state coverage updated; production operator/tenant policy publication and full SQL/native acceptance remain open. No whole-module or percentage credit.


V29 supplemental screen: `apps/photographer/app/perspective-replacements/[original].tsx`; loading/pending/unavailable/available/quality/Apply/applied states. Authored, NOT TESTED. Original-review navigation and complete runtime lifecycle remain open. Historical discovery counts above are not recalculated.

2026-10-13 route reconciliation: `new-route-inventory-current.json` adds eleven V29–V31 Expo routes absent from the historical screen array, with current source SHA, mounted component and route-shell states. It is a source inventory, not a browser/native state trace or whole-module acceptance. The historical discovery counts above remain historical; child-component states and runtime behavior still require the deferred test phase.
