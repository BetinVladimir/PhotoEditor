# V22 — текущий цикл целого модуля

Нормативные критерии: 07-execution-acceptance, 06-clients-integrations,
13-api-behavior, 15-execution-contracts и реестры contracts.
Пользователь подтвердил implementation-v2 как источник критериев. Отсутствие
отдельно названного Paper-only файла не означает остановку реализации.

## Сквозной охват

| Сценарий | Текущие точки реализации | Неподтверждённая часть |
|---|---|---|
| Создание сессии | capture-session-create, capture route, v2sessions.Create | Текущий browser/native цикл и denied-role DB |
| Список и карточка | capture route, sessions/[id]/index, OwnerAPI, v2sessions.Get/List | Сквозное исполнение с реальными данными |
| Правка policy/CAS | session-policy-editor, persistent-session-policy-editor, scoped draft/persistence, session route, Update | DOM restart/revision/retry covered; real offline bootstrap/proxies, browser/native CAS and physical DB remain unproven |
| Analysis | scene-analysis-request, session route, describe/plan adapters, admission/adoption | Физическая материализация и текущая registry/GC/purge проверка |
| Scene Board | sessions/[id]/scenes, scene-board-view, GetSceneBoard | Outliers сейчас null; защищённые превью добавлены, реальная native/browser приёмка не выполнена |
| Замена референса | sceneReferenceRequest, analyzeSession, переход runs/[id] | Сквозной новый анализ при неизменности старого |
| Одобрение групп/Apply | backend ApplyAnalysis; scene-apply/OwnerAPI.applySceneAnalysis, SceneApplyPanel, переход runs/[id] | Текущий реальный browser/native цикл решения и child Apply |
| 5000 кадров/качество | Ограничения DTO и virtualized lists; unit fixtures | Измеренный RAM, неизвестное время, ≥20% EV residual improvement, clipping ≤0.5pp и human nonregression |

Состояния session route: loading, loaded, paging, policy dirty, saving,
analyzing, load/reference failure, CAS conflict, dependency unavailable,
access denied, abort при смене scope. Scene route: loading, loaded,
submitting reference, uncertain response retry с тем же ключом,
definitive rejection, access revoked, reload, abort/late-response suppression.
Реализованные состояния UI Apply: explicit selected groups,
recipe loading/selection, submitting, uncertain response retry, conflict,
access revoked и child status. Компонентные тесты не доказывают реальное исполнение child Apply.

## Исправление клиентского контракта

readSceneBoardDocument теперь сохраняет проверенный SHA исходного плана.
sceneApplyDecision запрещает неизвестные группы и план без проверенного SHA.
freezeSceneApplyDecision проверяет DTO, дубликаты, UUID, SHA, int64 revision
и копирует вложенные значения до асинхронной отправки. OwnerAPI отправляет
решение на существующий endpoint с явным ключом повтора и AbortSignal;
ответ проверяется как JobTicket. Изменение обработки изображений не вносилось.

## Текущая проверка

- Photographer typecheck: PASS; полный Vitest после превью: 298 passed, 7 skipped.
- Go v2sessions/httpapi с -count=1: PASS. Условные DB-тесты не заменяют DB evidence.
- Четыре Python session unit suites: 17 passed.
- test_v22_scene_materialization: setup ERROR. После выхода из sandbox
  PostgreSQL 127.0.0.1:55432 отвечает Connection refused; миграции не выполнялись.

V22 не принят. Эти результаты подтверждают перечисленные компоненты;
полный пользовательский цикл, физическая БД и научные критерии остаются открыты.
UI решения Apply добавлен: явные группы, accepted StyleV2 того же проекта,
загрузка revision и проверка SHA анализа, ограниченная пагинация, AbortSignal,
повтор неизменного решения, блокировка после definitive conflict, RU/BG/EN.
Отзыв доступа снимает показанную доску через родительский экран.
Следующий шаг — физический сквозной цикл и оставшиеся outliers/визуальные
кадры/offline/quality gates. Docker daemon на машине не запущен.

## 2026-10-08 — визуальные кадры

SceneBoardDocument.frames теперь включает обязательный asset_id. Сервер
разрешает его через snapshot version→asset, проверяя tenant и project.
Клиент валидирует UUID, сохраняет binding и запрашивает thumb320 только
из смонтированных виртуальных ячеек через существующий mediaLinks ACL.
Версия, MIME, размер, expiry и URL проверяются перед показом; abort
подавляет поздний ответ, срок ссылки удаляет изображение. 401/403/404
очищает доску через родителя. Ссылки не записываются в local storage.

Тесты превью: 4 passed; document projection: проверка asset UUID добавлена.
Go v2sessions -count=1 PASS. JSON Schema static validation: 2254 checks PASS;
semantic reference suite: 54 PASS. Обновлён текущий schema SHA, предыдущий
сохранён для frozen tasks; closure/exposure/local portrait: 32 PASS.
Исправлен источник генерации существующего ApprovedSemanticEdit и CPU
semantic.local_exposure: generator сохраняет все29 операций.
check-docs сообщает BASELINE_DRIFT для workers/photo_worker/contracts/images.py;
baseline не переписан. Это открытый review gate, а не успешная приёмка.

Полный текущий Python platform: 2329 passed,2 skipped,2 failed
(MODEL_DISK_EXHAUSTED при фактических811MiB свободного места),7 setup errors
(sandbox loopback). Guard1GiB сохранён; общий suite не объявлен PASS.

Gateway rerun with permitted isolated loopback: 7 passed in4.05s.

## 2026-10-08 — численная регрессия на контролируемых пикселях

Добавлен tests/unit/test_v22_scene_pixel_quality.py: метрики рассчитываются
по RGB-пикселям, отдельно от descriptor-based solve_consistency. Проверены
≥20% снижение median EV residual на контролируемой сцене, clipping≤0.5pp,
сохранение RGB chromaticity без neutral WB, exact manual-lock и invalid-luma
bypass. Контрпример с ярким меньшинством доказывает: EV residual улучшается,
но clipping ухудшается сверх порога. Поэтому успешный solve_consistency
не заменяет измерение качества и human nonregression. Outliers пока не
реализованы: причины no_change/manual_locked не являются outlier evidence.
Эти тесты не доказывают production renderer, реальную фотосцену или human score.


### 2026-10-08 — V22 Apply cancelled discovery and bounded cursor

Panel discovery checks active AbortController before/after every page and between session/analysis reads. Unmount during session read no longer sends analysis/style requests; unmount during page fetch no longer follows its next cursor. Cursor must be string1..2048 before another network request. Immutable analysis revision must equal documented1 before styles are offered. Added three DOM regressions. Current photographer typecheck PASS; full frontend301 passed,7 skipped. Inventory source hashes refreshed. Full V22 acceptance still requires outlier assessment, offline scenarios, physical DB/browser/native and real/human quality evidence.


### 2026-10-08 — V22 accepted Apply terminal UI

Successful child admission marks the panel accepted before navigation; submission/selection/cancel remain blocked while transition is pending. Prevents a second new-key admission from the still-mounted panel. DOM regressions additionally cover reopening after discovery failure (fresh context, Apply disabled until explicit group selection), and clearing an old recipe before a failed replacement version fetch. Initial test assumed RN Web emits aria-checked=false; replaced with functional Apply-disabled assertion. Current typecheck PASS; final full frontend304 passed,7 skipped. Whole V22 remains open; these are component regressions, not physical acceptance.


### 2026-10-08 — V22 Apply scope isolation

SceneApplyPanel now owns a keyed inner scope for API/session/analysis/immutable board. Scope replacement destroys previous recipe/group decisions, revision, accepted status and uncertain retry attempt; aborts pending discovery/admission and releases parent reference lock on cleanup. Late ticket cannot navigate across tenant/API replacement. Two DOM regressions cover changed analysis with selected groups and changed tenant/API during pending admission. Current typecheck PASS; full frontend306 passed,7 skipped. Inventory hashes refreshed. No imaging/DB changes; whole module acceptance remains open.


### 2026-10-08 — V22 thumbnail ownership and disposal

Preview state records API/asset/version ownership. Render hides a mismatched link immediately, before effect cleanup; late expiry and image-error callbacks cannot clear a different scope. Cell version-change regression proves old signal abort and immediate old-image removal. Unmount regression proves no retained expiry timers. This is component ownership evidence, not measured browser/native5000-frame RAM acceptance. Full run initially exposed semantic-target refresh test clicking while disabled; test now waits for enabled Refresh before exercising revoked-access reload. Product semantic behavior unchanged. Current typecheck PASS; full frontend308 passed,7 skipped. V22/V20–V32 completion remains unproven.


### 2026-10-08 — V22 session lifecycle controls

SessionLifecycle mounted in session card: open→closed, closed→open, closed→archived with explicit terminal-archive confirmation; archived shows no actions. Requests preserve name/policy/capture policy, use exact revision and immutable retry key/body. Uncertain update blocks policy editing, reload and analysis until same request resolves. Definitive rejection requires reload before new decision. Policy editor readonly for closed/archived sessions. RU/BG/EN included. Three component regressions cover exact retry, archival confirmation and terminal state. Initial retry test clicked while button pending; now waits enabled button. Current photographer typecheck PASS, full frontend311 passed,7 skipped. Physical transition/ACL/full-module acceptance not proven; existing server state machine unchanged.


### 2026-10-08 — V22 session update cancellation

OwnerAPI.updateShootSession owns a copy of the request and propagates optional AbortSignal into PATCH. Session route passes its current lifetime signal for policy/lifecycle updates. SessionLifecycle ignores late completion/failure after unmount, so an old uncertain response cannot restore the parent pending lock. Cleanup releases the lock; regression flushes the rejected promise after unmount before checking callback count. Current typecheck PASS, full frontend312 passed/7 skipped; final targeted lifecycle4 passed. No server state machine/imaging changes. Physical DB and whole-module acceptance remain open.


### 2026-10-08 — V22 explicit server-policy conflict resolution

SessionPolicyEditor offers Use server policy after CAS comparison. This explicit choice adopts the authenticated current resource in the parent without a PATCH; revision-key remount resets the local draft. Existing apply-local action uses current revision and current calibration/camera offsets/capture policy while changing grouping controls. RU/BG/EN label added. Two DOM tests prove zero additional write on server choice and current calibration preservation on local resolution. Corrected incomplete test fixture rejected by typecheck. Final typecheck PASS; full frontend314 passed,7 skipped. Offline persistence/proxies, physical CAS and whole V22 acceptance remain open.


### 2026-10-08 — V22 manual-pick protection policy control

Added explicit localized checkbox for protect_manual_picks. editSessionPolicy accepts only optional boolean override and preserves all other settings. If protection was not locally changed, CAS resolution retains the current server protection setting; explicit local change participates in dirty/revert and uncertain request freezing. Comparison shows local/current protection values. Helper and DOM regressions verify explicit false preserves grouping/calibration and omitted override preserves protection. Current typecheck PASS; full frontend316 passed,7 skipped. Camera-offset editing, offline cache and outlier/physical/quality acceptance remain open.


### 2026-10-08 — V22 existing-camera offset editing

Policy editor exposes signed-seconds fields for existing camera_offsets; dirty/revert/retry includes offsets. CAS comparison shows current server offsets. Unchanged offsets remain from current server policy; only explicit local edits replace them. editSessionPolicy bounds list<=32 before mapping, unique lowercase SHA camera IDs, decimal integer seconds in[-86400,86400]; fractional/exponent/oversize reject. Localized RU/BG/EN labels. Current typecheck PASS, full frontend317 passed,7 skipped. New-camera discovery/addition and removal are not implemented by these controls; offline/physical/outlier/real quality acceptance remains open.

Correction to preceding verification: the first full run passed317, but repeat after final comparison change exposed semantic-preview test clicking disabled Reload before finally. Added enabled-button wait; product preview unchanged. Subsequent rerun exposed the same premature retry-click assumption in scene-apply-panel; final rerun pending.

Final current full frontend rerun after both enabled-button test waits:317 passed,7 skipped.


### 2026-10-08 — V22 removal of camera time offset

Added localized explicit removal for existing camera_offsets. Removal changes the local policy draft only, participates in dirty/CAS resolution and frozen same-key retries, and is restored by revert without a server write. Controls disabled during uncertain/pending save. Two DOM regressions prove an empty offset list is intentional, calibration retained, retry request/key unchanged and revert restores removed seconds. Current typecheck PASS; full frontend319 passed,7 skipped. Camera discovery/addition still absent; offline/outlier/physical/quality and complete V20–V32 acceptance remain open.


### 2026-10-08 — V22 adding a camera time-offset entry

Policy editor adds a new entry by stable camera identifier and signed seconds. Validates the complete candidate list before changing draft: format, duplicate, count32, integer bounds. New fields reset only after successful local addition/revert; unfinished new-camera input marks draft dirty and refuses policy save until added/cleared, preventing silently lost input. All controls obey pending/uncertain locks. Localized RU/BG/EN. DOM regression proves valid addition and duplicate refusal without a second row; typed policy helper remains admission boundary. Final typecheck PASS, full frontend320 passed,7 skipped. Automatic camera discovery/names are not added; offline/outlier/physical/quality and whole-module acceptance remain open.


### 2026-10-08 — V22 unfinished-camera/limit explanations and session naming

Localized camera guidance explains unfinished entry and count32. DOM regressions prove unfinished input cannot be silently dropped by save, revert clears it, and removing a row re-enables addition below the limit. Earlier full run322 passed/7 skipped. Added session-name editing through existing CAS request; Unicode code-point validation1..200 and lone-surrogate refusal, dirty/revert/uncertain locks included. Unchanged name retains current server name during explicit local policy resolution; current name shown separately in comparison to retain grouping presentation. Rename regression preserves exact policy. Initial run exposed changed grouping text expectation; restored separate grouping/name presentation. Final current typecheck PASS, full frontend324 passed,7 skipped. Whole-module V22 and V20–V32 acceptance remain unproven.


### 2026-10-08 — V22 failed conflict discovery

On CAS rejection, policy editor discards previously shown server comparison before reading current resource. Failed read blocks further save against stale context and offers explicit localized read-only retry; local name/grouping/protection/offset draft retained. Successful read restores comparison, and apply-local uses newly read revision. Regression proves read retry issues no additional save and resulting request uses revision5 with local gap120. Current typecheck PASS; full frontend325 passed,7 skipped. No physical DB/offline-persistence/complete-module claim.


### 2026-10-08 — V22 scoped policy draft storage foundation

Added SessionPolicyDraftStore for validated policy/name/base-revision snapshots. Keys include origin/user/tenant/session; exact shape, bound16KiB, TTL7days, nonzero IDs, int64 revision, typed controls/calibration/offsets validated. Writes own serialized copies. Global serialized mutation queue and origin epochs prevent old stores/queued writes from resurrecting logout data. AuthProvider logout now clears these records with other caches. Four tests cover roundtrip ownership, tenant/user separation, invalid/expired/rebound input, epoch/queued-write logout and other-origin preservation. Current typecheck PASS, full frontend329 passed,7 skipped. This is storage foundation only: editor persistence/explicit restoration, invalid partial-input and uncertain-request persistence, offline identity bootstrap/proxies and CAS reconnect integration remain to implement; no complete offline or V22 acceptance claim.


### 2026-10-08 — V22 partial policy draft boundary

SessionPolicyDraft now optionally retains bounded raw editor fields separately from validated base policy. Empty/partial numeric text and unfinished camera identifiers remain local draft data; they are not admitted policy. Exact local shape, string/count bounds and unique camera hashes are checked, and returned fields own their copies. SessionPolicyEditor exposes optional onLocalDraft callback for dirty raw values; explicit revert emits null. Regression verifies partial gap/camera input is surfaced without a server write and revert clears it. Current TypeScript check PASS; complete frontend suite331 passed,7 skipped (77 files passed,5 skipped). Storage roundtrip test proves partial values survive parsing and oversized/duplicate records reject. Editor-to-storage wiring, restoration/CAS comparison, uncertain immutable request/key persistence, offline identity/proxies, physical backend and whole V22 acceptance remain open. This evidence does not establish complete-module acceptance.


### 2026-10-08 — V22 explicit draft restoration coordinator

Added SessionPolicyPersistence coordinator over scoped draft store. Reads precede editing; an existing draft blocks changes until explicit restoration/discard. Restoration uses an owned copy of the actually loaded record, matches session and base revision, and refuses changed-revision restore with SESSION_DRAFT_REQUIRES_COMPARISON. Session baseline and returned raw fields are copied; initial pristine callback does not erase an existing record; disposal ignores future changes. Three regressions cover partial-field roundtrip, no silent erase, explicit same-revision restore/revert, changed-revision refusal/discard and disposed scope. Initial TypeScript run found incomplete test fixture; corrected fixture with actual session fields. Final TypeScript PASS, full frontend334 passed,7 skipped. Coordinator is not yet wired into session route/UI; uncertain request persistence and changed-revision comparison UI still required. No complete offline or V22 acceptance claim.


### 2026-10-08 — V22 session route draft persistence integration

Session route now uses PersistentSessionPolicyEditor. Authenticated /api/v1/me supplies user ID before scoped AsyncStorage creation; no editing until read finishes. Existing draft offers localized explicit restore/discard; restore initializes raw fields only for same revision. Changed revision retains the stored record and blocks automatic restore. Draft callback persists edits/revert; successful save clears stored draft. Before HTTP save coordinator durably records uncertain=true; restarted UI forbids restoring raw fields from unknown-result admission, preventing silent submission with a newly generated key. Immutable body/key recovery is still unimplemented; this conservative state is not complete reconnect behavior. DOM regression uses actual wrapper/scoped storage and proves partial input survives unmount, is not silently restored, explicit restore retains raw text and user lookup is authenticated. Route coordinator tests intentionally mock persistence wrapper; wrapper tested separately. Initial syntax/typecheck failure fixed. Final current typecheck PASS; frontend336 passed,7 skipped (79 passed files,5 skipped). RU/BG/EN draft messages added. Remaining: persisted exact request/key and retry recovery, changed-revision explicit comparison, denied-access purge, write/unmount race coverage, offline identity/bootstrap/proxies and real browser/native/backend acceptance. Whole V22 and V20–V32 remain unaccepted.


### 2026-10-08 — V22 denied-access draft purge

Added revokeSessionPolicyDrafts scoped to origin/tenant/session, removing matching users' records only. Scope epoch advances synchronously before deletion is queued; existing stores' reads/queued/future writes cannot resurrect revoked data, while another tenant remains usable. Session route invokes revocation on current-lifetime401/403/404 during bootstrap/list pagination/current policy/save/analysis. Persistent editor handles denied authenticated /me lookup without exposing cached fields. Storage regression verifies all matching users removed, queued/late writes fenced and another tenant retained; DOM regression proves /me403 purges cached draft and shows no editable policy. First full run337 total non-skipped tests had one semantic-review-close retry-click timing failure (336 passed); corrected test to wait for enabled retry button, leaving product behavior unchanged. Final TypeScript PASS, complete frontend338 passed,7 skipped. Physical storage failure/restart cleanup, route-level live authorization and real native/browser offline acceptance still need verification. Exact immutable request/key recovery and explicit changed-revision merge remain incomplete. No whole-module acceptance claim.


### 2026-10-08 — V22 durable immutable policy admission replay

Draft records now optionally contain attempt{key,request}; exact key/body shape, nonzero UUID, int64 revision, name/grouping/calibration/camera offsets and capture policy checks/calibration are validated and copied. Attempt requires uncertain=true. Coordinator prepare owns body/key, prevents replacing a pending attempt, persists before HTTP and reads back the exact persisted request; closed/revoked/failed persistence cannot proceed to transport. Persistent editor saves through prepare; definitive409/410/412/422 releases pending attempt while retaining fields for explicit CAS resolution. Reloaded uncertain draft offers explicit same-key/body replay via existing onSave coordinator, including when server revision differs; success clears stored draft. Unknown-result raw restore remains disabled. New coordinator regression proves ownership/key lock. DOM regression covers actual persisted lost-response, unmount/reload, original request/key replay and successful cleanup. Final current typecheck PASS; complete frontend340 passed,7 skipped. Still incomplete: changed-revision local comparison/merge, restart replay definitive-conflict UX, stale callback/success-cleanup races, real offline identity/bootstrap/proxies, browser/native and physical DB acceptance. This is not whole V22 acceptance.


### 2026-10-08 — V22 confirmed cleanup and explicit changed-revision draft review

SessionPolicyDraftStore.clearAttempt performs queued read/compare-key/delete under the existing serialized storage queue and checks scope epochs before/after read. Coordinator complete works after disposal; late confirmed response cannot erase a newer attempt with another key. Normal save and restart replay use complete rather than unconditional discard. Regression proves disposed success cleanup and newer-key preservation. Added explicit restoreCompared: own loaded non-uncertain local draft is reconciled with current open session; fields unchanged against saved baseline retain current server values, explicitly changed raw fields retain local values. Camera offsets use whole-list replacement only when locally changed, current calibration remains in session baseline. Comparison displays local/current grouping/name/protection/offsets and localized explicit review action; it initializes editor without server write. Helper regression proves server rename/neighbors/protection/offsets retained alongside partial local input and new revision recorded only on subsequent local persistence. Final typecheck PASS; complete frontend342 passed,7 skipped. DOM changed-revision action/new-save and real browser/native/CAS integrations remain to verify; replay definitive conflict UX and other offline gates still open. No whole V22 or V20–V32 acceptance.


### 2026-10-08 — V22 UI current-revision reconciliation and restarted rejection

Added DOM workflow proving saved revision3 draft is compared with revision5, no write occurs before explicit choice, local gap120 survives while unchanged server name/neighbors/calibration remain, and resulting save uses expected_revision5. Restart replay now handles definitive409/410/412/422 by removing immutable attempt/uncertainty durably while retaining raw draft, exposing explicit review rather than endless same-key conflict retry. DOM regression covers409 and retained local fields. Persistent editor now keys inner scope by API/origin/session/revision; late replay/discard success callbacks cannot clear parent dirty state after disposal. Final current TypeScript PASS; full frontend344 passed,7 skipped. Race-specific DOM coverage, actual current-policy re-fetch following restarted rejection, network-free identity/bootstrap/proxy cache, real browser/native, physical DB and complete-module gates remain open. Whole V22/V20–V32 acceptance not claimed.


### 2026-10-08 — V22 current policy gate after restarted rejection

Persistent editor now gates definitive restarted-attempt rejection on explicit authenticated current-policy read. Removes the rejected attempt while retaining raw fields, hides stale server comparison and restore/merge actions, offers read-only retry. Failed read preserves gate/draft and makes no new save; successful matching-session read passes current resource through route onUseCurrent (revision-keyed remount reloads retained draft) then comparison resumes. DOM regression now exercises rejection, failed read, enabled read retry, no additional mutation, successful read and retained local gap. Final current TypeScript PASS; frontend344 passed,7 skipped. No new test count: existing restart-rejection regression expanded. Route-level new revision remount and late callback race tests, real offline identity/proxy cache, browser/native/DB and full module gates remain outstanding. No complete V22/V20–V32 acceptance claim.


### 2026-10-08 — V22 authenticated scope replay race and failed durable admission

Added DOM scenario with persisted uncertain attempt for user3, deferred replay, API/user8 scope switch, new local gap90, then late success. Confirms old attempt alone removed, no late onDirty callback, new editor fields and user8 scoped record retained. Guarded discarded-scope discard failure state callback. Added DOM disk-full storage failure before immutable admission write: onSave never called, gap120 remains visible and no attempt persisted. Final TypeScript PASS; complete frontend346 passed,7 skipped. These regressions verify component/storage coordinator only; no real browser/native/network-free identity/physical storage restart or DB acceptance. Current-policy revision-remount route integration and full V22 module gates remain outstanding; broader V20–V32 goal unchanged.


### 2026-10-08 — V22 parent revision-remount regression

Added DOM Host regression with actual persistent/editor/storage components and React parent revision-keyed remount. Seeds old uncertain attempt, receives409, explicitly reads current revision5, remounts, verifies local gap120 retained against server90/neighbors8, explicitly reviews local changes, then saves expected_revision5/current name/calibration with a new key. Successful parent revision6 remount clears confirmed attempt and shows final gap120 with no obsolete saved-draft prompt. Full current typecheck and frontend347 passed,7 skipped; final stronger post-success UI assertions targeted persistent-editor suite8 passed. This is a parent/component integration harness, not the actual route/network/browser/native/DB. Remaining module acceptance gates unchanged; no complete V22 claim.


### 2026-10-08 — V22 persisted attempt contract validation

Coordinator prepare validates/copies immutable request before pinning memory; malformed key/body cannot poison future corrected admission. Readback compares validated owned request. Fixed capture calibration SHA to require actual string rather than regex coercion. Compared CapturePolicy with authoritative build-spec.py: checks minimum1/maximum6, now enforced in parser. New malformed-attempt regression covers key/extra fields/state/int64 overflow/duplicate or empty checks/queue age/non-string SHA, uncertainty pairing and owned capture arrays. Coordinator regression proves malformed attempt leaves no persisted pin and corrected request succeeds. Final TypeScript PASS; frontend349 passed,7 skipped. These tests cover persisted client admission contract, not full server schema/RLS/integration/real offline/module acceptance. Remaining V22 and whole V20–V32 objective unchanged.


### 2026-10-08 — V22 renewed backend/worker evidence and physical DB gate

Revalidated current implementation rather than historical green results. go test ./internal/v2sessions ./internal/v2input -count=1 PASS; DB-backed Go tests still conditional on PHOTO_TEST_DATABASE_URL, so this does not prove physical integration. System python3 lacks pytest; used existing repository .venv-platform without installing dependencies. Four Session describe/features/plan/adapter unit suites17 passed; scene pixel-quality synthetic suite5 passed. Physical scene-materialization test failed setup in sandbox (Operation not permitted); re-ran same test outside sandbox and confirmed127.0.0.1:55432 Connection refused, no migrations executed. Updated module inventory row to include persistent policy workflow. Remaining V22 acceptance: real DB fresh/upgrade/materialization/Apply/RLS/registry/GC/purge; real browser/native whole cycle; network-free identity/bootstrap and protected proxy caching; outlier assessment contract/runtime (currently null); actual5000-frame memory; real-photo/human quality. Existing synthetic proof is insufficient for those gates. Continue implementation on available independent gaps; do not mark goal blocked solely because DB unavailable. No whole-module acceptance.


### 2026-10-08 — V22 temporal-neighbor planning capacity

Optimized actual group_scenes neighbor discovery using inclusive bisect window over sorted known timestamps. Retains original global128-row block boundaries, cosine/top-k/ordinal tie-break/group-diameter/reference/correction rules; skips timestamps outside eligible interval before candidate scanning. Handles datetime min/max window overflow by saturating search bounds. Added5000-known-sparse singleton regression, exact-gap inclusion/one-microsecond exclusion and datetime extremes. Current describe/features/plan/adapter/pixel-quality suites26 passed. Measured current CPU descriptor construction+planning+validation with tracemalloc:5000 unknown0.615s/47,318,780 traced bytes;5000 known sparse0.785s/47,558,114 traced bytes on macOS arm64 Python3.11.3. Source SHA/platform/case results saved v22-plan-capacity-current.json. This is not dense worst-case, full worker RSS, RTX3060, image rendering or human quality evidence; no whole-module acceptance. Physical DB still unavailable from prior verified attempt.


### 2026-10-08 — V22 reproducible isolated CPU capacity benchmark

Added measure-v22-plan-capacity.py: each case runs in isolated child process with60s timeout, full5000-frame construction/grouping/plan validation, tracemalloc and OS process peak RSS, source SHA/platform/Python binding. Child errors/timeouts yield non-PASS status and exit1; output restricted to repository. Four current cases passed: unknown0.635s/93,470,720 RSS bytes; known sparse0.770s/95,338,496; known dense1-second spacing,k2:4.826s/105,447,424; same spacing,k64:12.247s/299,319,296. Updated v22-plan-capacity-current.json. Max traced allocation108,610,384 bytes. Results include Python/NumPy imports in child RSS but exclude actual decoder/render/model/whole worker. Dense sequential fixture is not all-identical timestamps or a qualified real-photo corpus; no RTX3060/human/full-module claim. Run with repository .venv-platform Python, no new dependencies.


### 2026-10-08 — V22 equal-timestamp burst capacity failure

Extended reproducible isolated benchmark with known_burst_64:5000 identical timestamps and valid identical embeddings,k64. Four prior cases passed on current source; burst child exceeded harness60s timeout and was terminated by subprocess.run. Harness exits1 and writes status=timeout, not PASS. This60s is the measurement harness bound, not a newly invented product SLO. No burst peak-memory/completed-plan evidence obtained. Current v22-plan-capacity-current.json records failure, superseding previous all-case green result. Added129-frame equal-timestamp/block-boundary permutation regression: complete plan/reference invariant under reversed input ordering, group partition validated. Current five worker suites27 passed. Next performance work must address dense temporal window/top-k and clustering costs while preserving exact grouping/calibration/tie-breaks; cannot widen harness timeout and call requirement accepted. Whole-worker/GPU/browser/native/DB/outlier/quality gates remain open.


### 2026-10-08 — V22 exact vectorized top-k and recovered burst capacity

Replaced per-candidate Python heap updates with bounded per-frame candidate-index/cosine arrays and exact NumPy lexsort(cosine descending,ordinal ascending). Original global128-row dot-product block boundaries, clamp, time window, cosine/luma qualification, sorted edge processing, cluster-diameter/reference/correction rules retained. Arrays hold at most4999 candidates; no global5000x5000 similarity allocation. Captured eight mixed-scene preoptimization golden plans under original source SHA0f2dc18e1452046b4e383d63738bc2b31ecda8a530a9ec1de3e39f79ea6d1193; current exact plan equality verified (IDs/ordinals/reference/confidence), saved baseline/equivalence artifacts. Eight retained regression tests assert grouping/reference exact and confidence tolerance1e-12 for portable floating calculation. Current five worker suites35 passed. Benchmark five cases passed under current sourcedeff6c5da565ff8c8fd39e9b03c6006204369c5ce552852e04b835d2606be26b: equal-time5000/k64 burst34.284s/335,298,560 peak RSS bytes versus prior60s timeout; dense1-second/k64:12.600s/340,590,592 RSS bytes. Dense k2:1.769s. Peak traced arrays/objects124,903,244 bytes. Current JSON supersedes timed-out prior source; no full worker/GPU/real-photo/human qualification or whole-module acceptance.


### 2026-10-08 — V22 read-only recovery and historical response fence

Separated mutation/busy lock from session read-only state: closed/archived persistent draft may be discarded locally or replay its exact prior attempt, while SessionPolicyEditor remains read-only and restore/merge unavailable. RU/BG/EN explanation added. Session route compares int64 response/current revisions and preserves newer authorized session on older idempotency replay response, preventing reopening/regressing archived state locally. Backend inspection confirms Update uses scoped idempotency after project authorization; actual DB replay still unproven. Three persistent-editor regressions cover closed/archive local discard and archived same-key/body replay with read-only fields; route regression covers older response retaining current heading. Initial edit command used wrong cwd and made no changes; corrected repository-root command. Route assertion initially used wrong accessibility role(header instead of rendered heading), corrected test. One full run had unrelated semantic-review-decisions retry failure; isolated3 tests and subsequent full rerun passed unchanged; root cause not established and no product fix claimed. Current final TypeScript PASS; full frontend353 passed,7 skipped. Whole-module/backend/native/browser/offline/quality acceptance remains open.

### 2026-10-08 — restored physical DB execution

Installed native Postgres.app18.4 enabled a new repository-contained isolated cluster. Corrected two previously unparsed V28 SQL CASE expressions (00832/00879); new empty database uninterrupted migrations PASS to894. Current v2sessions suite with actual PHOTO_TEST_DATABASE_URL:16pass events,0fail,0skip. Full source/log hashes in native-db-fresh-current.json; detailed output v22-native-db-current.jsonl. Fresh and scoped session integration evidence do not close upgrade, whole module browser/native/offline or real-photo3060/human qualification gates. Database availability is now resolved locally; module remains unaccepted.
