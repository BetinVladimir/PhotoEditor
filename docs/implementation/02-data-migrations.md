# 02. PostgreSQL, структуры и миграции

## Каноническая модель

Полный список таблиц/полей/ограничений: `contracts/tables.json`. SQL генерируется в `backend/db/migrations`, запросы sqlc — в `backend/db/queries`. ID — UUIDv4 из CSPRNG приложения; даты timestamptz UTC, bytes/cursors/revisions — bigint. API представляет bigint строкой decimal во избежание JS precision loss. SQL enum не использовать: text+CHECK позволяют expand/contract. JSONB всегда проверяется соответствующей схемой перед записью; схемы хранятся в `packages/contracts`.

Все tenant-таблицы имеют NOT NULL tenant_id, FK tenants(id), PK id и UNIQUE(tenant_id,id). Все ссылки на tenant сущности — FOREIGN KEY(tenant_id,x_id) REFERENCES target(tenant_id,id). Global users доступны по обычному user_id FK. Все *_by поля — users кроме типизированного principal. `ON DELETE RESTRICT` — default; каскад только для технических дочерних строк удаляемого aggregate после проверки references. Soft delete коллекции/asset не удаляет bytes.

Особые составные ограничения:

* asset_versions: UNIQUE(tenant_id,asset_id,id); item `(tenant_id,asset_id,asset_version_id)` ссылается на него.
* sections: UNIQUE(tenant_id,collection_id,id); item section FK включает collection_id.
* items: UNIQUE(tenant_id,id,asset_version_id); publication FK на этот tuple.
* share_links: UNIQUE(tenant_id,id,collection_id); share_session FK включает collection_id.
* tasks: UNIQUE(tenant_id,run_id,id); оба edge FK включают run_id.
* source_item_id/item_id в snapshots — исторические идентификаторы без FK живой membership: удаление membership не разрушает snapshot; version/artifact FKs сохраняются.
* styles/training/runs/cyclic references добавляются после создания всех таблиц. `collections.source_run_id ↔ runs.output_collection_id` DEFERRABLE INITIALLY DEFERRED и UNIQUE output_collection_id.
* `artifact_leases.holder_id` — полиморфный holder; service проверяет существование, cleanup имеет явный switch по holder_type. Не выдумывать FK на произвольный UUID.

`id` у технических таблиц сохраняется даже при дополнительном естественном UNIQUE. Малые JSON default `{}`/`[]` явно кастуются в jsonb. Размер style до 100,000,000 байт JCS; HTTP обычный JSON до 1 MiB, большие style imports через ingest artifact. MAX_PIXELS применяется до decode. Все счётчики CHECK >=0.

## RLS и SQL API

Миграция создаёт роли `photo_migrator` (DDL), `photo_api`, `photo_auth`, `photo_portal`, `photo_worker`, `photo_scheduler`, `photo_publisher`, `photo_maintenance`, `photo_fn_owner` (NOLOGIN). Runtime роли не владельцы и без BYPASSRLS. PUBLIC лишается CREATE на application schema и EXECUTE на privileged functions.

Для photo_api базовая tenant policy: `tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid`, USING и WITH CHECK. Вне tenant tx нет доступа. Middleware получает user через auth role, проверяет активную membership, затем `set_config('app.tenant_id',id,true)` в pgx transaction. Проектный ACL остаётся отдельной обязательной проверкой сервисов. RLS — defence-in-depth против межтенантной утечки, не замена ACL.

Гость не использует эту широкую policy. `photo_portal` получает EXECUTE только функций portal session lookup, item manifest, uploads и download jobs; каждая заново проверяет session, share version, collection status, publication и principal ownership. Guest principal из verified cookie, не из body. Внутри definer-функций `search_path=pg_catalog,photo`, таблицы fully-qualified, динамический SQL запрещён. `photo_fn_owner` имеет явную role-targeted RLS policy для необходимых таблиц; он NOLOGIN и не выдаётся runtime через SET ROLE. Это узкий, аудируемый доверенный путь; FORCE RLS не должен случайно обнулить его доступ. Права EXECUTE перечислены для каждой функции. Миграция `00194` исправляет исторический широкий `DELETE` grant: сначала отзывает `DELETE ON ALL TABLES`, затем выдаёт `photo_fn_owner` право удаления только на закрытый allowlist tenant-scoped таблиц, совпадающий с `finish_tenant_deletion`; `audit_log` входит явно, поскольку функция удаляет его не-lifecycle записи; `deletion_jobs`, `memberships` и `tenants` в DELETE-allowlist не входят.

Worker functions сверяют worker identity (DB role/session registration), task capability, lease и fencing; аргумент tenant только дополнительная проверка, task row — источник tenant. `claim_task`, `heartbeat_task`, `commit_task_success`, `commit_task_failure`, `read_task_context` не дают произвольный SELECT/UPDATE. Scheduler/publisher используют role-targeted функции на перечисленных служебных таблицах; отсутствует общий SQL обход RLS.

## Методы repository и транзакции

| Метод | Контракт |
|---|---|
| `WithTenantTx(ctx, principal, tenantID, fn) error` | membership вне pool-context; Begin; SET LOCAL; fn; commit/rollback; panic rollback |
| `LockCollection(tx,id,expectedRevision) Collection` | FOR UPDATE, scope/project/status; mismatch REVISION_CONFLICT |
| `CreateSnapshot(tx,SelectionSpec) SnapshotRef` | stable ordering position,id; snapshot rows и digest; artifact leases; source revision locked |
| `ReserveQuota(tx,ReserveRequest) Reservation` | locks tenant_usage → share_link; used+reserved+request<=limit, иначе QUOTA_EXCEEDED |
| `ConsumeReservation(tx,id,actualBytes,actualFiles) error` | CAS reserved→consumed, delta accounting exactly once; превышение ожидаемого — reject verification |
| `ReleaseReservation(tx,id,reason) error` | CAS reserved→released; повтор no-op; не вычитает consumed |
| `AppendEvent(tx,Event) Cursor` | lock tenant_event_cursors, increment, insert run_events; cursor order совпадает commit order |
| `PutArtifact(tx,VerifiedObject) ArtifactRef` | серверный key, digest/HEAD проверены до tx; canonical только fenced commit |
| `CommitOutputItem(tx,run,inputOrdinal,version) Item` | UNIQUE node result; новый asset/version; output collection revision++; retry no duplicate |
| `ResolveIdempotency(tx,scope,key,hash) CachedOrNew` | row lock, одинаковый hash возвращает сохранённый ответ; другой hash 409 |
| `TransferOwner(tx,from,to,revision) error` | lock tenant+memberships; active target; change memberships+tenant.owner одним deferred invariant |

Артефакты immutable по bytes/key/hash/profile. Триггеры запрещают UPDATE content у version/style_version/snapshot/result; административные status/expiry допускаются whitelist. Item нельзя перепривязать к другой version. Изменение рейтинга/секции не трогает bytes. `collections.revision` повышается любой видимой metadata/membership/publication мутацией ровно один раз на транзакцию.

## Расписание goose

| № / файл | Что создаётся | Предусловия, проверка и rollback |
|---|---|---|
| 00001_global_auth.sql | users/challenges/sessions/mail/rate limits | empty DB; OTP reuse/rotation; Down только dev при пустых данных |
| 00002_tenancy.sql | tenants/memberships/invitations/usage/audit, roles | owner invariant и двухтенантный отрицательный тест |
| 00003_catalog.sql | projects/ACL/artifacts/assets/versions/collections/items/publications | immutable triggers, composite FK; wrong tenant insert fails |
| 00004_ingest_sharing.sql | shares/sessions/uploads/parts/reservations | oversubscription race; quota release idempotent |
| 00005_styles_snapshots.sql | styles/versions/pairs/snapshots/assets bindings | JSON constraints, ordered digest, version immutability |
| 00006_workflow.sql | runs/tasks/attempts/edges/results/outbox/inbox/events/cache | cyclic deferred FKs; duplicate command/event; stale fence |
| 00007_editor_review_bulk.sql | reviews/overrides/drafts/bulk | CAS decisions, chunk conflict, no version update |
| 00008_delivery.sql | derivatives/presets/exports/downloads/intents/leases | same-version exports; ready requires all volumes |
| 00009_lifecycle_import.sql | idempotency/deletion/import | cleanup references, resume importer, indexes |
| 00010_security_functions.sql | grant matrix + final worker/portal/scheduler functions | all tables FORCE RLS; PUBLIC cannot execute; no tenant context leak |
| 00011_auth_functions.sql + 00167_default_export_presets.sql | auth bootstrap; atomic default Master/Web preset trigger and existing-tenant backfill | no model weights/secrets/demo tenant in production seed |

Каждая миграция в транзакции, goose advisory lock один. FK на будущие таблицы добавляется в 00006/00008/00010; endpoint не запускается до 00011. `CREATE INDEX CONCURRENTLY` для последующих больших таблиц — отдельная NO TRANSACTION migration, не смешивать с остальными DDL. Префиксы каталога 001…009 соответствуют 00001…00009.

Production rollback — переключение совместимого бинарника с сохранением expanded schema; разрушительные Down не исполняются. Перед schema upgrade: backup+restore rehearsal, maintenance pause scheduler, дождаться/остановить leases, apply migrations, verify, запустить роли. Для будущих несовместимых изменений: добавить nullable field → dual read/write → bounded backfill → validate constraint → switch readers → drop старое только в следующем major release. Backfill checkpoint в отдельной служебной таблице данной миграции, не через память процесса.

## Перенос legacy файлов и форматов

`LegacyImporter.scan(root)->LegacyImportPlan`: только выбранная пользователем директория, symlinks/path traversal отклонены, run manifests в пределах root; hash каждого входа и заявленного output; план не пишет в продукт. `validate(plan)->ImportReport` перечисляет missing/corrupt/unlinked/duplicate. `execute(plan,tenant,project,key)->ImportJob` идёт через обычный upload API, создаёт отдельные draft collections для исходников и каждого legacy run. `resume(job)` использует legacy_imports UNIQUE, не импортирует повторно. Оригиналы сохраняются на месте.

Read-only часть реализована в `workers/photo_worker/legacy_migrate.py`. Парсер
ограничивает JSON 32MiB, отвергает duplicate keys, NaN/Infinity, symlink,
absolute/traversal paths и любой manifest/input/output вне выбранного root.
Source и все заявленные report outputs хешируются потоково; plan стабильно
сортируется и получает SHA-256 canonical JSON. Повторный `validate` перечитывает
bytes и отмечает изменения после scan. Legacy resolved config сохраняется только
как provenance; неизвестный schema_version даёт `UNSUPPORTED_CONFIG`, без
автоматической подмены новым Style. CLI `python -m photo_worker.legacy_migrate ROOT`
печатает dry-run либо атомарно пишет `--output` только внутри ROOT. Проверка на
сохранённом e2e corpus: 13 runs, 248 файлов; originals после scan неизменны.
`execute` реализован поверх штатных owner multipart endpoints: каждый файл повторно
хешируется до первого API-вызова, upload begin/complete получают детерминированные
idempotency keys от plan hash + relative path + content hash, presigned parts
проверяются на полноту и загружаются строго по объявленному размеру. CLI включает
этот режим только явным `--execute-api`; bearer читается из `PHOTOEDITOR_TOKEN` и
не попадает в план или URL. Перед upload он идемпотентно создаёт отдельную draft
collection для оригиналов и для каждого legacy run; `--collection-id` позволяет
использовать заранее созданную collection только для оригиналов. После upload CLI
опрашивает owner upload status до `verified`, затем вызывает
`POST /legacy-imports/reconcile`. Endpoint принимает только upload текущего user,
точный expected SHA и существующую asset version того же project, повторяет ACL и
идемпотентно фиксирует `legacy_imports`. Повторный запуск плана переиспользует
collection/upload/import identities; конфликт другой версии или upload даёт409.

Старая directory-level схема integer 1 не принимается как style 1.0.0. `migrate_style(source,from,to)->MigrationResult` поддерживает только подлинные документированные style 1.0.0/1.1.0: сохранить прежние color parameters, новые beauty/geometry/extensions выключить, выпустить v2 и прогнать equivalence corpus. Если исходный renderer неизвестен/не реализован — CONFIG_INCOMPATIBLE, без притворного преобразования. Новый runtime не принимает старые schema напрямую.

Миграция весов/ICC не перезаписывает files по старому ID: новый lock_id, отдельные paths, doctor, regression, затем новые runs; активные runs продолжают старый lock. Старый lock удаляется только после отсутствия snapshots/replay retention.


## Дополнения к порядку создания

00001 также создаёт auth_mail_outbox; 00006 — scheduler_tenants, graph_expansions, resource_slots, resource_reservations из каталога. Все типизированные JSON columns имеют validators15. В пределах миграции сначала создать таблицы, затем циклические FK/триггеры — ссылки на ещё не созданные таблицы недопустимы. `resource_slots` — глобальный системный реестр физических slots, не tenant бизнес-таблица; доступ только function-only scheduler/worker, public/API deny. Tenant resource_reservations защищается FORCE RLS. Bootstrap node identity не создаёт membership.

Ресурсные slots связываются с task composite tenant/id nullable только в idle состоянии, CHECK обе ссылки NULL либо обе NOT NULL. Scratch reservations для job до первого task принадлежат quota_reservations(holder_type=download_job); после назначения узла добавляется физический resource_reservation. Не путать storage квоту tenant и свободное место конкретного worker.

`mail_payloads.invitation_id` FK добавляется00002 после создания invitations; глобальная delivery запись получает scope invitation через restricted mail function, не даёт public access к tenant приглашению. CHECK kind=otp требует challenge_id, kind=invite требует invitation_id; остальные secret payload поля encrypted.

### Migration 00189 — attested sensor alignment

Forward-only overload `adopt_ingest_image(...,p_metadata)` сохраняет canonical inspect metadata вместе с WorkingImage и отзывает scheduler EXECUTE со старой шестипараметровой функции. `sensor_alignment` разрешён только для RAW и содержит hashed physical-camera ID, sensor bounds, exact crop и orientation1. Это обязательный источник будущего Apply sensor-dust grouping; отсутствие mapping означает fail-closed.

Миграция `00195_contact_sheet_resources.sql` добавляет `tenant_static_resources`, FORCE RLS и две узкие `SECURITY DEFINER` функции reserve/complete для встроенного contact-sheet font. Байты сначала резервируются в `tenant_usage`, готовый object version атомарно превращается в artifact, а tenant purge удаляет resource row до artifacts. Та же миграция расширяет закрытый interactive kind/operation contract значением `contact_sheet`/`report.contact_sheet`.
