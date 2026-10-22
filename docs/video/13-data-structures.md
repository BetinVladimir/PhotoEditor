# 13. Структуры данных: нормативные определения

Статус: спроектировано, не реализовано (2026-10-22). Начало: [README](README.md). Метод — [12](12-decomposition-method.md). Это **нормативный** источник точных имён и типов для задач [15](15-task-cards.md). Более общие описания — [04](04-data-model-and-api.md) и [07](07-timeline-edl-and-render-spec.md); если здесь и там расхождение, остановиться (п. 5.4 документа 12).

Оглавление: DS-01 константы · DS-02…DS-08 SQL · DS-09…DS-12 TypeScript · DS-13…DS-17 Python · DS-18 Go · DS-19…DS-23 JSON по проводу · DS-24 коды ошибок · DS-25 SSE · DS-26 клиентские журналы попыток · DS-27 ключи локализации · DS-28…DS-29 SQL масштабирования (миграции 02663…02664) · DS-30 тела ошибок и расширение capabilities · DS-31 ссылки воспроизведения.

## DS-01. Константы (единственное место значений)

| Имя | Значение | Где используется |
|---|---|---|
| `TICKS_PER_SECOND` | `705600000` | время, клиент, Go, Python |
| `SUPPORTED_FPS` | `24/1, 25/1, 30/1, 50/1, 60/1, 24000/1001, 30000/1001, 60000/1001` | валидатор, компилятор |
| `MAX_DOC_BYTES` | `8388608` (8 МиБ канонического JSON) | VT-02 |
| `MAX_VIDEO_TRACKS` / `MAX_AUDIO_TRACKS` | `4` / `4` | VT-02 |
| `MAX_CLIPS` | `2000` | VT-02 |
| `MAX_DURATION_TICKS` | `7620480000000` (3 ч) | VT-02 |
| `MAX_LAYERS_AT_POINT` | `8` | компилятор |
| `MIN_SHOT_FRAMES` | `8` | детектор |
| `SNAP_PX` | `8` | редактор |
| `RULER_H` / `VIDEO_TRACK_H` / `AUDIO_TRACK_H` / `TRACK_GAP` / `HEADER_W` | `28` / `64` / `48` / `2` / `160` (px) | таймлайн, [23](23-editor-playback-timeline.md) 7 |
| `EDGE_PX` / `TRANSITION_HANDLE_PX` / `DRAG_THRESHOLD_PX` | `6` / `10` / `4` (px) | хит-тест, жесты |
| `ZOOM_LADDER` | `705600, 1411200, 3528000, 7056000, 14112000, 35280000, 70560000, 141120000, 352800000, 705600000, 1411200000, 3528000000, 7056000000` | масштаб таймлайна |
| `PHOTO_DEFAULT_TICKS` | `3528000000` (5 с) | фото из бина |
| `PRELOAD_LEAD_TICKS` | `1411200000` (2 с) | воспроизведение |
| `LINK_REFRESH_AGE_MS` / `DECK_MAX_FAILURES` / `DRIFT_MAX_MS` | `45000` / `3` / `120` | воспроизведение |
| `PREVIEW_FRAME_DEBOUNCE_MS` | `400` | точный кадр на паузе |
| `UNDO_MAX_STEPS` / `UNDO_MAX_BYTES` | `200` / `33554432` | хранилище таймлайна |
| `EFFECTS_PER_CLIP_MAX` / `MARKER_LABEL_MAX` | `16` / `200` | команды |
| `AUDIO_PREVIEW_CODECS` | `aac, mp3, opus, vorbis, flac, pcm_s16le, pcm_s24le, pcm_f32le` | монитор |
| `PLAYBACK_LINK_TTL_SECONDS` / `PLAYBACK_LINK_PER_MIN` | `60` / `120` | ссылки воспроизведения |
| `UPLOAD_MIN_PART_BYTES` | `33554432` (32 МиБ) | загрузка |
| `UPLOAD_MAX_PARTS` | `10000` (расчёт делит на `9000`) | загрузка |
| `UPLOAD_MAX_BYTES` | `17179869184` (16 ГиБ) | загрузка |
| `UPLOAD_PART_CONCURRENCY` | `3` | клиент |
| `PART_URL_TTL_SECONDS` | `60` | подпись частей |
| `UPLOAD_IDLE_EXPIRY_DAYS` | `7` | сессии |
| `LUT_MAX_BYTES` | `20971520` (20 МиБ) | `.cube` |
| `PREVIEW_MAX_FRAMES` | `16` | `video.preview_frames` |
| `PREVIEW_JPEG_QUALITY` / `PREVIEW_MAX_SIDE` | `90` / `1280` | превью |
| `LIST_MAX_LIMIT` | `100` | списки |
| `JOB_PROGRESS_MIN_INTERVAL_MS` | `2000` | задания |
| `DERIV_EST_FRACTION_PCT` | `15` | оценка производных |
| `DERIV_EST_CAP_BYTES` | `4294967296` | оценка производных |
| `DERIV_EST_BASE_BYTES` | `134217728` | оценка производных |
| `RENDER_EST_MARGIN_PCT` | `105` | оценка рендера |
| `RENDER_EST_BASE_BYTES` | `67108864` | оценка рендера |
| `EGRESS_PLAN_MULTIPLIER` | `10` | лимит трафика |
| `LOUDNESS_TOLERANCE_LU` | `0.5` | QC |
| `HARD_CUT_TOLERANCE_FRAMES` | `2` | F1 |
| `GRADUAL_IOU_MIN` | `0.3` | F1 |
| `VIDEO_OUTSTANDING_INGEST` / `_HEAVY` / `_LIGHT` / `_PREVIEW` | `50` / `4` / `8` / `4` | допуск заданий, [20](20-saas-scaling-operations.md) 3.1 |
| `VIDEO_DAILY_COST_SECONDS` | `14400` | суточный бюджет стоимости арендатора |
| `VIDEO_SHED_COST_SECONDS` | `21600` | порог перегрузки пула |
| `RETRY_AFTER_MIN_S` / `RETRY_AFTER_MAX_S` / `RETRY_AFTER_STEP_S` | `5` / `900` / `5` | подсказка повтора |
| `VIDEO_MAX_ACTIVE_UPLOADS` | `3` | активных загрузок на арендатора |
| `UPLOAD_BEGINS_PER_HOUR` | `120` | начатых загрузок в час на арендатора |
| `AUTOSAVE_MIN_INTERVAL_MS` / `AUTOSAVE_SERVER_MAX_PER_MIN` | `5000` / `12` | автосохранение, клиент / сервер |
| `PREVIEW_DEBOUNCE_MS` | `400` | пауза перед запросом предпросмотра кадров |
| `JOB_PROGRESS_DB_INTERVAL_MS` / `JOB_PROGRESS_DB_MIN_DELTA_PCT` | `10000` / `1` | запись прогресса в БД |
| `EGRESS_LEASE_MIN_BYTES` / `EGRESS_LEASE_MAX_BYTES` / `EGRESS_LEASE_TTL_SECONDS` | `8388608` / `268435456` / `30` | аренды бюджета трафика |
| `EGRESS_BUCKET_SECONDS` / `EGRESS_COUNTER_RETENTION_DAYS` | `3600` / `400` | агрегация и хранение счётчиков трафика |
| `STREAMS_PER_SESSION` / `STREAMS_PER_SHARE` | `2` / `4` | одновременных потоков портала |
| `STREAM_MAX_BYTES_PER_SECOND` / `STREAM_BURST_BYTES` | `4194304` / `8388608` | скорость потока и всплеск |
| `SOURCE_CACHE_TTL_SECONDS` | `1800` | кэш исходников запуска на узле |
| `MAX_TASK_ATTEMPTS_INFRA` | `3` | попытки при потере воркера |

## SQL (миграции 02651…02665, все forward-only; 02665 — только функции допуска и обслуживания, см. [14](14-function-catalog.md))

Общие правила для всех файлов (скопировать структуру из [`01219_v30_delivery_storage.sql`](../../backend/db/migrations/01219_v30_delivery_storage.sql) и [`02641_user_project_upload_completion.sql`](../../backend/db/migrations/02641_user_project_upload_completion.sql)):

1. Первая строка `-- +goose Up`; последний блок `-- +goose Down` с `DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;` внутри `StatementBegin/End`.
2. Таблицы в схеме `photo`; `id uuid PRIMARY KEY DEFAULT gen_random_uuid()`, `tenant_id uuid NOT NULL REFERENCES photo.tenants(id)`, `revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0)`, `created_at timestamptz NOT NULL DEFAULT now()`.
3. `UNIQUE (tenant_id,id)`; составные `FOREIGN KEY (tenant_id,…) … DEFERRABLE INITIALLY DEFERRED`.
4. В конце каждой таблицы: `ENABLE ROW LEVEL SECURITY`, `FORCE ROW LEVEL SECURITY`, `CREATE POLICY trusted_functions … TO photo_fn_owner USING (true) WITH CHECK (true)`, `REVOKE ALL … FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway`, `GRANT SELECT,INSERT,UPDATE,DELETE … TO photo_fn_owner`.
5. Функции: `LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo`, затем `ALTER FUNCTION … OWNER TO photo_fn_owner; REVOKE ALL … FROM PUBLIC; GRANT EXECUTE … TO <роль>`.
6. Состояния — `text` с `CHECK (state IN (…))` и `CHECK (length(state) BETWEEN 1 AND 256)`.

### DS-02. `photo.video_upload_sessions` (файл `02651_video_upload_sessions.sql`)

```sql
CREATE TABLE photo.video_upload_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  principal_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  media_kind text NOT NULL CHECK (media_kind IN ('video','audio','lut')),
  filename text NOT NULL CHECK (length(filename) BETWEEN 1 AND 255 AND filename !~ '[/\\\x00]'),
  declared_bytes bigint NOT NULL CHECK (declared_bytes BETWEEN 1 AND 17179869184),
  declared_sha256 text NOT NULL CHECK (declared_sha256 ~ '^[0-9a-f]{64}$'),
  request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[0-9a-f]{64}$'),
  part_bytes bigint NOT NULL CHECK (part_bytes >= 33554432),
  parts_total integer NOT NULL CHECK (parts_total BETWEEN 1 AND 10000),
  multipart_id text,
  object_key text NOT NULL CHECK (length(object_key) BETWEEN 1 AND 1024),
  object_version text CHECK (object_version IS NULL OR length(object_version) BETWEEN 1 AND 1024),
  state text NOT NULL CHECK (length(state) BETWEEN 1 AND 256 AND state IN
    ('initializing','uploading','completing','verifying','verified','rejected','aborted','expired')),
  reject_code text CHECK (reject_code IS NULL OR length(reject_code) BETWEEN 1 AND 64),
  reservation_id uuid,
  completion_token uuid,
  completion_lease_until timestamptz,
  expires_at timestamptz NOT NULL,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,principal_id,request_sha256),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK (parts_total = (declared_bytes + part_bytes - 1) / part_bytes),
  CHECK (state <> 'rejected' OR reject_code IS NOT NULL)
);
CREATE INDEX video_upload_sessions_project_page ON photo.video_upload_sessions(tenant_id,project_id,created_at,id);
CREATE INDEX video_upload_sessions_expiry ON photo.video_upload_sessions(expires_at) WHERE state IN ('initializing','uploading','completing');
```

Разрешённые переходы `state` — [04](04-data-model-and-api.md), раздел 3. Часть-метаданные (номера, ETag) хранятся в `photo.video_upload_parts(tenant_id, upload_id, part_no integer CHECK (part_no BETWEEN 1 AND 10000), etag text CHECK (length(etag) BETWEEN 1 AND 256), bytes bigint CHECK (bytes>0), PRIMARY KEY (tenant_id,upload_id,part_no))` в том же файле.

### DS-03. `photo.video_media` (файл `02652_video_media.sql`)

```sql
CREATE TABLE photo.video_media (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  kind text NOT NULL CHECK (kind IN ('video','audio','lut')),
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
  state text NOT NULL CHECK (state IN ('probing','ready','rejected','failed','withdrawing','deleted')),
  reject_code text CHECK (reject_code IS NULL OR length(reject_code) BETWEEN 1 AND 64),
  upload_id uuid NOT NULL,
  ingest_job_id uuid,
  source_artifact_id uuid NOT NULL,
  source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[0-9a-f]{64}$'),
  source_bytes bigint NOT NULL CHECK (source_bytes > 0),
  probe_artifact_id uuid,
  probe jsonb CHECK (probe IS NULL OR (jsonb_typeof(probe)='object' AND pg_column_size(probe) <= 16384)),
  proxy_artifact_id uuid,
  filmstrip_artifact_id uuid,
  waveform_artifact_id uuid,
  withdrawn_at timestamptz,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,source_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,upload_id) REFERENCES photo.video_upload_sessions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK (state <> 'rejected' OR reject_code IS NOT NULL),
  CHECK (state <> 'ready' OR (probe_artifact_id IS NOT NULL AND probe IS NOT NULL AND
        ((kind='video'  AND proxy_artifact_id IS NOT NULL AND filmstrip_artifact_id IS NOT NULL AND waveform_artifact_id IS NOT NULL) OR
         (kind='audio'  AND waveform_artifact_id IS NOT NULL AND proxy_artifact_id IS NULL AND filmstrip_artifact_id IS NULL) OR
         (kind='lut'    AND proxy_artifact_id IS NULL AND filmstrip_artifact_id IS NULL AND waveform_artifact_id IS NULL)))),
  CHECK (state <> 'deleted' OR withdrawn_at IS NOT NULL)
);
CREATE INDEX video_media_project_page ON photo.video_media(tenant_id,project_id,created_at,id) WHERE state <> 'deleted';
```

Для `kind='video'` без аудиодорожки `waveform_artifact_id` всё равно заполняется артефактом-пустышкой «нет звука» (JSON `{"channels":0}`), чтобы CHECK не ветвился.

### DS-04. Монтажи (файл `02653_video_sequences.sql`)

```sql
CREATE TABLE photo.video_sequences (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  fps_num integer NOT NULL CHECK (fps_num > 0),
  fps_den integer NOT NULL CHECK (fps_den > 0),
  width integer NOT NULL CHECK (width BETWEEN 16 AND 4096 AND width % 2 = 0),
  height integer NOT NULL CHECK (height BETWEEN 16 AND 2304 AND height % 2 = 0),
  head_revision bigint NOT NULL DEFAULT 0 CHECK (head_revision >= 0),
  created_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  archived_at timestamptz,
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK ((fps_num,fps_den) IN ((24,1),(25,1),(30,1),(50,1),(60,1),(24000,1001),(30000,1001),(60000,1001)))
);
CREATE TABLE photo.video_sequence_revisions (
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  sequence_id uuid NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  parent_revision bigint CHECK (parent_revision IS NULL OR parent_revision = revision - 1),
  document_artifact_id uuid NOT NULL,
  document_sha256 text NOT NULL CHECK (document_sha256 ~ '^[0-9a-f]{64}$'),
  summary jsonb NOT NULL CHECK (jsonb_typeof(summary)='object' AND pg_column_size(summary) <= 16384),
  label text NOT NULL CHECK (label IN ('autosave','manual','render','cut_application')),
  created_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id,sequence_id,revision),
  FOREIGN KEY (tenant_id,sequence_id) REFERENCES photo.video_sequences(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,document_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE photo.video_sequence_sources (
  tenant_id uuid NOT NULL,
  sequence_id uuid NOT NULL,
  revision bigint NOT NULL,
  source_kind text NOT NULL CHECK (source_kind IN ('video','audio','photo','lut')),
  source_id uuid NOT NULL,
  source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[0-9a-f]{64}$'),
  PRIMARY KEY (tenant_id,sequence_id,revision,source_kind,source_id),
  FOREIGN KEY (tenant_id,sequence_id,revision) REFERENCES photo.video_sequence_revisions(tenant_id,sequence_id,revision) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX video_sequence_sources_usage ON photo.video_sequence_sources(tenant_id,source_kind,source_id);
```

Триггер или `REVOKE UPDATE,DELETE` на `video_sequence_revisions` для всех ролей, кроме функции GC-свёртки `autosave` (TK-108): ревизии иначе неизменяемы.

### DS-05. Задания (файл `02654_video_jobs.sql`)

```sql
CREATE TABLE photo.video_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  kind text NOT NULL CHECK (kind IN ('ingest','detect_cuts','color_analyze','preview_frames','audio_analyze','render')),
  run_id uuid NOT NULL,
  media_id uuid,
  sequence_id uuid,
  sequence_revision bigint,
  params_artifact_id uuid NOT NULL,
  params_sha256 text NOT NULL CHECK (params_sha256 ~ '^[0-9a-f]{64}$'),
  state text NOT NULL CHECK (state IN ('queued','running','succeeded','failed','cancelled','expired')),
  stage text CHECK (stage IS NULL OR length(stage) BETWEEN 1 AND 64),
  progress numeric(5,4) NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 1),
  eta_seconds integer CHECK (eta_seconds IS NULL OR eta_seconds >= 0),
  backend_requested text NOT NULL CHECK (backend_requested IN ('auto','cuda','mlx','cpu')),
  backend_used text CHECK (backend_used IS NULL OR backend_used IN ('cuda','mlx','mps','cpu')),
  reservation_id uuid,
  cost_seconds integer NOT NULL DEFAULT 0 CHECK (cost_seconds >= 0),   -- FN-Q-09, считается один раз при создании
  pool text NOT NULL CHECK (pool IN ('video_light','video_heavy')),     -- учётный пул: audio_analyze, preview_frames → video_light; остальное → video_heavy
  error_code text CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),
  error_detail text CHECK (error_detail IS NULL OR length(error_detail) <= 512),
  created_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz,
  finished_at timestamptz,
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,run_id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,run_id) REFERENCES photo.runs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK (state <> 'failed' OR error_code IS NOT NULL),
  CHECK (kind NOT IN ('detect_cuts','color_analyze','audio_analyze') OR media_id IS NOT NULL),
  CHECK (kind NOT IN ('preview_frames','render') OR (sequence_id IS NOT NULL AND sequence_revision IS NOT NULL)),
  CHECK ((kind IN ('audio_analyze','preview_frames')) = (pool = 'video_light'))
) WITH (fillfactor = 70);   -- обновления прогресса остаются «горячими» (VID-SC-06)
-- допуск (FN-D-17) считает находящиеся в системе задания и стоимость очереди по этим индексам:
CREATE INDEX video_jobs_outstanding ON photo.video_jobs(tenant_id, kind) WHERE state IN ('queued','running');
CREATE INDEX video_jobs_pool_queued ON photo.video_jobs(pool) INCLUDE (cost_seconds) WHERE state = 'queued';
-- запрещены индексы по progress и eta_seconds (VID-SC-06).
CREATE TABLE photo.video_job_outputs (
  tenant_id uuid NOT NULL, job_id uuid NOT NULL,
  role text NOT NULL CHECK (length(role) BETWEEN 1 AND 64),
  ordinal integer NOT NULL CHECK (ordinal BETWEEN 0 AND 127),
  artifact_id uuid NOT NULL,
  PRIMARY KEY (tenant_id,job_id,role,ordinal),
  FOREIGN KEY (tenant_id,job_id) REFERENCES photo.video_jobs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
```

### DS-06. Рендеры (файл `02655_video_renders.sql`)

Колонки — как в [04](04-data-model-and-api.md), раздел 2.5, со следующими точными типами и CHECK:

| Колонка | Тип | CHECK |
|---|---|---|
| `id, tenant_id, project_id, sequence_id, job_id, created_by` | `uuid` | `tenant_id` FK, `UNIQUE(tenant_id,id)` |
| `sequence_revision, revision` | `bigint` | `> 0` |
| `document_sha256, preset_sha256, render_fingerprint` | `text` | `~ '^[0-9a-f]{64}$'` |
| `preset` | `jsonb` | `jsonb_typeof='object'`, размер ≤ 16384 |
| `output_artifact_id, qc_report_artifact_id` | `uuid` | NULL до `ready` |
| `output_bytes` | `bigint` | `> 0` при `ready` |
| `duration_ticks` | `bigint` | `BETWEEN 1 AND 7620480000000` при `ready` |
| `width, height` | `integer` | чётные, `BETWEEN 16 AND 4096 / 2304` |
| `fps_num, fps_den` | `integer` | пара из `SUPPORTED_FPS` |
| `video_codec` | `text` | `IN ('h264')` в V1 |
| `audio_codec` | `text` | `IN ('aac')` в V1 |
| `encoder, backend_used` | `text` | 1…64 символа |
| `state` | `text` | `IN ('queued','rendering','verifying','ready','failed','cancelled','deleted')` |
| `deleted_at` | `timestamptz` | NOT NULL при `deleted` |

`CHECK (state <> 'ready' OR (output_artifact_id IS NOT NULL AND qc_report_artifact_id IS NOT NULL AND output_bytes IS NOT NULL AND duration_ticks IS NOT NULL))`. Дополнительно `UNIQUE (tenant_id,job_id)` — один рендер на задание.

### DS-07. Выдача (файл `02656_video_delivery.sql`)

```sql
ALTER TABLE photo.collections ADD COLUMN content_kind text NOT NULL DEFAULT 'photo'
  CHECK (content_kind IN ('photo','video'));
CREATE TABLE photo.video_collection_items (
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  collection_id uuid NOT NULL,
  render_id uuid NOT NULL,
  ordinal integer NOT NULL CHECK (ordinal BETWEEN 0 AND 999),
  title text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
  published_at timestamptz,
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  PRIMARY KEY (tenant_id,collection_id,render_id),
  UNIQUE (tenant_id,collection_id,ordinal),
  FOREIGN KEY (tenant_id,collection_id) REFERENCES photo.collections(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,render_id) REFERENCES photo.video_renders(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
-- только СКАЧИВАНИЯ (одна строка на завершённое или прерванное скачивание); поток учитывается в DS-28
CREATE TABLE photo.video_egress_events (
  id uuid NOT NULL DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  share_id uuid,
  render_id uuid NOT NULL,
  kind text NOT NULL CHECK (kind = 'download'),
  bytes bigint NOT NULL CHECK (bytes >= 0),
  at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id, at),
  FOREIGN KEY (tenant_id,render_id) REFERENCES photo.video_renders(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
) PARTITION BY RANGE (at);
CREATE TABLE photo.video_egress_events_default PARTITION OF photo.video_egress_events DEFAULT;  -- месячные партиции создаёт FN-D-21
CREATE INDEX video_egress_events_period ON photo.video_egress_events(tenant_id,at);
```

Перед миграцией проверить, что в таблице `photo.collections` нет конфликтующего столбца `content_kind` (`grep -rn content_kind backend/db/migrations`); если есть — остановиться.

### DS-08. Квота (файл `02657_video_quota_roles.sql`)

1. Найти ограничение(я) на `photo.quota_reservations.holder_type` и на `photo.artifacts.output_role` (`grep -n "holder_type\|output_role" backend/db/migrations/*.sql | grep -i check`). Изменить **только** найденные CHECK: добавить значения `video_upload`, `video_job` для `holder_type` и роли `video_original, audio_original, video_probe, video_proxy, video_filmstrip, video_waveform, video_cut_report, video_color_report, video_preview_frame, video_sequence_document, video_render, video_qc_report` для `output_role`.
2. Если ограничения нет (значения свободны) — файл содержит только комментарий и `SELECT 1`, а решение записывается в отчёт задачи.
3. Не менять поведение функций резерва; новые функции видео вызывают существующие внутренние функции резервирования (список — `grep -n "FUNCTION photo.*reserve" backend/db/migrations/026*.sql`).

### DS-28. Учёт трафика потока: счётчики, аренды, бюджеты (файл `02663_video_egress_budget.sql`)

Правила — [20](20-saas-scaling-operations.md), 5.2. Строки создаются только функциями `FN-D-19`/`FN-D-20`/`FN-D-22`.

```sql
CREATE TABLE photo.video_egress_counters (        -- поток, агрегат по часу
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  share_id uuid NOT NULL, render_id uuid NOT NULL,
  kind text NOT NULL CHECK (kind = 'stream'),
  bucket_start timestamptz NOT NULL CHECK (bucket_start = date_trunc('hour', bucket_start)),
  bytes bigint NOT NULL DEFAULT 0 CHECK (bytes >= 0),
  requests bigint NOT NULL DEFAULT 0 CHECK (requests >= 0),
  PRIMARY KEY (tenant_id, share_id, render_id, kind, bucket_start),
  FOREIGN KEY (tenant_id,render_id) REFERENCES photo.video_renders(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE photo.video_egress_period (          -- общий лимит арендатора за месяц UTC
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  period_start date NOT NULL CHECK (period_start = date_trunc('month', period_start)::date),
  consumed_bytes bigint NOT NULL DEFAULT 0 CHECK (consumed_bytes >= 0),
  leased_bytes bigint NOT NULL DEFAULT 0 CHECK (leased_bytes >= 0),
  limit_bytes bigint NOT NULL CHECK (limit_bytes >= 0),
  PRIMARY KEY (tenant_id, period_start),
  CHECK (consumed_bytes + leased_bytes <= limit_bytes)
);
CREATE TABLE photo.video_share_budget (           -- лимит на ссылку; строка есть только если у ссылки задан лимит
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  share_id uuid NOT NULL,
  consumed_bytes bigint NOT NULL DEFAULT 0 CHECK (consumed_bytes >= 0),
  leased_bytes bigint NOT NULL DEFAULT 0 CHECK (leased_bytes >= 0),
  limit_bytes bigint NOT NULL CHECK (limit_bytes >= 0),
  PRIMARY KEY (tenant_id, share_id),
  CHECK (consumed_bytes + leased_bytes <= limit_bytes)
);
CREATE TABLE photo.video_egress_leases (
  id uuid NOT NULL DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  share_id uuid NOT NULL,
  period_start date NOT NULL,
  granted_bytes bigint NOT NULL CHECK (granted_bytes > 0),
  expires_at timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, id)
);
CREATE INDEX video_egress_leases_expiry ON photo.video_egress_leases(expires_at);
```
Инвариант: `consumed + leased ≤ limit` в обеих таблицах бюджета (нарушение невозможно по `CHECK`). `limit_bytes` общей строки берётся при первом обращении периода из существующего расчёта лимита трафика (`EGRESS_PLAN_MULTIPLIER` × включённый диск; функцию определяет TK-001, допущение A-5). Просроченная аренда (`expires_at < now()`) переводится согласователем в `consumed` (консервативно).

### DS-29. Лимиты и суточный расход арендатора (файл `02664_video_tenant_limits.sql`)

```sql
CREATE TABLE photo.video_tenant_limits (          -- строки только для переопределений; нет строки = значения DS-01
  tenant_id uuid PRIMARY KEY REFERENCES photo.tenants(id),
  max_outstanding_ingest integer CHECK (max_outstanding_ingest BETWEEN 0 AND 1000),
  max_outstanding_heavy  integer CHECK (max_outstanding_heavy  BETWEEN 0 AND 1000),
  max_outstanding_light  integer CHECK (max_outstanding_light  BETWEEN 0 AND 1000),
  max_outstanding_preview integer CHECK (max_outstanding_preview BETWEEN 0 AND 1000),
  daily_cost_seconds bigint CHECK (daily_cost_seconds IS NULL OR daily_cost_seconds >= 0),
  daily_cost_unlimited boolean NOT NULL DEFAULT false,   -- true => без суточного ограничения (NULL в daily_cost_seconds означает «значение по умолчанию»)
  max_active_uploads integer CHECK (max_active_uploads BETWEEN 0 AND 100),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE photo.video_tenant_usage_daily (
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  day date NOT NULL,                               -- UTC
  cost_seconds bigint NOT NULL DEFAULT 0 CHECK (cost_seconds >= 0),
  jobs_admitted integer NOT NULL DEFAULT 0 CHECK (jobs_admitted >= 0),
  PRIMARY KEY (tenant_id, day)
);
CREATE TABLE photo.video_tenant_rate (            -- окна для UPLOAD_BEGIN_RATE: один ряд на (арендатор, час)
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  hour_start timestamptz NOT NULL CHECK (hour_start = date_trunc('hour', hour_start)),
  upload_begins integer NOT NULL DEFAULT 0 CHECK (upload_begins >= 0),
  PRIMARY KEY (tenant_id, hour_start)
);
```
Для обеих таблиц и для таблиц DS-28 действуют общие правила SQL (RLS, `REVOKE`, `photo_fn_owner`). Строки `video_tenant_usage_daily` и `video_tenant_rate` старше 90 суток удаляет `FN-D-21`.

## TypeScript (пакет `packages/api-client/src/video-timeline.ts` и соседние)

### DS-09. Документ монтажа

```ts
export const TICKS_PER_SECOND = 705_600_000 as const;
export type Ticks = number;                    // целое, ≥ 0, ≤ Number.MAX_SAFE_INTEGER
export type Rational = { num: number; den: number }; // целые > 0
export type SourceRef =
  | { kind: 'video' | 'audio' | 'lut'; media_id: string; sha256: string }
  | { kind: 'photo'; asset_version_id: string; sha256: string };
export type EffectType = 'color.basic' | 'color.cdl' | 'color.curves' | 'color.lut3d' | 'color.match'
  | 'audio.gain' | 'audio.fade' | 'audio.duck';
export type Effect = { id: string; type: EffectType; enabled: boolean; params: Record<string, unknown>; meta?: Record<string, unknown> };
export type TransitionKind = 'cut' | 'crossfade' | 'dip_to_black' | 'fade_in' | 'fade_out';
export type Transition = { kind: TransitionKind; duration: Ticks; align?: 'center' | 'start' | 'end' };
export type ClipAudio = { gain_db: number; fade_in: Ticks; fade_out: Ticks; mute: boolean };
export type Transform = { scale: number; position: [number, number]; rotate: number; crop: [number, number, number, number] };
export type Motion = { kind: 'pan_zoom'; from: { scale: number; center: [number, number] }; to: { scale: number; center: [number, number] }; easing: 'linear' | 'ease_in_out' };
export type Clip = {
  id: string; source: SourceRef; at: Ticks;
  src_in?: Ticks; src_out?: Ticks;               // видео и аудио
  duration?: Ticks;                               // только фото
  speed?: Rational; fit?: 'fit' | 'fill' | 'none';
  transform?: Transform; motion?: Motion; audio?: ClipAudio;
  transition_in?: Transition; effects: Effect[];
};
export type Track = { id: string; kind: 'video' | 'audio'; locked?: boolean; visible?: boolean; muted?: boolean; gain_db?: number; clips: Clip[] };
export type Marker = { id: string; at: Ticks; kind: 'cut' | 'beat' | 'user'; label: string };
export type TimelineDocument = {
  schema_version: 1; sequence_id: string; base_revision: number;
  timebase: { ticks_per_second: 705600000 };
  video: { fps: Rational; width: number; height: number; color: { space: 'rec709'; range: 'limited' }; background: string };
  audio: { sample_rate: 48000 | 44100; channels: 2 };
  tracks: Track[]; markers: Marker[];
  master: { effects: Effect[]; audio: { loudness: { target_lufs: number; true_peak_db: number } } };
  rights: { music_declared: boolean; declared_at?: string };
};
```

Правила: идентификаторы `^[A-Za-z0-9_-]{1,64}$`; JSON-числа времени — целые; ключи вне типа запрещены (валидатор отвергает).

### DS-10. Проверка и команды

```ts
export type ValidationIssue = { rule: 'VT-01'|'VT-02'|'VT-03'|'VT-04'|'VT-05'|'VT-06'|'VT-07'|'VT-08'|'VT-09'|'VT-10';
  pointer: string;   // RFC 6901 JSON Pointer, например "/tracks/0/clips/2/src_out"
  code: string; };   // код из DS-24 (VIDEO_SEQUENCE_INVALID — общий) или уточнённый из таблицы VT ниже
export type ValidateContext = { sourceDurations: Record<string, Ticks>; sourceStates: Record<string, 'ready'|'other'>; sourceHashes: Record<string, string>; musicClipIds: string[]; forRender: boolean };
export type EditError = 'CLIP_NOT_FOUND'|'TRACK_NOT_FOUND'|'SPLIT_OUT_OF_RANGE'|'SPLIT_NOT_ALIGNED'|'SPLIT_PHOTO_UNSUPPORTED'
  |'TRIM_NOT_ALIGNED'|'TRIM_OVERLAP'|'TRIM_EMPTY'|'TRIM_BEFORE_SOURCE'|'TRIM_BEYOND_SOURCE'
  |'MOVE_NOT_ALIGNED'|'MOVE_OVERLAP'|'MOVE_TRACK_KIND'|'SPEED_RANGE'|'SPEED_OVERLAP'|'SPEED_TOO_SHORT'|'SPEED_PHOTO';
export type EditResult = { ok: true; doc: TimelineDocument; applied?: Array<{ boundary: string; clip: string; new: string; at: Ticks }> } | { ok: false; error: EditError };
export type Command = { name: string; apply(doc: TimelineDocument): EditResult; invert(before: TimelineDocument): Command };
```

Коды правил валидатора (поле `code`): `VT-01`→`DOC_SCHEMA`; `VT-02`→`DOC_LIMIT`; `VT-03`→`DOC_ID_DUPLICATE`; `VT-04`→`CLIP_OVERLAP`; `VT-05`→`CLIP_RANGE`; `VT-06`→`SOURCE_NOT_READY` (или `SOURCE_HASH_MISMATCH`); `VT-07`→`EFFECT_PARAM`; `VT-08`→`TRANSITION_INVALID`; `VT-09`→`SIDECHAIN_INVALID`; `VT-10`→`RIGHTS_REQUIRED`.

Ошибки команд FN-T-15…28 и диапазоны параметров эффектов и свойств клипа (дополнение VT-07) — [23](23-editor-playback-timeline.md), раздел 11; тип `EditError` расширяется этими кодами.

### DS-11. Загрузка (клиент)

```ts
export type VideoUploadState = 'initializing'|'uploading'|'completing'|'verifying'|'verified'|'rejected'|'aborted'|'expired';
export type VideoUploadBegin = { filename: string; media_kind: 'video'|'audio'|'lut'; bytes: number; sha256: string; request_sha256: string };
export type VideoUploadSession = { upload_id: string; tenant_id: string; project_id: string; part_bytes: number; parts_total: number; state: VideoUploadState; expires_at: string; revision: string };
export type VideoUploadPartUrl = { part_no: number; url: string; expires_at: string };
export type VideoUploadCompletePart = { part_no: number; etag: string };
export type VideoUploadCheckpoint = { upload_id: string; file_fingerprint: string; completed: VideoUploadCompletePart[]; updated_at: string };
```

`file_fingerprint` = `sha256(имя + "\n" + размер + "\n" + lastModified)` (для сопоставления при возобновлении).

### DS-12. Отчёт детекции границ (клиент)

```ts
export type BoundaryKind = 'cut' | 'dissolve' | 'fade_in' | 'fade_out' | 'flash';
export type Boundary = { id: string; kind: BoundaryKind; frame_in: number; frame_out: number; start_ticks: Ticks; end_ticks: Ticks; confidence: number };
export type CutReport = { schema_version: 1; media_id: string; source_sha256: string; fps: Rational; frame_count: number;
  method: 'model' | 'classic'; model: { name: string; weights_sha256: string } | null;
  backend_used: 'cuda'|'mlx'|'mps'|'cpu'; params: { mode: 'all'|'hard_only'; sensitivity: 1|2|3|4|5; min_shot_frames: number; include_flash: boolean };
  boundaries: Boundary[] };
```

Инварианты: `boundaries` отсортированы по `frame_in`, `id` = `b1…bn` подряд, `confidence ∈ [0;1]` с 4 знаками, `start_ticks = frame_in × frame_ticks`, `end_ticks = frame_out × frame_ticks`.

## Python (`workers/photo_worker/contracts/video.py`; база — `Contract` из [`contracts/base.py`](../../workers/photo_worker/contracts/base.py): `extra="forbid", strict=True`)

### DS-13. Контракты параметров и результатов операций

```python
from typing import Literal
from .base import Contract, SHA256
from pydantic import Field
from uuid import UUID

Backend = Literal["cuda", "mlx", "mps", "cpu"]
class Rational(Contract): num: int = Field(gt=0); den: int = Field(gt=0)

class ProbeStream(Contract):
    index: int = Field(ge=0); kind: Literal["video","audio","subtitle","data"]
    codec: str = Field(min_length=1, max_length=32)
    width: int | None = None; height: int | None = None
    fps: Rational | None = None; vfr: bool = False; rotation: int = 0
    pix_fmt: str | None = None; color_space: str | None = None; color_transfer: str | None = None
    channels: int | None = None; sample_rate: int | None = None; bit_rate: int | None = None
class ProbeSummary(Contract):
    schema_version: Literal[1]; container: str; duration_ticks: int = Field(ge=1)
    streams: list[ProbeStream] = Field(min_length=1, max_length=16)
    sha256: SHA256; bytes: int = Field(ge=1); hdr: bool = False
class ProbeResult(Contract):
    verdict: Literal["ready","rejected"]; reject_code: str | None; summary: ProbeSummary | None

class DetectCutsParams(Contract):
    schema_version: Literal[1]; media_id: UUID; source_sha256: SHA256
    mode: Literal["all","hard_only"]; sensitivity: int = Field(ge=1, le=5)
    min_shot_frames: int = Field(ge=1, le=240, default=8); include_flash: bool = False
    backend: Literal["auto","cuda","mlx","cpu"]
class PreviewFramesParams(Contract):
    schema_version: Literal[1]; sequence_id: UUID; revision: int = Field(ge=1)
    document_sha256: SHA256; moments: list[int] = Field(min_length=1, max_length=16)  # тики
class RenderParams(Contract):
    schema_version: Literal[1]; sequence_id: UUID; revision: int = Field(ge=1)
    document_sha256: SHA256; preset_id: Literal["h264_720p","h264_1080p","h264_1440p","h264_2160p"]
    max_output_bytes: int = Field(ge=1); backend: Literal["auto","cuda","mlx","cpu"]
```

Остальные операции (`video.proxy`, `video.thumbs`, `audio.waveform`, `video.color_analyze`, `audio.analyze`) имеют параметры `{schema_version:1, media_id, source_sha256}` (+ `range:[start_ticks,end_ticks]` для цветоанализа).

Дополнение к DS-13 (найдено ревью: в каталоге не было операции проверки LUT):

```python
class LutValidateParams(Contract): media_id: UUID; sha256: SHA256; bytes: int = Field(gt=0, le=20971520)
class LutValidateResult(Contract):
    schema_version: Literal[1]; kind: Literal["lut"]
    size: int = Field(ge=17, le=65); title: str | None = Field(default=None, max_length=256)
    domain_min: tuple[float, float, float]; domain_max: tuple[float, float, float]
    sha256: SHA256
```
На ошибку разбора операция завершается ошибкой `VIDEO_UNSUPPORTED_FORMAT` (DS-24), результат не создаётся.

### DS-14. Пресеты рендера

| `preset_id` | w×h | video bitrate | maxrate | bufsize | audio |
|---|---|---|---|---|---|
| `h264_720p` | 1280×720 | 5 000 000 | 6 000 000 | 12 000 000 | AAC 192 000, 48 кГц, стерео |
| `h264_1080p` | 1920×1080 | 10 000 000 | 12 000 000 | 24 000 000 | то же |
| `h264_1440p` | 2560×1440 | 18 000 000 | 21 600 000 | 43 200 000 | то же |
| `h264_2160p` | 3840×2160 | 40 000 000 | 48 000 000 | 96 000 000 | то же |

Значения идентичны `quota.json → presets` (порядок: w, h, bitrate, maxrate, bufsize). Контейнер MP4, `-movflags +faststart`, `-pix_fmt yuv420p`, `-map_metadata -1`.

### DS-15. План рендера

```python
class PlanInput(Contract): index: int; media_id: UUID | None; asset_version_id: UUID | None; sha256: SHA256; kind: Literal["video","audio","photo","lut"]
class RenderPlan(Contract):
    schema_version: Literal[1]; document_sha256: SHA256; preset_id: str; preset_sha256: SHA256
    inputs: list[PlanInput]; filter_complex: str; maps: list[str]; codec_args: list[str]
    decisions: list[dict]      # неявные решения компилятора (VID-T-07): {"code": str, "clip": str, "detail": str}
    tool_versions: dict[str, str]
# render_fingerprint = canonical_hash({document_sha256, preset_sha256, compiler_version, tool_versions, encoder, model_lock})
```

### DS-16. Генерация LUT

```python
class Lut3D(Contract):
    size: int = Field(ge=17, le=65); domain_min: tuple[float,float,float] = (0.0,0.0,0.0)
    domain_max: tuple[float,float,float] = (1.0,1.0,1.0)
    # data: ndarray float32 формы (size,size,size,3); порядок индекса: r + N*g + N*N*b (r меняется быстрее всего)
```

Файл `.cube` пишется построчно в этом порядке; формула индекса — в `vectors/color.json → lut.index_formula`.

### DS-17. Результат QC

```python
class QcReport(Contract):
    schema_version: Literal[1]; duration_ticks: int; expected_duration_ticks: int; frame_ticks: int
    duration_ok: bool; streams_ok: bool; decode_ok: bool; size_ok: bool
    loudness_lufs: float | None; loudness_ok: bool | None; sha256: SHA256; bytes: int
    verdict: Literal["pass","fail"]; failures: list[str]
```

## Go (`backend/internal/video/**`)

### DS-18. Основные структуры

```go
package video

type UploadState string // 'initializing' … 'expired', значения как в DS-02
type MediaKind string   // "video" | "audio" | "lut"
type MediaState string
type JobKind string
type JobState string
type RenderState string

type Scope struct{ TenantID, ProjectID uuid.UUID }

type UploadBeginRequest struct {
    Filename string `json:"filename"`
    MediaKind MediaKind `json:"media_kind"`
    Bytes int64 `json:"bytes"`
    SHA256 string `json:"sha256"`
    RequestSHA256 string `json:"request_sha256"`
}
type UploadPlan struct{ PartBytes int64; PartsTotal int }
type Admission struct{ Used, Reserved, Limit, Request int64 }

type SequenceSaveRequest struct {
    ExpectedRevision int64 `json:"expected_revision"`
    Document json.RawMessage `json:"document"`
    Label string `json:"label"`
}
type APIError struct{ Code string `json:"code"`; Message string `json:"message,omitempty"`; Detail map[string]any `json:"detail,omitempty"` }
```

Пакеты: `video/upload` (план, подпись частей), `video/media`, `video/sequence`, `video/job`, `video/quota`, `video/share`, `video/timeline` (каноническая форма + валидатор Go, DS-09).

## JSON по проводу

### DS-19. Тела запросов и ответов (основные)

| Эндпоинт | Запрос | Ответ (успех) |
|---|---|---|
| `POST …/video-uploads` | `UploadBeginRequest` | `{upload_id, part_bytes, parts_total, state, expires_at, revision}` |
| `POST …/video-uploads/{id}/parts` | `{part_numbers:[int]}` (1…100 за раз) | `{urls:[{part_no,url,expires_at}]}` |
| `POST …/video-uploads/{id}/complete` | `{parts:[{part_no,etag}]}` | `{state:'verifying', media_id}` |
| `PUT …/video-sequences/{id}` | `{expected_revision,document,label}` | `{revision, document_sha256, summary}`; `409` → `{code:'VIDEO_SEQUENCE_REVISION_CONFLICT', detail:{head_revision}}` |
| `POST …/cut-detections` | `{mode,sensitivity,backend,min_shot_frames?,include_flash?}` | `{job_id}` |
| `POST …/cut-applications` | `{expected_revision,job_id,accepted:[string],mode:'split_at_start' \| 'split_at_middle' \| 'markers_only'}` | `{revision, applied:[…]}` |
| `POST …/renders` | `{revision,preset_id,name,rights_declaration:{music_declared:boolean}}` | `{render_id,job_id}` |
| `POST …/video-deliveries` | `{render_ids:[uuid],name,expires_at,secret?,allow_download,egress_limit_bytes?}` | `{delivery_id, collection_id}` |

Все ответы-списки: `{items:[…], next_cursor: string|null}`; `limit` 1…100.

### DS-20. Сводка `probe` (jsonb в `video_media.probe`, ≤ 16 КиБ)

`ProbeSummary` из DS-13 без списка потоков свыше 16 (остальное — в артефакте `video_probe`). Дополнительные ключи: `warnings: string[]` из набора `VFR`, `HDR`, `ROTATED`, `ODD_FPS`, `NO_AUDIO`, `MULTI_AUDIO`.

### DS-21. Ответ capabilities

```json
{ "enabled": true, "billing_mode": "beta_free",
  "limits": { "max_upload_bytes": 17179869184, "max_duration_ticks": 7620480000000, "max_width": 4096, "max_height": 2304 },
  "formats": { "video": ["mp4","mov","mkv","webm"], "audio": ["mp3","wav","flac","m4a","aac"], "lut": ["cube"] },
  "operations": [ { "name": "video.detect_cuts", "backends": ["cuda","mlx","cpu"], "available": true, "reason": null } ],
  "presets": ["h264_720p","h264_1080p","h264_1440p","h264_2160p"] }
```

### DS-22. Список допустимых расширений и контейнеров

Принимаемые контейнеры V1 — по результату probe, а не расширению: `mov,mp4,m4a,matroska,webm` (формат ffprobe `format_name`). Плейлисты, `concat`, `.m3u8`, `file:` ссылки отклоняются (`VIDEO_UNSUPPORTED_FORMAT`).

### DS-23. Манифест замыкания источников рендера

```json
{ "schema_version": 1, "document_sha256": "…",
  "sources": [ { "kind": "video", "media_id": "…", "sha256": "…", "artifact_id": "…", "bytes": 123 } ] }
```
Хранится артефактом (роль `video_render_manifest` в ДОПОЛНЕНИЕ к списку DS-08 — **добавить в миграцию**, если CHECK на `output_role` существует).

### DS-30. Тела ошибок масштабирования и расширение capabilities

Ошибки возвращаются как `APIError` (DS-18); дополнительные поля лежат в `detail`:

```json
{ "code": "VIDEO_TENANT_LIMIT", "detail": { "limit_code": "OUTSTANDING_HEAVY", "limit": 4, "current": 4, "retry_after_s": null } }
{ "code": "VIDEO_TENANT_LIMIT", "detail": { "limit_code": "AUTOSAVE_RATE", "limit": 12, "current": 12, "retry_after_s": 20 } }
{ "code": "VIDEO_CAPACITY_BUSY", "detail": { "retry_after_s": 120 } }
{ "code": "VIDEO_STREAM_LIMIT", "detail": { "retry_after_s": 5 } }
```
Для `VIDEO_TENANT_LIMIT`, где повтор возможен по времени (`DAILY_COST` — до 00:00 UTC, `UPLOAD_BEGIN_RATE` — до начала следующего часа UTC, `AUTOSAVE_RATE` — 1…60), `retry_after_s` — целое ≥ 1 и совпадает с заголовком `Retry-After`; для остальных `limit_code` — `null`. `VIDEO_CAPACITY_BUSY`: `retry_after_s` из `FN-Q-10`, заголовок `Retry-After` — то же число.

Расширение DS-21 (`capabilities`), все поля обязательны:

```json
{ "limits": { "outstanding": { "ingest": 50, "heavy": 4, "light": 8, "preview": 4 },
              "daily_cost_seconds": 14400, "daily_used_cost_seconds": 0, "max_active_uploads": 3 },
  "stream_mode": "proxy",
  "detect_cuts_methods": ["classic"],
  "queue_hint": { "pool": "video_heavy", "busy": false, "retry_after_s": null } }
```
`daily_cost_seconds` = `null` при отсутствии ограничения. `queue_hint.busy = true` тогда и только тогда, когда `poolAheadCost > VIDEO_SHED_COST_SECONDS`; тогда `retry_after_s` — целое.
`detect_cuts_methods` — непустой массив: всегда содержит `classic`, содержит `model` только при утверждённой записи замка ([22](22-tools-models-licenses.md), 7.1). Значение `limit_code = PLAYBACK_LINK_RATE` (`VIDEO_TENANT_LIMIT`) — превышение `PLAYBACK_LINK_PER_MIN` запросов ссылок воспроизведения в минуту на пользователя, `retry_after_s` — до начала следующей минуты.

### DS-31. Ссылки воспроизведения ([23](23-editor-playback-timeline.md), раздел 15)

```ts
export type PlaybackLinkRequest = { variant: 'proxy' | 'original_audio' };
export type PlaybackLink = { url: string; expires_at: string /* RFC 3339, ≤ 60 с от выдачи */; mime: string; codec: string };
```
```go
type PlaybackLinkRequest struct { Variant string `json:"variant"` } // "proxy" | "original_audio"
type PlaybackLinkResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	MIME      string    `json:"mime"`
	Codec     string    `json:"codec"`
}
```
Клиент отвергает ответ: не `https` (кроме `http` для `localhost`, `[::1]`, `127.x.x.x`), учётные данные или фрагмент в URL, `expires_at` в прошлом, `url` длиннее 8192 символов, лишние ключи — по образцу [`editor-source-preview-link.ts`](../../apps/photographer/src/editor-source-preview-link.ts).

## DS-24. Коды ошибок

| Код | HTTP | Кто выдаёт | Повторяемо клиентом |
|---|---|---|---|
| `VIDEO_UNSUPPORTED_FORMAT` | 422 | probe → media | нет |
| `VIDEO_CODEC_UNSUPPORTED` | 422 | probe | нет |
| `VIDEO_CORRUPT` | 422 | probe/декодер | нет |
| `VIDEO_TOO_LARGE` | 413 | begin, probe | нет |
| `VIDEO_TOO_LONG` | 422 | probe | нет |
| `VIDEO_RESOLUTION_UNSUPPORTED` | 422 | probe | нет |
| `VIDEO_QUOTA_EXCEEDED` | 402 | begin, job-create | нет (после очистки — да) |
| `VIDEO_BACKEND_UNAVAILABLE` | 503 | job-create | да, позже |
| `VIDEO_SOURCE_MISSING` | 409 | render, validator | нет |
| `VIDEO_SOURCE_NOT_READY` | 409 | save, render | да, позже |
| `VIDEO_SEQUENCE_INVALID` | 422 | save | нет, исправить документ |
| `VIDEO_SEQUENCE_REVISION_CONFLICT` | 409 | save | после перезагрузки |
| `VIDEO_RIGHTS_DECLARATION_REQUIRED` | 422 | render | после декларации |
| `VIDEO_RENDER_PRESET_INVALID` | 422 | render | нет |
| `VIDEO_JOB_NOT_CANCELLABLE` | 409 | cancel | нет |
| `VIDEO_JOB_STALE` | 409 | cut-applications, preview | нет |
| `VIDEO_SHARE_REVOKED` | 410 | портал | нет |
| `VIDEO_SHARE_EXPIRED` | 410 | портал | нет |
| `VIDEO_EGRESS_LIMIT` | 429 | портал | позже |
| `UPLOAD_EXPIRED` | 410 | upload | перезапуск |
| `UPLOAD_REJECTED` | 422 | upload | нет |
| `UPLOAD_HASH_MISMATCH` | 422 | ingest | нет |
| `VIDEO_UPLOAD_RECONCILIATION_REQUIRED` | 409 | begin | через attempt-status |
| `VIDEO_TENANT_LIMIT` | 429 | допуск, begin, save (DS-30) | по `limit_code`: после завершения заданий / через `retry_after_s` |
| `VIDEO_CAPACITY_BUSY` | 503 | допуск (DS-30) | да, через `retry_after_s` |
| `VIDEO_STREAM_LIMIT` | 429 | портал | да, через 5 с |
| `VIDEO_METHOD_UNAVAILABLE` | 422 | job-create (`detect_cuts`, `method = model` без утверждённой записи замка) | нет; выбрать `classic` |
| `VIDEO_PLAYBACK_UNSUPPORTED` | 409 | playback-links (вид медиа или кодек не воспроизводится в браузере) | нет |
| `OUTPUT_EXCEEDS_RESERVATION` | — (код задания) | воркер | нет |
| `VIDEO_JOB_ABANDONED` | — (код задания) | dead letter | вручную («повторить») |
| `IDEMPOTENCY_KEY_REUSED` | 409 | общий | нет |

Тело ошибки — `APIError` (DS-18). Тексты для пользователя берутся из локализации по `code` (DS-27).

## DS-25. События SSE

| Событие | Полезная нагрузка (все поля обязательны) |
|---|---|
| `video.media.state` | `{media_id, state, project_id}` |
| `video.job.progress` | `{job_id, kind, progress (0…1, 4 знака), stage, eta_seconds или null}` |
| `video.job.state` | `{job_id, kind, state, error_code или null}` |
| `video.render.state` | `{render_id, state}` |
| `video.delivery.state` | `{delivery_id, state}` |
| `video.quota.changed` | `{used, reserved, limit}` |

Запрещены: имена файлов, ключи объектов, URL.

## DS-26. Журналы попыток (клиент, `AsyncStorage`)

Образец — любой `*-attempt-store.ts` в [`apps/photographer/src`](../../apps/photographer/src). Запись:

```ts
export type AttemptRecord<TIntent> = { key: string /* Idempotency-Key, uuid v4 */; intent_sha256: string; intent: TIntent;
  state: 'pending'|'ambiguous'|'done'|'failed'; created_at: string; updated_at: string; result_ref?: string };
```
Секрет ссылки выдачи и подписанные URL в журнал **не пишутся** (VID-UI-09).

## DS-27. Ключи локализации

Формат `video_<экран>_<элемент>` (в нижнем регистре, `_`), для ошибок — `video_error_<code в нижнем регистре>`; все ключи есть в `locales/en/common.json`, `ru`, `bg`. Пример: `video_upload_queue_title`, `video_error_video_quota_exceeded`. Для масштабирования обязательны: `video_error_video_tenant_limit_<limit_code в нижнем регистре>` (по одному на каждое значение `limit_code`), `video_error_video_capacity_busy` (с подстановкой секунд), `video_error_video_stream_limit`, `video_error_output_exceeds_reservation`, `video_error_video_job_abandoned`, `video_notice_retry_in_seconds`.
