# 16. Внутренние сервисные контракты

Этот документ завершает границу API/SQL/worker. Он не расширяет public API; все `/internal/v1` маршруты mTLS, не доступны через Caddy public domain. Service JWT audience=photo-internal, scope allowlist и короткий TTL60s; task capability привязан task/attempt/fence/worker identity. Bootstrap rotation credentials вне клиентских bundles.

| Функция / маршрут | Вход → выход; атомарные условия |
|---|---|
| `POST /internal/v1/workers/register` | service identity,node_id,doctor_report_hash,lock_id,backends,capacities→worker_id,heartbeat interval; администратор заранее разрешает node, саморегистрация public forbidden |
| `POST /internal/v1/tasks/{id}/credentials` | attempt,fence→STS task credentials либо object gateway scoped token; lease alive, worker matches; no requested arbitrary keys |
| `GET /internal/v1/tasks/{id}/objects/{artifact}` | gateway exact task input artifact stream; Range bounded; validate hash client side |
| `PUT /internal/v1/tasks/{id}/outputs/{role}` | scoped new attempt key only; bounded stream; output role allowlist; no overwrite canonical |
| `claim_task(task_id,dispatch_attempt,worker_id)` | ClaimResult{claimed,terminal,busy,stale,cancelled}; claimed Lease+TaskContextRef, locks task+slot, confirms DB command fields |
| `read_task_context(task_id,attempt,fence)` | operation,input manifests,params/model/profile locks,ACL-bound refs; valid service/task only |
| `heartbeat_task(task_id,attempt,fence,progress)` | lease_until,cancel_requested; CAS current fence; caps hard deadline; extends physical reservation |
| `commit_task_success(task_id,attempt,fence,outputs,metrics)` | ResultRef; one canonical task output, durable event; replays same result_digest idempotent; mismatch COMMIT_CONFLICT |
| `commit_task_failure(task_id,attempt,fence,error,metrics)` | state+event; no direct next task creation; retry decision Go |
| `claim_outbox_batch(role,limit,lease_seconds)` | bounded envelope rows where destination allowlist; own publish lease; no arbitrary user payload |
| `mark_outbox_sent(event_id,publish_lease)` | only positive confirm handler; repeated safe; unmatched lease fail |
| `list_schedulable_tenants(cursor,limit)` | tenant IDs with runnable state only; no filenames/memberships |
| `reserve_resources(task,node,estimate)` | slots+quota+resource reservation tx, ready remains if unavailable |
| `portal_lookup_session(session_hash)` | opaque validated GuestPrincipal, current link version/expiry; only portal role |
| `portal_manifest(session_hash,cursor,filters)` | query includes publication,item/version,collection status,view permission; no broad SELECT |
| `portal_begin_upload/session_complete` | derived tenant/collection+own principal, current quota/policy; lifecycle09 |
| `portal_create_download/session_intent` | snapshot scope+current policy and own jobs; all permission checks09 |

SQL helper functions `SECURITY DEFINER` limited with fully qualified names, explicit arg types, search_path fixed, REVOKE PUBLIC. Return typed records, not arbitrary JSON SQL runner. Query context reset at transaction end. Task credentials/claim functions never trust app.tenant_id as sole authorization. Native MPS has dedicated DB role worker-only execute and AMQP consume class permissions; object access через same task endpoint. Loopback ports for DB/Rabbit if needed, no user network exposure.

## Repository generation rules

Каждая mutable сущность получает `GetByIDForUpdate`, immutable `GetByID`, списки keyset query, inserts named fields, state CAS update with expected_revision/fence. Methods not needed by exposed service are not generated as generic CRUD. sqlc queries take tenant_id explicitly даже при RLS, не скрывают текущий tenant в SQL magic. RETURNING new revision для каждой мутации. `rows affected=0` различать hidden/notfound и conflict без leaking hidden entity.

`WithIdempotentMutation` порядок locks: idempotency row→tenant quota→project→collections sorted UUID→aggregate style/run→tasks→artifacts. Если функция не использует часть порядка, пропускает её, не инвертирует. External IO никогда внутри tx. Serialization/deadlock SQLSTATE40001/40P01 retry entire short transaction max3 с тем же key, без повторения внешнего SMTP/S3 side effect. Unique conflicts convert stable semantic code.

## Полный набор структур Go

`TenantID/UserID/RunID/TaskID/ArtifactID` отдельные named UUID types, не взаимозаменяемые строки. `Revision int64`, `Digest [32]byte`, `Role enum string`, `ResourceClass enum string`, `TaskState/RunState/ReviewDecision enums`. `Clock`/`RandomSource` interfaces внедряются для deterministic tests; production crypto/rand и UTC server time. `ObjectStore`, `QueuePublisher`, `Mailer`, `ModelCapabilityRepository`, `AuditWriter` typed ports. Service methods не получают concrete HTTP request и не читают global ENV внутри business logic. `DomainError` wraps code,args,retryable,http_status,cause (cause private log only).

HTTP middleware order: request_id→body limit→trusted proxy/IP→CORS→auth/session→CSRF→principal role→tenant/project authorization→handler. Panic recovery outside handler, redacted500. Public auth routes исключены из tenant middleware, но сохраняют rate/Origin checks. Poll endpoints use current ACL read, не stale cached principal.

## Проверка интеграции

Контрактными тестами должны быть покрыты все combinations возвращаемого ClaimResult, вся таблица state transitions05, repeated outbox/event delivery, worker mismatch, task field spoofing, credentials expired before upload, revoke while archive builds, asset GC attach race. Коды COMMIT_CONFLICT/FEATURES_NOT_READY добавляются к common/errors15 и shared locales, не raw text.


## Порядок блокировок задач и SMTP exception

Для task/resource транзакций общий порядок: physical resource_slots sorted →tenant_usage →runs →tasks sorted →artifacts. Reaper/heartbeat/commit берут slot reference сначала из read-only lookup, затем повторно сверяют под lock; это устраняет инверсию task→slot относительно dispatch. Idempotency lock для HTTP предшествует domain locks, но HTTP не резервирует physical slot; он резервирует tenant quota. Event consumer не держит publisher/network locks. Артефактные GC транзакции не блокируют task после artifact: проверяют active references snapshot query и ref-lock в том же согласованном порядке.

SMTP — явно глобальный auth transport, потому что OTP создаётся до выбора/создания tenant. Отдельный `MailCommandV1={schema_version:1,event_id,operation:mail.send,payload_id,attempt,correlation_id}` не содержит tenant_id/OTP/email plaintext. Routing `mail.send`, queue q.worker.mail, schema selection по отдельной Go mail registry. `claim_mail(payload_id,attempt,worker_id)`, `heartbeat_mail`, `finish_mail(fence,status)` operate auth tables with fence/lease, no image-task FK. auth_mail_outbox publisher follows confirms, consumer ack после saved send outcome; неизвестный SMTP outcome может дать повтор того же письма. Не придумывать служебный tenant для обхода RLS. Техническая доставка invitation использует такой же encrypted mail payload с kind=invite и invitation reference; OTP TTL не распространяется на invitation7d.

## Реализованная SQL-граница задач (миграции 00015–00016)

`claim_task` дополнительно принимает весь `TaskCommandV1` и сравнивает его с сохранённым `dispatch_command`. `worker_registrations.db_principal` сверяется с `session_user`, а не изменяемым `current_user`/tenant setting; регистрация должна быть enabled. Выдача требует действующего slot и reservation на разрешённом node. `timeout_seconds` задачи задаёт hard deadline, heartbeat не может его отодвинуть.

`commit_task_success` принимает канонические RFC8785 байты JSON `{outputs,metrics,review_required}` как UTF-8 text; БД сама вычисляет SHA256 этих байтов. Повтор сравнивает и digest, и полные JSON-значения. Каждый output обязан быть ready и иметь gateway-attestation `verified_at`, `created_by_task_id`, `produced_attempt`, `produced_fence`. Воркер не имеет прямых прав на таблицы и не может выставить attestation. Функции gateway и admission реализуются отдельно; наличие SQL-функций задач само по себе не означает готовый worker service.

`commit_task_failure` сохраняет error/metrics в attempt и событие; состояние `retry_wait` с NULL next_attempt_at означает «ожидается решение Go scheduler». `Recovery.Recover` вызывает `pending_task_failures`, чистый `DecideRetry`, затем CAS `plan_task_retry` и `activate_task_retries`. Reaper аннулирует fence; OOM уменьшает только batch/tile с явно заданными безопасными minimum. CPU fallback требует одновременно allow_cpu_fallback и cpu_qualified и не меняет model lock.

Reaper и activation изменяют максимум одну задачу в одной короткой транзакции; Go повторяет вызов до лимита batch. Это предотвращает удержание run lock от предыдущей задачи при захвате slot следующей. Конкурирующие экземпляры сверяют состояние повторно под блокировкой.

## Admission и scheduler (миграции 00017–00018)

Добавлены глобальные `resource_nodes` и `operation_registry`; worker роли не имеют прямого SELECT/UPDATE. Operator provisioning устанавливает бюджеты и doctor report hash. `report_worker_capacity` принимает только зарегистрированный session_user/worker и ограничивает показания сверху бюджетом. `qualified_operations` — объект `{operation:[backend,...]}`, а не независимые списки операций и backend: проверяется конкретная комбинация. `resource_nodes.enabled=false` останавливает новые dispatch; для немедленного прекращения попыток отзывается worker registration.

Порядок блокировок уточнён: scheduler tenant fairness row (только dispatch) → physical node → slot → tenant_usage → run → task → artifacts. `lock_task` повторно сверяет назначенный slot под task lock; смена назначения возвращает SQLSTATE40001. Go и Python повторяют короткую транзакцию максимум три раза, без повторения внешних эффектов.

`reserve_dispatch` принимает task/node и физические оценки RAM/VRAM/scratch; сам строит command из DB task и pinned operation registry. Обязательны sealed active run, ready task, активный qualified worker, model lock, свежие capacity report (30s), свободный slot, суммарные бюджеты и GPU quota. Одновременно допускается максимум одна GPU-heavy задача на node. Существующие reservations учитываются до их явного освобождения reaper, даже если они просрочены. Доступная измеренная память дополнительно ограничивает budget; это консервативная политика, она может недоиспользовать память, но не обещает её сверх резерва.

Execution profile содержит `resource_estimate={ram_bytes,vram_bytes,scratch_bytes,output_bytes,pixel_count}`; все поля обязательны и ограничены JCS safe integer. `max_pixels` по умолчанию 200000000, административный override фиксируется в profile. CPU fallback требует `cpu_resource_estimate` и заменяет физическую оценку вместе с backend/precision; model lock неизменен. Atomic operation сохраняет свою resource class, в том числе GPU-heavy slot при разрешённом CPU fallback на том же классе worker.

`pick_ready_task` удерживает tenant fairness row до commit. `eligible_dispatch_nodes` возвращает до 32 candidates. Неуспешный node пробуется в savepoint и откатывается перед следующим, освобождая его locks. `record_dispatch` идемпотентен по outbox event и повышает virtual_finish (media1/cpu2/archive3/gpu5/collection2). После пяти previews выбирается старейший batch, если он есть. Временно недоступная задача откладывается на 2s с сохранённой причиной. `PHOTO_ROLE=scheduler` выполняет dispatch/recovery; создание/advance графов — отдельная граница, пока не завершённая.

## Wire-контракт клиента object gateway

`workers/photo_worker/runtime/gateway.py` использует точный HTTPS origin без path/query/userinfo. Серверный сертификат и hostname проверяются; client certificate/key загружаются из операторских файлов, private key имеет права 0600 или строже. Redirect не поддерживается. Task capability передаётся как Bearer; дополнительные X-Photo-Attempt, X-Photo-Fence, X-Photo-Worker должны совпадать с подписанными claims и DB lease на стороне шлюза.

GET object возвращает status200, единственный Content-Length, X-Photo-SHA256, X-Photo-Artifact-ID и identity encoding, без Transfer-Encoding. Клиент отклоняет лишний размер, неверный ref/hash и незавершённый поток; фактический SHA256 проверяется materialize. Чтение допускает только ограниченные блоки, проверка отмены выполняется до/после сетевого чтения.

PUT output role содержит Content-Length, Content-Type, X-Photo-SHA256 и UUID Idempotency-Key. Клиент разрешает только роли из allowlist операции, ограничивает размер файла, вычисляет хеш до передачи и повторно во время отправки. Ответ200/201 — строгий ArtifactRef JSON. Неизвестный результат PUT автоматически не повторяется; возможный orphan не видим пользователю без fenced commit задачи. Серверная реализация обязана обеспечить собственные ограничения и attestation, эти гарантии нельзя выводить только из клиентских проверок.

GatewayError DEPENDENCY_UNAVAILABLE сохраняет retryable=true только для временных сетевых/server ошибок. Неверный TLS config, hash, размер, scratch quota не объявляются временными. Lease loss останавливает выполнение без попытки публикации. Локальные exception text и HTTP body не записываются в TaskError.

На этом этапе проверен клиент через настоящий локальный mTLS HTTPS transport. Серверный Go gateway, выпуск task capability и интеграция с MinIO остаются незавершёнными; тестовый HTTPS handler не является их реализацией.

## Серверная авторизация шлюза (00019)

`photo_gateway` имеет только EXECUTE на `authorize_gateway_task`, без доступа к таблицам artifacts/tasks. Функция связывает task/attempt/fence/worker с текущими lease, deadline, resource slot, enabled worker registration, certificate_sha256, operation/backend qualification и model lock. Cancel/revoke немедленно запрещает новый object request. Уже выполняющаяся задача сохраняет доступ при run.waiting_review; новые dispatch в этом состоянии по-прежнему не создаются.

`gateway.Authorizer.Resolve` выводит tenant/run из canonical task, а `Authorize` дополнительно сравнивает полную TaskIdentity. `CapabilitySigner` использует отдельный Ed25519 key ring и фиксированный audience photo-internal; токен содержит UUID jti, certificate fingerprint и scopes objects:read/outputs:write. Fence передаётся строкой int64. TTL не превышает минимум из 60s, lease_until, deadline и срока сертификата. Проверка токена не заменяет вызов Authorizer для каждого GET/PUT. `VerifiedPeer` принимает только TLS leaf из VerifiedChains завершённого handshake, заголовки reverse proxy не являются удостоверением воркера.

Проверены подпись/expiry/scopes/certificate binding и отказ БД после cancel/revoke. HTTP wiring и MinIO остаются отдельной незавершённой частью.


## Выдача токенов и адаптер MinIO

`gateway.HTTP.Handler` реализует POST credentials: mTLS verified leaf, один X-Photo-Worker, canonical task UUID; JSON `{ "attempt": 1, "fence": "1" }` до 1024 байт, без повторных/лишних ключей. Read deadline 5s, DB timeout 5s. Response `{token,token_type:"Bearer",expires_at,identity}` с Cache-Control:no-store; identity включает canonical tenant/run/task/worker/attempt/fence. Scopes фиксированы сервером. Отзыв возвращает403 LEASE_LOST, недоступность БД503 DEPENDENCY_UNAVAILABLE без connection details. Серверный handler существует отдельно от public API; production listener и GET/PUT wiring ещё не подключены.

`objectstore.Store` использует MinIO SDK с обязательным bucket versioning и полным SHA-256. Generated key содержит tenant/task/attempt/fence/artifact UUID. PutImmutable применяет If-None-Match:* и для multipart completion; сверяет фактические размер/хеш и HEAD точной версии. При несовпадении удаляет только созданную им версию. Open читает exact VersionID и повторно проверяет хеш при EOF. Преждевременное закрытие не означает проверенный объект. Ключи и bucket не возвращаются worker-клиенту. DB authorization, output role allowlist, квоты и fencing финализации должны быть реализованы поверх adapter; сам adapter не заменяет эти проверки.

`runtime.credentials.TaskCredentials` — callable token provider для существующего Gateway. Проверяет exact identity всех шести полей, Bearer token format, expiry не дальше 60s/дедлайна; кеш использует monotonic clock и margin5s. Обновление сериализуется lock, отмена проверяется до/после HTTP. Ошибка обновления не оставляет прежний токен в кеше; request status401/403/409/410 означает lease loss. Оператор синхронизирует UTC часы. Сервер выдачи и Python-клиент проверены через реальные TLS sockets по отдельности; сквозной worker→Go→PostgreSQL→MinIO тест пока не выполнен.

## Ограничение чтения входов (00020)

`photo.gateway_input(task,attempt,fence,worker,certificate_sha256,artifact)` повторяет live authorization и возвращает только ready/nonexpired/versioned артефакт из tenant задачи, присутствующий в сохранённых dispatch_command.inputs или params с тем же SHA256. Наличие другого артефакта в tenant не даёт доступа. Gateway role имеет EXECUTE, а не SELECT таблиц. `Authorizer.Input` дополнительно сверяет полную TaskIdentity и configured bucket. `HTTP.AuthorizeRequest` требует единственные канонические заголовки attempt/fence/worker, Bearer scope и verified TLS leaf; подпись не заменяет актуальную проверку SQL. SQL integration tests добавлены, но их текущий запуск заблокирован недоступным Docker/PostgreSQL; это ещё не проверенная миграция.

## HTTP чтение артефактов

`GET /internal/v1/tasks/{task}/objects/{artifact}` теперь подключён в gateway.HTTP.Handler. После AuthorizeRequest и Input проверяет deployment MaxDownloadBytes, открывает точную версию и отправляет Content-Length, X-Photo-Artifact-ID, X-Photo-SHA256, no-store. Буфер64KiB, write deadline15s; live task authorization повторяется каждые5s между чтениями, hard task deadline ограничивает request context. Ошибка после начала ответа прерывает HTTP stream через ErrAbortHandler, не подменяет остаток JSON.

Range допускает только один явный `bytes=start-end`, включительные границы, не более16MiB, без suffix/open-ended/multiple ranges. Ответ206 содержит Content-Range и полный SHA объекта (не SHA диапазона). Несовпадающий/слишком большой range получает416 до обращения к S3. Full read проверяет SHA при EOF; partial read проверяет точную version/metadata и длину, но не объявляется проверкой полного SHA. Python materializer использует только полный GET200. Unit tests проверяют чтение, диапазоны, quota/authorization refusal и abort при отзыве; реальные PostgreSQL/MinIO проверки нового пути пока ожидают восстановления Docker.

## PUT orchestration boundary

`gateway.UploadService.Upload` и PUT handler реализованы поверх обязательного `OutputRepository`. HTTP требует единственные SHA256/Content-Type/Idempotency-Key, canonical UUID, явный Content-Length без Transfer-Encoding, server max upload bytes. `Begin` и `Complete` получают отдельные context timeout5s, S3 IO выполняется между ними без SQL-транзакции. Ticket содержит server artifact UUID, upload UUID и canonical tenant/task/attempt/fence key; неправильный key отвергается. Перед Complete проверяются version, size, sha и MIME возвращённого объекта. Повтор уже завершённого idempotency key возвращает200 ArtifactRef без новой записи, новая загрузка201. Неизвестный outcome не вызывает спекулятивный DELETE или release квоты. Upload reader проверяет lease через5s между чтениями и ограничивает блок64KiB/read deadline15s.

`OutputRepository` пока только типизированный контракт: его PostgreSQL реализация должна атомарно резервировать quota/role count, отклонять concurrent pending replay, повторно проверять fence/certificate на Complete и сохранять verified artifact. Без этого dependency PUT возвращает503. Контрактные Go-тесты orchestration/HTTP проходят, но они не доказывают SQL-конкурентность, cleanup и сквозной upload; эти части остаются обязательными для завершения.

## SQL output repository (00021)

`SQLOutputs` теперь реализует OutputRepository через короткие `database.InTransaction` вызовы. Перед mutation проверяет canonical tenant/run; SQL begin/complete повторяют live authorization после `lock_task` в порядке node→slot→tenant_usage→run→task→upload. `operation_output_policies` содержит role/MIME/max_bytes/max_count, FK на operation/version; отсутствие policy запрещает запись. Seed политик всех операций ещё требуется.

`begin_output` ограничивает суммарный output_bytes текущей попытки, максимум128 записей и количество роли, а также `tenant.quotas.storage_bytes` относительно storage_bytes+reserved_bytes. Создаёт уникальный ticket/key и увеличивает reserved_bytes; pending duplicate получает busy, отличающиеся metadata conflict, завершённый идентичный запрос replay. `complete_output` требует тот же worker/certificate/attempt/fence и проверенные size/SHA/MIME/version, вставляет artifact с verified_at/produced_attempt/produced_fence/output_role и ровно один раз переносит reserved_bytes в storage_bytes.

При потере аренды pending bytes сохраняются до безопасной очистки точного attempt key; механизма reconciliation/cleanup пока нет. Worker/public SQL roles не имеют доступа к таблицам output_uploads/policies, gateway получает только EXECUTE. Эти функции пока не прошли PostgreSQL integration: dev daemon недоступен; Go build/unit проходят, но не являются доказательством корректности PL/pgSQL.

## Очистка неподтверждённых upload (00022)

`claim_output_cleanup` выдаёт maintenance-role один orphan ticket с UUID token/lease60s, после grace3h. Исключены ready uploads, текущая действующая попытка и любой существующий artifact с этим ID. Блокировки следуют lock_task→upload; external S3 IO вне транзакции. `OutputCleaner.CleanOne` повторно проверяет bucket и generated exact attempt key, удаляет multipart/версии через bounded40s PurgeAttempt, затем подтверждает SQL finish с5s timeout. Ошибка удаления не освобождает quota. Finish требует current token/live cleanup lease и уменьшает reserved_bytes только при первом cleaning→cleaned.

Cleaned tombstone сохраняется и повторно сканируется каждые24h, чтобы удалить поздний S3 completion после неизвестного сетевого outcome. Это eventual cleanup orphan bytes; поздняя запись может временно существовать физически после освобождения логического резерва. Публикация такой записи запрещена состоянием upload и task fence. Ready/canonical артефакты эта процедура никогда не удаляет; полный artifact retention/GC остаётся отдельной работой. Unit tests портов проходят; SQL22 и real MinIO exact-key purge ещё не проверены при недоступном Docker. Loop сервиса maintenance пока не подключён.

## Worker startup/liveness (00025–00026)

`worker_runtime_operations(worker,class)` возвращает только CPU-qualified операции enabled регистраций этого session_user/node, не даёт SELECT таблиц. `photo-worker run` сравнивает их с исполняемой сборкой до потребления. `heartbeat_cpu_worker(worker,class,operations,ram,scratch)` требует единый node и одну resource class для process identity, проверяет certificate registration и installed operation set, блокирует node→worker rows и повторно сверяет регистрацию. CPU node получает bounded RAM/scratch и fresh reported_at; GPU node получает только worker.last_seen, чтобы CPU-измерения не освежали непроверенную VRAM.

Reporter работает на отдельной SQL connection/thread каждые10s, сначала синхронно до consuming. Любая ошибка/revocation останавливает consumer и ExecutionContexts. SIGTERM останавливает reporter и восстанавливает signal handlers. GPU capacity reporting остаётся отдельной незавершённой частью. 80 Python platform tests прошли; SQL25–26 integration тесты добавлены, но при недоступном PostgreSQL не считаются проверенными.


### Большие StyleV2 artifacts

Лимит portable style из02 —100000000 байт. `runtime/parameters.py:read_style` читает только StyleV2 до этого предела через тот же authenticated artifact transport: chunks<=65536, SHA исходных байтов, запрет duplicate JSON keys/non-finite/unsafe JCS integers, затем strict Pydantic schema. Stream закрывается при любой ошибке/отмене. Bytearray освобождается после получения immutable bytes перед JSON validation. Обычный `read_contract` остаётся ограничен1MiB; даже StyleV2 через этот обычный метод не получает повышенный лимит автоматически.

`TaskArtifacts.style(ref)` проверяет ref среди declared task inputs и admitted resource_estimate.ram_bytes>=3GiB перед открытием потока. Это консервативный минимальный допуск для больших профилей с несколькими представлениями JSON в памяти, а не измеренный универсальный peak. Максимальный style100MB и worst-case dense adaptive LUT ещё требуют отдельного stress/RSS gate. GATEWAY max_download_bytes также должен разрешать этот размер; данные не переносятся в task command/AMQP. Ошибки не возвращают частично разобранный профиль.

Шесть reader tests прошли: корректный style>1MiB, неизменный обычный limit, hash mismatch, duplicate keys, ограничение потока/закрытие, undeclared ref и недостаточный RAM до IO. Evidence: work/evidence/style-artifact-reader-tests.log. Legacy baseline сохранён. Runtime style.resolve/output lifecycle ещё не реализован этой итерацией; чтение большого профиля само по себе не подтверждает готовность adaptive pipeline.


### ResolvedStyle и runtime style.resolve

CPU operation `style.resolve` теперь подключена. ResolveParams={style:ArtifactRef,image:ArtifactRef,stats:ArtifactRef|null,strict_ood:false,group_prototype_id:null|string<=120}. Нужен admitted RAM>=3GiB и backend=cpu. StyleV2 читается через отдельный100MB reader, image/Stats20 через обычные1MiB contracts. Example style отклоняется. Адаптивный профиль требует stats; fixed допускает отсутствие stats, но ненадёжный/отсутствующий person evidence отключает активную local часть с review.

Stats20 получил nullable source_image. `color.stats` всегда записывает точный ref своего WorkingImage. `style.resolve` принимает переданную статистику только при source_image==params.image; старые standalone численные Stats20 без source_image сохраняются как контракт, но не принимаются runtime resolver. Это исключает использование признаков другого изображения. Для adaptive source RGB/alpha загружаются как declared artifacts, coverage counts считаются блоками; person reliable требует mask_confidence>=.75 и person coverage>=.01.

ResolvedStyle содержит params,mode,fixed либо AbsoluteLUTRef,local,strength,modules,neighbors,distance,uncovered_fraction,reasons,needs_review,hash. modules сохраняет typed beauty/defects/geometry/extensions/models из immutable source; разрешение цвета не означает выполнения остальных эффектов. Modes:fixed,adaptive,fixed_fallback,group_fixed,skip. Strict OOD для обычного adaptive создаёт mode=skip с отсутствующими color/LUT и disabled local; downstream orchestration обязана пропустить item. Это явный результат политики, не технический exception с повтором. Group-fixed по-прежнему сохраняет выбранный prototype и review даже при strict OOD.

AbsoluteLUTRef={artifact,size:33|65,dtype:float32,order:RGB_C,profile_id:work_rec2020_gamma24_v1}. Данные — little-endian float32 NPY [size,size,size,3], конечные[0,1], отдельный role absolute_lut. Adapter резервирует nbytes+8192, пишет приватный NPY, staging возвращает ref, затем scratch удаляется. В task outputs возвращаются LUT ref (если есть) и resolved_style ref; downstream должен объявлять binary ref в inputs отдельно. Fixed GlobalColor остаётся в малом JSON. Modules и manifest ограничены обычным1MiB output budget.

`ResolvedStyle.hash=SHA256(JCS(body без hash))`, дополнительно к SHA самого artifact. Конструктор create проверяет body и рассчитывает hash; reader проверяет hash и cross-invariants: правильный вид цвета для режима, normalized unique neighbors, group binding, OOD metrics/reasons и needs_review. Изменённый strength с прежним hash отклоняется.

Миграция32 разрешает resolved_style application/json<=1MiB/count1 и absolute_lut application/octet-stream<=3303692/count1. 167 platform tests прошли: fixed/adaptive staging, LUT content, strict skip, explicit group preservation, stats source mismatch до pixel IO и hash tampering. Evidence: work/evidence/resolve-adapter-platform-tests.log. SQL20–32 ещё не проверены на живых сервисах; apply graph/renderer и остальные эффекты остаются незавершёнными.


Чтение resolved LUT: `runtime/arrays.py:read_lut(path,size,check_active)` принимает только size33/65 и exact little-endian float32 C-order NPY[side,side,side,3], конечные значения[0,1]. Использует общий ограниченный header/SHA-preverified-file/mapping lifecycle механизм с read_array, но отдельный shape gate. Image reader не расширен до произвольных4D массивов; residual17 через read_lut не принимается. Потребитель должен материализовать ArtifactRef через TaskArtifacts перед вызовом и не удерживать borrowed mapping после context exit. 18 array/resolver adapter tests прошли, включая65³, range rejection, image/LUT confusion и закрытие mapping; evidence work/evidence/lut-reader-tests.log. Image color/local adapters ещё требуют подключения.
