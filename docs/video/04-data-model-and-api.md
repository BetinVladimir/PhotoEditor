# 04. Данные и API

Статус: спроектировано, не реализовано (2026-10-22). Начало: [README](README.md). Имена таблиц, колонок и путей — предлагаемые; перед реализацией сверяются с генераторами контрактов ([`docs/implementation-v2/build-database.py`](../implementation-v2/build-database.py), [`build-spec.py`](../implementation-v2/build-spec.py), [`check-docs.py`](../implementation-v2/check-docs.py)) и с `sqlc` ([`sqlc.yaml`](../../sqlc.yaml)).

## 1. Принципы

- Схема `photo`, все таблицы с `tenant_id`, составными внешними ключами `(tenant_id, …)` и `UNIQUE (tenant_id, id)` — как в [`01219_v30_delivery_storage.sql`](../../backend/db/migrations/01219_v30_delivery_storage.sql).
- Миграции forward-only, номера после `02650`; функции с `SECURITY DEFINER`, фиксированным `search_path`, владельцем `photo_fn_owner`, `REVOKE ALL … FROM PUBLIC` и точечным `GRANT` (образец — [`02641_user_project_upload_completion.sql`](../../backend/db/migrations/02641_user_project_upload_completion.sql)). Прямых DML-прав приложению на новые таблицы нет.
- Крупные неизменяемые данные (документ монтажа, отчёты, probe) хранятся артефактами в MinIO, в БД — хеш, размер и ограниченная сводка (`jsonb` ≤ 16 КиБ).
- Каждая мутация имеет идемпотентный ключ и запись-квитанцию (см. [generic-idempotency-retention-contract.md](../implementation-v2/generic-idempotency-retention-contract.md)).
- Состояние объекта меняют только функции БД, а не произвольные UPDATE из Go.

## 2. Таблицы

### 2.1 `video_upload_sessions`

Параллельна `photo.upload_sessions` (фото-загрузка завязана на `ingest`, RAW и `assume_profile`, поэтому не расширяется).

| Колонка | Тип / правило |
|---|---|
| `id`, `tenant_id`, `project_id`, `principal_id` | uuid; principal — пользователь |
| `media_kind` | `video` \| `audio` \| `lut` |
| `filename` | 1–255 символов, без `/`, `\`, NUL |
| `declared_bytes`, `declared_sha256` | размер ≤ политики, 64 hex |
| `request_sha256` | хеш канонического запроса (закрепляет повтор begin) |
| `part_bytes`, `parts_total` | вычисляются сервером; `parts_total = ceil(bytes/part_bytes) ≤ 10 000` |
| `multipart_id`, `object_key`, `object_version` | идентификаторы MinIO |
| `state` | `initializing, uploading, completing, verifying, verified, rejected, aborted, expired` (как `RemoteUploadState` в [`upload.ts`](../../packages/api-client/src/upload.ts)) |
| `reservation_id` | резерв квоты на `declared_bytes` |
| `completion_token`, `completion_lease_until` | защита завершения от гонок (как у фото) |
| `expires_at` | по умолчанию 7 суток простоя |
| `revision` | CAS |

### 2.2 `video_media`

| Колонка | Тип / правило |
|---|---|
| `id`, `tenant_id`, `project_id` | uuid |
| `kind` | `video` \| `audio` \| `lut` (файл `.cube`, ≤ 20 МиБ, проверяется `video.lut_validate`) |
| `name` | 1–255 |
| `state` | `probing, ready, rejected, failed, withdrawing, deleted` |
| `reject_code` | код из раздела 6, обязателен при `rejected` |
| `source_artifact_id`, `source_sha256`, `source_bytes` | оригинал (иммутабелен) |
| `probe_artifact_id`, `probe` | отчёт probe и ограниченная сводка (контейнер, кодеки, длительность в тиках, размер кадра, fps как рациональное число, VFR, поворот, цветовые метаданные, число и раскладка аудиопотоков) |
| `proxy_artifact_id`, `filmstrip_artifact_id`, `waveform_artifact_id` | производные; обязательны при `ready` для видео (для аудио — только волна) |
| `ingest_job_id`, `upload_id` | связи |
| `created_by`, `created_at`, `revision`, `withdrawn_at` | служебные |

Инвариант: `ready` ⇒ все обязательные производные `ready` и целы по SHA-256.

### 2.3 `video_sequences` и ревизии

`video_sequences`: `id, tenant_id, project_id, name, fps_num, fps_den, width, height, head_revision, created_by, created_at, updated_at, archived_at, revision`.

`video_sequence_revisions` (append-only): `tenant_id, sequence_id, revision (bigint, с 1), parent_revision, document_artifact_id, document_sha256, summary jsonb (число клипов, длительность в тиках, дорожки), label ('autosave' | 'manual' | 'render' | 'cut_application'), created_by, created_at`. `UNIQUE (tenant_id, sequence_id, revision)`.

`video_sequence_sources`: `tenant_id, sequence_id, revision, source_kind ('video'|'audio'|'photo'|'lut'), source_id, source_sha256` — индекс использования (для «где используется», предупреждений при удалении и заморозки рендера).

Сохранение ревизии — функция БД `save_video_sequence_revision(tenant, sequence, expected_revision, document_artifact, sha, summary, sources, label, idempotency)`: проверяет, что `expected_revision = head_revision`, что все источники принадлежат тому же tenant/project и в состоянии `ready`, что хеш документа совпадает с артефактом; в одной транзакции вставляет ревизию и источники, сдвигает `head_revision`. Несовпадение — `VIDEO_SEQUENCE_REVISION_CONFLICT` с текущей головой в ответе.

### 2.4 `video_jobs`

| Колонка | Тип / правило |
|---|---|
| `id, tenant_id, project_id` | |
| `kind` | `ingest, detect_cuts, color_analyze, preview_frames, audio_analyze, render` |
| `run_id` | связь с существующей моделью runs/tasks ([`backend/internal/run`](../../backend/internal/run)) |
| `media_id`, `sequence_id`, `sequence_revision` | субъект (нужное по виду) |
| `params_artifact_id`, `params_sha256` | неизменяемые параметры |
| `state` | `queued, running, succeeded, failed, cancelled, expired` |
| `stage`, `progress numeric(5,4)`, `eta_seconds` | наблюдаемое состояние; в БД пишется не чаще `JOB_PROGRESS_DB_INTERVAL_MS` и при изменении ≥ `JOB_PROGRESS_DB_MIN_DELTA_PCT` либо смене стадии (SSE — чаще, из памяти); [20](20-saas-scaling-operations.md), 5.1 |
| `cost_seconds`, `pool` | стоимость задания (`FN-Q-09`) и учётный пул (`video_light`/`video_heavy`) для допуска, [20](20-saas-scaling-operations.md), раздел 3 |
| `backend_requested` / `backend_used` | `auto, cuda, mlx, cpu` / фактический |
| `reservation_id` | резерв квоты |
| `error_code`, `error_detail` | код и ограниченный текст (≤ 512 символов, без путей и данных других tenant) |
| `created_by, created_at, started_at, finished_at, revision` | |

`video_job_outputs (tenant_id, job_id, role, ordinal, artifact_id)` — выходы по ролям.

### 2.5 `video_renders`

`id, tenant_id, project_id, sequence_id, sequence_revision, document_sha256, preset jsonb, preset_sha256, job_id, output_artifact_id, output_bytes, duration_ticks, width, height, fps_num, fps_den, video_codec, audio_codec, encoder, backend_used, render_fingerprint, qc_report_artifact_id, state ('queued','rendering','verifying','ready','failed','cancelled','deleted'), created_by, created_at, deleted_at, revision`.

`render_fingerprint` = SHA-256 канонического объекта {хеш документа, пресет, версия компилятора графа, версии ffmpeg и кодеков, замок моделей, энкодер}. Он объясняет, почему два рендера одной ревизии на разных бэкендах могут отличаться побайтно ([07](07-timeline-edl-and-render-spec.md)).

### 2.6 Выдача

- `collections.content_kind text NOT NULL DEFAULT 'photo' CHECK (content_kind IN ('photo','video'))`; существующие строки не меняются.
- `video_collection_items (tenant_id, collection_id, render_id, ordinal, title, published_at, revision)`; `UNIQUE (tenant_id, collection_id, render_id)`.
- `video_egress_events (id, tenant_id, share_id, render_id, kind ('download'), bytes, at)` — только скачивания, append-only, партиционирована по месяцам; потоковый трафик учитывается агрегатами и арендами (`video_egress_counters`, `video_egress_period`, `video_share_budget`, `video_egress_leases`, DS-28; [20](20-saas-scaling-operations.md), 5.2); агрегируется в счётчик исходящего трафика биллинга ([billing-prelaunch-plan](../implementation-v2/billing-prelaunch-plan-2026-10-18.md), раздел 2A); если счётчик ещё не реализован, таблица является источником, а агрегат строится из неё.

Существующие `share_links` и сессии портала не меняются ([`00010_constraints_security.sql`](../../backend/db/migrations/00010_constraints_security.sql)).

### 2.7 Квота

Расширяются существующие `photo.quota_reservations`: допустимые `holder_type` дополняются `video_upload` и `video_job`; поведение резерва — по образцу `holder_type = 'upload'` ([02641](../../backend/db/migrations/02641_user_project_upload_completion.sql)). Категория учёта выводится из `photo.artifacts.output_role`; для видео вводятся роли: `video_original`, `audio_original`, `video_probe`, `video_proxy`, `video_filmstrip`, `video_waveform`, `video_cut_report`, `video_color_report`, `video_preview_frame`, `video_sequence_document`, `video_render`, `video_qc_report`, `video_render_manifest` (манифест замыкания источников, DS-23). Подробности — [05](05-storage-quota-retention.md).

## 3. Автоматы состояний

| Объект | Переходы | Запреты |
|---|---|---|
| Upload | `initializing → uploading → completing → verifying → verified`; `uploading/completing → aborted`; любое активное `→ expired`; `verifying → rejected` | `verified/rejected/aborted/expired` терминальны; повторный `complete` одной попытки возвращает тот же результат |
| Media | `probing → ready \| rejected \| failed`; `ready → withdrawing → deleted` | использовать в рендере можно только `ready` |
| Job | `queued → running → succeeded \| failed \| cancelled`; `queued/running → expired` | `cancelled` не переходит в `succeeded`; результаты отменённого задания не публикуются |
| Render | `queued → rendering → verifying → ready \| failed`; любое нетерминальное `→ cancelled`; `ready → deleted` | публиковать (выдавать) можно только `ready` |

## 4. Эндпоинты владельца

Префикс `/api/v1/tenants/{t}`. Идентификаторы в пути — UUID; все POST/PUT/DELETE принимают `Idempotency-Key`. Ответ на каждый запрос проверяется клиентом на принадлежность tenant/project (образец проверок — `assert*Scope` в клиентских модулях).

| Метод и путь | Назначение | Заметки |
|---|---|---|
| `GET /projects/{p}/video/capabilities` | политика: лимиты, форматы, доступные операции и бэкенды, причины недоступности | основа `useVideoCapabilities` |
| `GET /video/usage` | занятое по категориям, резервы, трафик | tenant-уровень, для V-13 |
| `POST /projects/{p}/video-uploads` | begin | тело: `filename, media_kind, bytes, sha256, request_sha256`; ответ: `upload_id, part_bytes, parts_total`; отказ до передачи при нехватке места |
| `POST …/video-uploads/attempt-status` | сверка неоднозначного begin | как [`interchange-archive-uploads/attempt-status`](../../backend/internal/httpapi/interchange_archive_attempt_status.go) |
| `POST …/video-uploads/{id}/parts` | подписанные URL частей | `part_numbers[]`, URL живут ≤ 60 с |
| `POST …/video-uploads/{id}/complete` | завершение | список `{part_no, etag}`; запускает ingest |
| `GET …/video-uploads/{id}` | статус | |
| `POST …/video-uploads/{id}/abort` | отмена | освобождает резерв, чистит части |
| `GET /projects/{p}/video-media` | список | фильтры `kind, state`, курсор `after`, `limit ≤ 100` |
| `GET /projects/{p}/video-media/{id}` | карточка | |
| `DELETE /projects/{p}/video-media/{id}` | удаление | `expected_revision`, `confirm_in_use`; освобождает квоту после физического удаления |
| `GET /projects/{p}/video-media/{id}/proxy` | чтение proxy владельцем | аутентифицированный запрос диапазона → `307` на подписанную ссылку ≤ 60 с (режим `redirect`, решение D-15 в [20](20-saas-scaling-operations.md)) |
| `POST /projects/{p}/video-media/{id}/playback-links` | ссылка для монитора редактора и плеера (`<video>` не передаёт `Authorization`) | тело `{variant: proxy|original_audio}`; ответ `{url, expires_at, mime, codec}` (DS-31), срок ≤ 60 с; `409 VIDEO_PLAYBACK_UNSUPPORTED`; [23](23-editor-playback-timeline.md), раздел 15 |
| `POST /projects/{p}/video-media/{id}/derivative-links` | подписанные ссылки на раскадровку и волну | ≤ 60 с |
| `POST /projects/{p}/video-media/{id}/cut-detections` | запуск детекции границ | тело: `mode, sensitivity, backend`; ответ `job` |
| `POST /projects/{p}/video-media/{id}/color-analyses` | запуск цветоанализа | |
| `POST /projects/{p}/video-media/{id}/audio-analyses` | анализ темпа/громкости | |
| `POST /projects/{p}/video-sequences` | создать монтаж | |
| `GET /projects/{p}/video-sequences` | список | |
| `GET /projects/{p}/video-sequences/{id}` | голова и сводка | |
| `GET /projects/{p}/video-sequences/{id}/revisions/{rev}` | документ ревизии | ответ ограничен 8 МиБ |
| `PUT /projects/{p}/video-sequences/{id}` | сохранить ревизию | `expected_revision, document, label`; `409 VIDEO_SEQUENCE_REVISION_CONFLICT` |
| `POST /projects/{p}/video-sequences/{id}/duplicate`, `…/archive` | копия, архив | |
| `POST /projects/{p}/video-sequences/{id}/cut-applications` | применить принятые границы | `expected_revision, job_id, accepted[], mode` → новая ревизия |
| `POST /projects/{p}/video-sequences/{id}/preview-frames` | превью-кадры с эффектами | список кадров (≤ 16) и ревизия; ответ `job`; быстрый пул |
| `POST /projects/{p}/video-sequences/{id}/renders` | запуск рендера | тело: `revision, preset, name, rights_declaration`; резерв квоты в той же транзакции |
| `GET /video-jobs/{id}` | статус задания | |
| `POST /video-jobs/{id}/cancel`, `…/retry` | отмена, повтор | журнал попыток клиента |
| `GET /video-jobs/{id}/outputs/{role}` | чтение выходного документа | только JSON-роли ≤ 8 МиБ |
| `GET /projects/{p}/video-renders`, `GET …/{id}`, `DELETE …/{id}` | рендеры | |
| `POST …/video-renders/{id}/download-intent` | намерение скачивания | далее общий `/api/v1/downloads/redeem` ([`download_redemption.go`](../../backend/internal/httpapi/download_redemption.go)) |
| `POST /projects/{p}/video-deliveries` | создать выдачу | `render_ids[], name, expires_at, secret?, allow_download, egress_limit_bytes?` |
| `GET /projects/{p}/video-deliveries`, `GET …/{id}`, `GET …/{id}/stats` | список, карточка, статистика | |
| `POST …/video-deliveries/{id}/publish`, `…/unpublish`, `…/revoke-link`, `…/rotate-secret`, `…/extend` | управление | `expected_revision` |

События SSE (общий поток `/api/v1/tenants/{t}/events`, [`event_sse.go`](../../backend/internal/httpapi/event_sse.go)): `video.media.state`, `video.job.progress`, `video.job.state`, `video.render.state`, `video.delivery.state`, `video.quota.changed`. Полезная нагрузка содержит идентификаторы и состояние, не содержит имён файлов и путей.

Эндпоинты портала — в [08](08-delivery-portal.md).

## 5. Допуск и гейтинг

1. Каждая операция проверяется на сервере по capability (`video` включён для tenant, операция квалифицирована на запрошенном бэкенде, есть живой воркер нужного класса). Нет квалификации — `VIDEO_BACKEND_UNAVAILABLE` с причиной; клиент показывает её через `CapabilityNotice`.
2. Резерв квоты и создание run/outbox — в одной транзакции; нехватка места — отказ без побочных эффектов.
3. Повтор с тем же `Idempotency-Key` и тем же телом возвращает исходный ответ; с другим телом — `409 IDEMPOTENCY_KEY_REUSED` (поведение существующих эндпоинтов).
4. Лимиты (число активных заданий, размер, длительность) читаются из политики ёмкости, а не из кода; администратор меняет их без релиза (по образцу [`reconfigure-worker-capacity.sql`](../../deploy/reconfigure-worker-capacity.sql)).

## 6. Коды ошибок

| Код | Когда |
|---|---|
| `VIDEO_UNSUPPORTED_FORMAT`, `VIDEO_CODEC_UNSUPPORTED`, `VIDEO_CORRUPT` | отказ по результату probe/декодирования |
| `VIDEO_TOO_LARGE`, `VIDEO_TOO_LONG`, `VIDEO_RESOLUTION_UNSUPPORTED` | превышен лимит политики |
| `VIDEO_QUOTA_EXCEEDED` | не хватает места или трафика |
| `VIDEO_BACKEND_UNAVAILABLE` | нет квалифицированного воркера/модели |
| `VIDEO_SOURCE_MISSING`, `VIDEO_SOURCE_NOT_READY` | источник удалён или не готов |
| `VIDEO_SEQUENCE_INVALID`, `VIDEO_SEQUENCE_REVISION_CONFLICT` | документ не прошёл проверку; устаревшая ревизия |
| `VIDEO_RIGHTS_DECLARATION_REQUIRED` | рендер с музыкой без декларации |
| `VIDEO_RENDER_PRESET_INVALID` | неизвестный или запрещённый пресет |
| `VIDEO_JOB_NOT_CANCELLABLE`, `VIDEO_JOB_STALE` | отмена терминального задания; результат относится к другой ревизии |
| `VIDEO_SHARE_REVOKED`, `VIDEO_SHARE_EXPIRED`, `VIDEO_EGRESS_LIMIT` | для портала |
| `VIDEO_TENANT_LIMIT`, `VIDEO_CAPACITY_BUSY`, `VIDEO_STREAM_LIMIT` | лимиты арендатора, перегрузка пула, потоки портала ([20](20-saas-scaling-operations.md), DS-30) |
| `UPLOAD_EXPIRED`, `UPLOAD_REJECTED`, `UPLOAD_HASH_MISMATCH`, `VIDEO_UPLOAD_RECONCILIATION_REQUIRED` | поведение загрузки (последний — аналог `ARCHIVE_BEGIN_RECONCILIATION_REQUIRED` архивов) |

## 7. Обновляемые контракты и генераторы

Нормативные артефакты в [`docs/implementation-v2/contracts`](../implementation-v2/contracts) обновляются **генераторами**, а не вручную: [`openapi.json`](../implementation-v2/contracts/openapi.json), [`endpoints.json`](../implementation-v2/contracts/endpoints.json), [`schemas.json`](../implementation-v2/contracts/schemas.json), [`operations.json`](../implementation-v2/contracts/operations.json), [`database.json`](../implementation-v2/contracts/database.json), [`schema-reference.sql`](../implementation-v2/contracts/schema-reference.sql), [`function-index.json`](../implementation-v2/contracts/function-index.json). Реестр текущего состояния — [`current-operation-scope-2026-10-17.json`](../implementation-v2/current-operation-scope-2026-10-17.json) и [`current-endpoint-scope-2026-10-17.json`](../implementation-v2/current-endpoint-scope-2026-10-17.json) дополняются новой записью области «Видео» с датой.

## 8. Требования

| ID | Требование |
|---|---|
| VID-D-01 | Все новые таблицы имеют `tenant_id`, составные FK, `UNIQUE (tenant_id, id)`, ограничения длины и `CHECK` состояний; нет прямых DML-прав приложению. |
| VID-D-02 | Ревизия монтажа неизменяема; `head_revision` сдвигается только функцией сохранения с проверкой `expected_revision` и источников. |
| VID-D-03 | `ready` у медиа невозможно без целых обязательных производных. |
| VID-D-04 | Любое видео-задание связано с резервом квоты; завершение освобождает остаток и фиксирует фактические байты в одной транзакции. |
| VID-D-05 | Удаление медиа/рендера не оставляет сирот в MinIO: физическое удаление подтверждается до освобождения квоты (правило существующего `remove_confirmed.go`). |
| VID-D-06 | Миграции forward-only, применимы на пустой и на текущей базе; существующие данные не перезаписываются. |
| VID-API-01 | Каждая мутация идемпотентна; повтор не дублирует эффекты и не списывает квоту дважды. |
| VID-API-02 | Ответы проверяются клиентом на область (tenant/project/объект); чужой tenant неотличим от «не найдено». |
| VID-API-03 | Список ограничен курсором и `limit ≤ 100`; ответы документов ≤ 8 МиБ. |
| VID-API-04 | События SSE не содержат имён и путей; клиент восстанавливается по курсору. |
| VID-API-05 | Контракты (`openapi`, `endpoints`, `schemas`, `operations`, `database`) обновлены генераторами и проходят `check-docs.py`. |
