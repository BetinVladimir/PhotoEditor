# 11. Индекс артефактов

Статус: спроектировано, не реализовано (2026-10-22). Начало: [README](README.md). Раздел A — существующие файлы, на которые опирается ТЗ (ссылки проверены на существование 2026-10-22; существование не означает приёмку). Раздел B — новые и изменяемые артефакты (пути предлагаемые, файлов ещё нет).

## A. Существующие артефакты

### A.1 Приложение фотографа (`apps/photographer`)

| Файл | Зачем нужен ТЗ |
|---|---|
| [`app/_layout.tsx`](../../apps/photographer/app/_layout.tsx) | корневой layout: Paper-провайдер, локаль, авторизация |
| [`app/deliveries/new.tsx`](../../apps/photographer/app/deliveries/new.tsx), [`app/[locale]/deliveries/new.tsx`](../../apps/photographer/app/[locale]/deliveries/new.tsx), [`screens/deliveries/new.tsx`](../../apps/photographer/screens/deliveries/new.tsx) | образец тройки файлов маршрута |
| [`src/localized-router.tsx`](../../apps/photographer/src/localized-router.tsx) | локализованная навигация |
| [`src/paper-theme.ts`](../../apps/photographer/src/paper-theme.ts), [`src/paper-provider.tsx`](../../apps/photographer/src/paper-provider.tsx), [`src/paper-ui-policy.test.ts`](../../apps/photographer/src/paper-ui-policy.test.ts) | тема Paper и запрет примитивов RN |
| [`src/owner-api.ts`](../../apps/photographer/src/owner-api.ts) | клиент владельца: образец методов загрузки архивов, доставок, квот |
| [`src/interchange-archive-screen.tsx`](../../apps/photographer/src/interchange-archive-screen.tsx), [`-controller.ts`](../../apps/photographer/src/interchange-archive-controller.ts), [`-attempt-store.ts`](../../apps/photographer/src/interchange-archive-attempt-store.ts), [`-source.ts`](../../apps/photographer/src/interchange-archive-source.ts), [`-upload.ts`](../../apps/photographer/src/interchange-archive-upload.ts) | образец возобновляемой загрузки с журналом |
| [`src/editor-store.ts`](../../apps/photographer/src/editor-store.ts), [`editor-draft-cache.ts`](../../apps/photographer/src/editor-draft-cache.ts), [`editor-save-edits-attempt.ts`](../../apps/photographer/src/editor-save-edits-attempt.ts), [`editor-draft-conflict.tsx`](../../apps/photographer/src/editor-draft-conflict.tsx), [`editor-render-submit.ts`](../../apps/photographer/src/editor-render-submit.ts) | образец версионирования, черновика, конфликта и отправки рендера |
| [`src/run-watch.ts`](../../apps/photographer/src/run-watch.ts), [`screens/runs/[id].tsx`](../../apps/photographer/screens/runs/[id].tsx) | прогресс заданий по SSE |
| [`src/delivery-create-screen.tsx`](../../apps/photographer/src/delivery-create-screen.tsx), [`delivery-create-attempt-store.ts`](../../apps/photographer/src/delivery-create-attempt-store.ts), [`collection-access-links.tsx`](../../apps/photographer/src/collection-access-links.tsx), [`access-link-attempt-store.ts`](../../apps/photographer/src/access-link-attempt-store.ts) | образец выдачи и ссылок |
| [`src/interchange-prepared-review-screen.tsx`](../../apps/photographer/src/interchange-prepared-review-screen.tsx), [`import-mask-decision-attempt.ts`](../../apps/photographer/src/import-mask-decision-attempt.ts) | образец «решение человека по результату AI» |
| [`src/capabilities-response.ts`](../../apps/photographer/src/capabilities-response.ts), [`editor-capabilities.ts`](../../apps/photographer/src/editor-capabilities.ts) | гейтинг функций ответом сервера |
| [`src/use-read-resource.ts`](../../apps/photographer/src/use-read-resource.ts), [`read-deadline.ts`](../../apps/photographer/src/read-deadline.ts), [`locale-context.tsx`](../../apps/photographer/src/locale-context.tsx), [`catalog-style.ts`](../../apps/photographer/src/catalog-style.ts) | чтение ресурсов, дедлайны, локали, общие стили |
| [`screens/settings/quotas.tsx`](../../apps/photographer/screens/settings/quotas.tsx), [`screens/projects/[id]/index.tsx`](../../apps/photographer/screens/projects/[id]/index.tsx) | изменяемые экраны |
| [`package.json`](../../apps/photographer/package.json) | зависимости (Expo, Paper, noble-hashes) |

### A.2 Клиентские пакеты

| Файл | Зачем |
|---|---|
| [`packages/api-client/src/upload.ts`](../../packages/api-client/src/upload.ts) | текущий multipart-клиент, лимиты 512 МиБ/16 МиБ |
| [`packages/api-client/src/sse.ts`](../../packages/api-client/src/sse.ts) | поток событий |
| [`packages/api-client/src/canonical.ts`](../../packages/api-client/src/canonical.ts) | каноническая сериализация |
| [`packages/api-client/package.json`](../../packages/api-client/package.json) | пакет клиента |

### A.3 Портал

| Файл | Зачем |
|---|---|
| [`apps/portal/src/app/(localized)/[locale]/s/[share_id]/page.tsx`](../../apps/portal/src/app/%28localized%29/[locale]/s/[share_id]/page.tsx) | вход по ссылке |
| [`apps/portal/src/components/Portal.tsx`](../../apps/portal/src/components/Portal.tsx), [`lib/api.ts`](../../apps/portal/src/lib/api.ts), [`lib/types.ts`](../../apps/portal/src/lib/types.ts) | текущий клиент портала |
| [`lib/download-recovery.ts`](../../apps/portal/src/lib/download-recovery.ts), [`lib/download-preparation.ts`](../../apps/portal/src/lib/download-preparation.ts) | восстановление скачивания |

### A.4 Бэкенд

| Файл / каталог | Зачем |
|---|---|
| [`backend/internal/objectstore/`](../../backend/internal/objectstore) (`multipart.go`, `minio.go`, `signed_read_url.go`, `archive_cors.go`, `remove_confirmed.go`, `tenant_purge.go`, `original.go`) | MinIO, загрузка, подписи, удаление, очистка tenant |
| [`backend/internal/gateway/`](../../backend/internal/gateway) (`upload.go`, `download.go`, `large_io_test.go`) | путь воркера к объектам |
| [`backend/internal/httpapi/`](../../backend/internal/httpapi) (`interchange_archive_upload.go`, `interchange_archive_attempt_status.go`, `delivery.go`, `delivery_download_intent.go`, `download_redemption.go`, `event_sse.go`) | образцы эндпоинтов |
| [`backend/internal/portal/service.go`](../../backend/internal/portal/service.go) | сессия и данные портала |
| [`backend/internal/run`](../../backend/internal/run), [`workflow`](../../backend/internal/workflow), [`queue`](../../backend/internal/queue), [`billing`](../../backend/internal/billing), [`tenant`](../../backend/internal/tenant), [`admin`](../../backend/internal/admin), [`canonical`](../../backend/internal/canonical), [`asset`](../../backend/internal/asset), [`collection`](../../backend/internal/collection) | заимствуемые модули |
| [`backend/db/migrations/02641_user_project_upload_completion.sql`](../../backend/db/migrations/02641_user_project_upload_completion.sql) | образец завершения загрузки с резервом квоты |
| [`02598…02609`](../../backend/db/migrations/02600_billing_quota_accounting.sql) (billing) | учёт квот и резервов |
| [`01219_v30_delivery_storage.sql`](../../backend/db/migrations/01219_v30_delivery_storage.sql) | образец таблиц доставки |
| [`00010_constraints_security.sql`](../../backend/db/migrations/00010_constraints_security.sql), [`00100_artifact_gc.sql`](../../backend/db/migrations/00100_artifact_gc.sql) | `share_links`, GC артефактов |

### A.5 Воркеры и развёртывание

| Файл | Зачем |
|---|---|
| [`workers/photo_worker/runtime/service.py`](../../workers/photo_worker/runtime/service.py), [`cuda_budget.py`](../../workers/photo_worker/runtime/cuda_budget.py), [`metal_budget.py`](../../workers/photo_worker/runtime/metal_budget.py), [`restore_cleanup.py`](../../workers/photo_worker/runtime/restore_cleanup.py) | конфигурация, бюджеты, очистка |
| [`workers/photo_worker/models/`](../../workers/photo_worker/models) (`backend_support.py`, `lock.py`, `install.py`, `doctor.py`, `bundle.py`, `pool.py`) | модели и бэкенды |
| [`workers/photo_worker/operations/`](../../workers/photo_worker/operations), [`contracts/`](../../workers/photo_worker/contracts), [`algorithms/color.py`](../../workers/photo_worker/algorithms/color.py), [`algorithms/torch_color.py`](../../workers/photo_worker/algorithms/torch_color.py), [`algorithms/statistics.py`](../../workers/photo_worker/algorithms/statistics.py), [`algorithms/style.py`](../../workers/photo_worker/algorithms/style.py), [`color/icc.py`](../../workers/photo_worker/color/icc.py) | операции и численные модули |
| [`workers/pyproject.toml`](../../workers/pyproject.toml), [`requirements-cuda.txt`](../../workers/requirements-cuda.txt), [`workers/tests`](../../workers/tests) | зависимости и тесты |
| [`deploy/compose.rtx-worker.yaml`](../../deploy/compose.rtx-worker.yaml), [`compose.yaml`](../../deploy/compose.yaml), [`provision-rtx-worker.sql`](../../deploy/provision-rtx-worker.sql), [`activate-rtx-worker.sql`](../../deploy/activate-rtx-worker.sql), [`reconfigure-worker-capacity.sql`](../../deploy/reconfigure-worker-capacity.sql) | развёртывание и ёмкость воркеров |

### A.6 Документация и контракты

| Файл | Зачем |
|---|---|
| [`docs/repository-layout.md`](../repository-layout.md), [`implementation-v2/README.md`](../implementation-v2/README.md) | карта и вход в документацию |
| [`implementation-v2/photographer-paper-ui-contract.md`](../implementation-v2/photographer-paper-ui-contract.md) | правила UI |
| [`implementation-v2/mvp-web-release-plan-2026-10-20.md`](../implementation-v2/mvp-web-release-plan-2026-10-20.md) | граница релиза |
| [`implementation-v2/billing-prelaunch-plan-2026-10-18.md`](../implementation-v2/billing-prelaunch-plan-2026-10-18.md) | тарифы, диск, трафик |
| [`implementation-v2/19-runtime-protocol.md`](../implementation-v2/19-runtime-protocol.md), [`18-capacity-operations.md`](../implementation-v2/18-capacity-operations.md), [`17-integration-security.md`](../implementation-v2/17-integration-security.md), [`11-consent-lifecycle.md`](../implementation-v2/11-consent-lifecycle.md), [`09-workflow-transactions.md`](../implementation-v2/09-workflow-transactions.md), [`03-database.md`](../implementation-v2/03-database.md) | протокол, ёмкость, безопасность, согласия, workflow, БД |
| [`implementation-v2/contracts/`](../implementation-v2/contracts) (`operations.json`, `openapi.json`, `endpoints.json`, `schemas.json`, `database.json`, `schema-reference.sql`, `function-index.json`, `collection-publication-contract.md`, `delivery-metadata-crosslang.json`) | нормативные контракты |
| [`implementation-v2/capacity-policy-operator.md`](../implementation-v2/capacity-policy-operator.md), [`metal-alternatives-implementation.md`](../implementation-v2/metal-alternatives-implementation.md), [`project-asset-withdrawal-contract.md`](../implementation-v2/project-asset-withdrawal-contract.md), [`generic-idempotency-retention-contract.md`](../implementation-v2/generic-idempotency-retention-contract.md), [`v32-tenant-purge-buckets-runtime.md`](../implementation-v2/v32-tenant-purge-buckets-runtime.md) | профильные контракты |
| [`implementation-v2/runbooks/`](../implementation-v2/runbooks) | процедуры квалификации и эксплуатации воркеров |
| [`implementation-v2/build-database.py`](../implementation-v2/build-database.py), [`build-spec.py`](../implementation-v2/build-spec.py), [`check-docs.py`](../implementation-v2/check-docs.py), [`sqlc.yaml`](../../sqlc.yaml) | генераторы и проверки |
| [`locales/`](../../locales), [`schemas/config.schema.json`](../../schemas/config.schema.json), [`release/`](../../release) | переводы, пример закрытой схемы, SBOM |

## B. Новые и изменяемые артефакты (предлагаемые)

| Артефакт | Тип | Пакет работ |
|---|---|---|
| `docs/video/*.md` | документация (этот каталог) | WP-00 |
| `backend/db/migrations/02651…` | миграции | WP-04 |
| `backend/internal/video/{media,sequence,job,quota,share}` | Go-модули | WP-04…13 |
| `backend/internal/httpapi/video_*.go` | эндпоинты | WP-05…13 |
| `backend/internal/objectstore/video_upload.go` | загрузка | WP-05 |
| `docs/implementation-v2/contracts/*` (регенерация) | контракты | WP-01 |
| `workers/photo_worker/operations/video/*`, `operations/audio/*` | операции | WP-06…12 |
| `workers/photo_worker/contracts/video.py` | контракты Params/Result | WP-01 |
| `workers/photo_worker/runtime/ffmpeg.py`, `runtime/mlx_budget.py` | среда | WP-03 |
| `workers/photo_worker/algorithms/video_lut.py` | генератор LUT | WP-10 |
| `workers/photo_worker/operations/video/graph.py` | `compile_graph` | WP-12 |
| `deploy/compose.video-worker*.yaml`, SQL провижининга | развёртывание | WP-03, 15 |
| `packages/api-client/src/video-{upload,timeline,api}.ts` | клиент | WP-05, 09 |
| `apps/photographer/{app,app/[locale],screens}/video/**`, `src/video-*.ts(x)` | приложение | WP-05…13 |
| `apps/portal/src/components/Video*.tsx`, `lib/video-*.ts` | портал | WP-13 |
| `locales/{en,ru,bg}/common.json` (ключи `video_*`) | переводы | все |
| `tests/fixtures/video/manifest.json`, `tests/contracts/video-*` | тестовые материалы | WP-01, 14 |
| `docs/implementation-v2/runbooks/video-worker-*.md` | эксплуатация | WP-15 |
| `backend/internal/video/quota/scale.go` | допуск, стоимость, Retry-After, аренды (`FN-Q-09…13`) | WP-17 |
| `backend/db/migrations/02663…02665_video_*.sql` | бюджеты трафика, лимиты арендатора, допуск и обслуживание | WP-17 |
| `backend/internal/portal/video_stream_limit.go` | лимиты потоков и аренды (`FN-G-14`) | WP-17 |
| `workers/photo_worker/runtime/source_cache.py` | кэш исходников запуска (D-14) | WP-17 |
| `workers/photo_worker/operations/video/lut_validate.py` | проверка `.cube` (`FN-W-12`) | WP-06 |
| `tests/load/video/*` | `TC-LOAD-05…10` | WP-17 |
| `workers/photo_worker/models/license_policy.py`, `license_gate.py` | политика лицензий, гейт SBOM (FN-X-01…03) | WP-18 |
| `workers/photo_worker/runtime/ffmpeg_build.py`, `deploy/images/ffmpeg-lgpl/` | LGPL-сборка ffmpeg, выбор кодировщика, сканирование образа | WP-18 |
| `apps/photographer/src/video-compose.ts`, `video-playback-state.ts`, `video-playback-engine.web.ts`, `video-audio-graph.web.ts` | состав кадра, автомат и движок воспроизведения, звук | WP-19 |
| `apps/photographer/src/video-timeline-geometry.ts`, `video-timeline-gesture.ts`, `video-timeline-canvas.web.tsx`, `video-shortcuts.ts` | геометрия, жесты, отрисовка, клавиши | WP-19 |
| `apps/photographer/src/video-monitor.web.tsx`, `video-inspector.tsx`, `video-bin.tsx`, `video-color-*-math.ts`, `video-scopes-math.ts` | монитор, инспектор, бин, цвет | WP-19 |
| `backend/internal/httpapi/video_playback_links.go` | ссылки воспроизведения (FN-G-15) | WP-19 |
| `docs/implementation-v2/runbooks/video-scaling.md` | эксплуатация масштабирования | WP-17 |
