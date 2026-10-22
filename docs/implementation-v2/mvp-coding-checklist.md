# Детальный прогресс кодирования MVP

Базовая версия: 2026-10-15; текущий учёт: 2026-10-17. **68/70 = 97,142857%** остаточного плана кодирования. Запуск проверок отложен. Открыты P0.2.06 и P0.7.10; текущие source acceptance перечислены в машинном реестре. Исторические записи ниже сохраняют прежние состояния и проценты.

Это консервативный процент остаточного плана, не всей системы. Открытая строка может содержать частичный код. Основной машинный реестр: [mvp-coding-progress.json](mvp-coding-progress.json).

## P0.1: 10/10 — coding_closed_unverified

- [x] **P0.1.01** DTO и клиентская проверка preview — [semantic-review-preview.ts](../../packages/api-client/src/semantic-review-preview.ts); [semantic-review-preview.test.ts](../../packages/api-client/src/semantic-review-preview.test.ts)
- [x] **P0.1.02** Независимый local-exposure preview kernel и loader — [preview_loader.py](../../workers/photo_worker/operations/semantic/preview_loader.py); [test_v27_preview_loader.py](../../tests/test_v27_preview_loader.py)
- [x] **P0.1.03** Presentation, overlay и numerical evidence — [preview_presentation.py](../../workers/photo_worker/operations/semantic/preview_presentation.py); [test_v27_preview_presentation.py](../../tests/test_v27_preview_presentation.py)
- [x] **P0.1.04** Авторизованный provider и публикация preview — [preview_complete.py](../../workers/photo_worker/operations/semantic/preview_complete.py); [test_v27_preview_complete.py](../../tests/test_v27_preview_complete.py)
- [x] **P0.1.05** Подготовка и атомарная запись shown Apply batch — [shown_batch_preparation.go](../../backend/internal/semanticreview/shown_batch_preparation.go); [shown_batch_preparation_test.go](../../backend/internal/semanticreview/shown_batch_preparation_test.go); [02232_v27_shown_apply_atomic_commit.sql](../../backend/db/migrations/02232_v27_shown_apply_atomic_commit.sql)
- [x] **P0.1.06** Exact-byte worker adoption и canonical result — [shown_exposure_adoption.py](../../workers/photo_worker/v2/shown_exposure_adoption.py); [test_v27_shown_exposure_adoption.py](../../tests/test_v27_shown_exposure_adoption.py); [02242_v27_shown_exposure_adoption_commit.sql](../../backend/db/migrations/02242_v27_shown_exposure_adoption_commit.sql)
- [x] **P0.1.07** Принятая asset version, API и переход в editor — [shown_applied_version.go](../../backend/internal/semanticreview/shown_applied_version.go); [shown_applied_version_test.go](../../backend/internal/semanticreview/shown_applied_version_test.go); [semantic_shown_version.go](../../backend/internal/httpapi/semantic_shown_version.go); [semantic_shown_version_test.go](../../backend/internal/httpapi/semantic_shown_version_test.go)
- [x] **P0.1.08** Durable semantic Apply journal и fencing receipt — [semantic-apply-attempt-store.ts](../../apps/photographer/src/semantic-apply-attempt-store.ts); [semantic-apply-attempt-store.test.ts](../../apps/photographer/src/semantic-apply-attempt-store.test.ts)
- [x] **P0.1.09** Preview всех остальных включённых AI-операций
- [x] **P0.1.10** Окончательные отказы, активация и полная трасса semantic review — [текущий реестр](audit/semantic-module-coding-closure-2026-10-16.json); runtime-проверки не запускались.

## P0.2: 9/10

- [x] **P0.2.01** Строгий capability DTO и классификация backend — [operation-availability.ts](../../apps/photographer/src/operation-availability.ts); [operation-availability.test.ts](../../apps/photographer/src/operation-availability.test.ts)
- [x] **P0.2.02** Foreground polling, deadline и поздние ответы — [use-editor-capabilities.ts](../../apps/photographer/src/use-editor-capabilities.ts); [use-editor-capabilities.test.tsx](../../apps/photographer/src/use-editor-capabilities.test.tsx)
- [x] **P0.2.03** Вложенные editor actions и повторная проверка admission
- [x] **P0.2.04** Раздельная политика Metal и RTX memory cap
- [x] **P0.2.05** Матрица всех операций supported_backends/model locks
- [ ] **P0.2.06** Установка worker и положительный remote RTX путь
- [x] **P0.2.07** SQL admission каждой включённой операции — coding_closed_unverified; audit/sql-admission-current-source-closure-2026-10-17.md; execution deferred.
- [x] **P0.2.08** Allocator и heartbeat всех backend
- [x] **P0.2.09** Автоотключение CUDA-only во всех клиентских путях
- [x] **P0.2.10** Сводная трасса capability без неизвестных веток

## P0.3: 10/10 — coding_closed_unverified

- [x] **P0.3.01** Файловый инвентарь 41 экрана и сценарий полноты — [routes.ts](../../apps/photographer/src/routes.ts); [routes.test.ts](../../apps/photographer/src/routes.test.ts)
- [x] **P0.3.02** Контракты delivery/replacement/interchange API — coding_closed_unverified; audit/screen-api-dto-current-source-2026-10-17.md; execution deferred.
- [x] **P0.3.03** Инкрементальный OpenAPI projector и сценарии — [sync-v2-openapi.py](../../scripts/sync-v2-openapi.py); [test_sync_v2_openapi.py](../../scripts/test_sync_v2_openapi.py)
- [x] **P0.3.04** Точные API каждого оставшегося экрана
- [x] **P0.3.05** Роли и tenant scope каждого экрана — coding_closed_unverified; audit/workspace-role-current-source-2026-10-17.md; execution deferred.
- [x] **P0.3.06** Capability gates каждого экрана — whole41-screen source accepted; audit/capability-current-whole-source-2026-10-17.md; runtime qualification deferred.
- [x] **P0.3.07** Навигация и параметры всех 41 переходов — coding_closed_unverified; audit/navigation-current-whole-source-review-2026-10-17.md; execution deferred.
- [x] **P0.3.08** Loading/empty/error/cancel/retry состояния — coding_closed_unverified; audit/screen-state-parent-coding-acceptance-2026-10-17.md; execution deferred.
- [x] **P0.3.09** Сверка всех endpoint parameters и DTO
- [x] **P0.3.10** Итоговая карта экранов без pending

## P0.4: 10/10 — coding_closed_unverified

- [x] **P0.4.01** Import: полная трасса и сценарии
- [x] **P0.4.02** Session: полная трасса и сценарии
- [x] **P0.4.03** Collection: полная текущая карта принята по исходникам; audit/collection-coding-parent-acceptance-2026-10-17.md; execution deferred.
- [x] **P0.4.04** Editor: whole source reaccepted after exact Semantic receipt recovery and bounded nested lifecycle corrections; execution deferred. See audit/editor-final-independent-review-2026-10-17.md.
- [x] **P0.4.05** Apply v4: полная трасса и сценарии — coding_closed_unverified, независимый source review 2026-10-17; проверки отложены
- [x] **P0.4.06** Review: полная трасса и сценарии
- [x] **P0.4.07** Export: полная трасса и сценарии
- [x] **P0.4.08** Delivery: required bundle и публикация
- [x] **P0.4.09** Download: полная трасса и сценарии (coding_closed_unverified; execution deferred)
- [x] **P0.4.10** Сохранность RAW/ICC/straight alpha/protected pixels — coding_closed_unverified; audit/raw-icc-alpha-protected-current-source-closure-2026-10-17.md; deep qualification deferred.

## P0.5: 10/10 — coding_closed_unverified

- [x] **P0.5.01** Semantic transfer: полная трасса ordinary worker/adopter принята по исходникам; audit/semantic-whole-parent-coding-acceptance-2026-10-17.md; execution deferred.
- [x] **P0.5.02** Open-eyes: полная трасса
- [x] **P0.5.03** Glare: полная трасса
- [x] **P0.5.04** Hair/clothing: полные трассы (coding_closed_unverified; execution deferred)
- [x] **P0.5.05** Reflection: полная трасса — coding_closed_unverified; независимое чтение текущих исходников, execution отложен (audit/reflection-current-source-closure-2026-10-17.md).
- [x] **P0.5.06** Relight: полная трасса
- [x] **P0.5.07** Panorama: полная трасса
- [x] **P0.5.08** Focus stack: полная трасса
- [x] **P0.5.09** Negative/perspective: полные трассы
- [x] **P0.5.10** Остальные AI, replay/cancel/GC и решение фотографа — coding_closed_unverified; audit/remaining-ai-whole-source-consolidation-2026-10-17.md; execution deferred.

## P0.6: 10/10 — coding_closed_unverified

- [x] **P0.6.01** Durable prepared-review journal и recovery — [prepared-review-attempt-store.ts](../../apps/photographer/src/prepared-review-attempt-store.ts); [prepared-review-attempt-store.test.ts](../../apps/photographer/src/prepared-review-attempt-store.test.ts)
- [x] **P0.6.02** Mask grant/revoke CAS и delayed replay — [трасса кода и сценариев](audit/import-mask-cas-coding-trace.md)
- [x] **P0.6.03** ACL/RLS и отрицательные роли включённых веток — coding_closed_unverified; audit/acl-rls-current-source-closure-2026-10-17.md
- [x] **P0.6.04** Quota и reservation lifecycle — coding_closed_unverified; [parent source acceptance](audit/storage-lifecycle-parent-acceptance-2026-10-17.md); execution deferred.
- [x] **P0.6.05** Idempotency и окончательные отказы — [source acceptance](audit/idempotency-current-source-review-2026-10-17.md), coding_closed_unverified; проверки не запускались.
- [x] **P0.6.06** Leases/fences и output registry — coding_closed_unverified; [parent source acceptance](audit/storage-lifecycle-parent-acceptance-2026-10-17.md); execution deferred.
- [x] **P0.6.07** Object GC и физический tenant purge — coding_closed_unverified; [parent source acceptance](audit/storage-lifecycle-parent-acceptance-2026-10-17.md); execution deferred.
- [x] **P0.6.08** Backup/restore и политика хранения — coding_closed_unverified; source inventory: audit/backup-final-coding-inventory-2026-10-17.md
- [x] **P0.6.09** Readiness/startup и отключённые функции — coding_closed_unverified; audit/readiness-startup-coding-inventory-2026-10-17.md
- [x] **P0.6.10** Forward-only миграции: владельцы, ACL, fresh/upgrade сценарии — coding_closed_unverified; audit/migration-forward-only-coding-inventory-2026-10-17.md

## P0.7: 9/10

- [x] **P0.7.01** Актуальные операции и MVP scope — coding_closed_unverified; current-operation-scope-2026-10-17.json; independent parent source acceptance, runtime deferred.
- [x] **P0.7.02** Актуальные endpoint и MVP scope — текущий уникальный инвентарь323 method/path; исходные семейные карты и последующие добавления сохранены, execution deferred.
- [x] **P0.7.03** Ворота операций: вход и authority — coding_closed_unverified; audit/operation-entry-authority-parent-coding-acceptance-2026-10-17.md.
- [x] **P0.7.04** Ворота операций: dispatch и worker
- [x] **P0.7.05** Ворота операций: result и adoption
- [x] **P0.7.06** Ворота операций: UI и recovery
- [x] **P0.7.07** Ворота endpoint: контракт и caller — coding_closed_unverified; текущий реестр323 identities, source contract packaging; execution deferred.
- [x] **P0.7.08** Ворота endpoint: auth/service/response/scenarios
- [x] **P0.7.09** Доказанные gates отложенных функций — coding_closed_unverified; audit/deferred-activation-parent-acceptance-2026-10-17.md.
- [ ] **P0.7.10** Freeze: ноль известных code_gap/not_verified в MVP


## Подробность следующих отчётов

По запросу пользователя от 2026-10-15 крупные открытые пункты разбиваются на подпункты с фиксированными весами: сумма весов детей равна весу родителя. В отчёте показываются закрытые подпункты, оставшиеся подпункты, их вклад в общий процент и статус тестирования. Декомпозиция фиксируется до начисления прогресса и включает оставшиеся интеграции. Код вместе с написанными сценариями и проверенной трассой даёт зачёт; количество файлов и субъективная оценка частичной готовности — нет. До фиксации декомпозиции частичная работа не увеличивает процент. Текущий зачёт: **25/70 = 35,714286%**. Более ранние значения в журнале ниже отражают состояние на момент соответствующей записи.

### 2026-10-17 — P0.2.09, relight live admission

Добавлена проверка актуальной доступности image.relight, depth.predict и mask.person непосредственно перед POST, включая отзыв во время записи журнала. Для неизвестного результата сохранено восстановление lookup-only. Добавлен DOM-сценарий отзыва без повторного отображения. Сценарии не запускались. Реестр: audit/relight-live-capability-coding-trace-2026-10-17.json. Пункт P0.2.09 остаётся implementing; счётчик 20/70 (28.571429%) не изменён.

2026-10-17: добавлен live admission для optical.negative; написаны DOM-сценарии отзыва без rerender для negative, open-eyes и glare donor. Сценарии не запускались. P0.2.09 остаётся implementing, 20/70. Evidence: audit/optical-portrait-live-admission-coding-trace-2026-10-17.json.

2026-10-17: live admission добавлен для optical.perspective и optical.perspective_lines, включая повторный запуск детекции после await. DOM-сценарий отзыва при записи журнала без rerender написан, не запущен. Подготовка delivery snapshot не требует GPU. P0.2.09 implementing, 20/70. Evidence: audit/perspective-live-capability-coding-trace-2026-10-17.json.

2026-10-17: готовые результаты perspective и metadata apply отделены от доступности inference worker; мутации apply и публикации ручных линий перечитывают hard gate после await. Unit-сценарии написаны, не запускались. P0.2.09 implementing, 20/70. Evidence: audit/perspective-result-admission-coding-trace-2026-10-17.json.

2026-10-17: accepted журналы portrait/restoration доступны для авторизованного открытия без inference capability; new/unknown POST остаются заблокированными. Local portrait читает журнал до проверки доступности. Open-eyes сохраняется для восстановления. DOM-сценарии написаны, не запускались. 20/70; P0.2.09 implementing. Evidence: audit/portrait-accepted-recovery-coding-trace-2026-10-17.json.

2026-10-17: panorama/focus stack проверяют свежую готовность backend и актуальность route после await. Accepted receipt обходится без нового inference admission. Unit-сценарии написаны, не запускались. P0.2.09 implementing; 20/70. Evidence: audit/multi-backend-admission-coding-trace-2026-10-17.json.

2026-10-17: negative accepted receipt восстанавливается и открывается без доступного inference worker; hard editor gate сохранён. DOM-сценарий remount/open без повторного POST/snapshot написан, не запускался. P0.2.09 implementing, 20/70. Evidence: audit/negative-accepted-recovery-coding-trace-2026-10-17.json.

2026-10-17: run cancel/retry проверяют monitor/lifecycle перед POST после auth/journal await; при смене scope журнал сохраняется для восстановления. Unit-сценарии написаны, не запускались. 20/70. Evidence: audit/run-mutation-scope-coding-trace-2026-10-17.json.

2026-10-17: P0.3.04 — расширен API-инвентарь multi screen до admission, review, apply и CAS. Сценарий покрытия написан, не запускался. Пункт открыт, 20/70. Evidence: audit/multi-route-inventory-coding-trace-2026-10-17.json.

2026-10-17: P0.3.04 — объявлены четыре существующих multi admission/configuration endpoint и bounded DTO schemas, editor authority, 200/202. Сценарий написан; генератор OpenAPI и тесты не запускались. Collection version GET/PUT contracts остаются разрывом. 20/70. Evidence: audit/multi-admission-endpoint-contracts-2026-10-17.json.

2026-10-17: collection version GET/PUT контракты объявлены с editor authority; добавлены DTO и projector If-Match CAS/UUID/no retry ledger. Сценарии написаны; генератор и тесты не запускались. 20/70; P0.3.04 открыт. Evidence: audit/collection-version-endpoint-contracts-2026-10-17.json.

2026-10-17: контракт delivery signing discovery объявлен, API-инвентарь создания доставки дополнен; projector описывает repeated MIME JPEG/PNG. Сценарии написаны; генератор и тесты не запускались. P0.3.04 открыт, 20/70. Evidence: audit/delivery-signing-discovery-contracts-2026-10-17.json.

2026-10-17: delivery create saved ticket проверяется авторизованным workflow GET перед переходом; matching/foreign/revoked DOM-сценарии написаны, не запускались. Повторного POST нет. Полный модуль открыт, 20/70. Evidence: audit/delivery-ticket-authority-coding-trace-2026-10-17.json.

2026-10-17: устранены блокировки delivery recovery после временного отказа проверки ticket и ошибки навигации. GET-only retry сценарии написаны, не запускались. Модуль открыт, 20/70. Evidence: audit/delivery-recovery-retry-coding-trace-2026-10-17.json.

2026-10-17: исправлена reconstruction publication envelope: сохранены timestamps DTO, document исключён из data. Strict validator не ослаблен. Regression/fixture код обновлён, тесты не запускались. 20/70; delivery открыт. Evidence: audit/delivery-publication-envelope-coding-trace-2026-10-17.json.

2026-10-17: download token preflight строго валидирует опубликованный package DTO до IO. Malformed timestamps/revision/workflow/manifest сценарии написаны, не запускались. 20/70; P0.4.09 открыт. Evidence: audit/delivery-download-package-binding-coding-trace-2026-10-17.json.

2026-10-17: server download receipt decoder ограничивает JSON bytes, запрещает unknown fields/trailing JSON и чрезмерный expiry. Unit-сценарии написаны, не запускались. P0.4.09 открыт; 20/70. Evidence: audit/delivery-download-receipt-coding-trace-2026-10-17.json.

2026-10-17: redemption проверяет cancellation между DB/storage/sign/authority и expiry перед URL exposure. Unit-сценарии написаны, не запускались. P0.4.09 открыт, 20/70. Evidence: audit/download-redemption-cancellation-coding-trace-2026-10-17.json.

2026-10-17: strict canonical download bearer проверяется до DB. Добавлены сценарии cancellation during final authority и malformed bearer no-IO. Не запускались. P0.4.09 remaining: effective SQL expiry/one-use/revoke-after-sign/version substitution/cross-tenant coverage. 20/70. Evidence: audit/download-final-authority-coding-trace-2026-10-17.json.

### 2026-10-17 — Скорейший первый релиз: download expiry

Исправлен реальный кодовый дефект: выдача download intent проверяла срок по времени начала запроса, включая возврат после commit. Теперь время обновляется внутри транзакции и перед ответом; SQL требует живой срок и совпадение с сохранённой квитанцией; отменённый запрос после commit не получает bearer. Добавлен unit-сценарий истечения квитанции за время запроса. Тесты, сборки и миграции не запускались. P0.4.09 не закрыт; показатель остаётся 20/70 = 28,571429%. Расширенные lifecycle/SQL проверки остаются в тестовом backlog, не повод повторять полный аудит перед следующим модулем.

### 2026-10-17 — Download: роль и SQL lifecycle сценарии

DownloadIntentService приведён к нормативной роли editor вместо viewer; tenantRequest(mutate=true) проверяет mutation authentication/CSRF, а не роль, поэтому минимальную роль должен требовать сервис. Написан DB integration scenario viewer → FORBIDDEN до idempotency persistence. Для общей portal item ветки написаны сценарии одноразовости, истечения до consume и после подписания, отзыва session/link/permission, подмены object version и foreign tenant item/preset с вызовами от photo_portal. Фикстура теперь использует уникальные bearer hashes для одновременного существования двух tenants. Эти сценарии не подменяют положительную published-package фикстуру Delivery-kind, которая остаётся открытой. В API-инвентарь delivery detail добавлен фактический GET me перед durable journal. Ничего не запускалось. Показатель без изменения: 20/70 = 28,571429%.

### 2026-10-17 — Delivery: восстановление публикации при сбое metadata GET

Устранена блокировка retry: durable publication attempt восстанавливался из журнала, но после transient GET failure package оставался null, кнопка была скрыта и publish guard отказывал. Теперь явный повтор доступен без package только для уже существующего immutable attempt; body/key/revision сохраняются, fresh publication без загруженного package запрещена. При явном 401/403/404/410 metadata GET память retry очищается и показывается потеря доступа, а durable запись неизвестного исхода не удаляется. DOM-сценарии transient GET → exact-key/CAS POST → published/cleanup и forbidden GET → no replay написаны, не запускались. P0.4.08 остаётся открыт; показатель 20/70 = 28,571429%.

### 2026-10-17 — Delivery detail: восстановление account bootstrap

Маршрут delivery detail больше не остаётся на permanent error или бесконечном spinner после неудачного GET me. Добавлены explicit refresh, timeout 8 секунд, предварительная проверка tenant/project/package UUID до IO, abort при уходе в background и повторная авторизация при foreground return. Поздний ответ старого запроса не создаёт journal для текущего account/route. Журнал создаётся после успешного me и не удаляется при сетевой ошибке. Написаны пять DOM-сценариев: retry после network failure, invalid scope без IO, timeout/late response, account-client replacement, background/foreground. Тесты и сборки не запускались. P0.4.08 и P0.3.08 остаются открыты; 20/70 = 28,571429%.

### 2026-10-17 — Delivery create bootstrap и отмена refresh

Маршрут создания delivery теперь допускает повтор account read, проверяет scope до IO, имеет deadline 8 секунд, отменяет чтение в background и заново получает identity после foreground. Написаны пять DOM-сценариев. Связанный дефект ApiClient устранён: abort локального или shared refresh больше не вызывает auth.clear в JSON/binary request; обычный отказ refresh сохраняет прежнюю очистку. Уже отменённый JSON request не отправляется, AbortError не запускает transport retry. Написаны шесть unit-сценариев. Ничего не запускалось; показатель без изменения 20/70 = 28,571429%, модули остаются открыты.

### 2026-10-17 — Общий API: отмена backoff/CSRF/late JSON

Backoff теперь проверяет уже отменённый signal и удаляет abort listener как после таймера, так и после отмены. raw JSON request повторяет abort gate перед transport после ожидания CSRF, отменяет тело позднего response и не возвращает JSON после отмены во время decoding. Это закрывает общую связь cancel для delivery и других экранов, не заменяет модульные проверки. Написаны четыре unit-сценария: backoff abort без второго запроса/таймера, cleanup listener после retry, отмена CSRF без POST, late JSON rejection. Тесты/сборки не запускались. Показатель без изменения 20/70 = 28,571429%.

### 2026-10-17 — Отмена binary image stream

requestBytes после transport отбрасывает отменённый response до refresh/decoding и отменяет его body. Активный reader получает abort listener, который отменяет зависший read; после read повторяется signal gate, finally удаляет listener и освобождает lock. Написаны сценарии stalled reader cancellation и cancelled late 401 без refresh/logout. Ничего не запускалось. Текущий цикл исправлений транспорта закончен; далее обязательный session/collection путь, без расширения редких транспортных сценариев. Общий показатель 20/70 = 28,571429%, не подтверждённая готовность сборки.

### 2026-10-17 — Session scenes: account/origin isolation

SceneScope пересоздаётся при смене account API client/origin, поэтому старые locks/submitting/attempts не остаются в новом контексте, старый controller отменяется. Reference и Apply recovery используют auth.origin вместо build environment; SceneApplyPanel принимает origin от маршрута и включает его в scope. Collection GET и создание selection snapshot при ручном reference теперь получают lifecycle signal. Написаны три DOM-сценария: reference recovery при отличающемся build origin, отмена old-account submission/late ticket без перехода, Apply journal под текущим origin. Тесты/сборки не запускались. Показатель остаётся 20/70 = 28,571429%; session module открыт.

### 2026-10-17 — Session scenes: background/foreground recovery

SessionScenes использует общий useReviewForeground: background размонтирует SceneScope и дочерние Apply/media, отменяет lifecycle controller; foreground создаёт новую scope generation и заново читает account/journals/board. Durable uncertain attempt не удаляется; старый ответ не записывает ticket и не вызывает navigation. Написаны два DOM-сценария: отмена pending reference → foreground → exact body/key retry и initially background без account/board IO. Ничего не запускалось. Session module остаётся открыт, показатель 20/70 = 28,571429%.

### 2026-10-17 — Session main: account cancellation и подтверждённый recovery gap

SessionScope пересоздаётся по account client/origin вместе с session/tenant. Старый analyze controller отменяется; late ticket не вызывает navigation. Collection GET и selection snapshot preparation получают общий lifecycle signal. Написаны два DOM-сценария. Обнаружен обязательный кодовый разрыв P0.4.02: initial analysis attempt хранится только в useRef; требуется durable pre-send body/key journal и accepted ticket recovery до разрешения restart/background восстановления. Не подменять этот разрыв простым unmount/очисткой памяти. Проверки не запускались, показатель 20/70 = 28,571429%.

### 2026-10-17 — Initial session analysis: durable store

Добавлен SessionAnalysisAttemptStore на существующем semanticJournalLock: строгие scope/request/ticket поля, initial-only пустой manual_references, immutable body/key, монотонное подтверждение ticket, discard только неподтверждённого запроса, logout epoch/generation fencing. Очистка подключена к AuthProvider. Написаны четыре unit-сценария persistence/receipt, changed request/key/scope refusal, manual reference refusal и logout. Store ещё не подключён к SessionPage: это следующий обязательный кодовый шаг, разрыв recovery не закрыт. Ничего не запускалось, показатель 20/70 = 28,571429%.

### 2026-10-17 — Initial session analysis: подключение journal к экрану

SessionPage получает текущего пользователя, восстанавливает journal до чтения списка, сохраняет body/key до analyze POST и ticket до navigation. Повтор читает journal, сверяет immutable body/key и не создаёт новый snapshot; accepted ticket проверяется текущим GET run, затем переход и exact key/run cleanup. При metadata load failure сохранённая попытка имеет отдельную кнопку retry. Definitive sent rejection удаляет только неподтверждённую запись; ambiguous ошибки сохраняют её. Написаны три route DOM сценария и exact accepted cleanup unit case; прежние route fixtures изолируют store mock, реальные UUID/journal integration остаются тестовой фазе. API-инвентарь включает me/run/collection/snapshot/list analyses. Ничего не запускалось, session module открыт, 20/70 = 28,571429%.

### 2026-10-17 — Initial session analysis: foreground и real-journal route scenarios

SessionPage размонтирует scope в background, отменяет активный controller и создаёт новую scope generation при foreground; journal сохраняет uncertain body/key. Написаны два lifecycle сценария. Новый session-analysis-recovery.dom.test.tsx использует настоящий SessionAnalysisAttemptStore с valid UUID и memory AsyncStorage (заменена только межвкладочная lock граница): persist-before-POST/exact remount replay, storage failure без POST, сохранённый accepted ticket после ошибки GET run без повторной отправки. Все пять сценариев только написаны, не запускались; никакого PASS не заявляется. Полный session server/worker/result/inventory остаётся открыт. Показатель 20/70 = 28,571429%.

### 2026-10-17 — Session Analyze: bound admission receipt

Service.Analyze теперь пропускает fresh/pending/sealed replay через admitAnalysis с проверкой deterministic RunID, WorkflowID и queued state. Добавлены ctx gates до probe, после probe, до/после Admit; отменённый результат не возвращает ticket. HTTP маршрут прекращает ответ при отменённом context. Написаны шесть сценариев подмены run/workflow/state на pending/replayed и три cancellation stage сценария. Координаторные исходники подтверждают queued как форму admission receipt; runtime не проверялся. Ничего не запускалось, session module открыт, 20/70 = 28,571429%.

### 2026-10-17 — приоритет скорейшего первого релиза, session Apply

Сохраняется порядок из `first-release-execution-order-2026-10-16.md`: обязательный код MVP и модульные сценарии сейчас, глубокое тестирование после закрытия кодовых разрывов. В `apply_analysis.go` добавлены cancellation gates до зависимостей/запуска и после commit/replay; replay без workflow ID отклоняется. В `apply_analysis_test.go` написан сценарий отмены до admission. Тесты и сборки не запускались. P0.4.02 остаётся открыт; процент не повышен: 20/70 = 28,571429% остаточного плана.

### 2026-10-17 — Scene Apply: accepted receipt recovery

Подтверждённый ticket проходит текущий authenticated GET run и exact ID check до navigation. Ошибка проверки/перехода повторяет только GET, без ticket downgrade и нового Apply POST. Восстановление уже принятого run не блокируется более новым board; неподтверждённый старый plan по-прежнему отклоняется. HTTP Apply не выдаёт ответ после отмены context. Два DOM-сценария написаны, не запускались. Session module остаётся открыт, показатель 20/70 = 28,571429%.

### 2026-10-17 — Parent scenes accepted recovery

Родительские пути восстановления reference/Apply проверяют текущий GET run и точный ID перед переходом. Accepted Apply сохраняется в retry state до проверки; reference retry перечитывает journal и не отправляет POST при уже принятом ticket. Реестр маршрута дополнен фактическими API зависимостями. Два DOM-сценария восстановления после ошибки GET написаны, не запускались. Полный session module остаётся открыт: 20/70 = 28,571429%.

### 2026-10-17 — Session recovery: access revocation / rejection boundary

Initial recovery 401/403/404 скрывает metadata и retry refs, durable journal сохраняется. Отказ чтения принятого run не считается отказом POST: reference/Apply discard выполняется только для отправленного неподтверждённого запроса; после записи accepted ticket sent сбрасывается. Панель также сохраняет accepted retry при GET conflict. Написаны два DOM-сценария access revocation и GET conflict, ничего не запускалось. Чтением worker service подтверждены CPU session.plan registration и model-gated session.describe registration; это не runtime evidence. Session module открыт; 20/70 = 28,571429%.

### 2026-10-17 — Scene board: final authorization and adoption check

После object IO сервер повторно проверяет project authorization и exact adopted artifact ID/SHA/bucket/key/version/bytes, ready/verified/expiry, workflow v2.2/run review state и artifact link. Entry/final cancellation возвращает пустой результат. Написан unit cancellation case, не запускался. Интеграционные сценарии отзыва ACL/истечения artifact во время IO остаются обязательными в глубокой тестовой фазе. Session module остаётся открыт, 20/70 = 28,571429%.

### 2026-10-17 — Session plan worker cancellation

В session.plan добавлены active lease checks перед inputs, каждым shard, grouping, каждой correction group, stage correction и publication root. Отмена не публикует финальный review result; алгоритмы группировки/коррекции сохранены. Два unit-сценария написаны, не запускались. Исходники workflow/service.go подтверждают подключение orchestrator/materializer и readiness gates. Полная coding closure session остаётся неподтверждённой; 20/70 = 28,571429%.

### 2026-10-17 — Shared session object reader

Plan/frozen input/corrections reader теперь требует explicit nonempty key/version (не null), зависимости и active context до IO; после отменённого IO bytes не выдаются. Написаны три unit-сценария pinned metadata/late cancellation/valid canonical read. Ничего не запускалось. Editor authorization в Analyze подтверждена по исходнику. Client session capability display/admission inventory остаётся конкретным открытым пунктом session closure. 20/70 = 28,571429%.

### 2026-10-17 — Fresh session capability admission

Initial/reference fresh analysis проверяет текущие capabilities session.describe и session.plan до создания selection snapshot. Общая строгая operationAvailable используется для обеих операций; поздний ответ при отмене отклоняется. Persisted pending/accepted recovery не блокируется клиентской проверкой новых операций. Два unit-сценария написаны, route fixtures и API inventory обновлены. Тесты не запускались; 20/70 = 28,571429%, session module открыт.

### 2026-10-17 — Session fresh/recovery boundary scenarios

Исправлена capability fixture основного session route. Два DOM-сценария: unavailable fresh analysis не читает collection/не создаёт snapshot/не отправляет POST/не пишет journal; persisted uncertain attempt повторяет точное тело/ключ без new-operation capability check. После durable accepted ticket initial analysis сбрасывает sent, поэтому GET run failure не классифицируется как mutation rejection. Ничего не запускалось. Session module открыт; 20/70 = 28,571429%.

### 2026-10-17 — Main session project edit permission

Session route читает server projectAccess и скрывает lifecycle/policy/new-analysis controls для viewer проекта, сохраняя history и accepted-run recovery. Save/fresh analysis guards учитывают canEdit. Написан viewer DOM-сценарий, fixtures и API inventory обновлены. Ничего не запускалось; next: scene-board mutation controls. Session module открыт; 20/70 = 28,571429%.

### 2026-10-17 — Scenes project viewer controls

Scenes читает projectAccess до board и сохраняет explicit canEdit. Viewer получает board без reference callback и Apply panel; explicit viewer pending mutations защищены handler guard. Accepted-run recovery выполняется отдельно. Написан DOM-сценарий viewer props/actions, API inventory обновлён. Ничего не запускалось; полный session state/scenario inventory ещё открыт, 20/70 = 28,571429%.

### 2026-10-17 — Lifecycle durable recovery and definitive rejection

После sent lifecycle POST окончательные 409/410/412/422 очищают только matching key journal attempt; неопределённая сеть сохраняет запись. В persistent policy editor DOM suite написан сценарий с настоящим SessionPolicyDraftStore: pre-send close request восстанавливается при новой server revision, повторяется с прежними body/key и очищается после подтверждения. Тесты не запускались. Session module открыт; 20/70 = 28,571429%.

### 2026-10-17 — Atomic lifecycle journal reservation

reserveLifecycle объединяет read/check/first write в existing store queue; точный повтор не перезаписывает запись, другой attempt или policy draft отклоняется. Написан unit с реальным store и конкурирующими reservations. Тесты не запускались. Межвкладочная блокировка общего policy journal ещё не решена; этот шаг подтверждает только queue-local serialization. 20/70 = 28,571429%.

### 2026-10-17 — Shared policy journal lock and immutable attempt

Store operations используют semanticJournalLock. Late ordinary write не может удалить/сменить существующий attempt; explicit rejection требует matching key, persistence release пути обновлены. Unit stale write scenario написан. DOM suites заменяют browser lock boundary; межвкладочное исполнение и persistent logout-generation fencing остаются открытыми. Ничего не запускалось, 20/70 = 28,571429%.

### 2026-10-17 — Persisted policy logout/revocation fencing

Общие origin/scope generation markers меняются под shared lock до удаления draft records. Старый store отказывает записи и не читает draft после смены поколения; stale clear не удаляет новые данные. Написан сценарий внешней смены поколения, assertions считают пользовательские records отдельно от tombstones. Реальное multi-tab выполнение не проверялось; тесты не запускались. 20/70 = 28,571429%.

### 2026-10-17 — Session state inventory / stale clear

Обычный clear не удаляет reserved attempt; explicit discard требует оригинальный key. Написан unit-сценарий. Current source map содержит 12 state families с implementation/scenario paths и текущими SHA. Остался final descriptor scenario review перед coding closure; фактические тесты не запускались. 20/70 = 28,571429%.

### 2026-10-17 — P0.4.02 Session coding closure (unverified)

Final descriptor review: frozen source/consent/model lock gates присутствуют. Добавлены active checks после inference и перед faces/root publication, написаны два unit-сценария. Current source map обновлён для 12 state families. Известные обязательные кодовые разрывы данного пункта закрыты; статус coding_closed_unverified. Тесты/сборки/миграции не запускались; runtime, privileges, concurrency/multi-tab, physical storage, GPU/quality остаются обязательными тестовыми gates. Засчитан один фиксированный пункт: 21/70 = 30%, прирост 1,428571 п.п. остаточного плана, не всей системы.

### 2026-10-17 — P0.4.03 collection lifecycle wiring

Collection scope пересоздаётся по account client/origin/foreground; background отменяет запросы. Snapshot/contact-sheet POST/moderation получили lifecycle signal. Contact-sheet API допускает caller-owned key для следующего durable recovery шага. Два DOM-сценария написаны, не запускались. Remaining: durable contact-sheet request/ticket recovery, project permissions, full module inventory. 21/70 = 30%.

### 2026-10-17 — Collection project permissions / revoked access

Collection проверяет server can_edit для mutation controls/handlers. 401/403 (и missing authoritative collection) очищают metadata/items/links и отменяют остаточные запросы; thumbnail 404 не приравнивается к отзыву доступа. Два DOM-сценария написаны, API inventory расширен. Ничего не запускалось; durable contact-sheet recovery остаётся следующим кодовым разрывом. 21/70 = 30%.

### 2026-10-17 — Contact sheet expired snapshot replay boundary

Selection expiry перенесена внутрь fresh idempotency callback; текущие owner/project Editor checks сохраняются перед replay. Expired snapshot не блокирует уже сохранённый accepted response. Entry cancellation/HTTP cancelled response gate добавлены, unit написан. Real-journal DOM и expired-selection replay SQL fixture ещё требуются; тесты не запускались. 21/70 = 30%.

### 2026-10-17 — Real-journal contact sheet route scenarios

Три DOM-сценария используют реальный ContactSheetAttemptStore и memory AsyncStorage: pre-send persistence/exact remount retry, accepted run recovery без второго POST, storage failure без POST. Shared browser lock boundary заменена явно. Тесты не запускались. Remaining: expired-selection replay SQL fixture и полный collection server/worker/state inventory. 21/70 = 30%.

### 2026-10-17 — Contact-sheet receipt recovery

Повтор принятого contact-sheet запроса возвращает сохранённый receipt до подготовки шрифта/объектного хранилища, после текущей tenant/project авторизации и сверки точного тела. Добавлен интеграционный сценарий истёкшего snapshot, отсутствующего ResourceManager, конфликта тела и архивирования проекта. Сценарии не запускались; итоговый инвентарь Collection остаётся открыт. Процент без изменения: 21/70 = 30%.

### 2026-10-17 — Collection comparison access loss

Исправлено подавление отказа авторизации при пакетной модерации: 401/403/404 передаются родительскому экрану, дальнейшие записи прекращаются. Сравнение preview передаёт 401/403 наверх и отменяет соседние чтения; 404 отсутствующей производной остаётся локальной ошибкой. Добавлены unit/DOM сценарии, не запускались. P0.4.03 остаётся implementing до итогового инвентаря; 21/70 = 30%.

### 2026-10-17 — Contact-sheet worker cancellation

Проверка lease добавлена на входе, перед каждым preview, перед staging страницы и manifest. Написаны сценарии отмены до чтения параметров и во время декодирования без staging; шрифт fixture привязан к bundled NotoSans из репозитория. Инвентарь Collection API дополнен me/run/editor-draft/media-links/color-label. Тесты не запускались, P0.4.03 открыт, процент 21/70 = 30%.

### 2026-10-17 — Collection previews beyond initial window

Устранён разрыв: превью кадров после первых 24 запрашиваются при появлении в видимой области FlatList. Общая очередь ограничивает параллелизм четырьмя запросами, устраняет дубли и не запускает ожидающие запросы после отмены scope. Написаны unit-сценарии параллелизма, отмены и ошибки одного чтения; не запускались. Прочитаны server CAS/moderation и selection authorization; полный инвентарь Collection ещё открыт. 21/70 = 30%.

### 2026-10-17 — Selection receipt collection binding

Исправлен реальный дефект Selection.Create: legacy request_hash не включал collection_id из URL; одинаковый ключ и тело могли вернуть snapshot другой доступной коллекции. Перед возвратом receipt проверяются tenant, collection, principal и kind сохранённого snapshot, без изменения существующих ключей/хешей. Написан PostgreSQL сценарий корректного legacy replay, отказа cross-collection replay и сохранности оригинального receipt; не запускался. P0.4.03 остаётся implementing; 21/70 = 30%.

### 2026-10-17 — Collection archived/in-flight states

Архивная коллекция доступна для чтения с отключёнными свежими мутациями. Apply navigation и создание contact-sheet блокируются при moderation/label mutation. Новый contact-sheet использует locale пользователя, replay сохраняет исходную locale. Написаны DOM сценарии archived/read-only и in-flight moderation; не запускались. P0.4.03 остаётся implementing; 21/70 = 30%.

### 2026-10-17 — Collection thumbnail expiry

Thumb640 принимается только для точного asset_version_id и с конечным будущим expires_at. Scope-таймер удаляет URL после истечения, abort отменяет все таймеры; Reload получает свежие ссылки. Написаны DOM сценарии чужой/истёкшей ссылки и удаления отображаемого thumbnail после срока; не запускались. P0.4.03 открыт, 21/70 = 30%.

### 2026-10-17 — Contact-sheet results on run screen

Подключены contact_page PNG результаты к terminal export run через существующий authorized runOutputs API: явное открытие, проверка формата/срока, удаление истёкших ссылок, Refresh, abort и callback потери доступа. DOM сценарии написаны, не запускались. Итоговый инвентарь ещё открыт; 21/70 = 30%.

### 2026-10-17 — Contact-sheet canonical page ordering

PNG упорядочиваются по SHA/size-проверенному canonical manifest, а не порядку SQL rows. Использован существующий bounded stream reader с contact_manifest лимитом 262144 байта. Проверяются точные output hashes, последовательность страниц/offsets и total_items. Добавлены parser unit-сценарии, адаптированы DOM-сценарии; не запускались. 21/70 = 30%, итоговый инвентарь открыт.

### 2026-10-17 — Immutable contact PNG links

Подтверждён FK runs.input_snapshot_id → snapshots: snapshot существующего run не может быть физически удалён GC. Лишний fallback не добавлялся. PNG URL проверяется на единственный непустой versionId, HTTPS (HTTP только loopback), отсутствие credentials/fragment и будущий срок; проверка повторяется перед открытием. Написаны unit-сценарии, не запускались. 21/70 = 30%.

### 2026-10-17 — Collection project entry route

Охват дополнен списком коллекций проекта. Исправлены загрузка только первых 100, отсутствие отмены при смене scope и отсутствие can_edit gating создания. Pagination проверяет принадлежность project, дубли и cursors; написаны unit-сценарии. Конкретный остаток: durable create body/key/receipt recovery, DOM состояния входного маршрута, итоговая source map. Ничего не запускалось; 21/70 = 30%.

### 2026-10-17 — Durable collection creation

Добавлен project/user/tenant/origin-scoped журнал создания: неизменяемые body/key до POST, монотонный accepted collection ID, authoritative GET созданной коллекции перед exact cleanup, повтор с прежними параметрами, logout fencing/cleanup. Написаны unit-сценарии; не запускались. Остаток: entry-route DOM recovery/access/lifecycle и итоговый source inventory. 21/70 = 30%.

### 2026-10-17 — Project collection create DOM/lifecycle

Написаны шесть DOM сценариев с реальным journal: exact uncertain retry, accepted GET-only recovery, persistence failure, viewer, late response при смене project, initial background. Project scope пересоздаётся по account/origin/project/foreground; отмена проверяется до user/journal чтений. Тесты не запускались. Остаток P0.4.03: итоговая current source map входного/detail/result маршрутов. 21/70 = 30%.

### 2026-10-17 — P0.4.03 coding closed, unverified

Зафиксирована current source map: project entry/create, gallery/pagination/preview, comparison/CAS, selection receipt binding, contact recovery/resources/worker/result manifest. Исправлен accepted-create GET recovery для viewer. Run result scope привязан к account/origin/foreground; API inventory обновлён. Сценарии написаны, ничего не запускалось. Исторические PASS не использованы. Учтена одна фиксированная единица: 22/70 = 31,428571%. Следующий модуль — P0.4.05 Apply v4.

### 2026-10-17 — P0.4.05 Apply v4 started

Добавлены ACL/archived guards, account/origin/foreground scope, recovery сохранённого attempt независимо от styles catalog, accepted-run GET до navigation, reload catalog после acknowledged clear. DOM сценарии обновлены/написаны, не запускались. Остаток: definitive rejection recovery, server/state-machine/worker inventory, lifecycle scenarios. 22/70 = 31,428571%.

### 2026-10-17 — Apply definitive rejection recovery

Явный allowlist кодов admission разрешает exact pending-key discard и повторное authoring; unknown/network/idempotency/dependency errors сохраняют identity. Accepted receipt не удаляется discardRejected. Написаны unit/DOM сценарии, не запускались. Остаток: server/state-machine/worker inventory и lifecycle/authorization scenarios. 22/70 = 31,428571%.

### 2026-10-17 — Apply accepted styles/access revocation

Версии без approval_status=accepted отключены и не проходят preflight. Общая revoke обработка 401/403/404 отменяет scope, очищает каталог/поля/receipt UI и блокирует admission; durable journal сохраняется. Active guard pagination учитывает abort. Написаны два DOM сценария, не запускались. 22/70 = 31,428571%.

### 2026-10-17 — Identity Apply override precedence

Исправлен style.resolve: индивидуальный override больше не удаляет общие job modules при identity Apply. Объединяются non-null поля, индивидуальные имеют приоритет. Написан regression с отличающимися от identity RGB gains/saturation и индивидуальной strength; не запускался. 22/70 = 31,428571%.

### 2026-10-17 — Apply extension merge alignment

Worker теперь объединяет явно заданные extension modules с унаследованными, как Go EffectiveApplyV4Style, вместо замены всего раздела. Написаны worker regression и Go planner preservation сценарии для сохранения restoration при другой индивидуальной extension поправке. Не запускались. 22/70 = 31,428571%.

### 2026-10-17 — Apply identity без overrides

Исправлен отказ style.resolve на допустимом нейтральном Apply: пустой patch теперь получает явную нейтральную strength=1. Добавлен сценарий с проверкой канонического ResolvedStyle и отключённых модулей; в двух предыдущих fixture добавлена обязательная revision. Тесты не запускались. P0.4.05 остаётся implementing; 22/70 (31.428571%) без изменения.

### 2026-10-17 — Уточнение identity и разрыв strength

Предыдущий вывод о допустимости identity без overrides отменён: ResolveParams требует explicit modules. Нейтральный fallback удалён, сценарий проверяет INPUT_INVALID и отсутствие staged outputs. Найден незакрытый разрыв P0.4.05: backend/internal/run/apply.go сохраняет request strength в run config, но ApplyCoordinator.build не декодирует это поле и не передаёт его в style.resolve. Требуется согласовать приоритет с job/item strength и frozen style до исправления. Тесты не запускались; 22/70 без изменения.

### 2026-10-17 — Передача request strength

ApplyCoordinator декодирует frozen request strength и задаёт её как исходный job module для style.resolve. Приоритет: per-image strength > explicit job strength > request strength > исходный style при отсутствии поля в историческом run. Явный 0 сохраняется; null не перекрывает значение; остальные job modules сохраняются. Невалидный request strength отклоняется до публикации task. Добавлены Go-сценарии приоритетов, диапазона и старых конфигураций. Тесты не запускались; весь Apply пока не закрыт; 22/70.

### 2026-10-17 — Уточнение Metal по указанию пользователя

При наличии совместимой альтернативы использовать её вместо отключения функции. README содержит фактическую матрицу MPS, CPU и ещё не подключённых Core ML alternatives. Общий _select_cuda_only_backend остаётся открытым разрывом: заменить пооперационным выбором backend после инвентаризации зависимостей. Квалификацию выполнять в фазе глубоких тестов; не считать описанные альтернативы реализованными.

### 2026-10-17 — Resolver lifecycle и фикстуры

Два новых identity-сценария теперь открывают реальную ScratchSpace, требуемую stage_contract. Добавлены сценарии отмены на progress 0/3 без staged outputs, а также frozen request strength с индивидуальным override и явным нулём. Не запускались. Metal: подтверждены дополнительные CUDA-проверки region renderer и learning installer; одного удаления общего selector недостаточно, adapters требуют согласованной доработки. 22/70 без изменения.

### 2026-10-17 — Job modules в admission и графе Apply

Исправлено игнорирование job overrides при определении ресурсов и effective graph style: merge выполняется до per-image override. Local включён в mergeOverrideNeeds, чтобы person-mask requirements учитывали его включение. Добавлены сценарии наследования и приоритета. Не запускались; Apply остаётся implementing, 22/70.

### 2026-10-17 — Nullable override modules

Планировщик v4 и admission сохраняют унаследованные модули при top-level null, как Python resolver. Ранее extensions:null отклонялся планировщиком, а local:null мог терять семантику наследования. Null base style теперь отклоняется до изменения map. Добавлены Go-сценарии, без запуска. 22/70; Apply implementing.

### 2026-10-17 — Замена named modules в admission

Исправлен mergeOverrideNeeds: JSON больше не декодируется поверх прежней структуры. Named module сначала декодируется в новую typed структуру, затем заменяет прежнюю; отсутствующие поля не сохраняют старые требования upscale/dust/defects. Неназванные extension modules сохраняются. Добавлен регрессионный сценарий без запуска. 22/70; Apply остаётся открыт.

### 2026-10-17 — Admission job override

Добавлены ранние проверки strength: JSON number, finite, [0,1]; остальные modules обязаны быть объектами. Trailing JSON отклоняется. Добавлены негативные сценарии. Исправлен worker fixture restoration: обязательны denoise_strength и sensor_dust. Полная nested semantic validation остаётся предметом сверки; тесты не запускались, 22/70.

### 2026-10-17 — Параллельный аудит Apply lifecycle

Найдены два P1: частичные записи при false в adopt_apply_results; неверный pre-composition baseline для quality после crop/outpaint без upscale. Зафиксированы в audit/apply-current-open-defects-2026-10-17.json. Первый исправляется отдельной forward-only миграцией и regression; второй требует geometry-aware baseline, запрещено обходить проверку self-baseline. Общий прогресс 22/70, тесты не запускались.

### 2026-10-17 — Shape extensions до admission

Job extensions теперь проверяются по закрытому набору имён portable Extensions; неизвестные поля, неверные enabled unions, не-object модули и region_adjustments вне array≤128 отклоняются до capability lookup. Nullable named extensions сохраняют семантику контракта. Это shape validation, не полная проверка числовых/модельных вложенных параметров. Сценарии написаны, не запускались; 22/70.

### 2026-10-17 — Интеграция параллельных исправлений Apply

Подготовлены02441 atomic adoption,02442 cancel-aware failure и composition quality с exact adopted target без self-baseline. Добавлены SQL graphv3/v4/гонка отмены, клиентские late-response/401/403 сценарии. Restoration job override теперь проверяет все обязательные поля, enum и диапазон denoise_strength. Статический review выполнен; миграции не применены, тесты не запускались. Три дефекта coding_fixed_unverified; полная nested validation и reconciliation ещё открыты; 22/70.

### 2026-10-17 — Apply review discovery

Run screen теперь показывает RunReviews и для kind=apply, позволяя открыть review композиции/дефектов без ручного поиска. Добавлен DOM-сценарий перехода, API inventory Apply дополнен GET routes. Тесты не запускались; full validation в работе; 22/70.

### 2026-10-17 — Полные job module validators

Подключены validApplyJobColor (GlobalColor/residual LUT), validApplyJobModule (local/beauty/defects/geometry) и validApplyJobExtension (extensions кроме отдельно проверяемой restoration). Проверяются обязательные/неизвестные поля, диапазоны, типы, enums, cross-field инварианты и defaults. Добавлены граничные и API integration сценарии, без запуска. Cross-module dependencies и финальная lifecycle сверка остаются открыты; Apply не закрыт; 22/70.

### 2026-10-17 — Зависимость generated background

После объединения style/job/item модулей admission отклоняет background.generate без enabled generation, как StyleV2 worker. Написан сценарий отключения generation override и переключения background на keep. Extension dispatcher использует strict object parser для duplicate/unknown/trailing rejection и делегирует полные модульные проверки. Тесты не запускались; 22/70, cross-module audit продолжается.

### 2026-10-17 — Run review ACL teardown

RunReviews сообщает 401/403 родительскому экрану; тот отменяет stream/recovery и очищает run/actions. 404/410 остаются ошибкой review list, не трактуются как общий отзыв доступа. Написаны callback-сценарии, API inventory run дополнен me/reviews. Без запуска; 22/70.

### 2026-10-17 — Region admission recovery

Клиент распознаёт подтверждённые rollback codes REGION_MASK_REQUIRED, REGION_MASK_GEOMETRY_UNSUPPORTED, OVERRIDE_MASK_INVALID(409), REGION_MASK_LIMIT_EXCEEDED(422) как definitively rejected: pending identity можно снять и исправить запрос. Сетевые/неизвестные ошибки сохраняют journal. Добавлены сценарии без запуска. Explicit masks+body warp пока безопасно отвергаются и остаются coding gap; это не полное завершение regions. 22/70.

### 2026-10-17 — Masking model dependencies

Effective masking slot теперь учитывается в admission: nonidentity style обязан содержать привязки scene/face/interactive моделей до запуска worker. Null очищает slot и требования. Написан regression. Import остаётся открыт из-за archived finalization после Originals.Prepare; Apply external masks+body transform реализуется. Без выполнения тестов, 22/70.

### 2026-10-17 — Shared auth identity integration

Найден конкретный интеграционный разрыв subscription→memoized client: useAuth с прежней object identity не инвалидирует useMemo([auth]) у routes. Передано исправление per-session facade с bound методами и hook/client regression. Не считать account isolation закрытой только по subscription. Import archive settlement и Apply mask transforms продолжаются. Без тестирования; 22/70.

### 2026-10-17 — Review lifecycle integration

Review detail now unmounts authorized media/actions in background and on auth identity changes; foreground reloads current authority. Mutation access loss clears media/context without deleting durable relight decision journals. Selected-preview authorization failures propagate to the loading boundary. DOM scenarios authored for background/foreground, account replacement and 401/403/404/410 decisions; not executed. P0.4.06 remains implementing pending whole Review linkage/recovery closure; progress unchanged at 22/70.

### 2026-10-17 — Import whole coding closure integrated

P0.4.01 coding_closed_unverified. Reviewed current source map and final archived destination SQL/repository/cancellation integration, including project without collection. Source map covers upload/inspection/decode/original/catalog/failure/recovery/confirmed staging cleanup; authored scenarios include usable live adoption, settled replay, archived refusal, quota settlement and preserved originals. Execution, migrations, object storage and real photo quality remain unverified. Fixed residual MVP progress: 23/70 = 32.857143%.

### 2026-10-17 — Editor preview pool admission

Corrected interactive preview capability lookup to media resource class with CPU backend, matching migration00164 and operation registry. Generic CPU-pool capacity does not substitute media worker installation. Updated authored admission scenarios; no execution. Full extended recipe preview linkage remains a concrete Editor coding gap, assigned to Editor agent. Progress unchanged at23/70.

### 2026-10-17 — Apply regional batch integration

Source-reviewed regional transform batch, exact source/target/provenance binding, synchronous staged uploads and released sequential scratch,62-unique-mask cap, declaredresourceprofile and02444 timeout. Region defect coding_fixed_unverified; no execution. Whole Apply stays implementing: generationsteps andconservative_auto must not silently fall back tograph3; body+portrait linkage pending agentpass. Progress23/70 unchanged.

### 2026-10-17 — Regional gateway publication capacity

Integration found original mask.transform policies still limited eachrole to1 despite batchworker. Forward-only02444 now allows63 transformed masks and63 PNGs, preserving per-file bounds and single rootmanifest; maximum batch127outputs stays below128gateway cap. Authored actual begin_output ticket scenario for63successes perrole,64th rejection, singlemanifest, MIME refusal andexactreservation254bytes. Not executed; wholeApply remainsimplementing.

### 2026-10-17 — Download retryable lookup failures

DownloadIntentService target and pinned-intent lookups no longer convert infrastructure errors/cancellation into definitive DOWNLOAD_REVOKED or DOWNLOAD_INTENT_INVALID. Only pgx.ErrNoRows maps to refusal; malformed or mismatched successful rows remain refused. Authored cancellation/deadline/outage/wrapped-no-row scenarios, unexecuted. Positive published Delivery SQL lifecycle fixture still pending; P0.4.09 remains open,23/70 unchanged.

### 2026-10-17 — Published Delivery manifest scenarios

Authored real SQL Delivery-kind manifest bearer lifecycle with privileged persisted state fixture: first prove successful issuance, then assert pinned version andone-use, late expiry/projectarchive/download/session/publication/objectversionrevocation, and foreignactor/tenant refusal. No function mocked or SQLgate bypassed; fixture is not proof of assembly or slotexport/signingcausality. Unexecuted. P0.4.09 remainsopen forslot/signing/postsign scenarios; progress23/70.

### 2026-10-17 — Review whole closure and Download slot coverage

P0.4.06 coding_closed_unverified after integration review of exact server Plan hash recovery and physical detail pins, paging and lifecycle scope. Generic decision safety uses immutable CAS/hash rather than assuming server idempotency-key consumption. Runtime gates retained. Progress24/70=34.285714%. Authored positive provenance-off Delivery slot lineage and late canonicalresult/adoption/producerfence refusal; no execution and no Download closure, requiredsigning/postsign remains.

### 2026-10-17 — Download post-sign scenarios

Authored current-authority scenarios for consumed Delivery manifest/slot bearer: exact storage coordinates and SHA/size/MIME, late download revoke, one-use preservation and restored caller tenant context. No execution; required-signing positive closure fixture still pending. Residual progress24/70 unchanged.

### 2026-10-17 — Conservative operation registration

Added distinct CPU defects.auto_repair and sensor.dust_auto_repair v1 in both executable catalogs, sharedconservativeworker module and explicit Params/Report names. Forward-only02446 registers operation/outputpolicies with requiredexistingcontractpreconditions; same media/byte/count bounds, reviewedoperationversions untouched. Authored DBregistration/privilege scenarios, unexecuted. Fullengine/admission/replay remains agentowned; no closure credit.

### 2026-10-17 — Required-signing manifest authority scenarios

Authored positive required-signing manifest with deterministic signingpolicy artifact/backendpublication and retainedclosure digest; late policyexpiry/closureremoval blocks post-sign authority and newintent issuance. Fixture seeds persistedstate, not physical signing or assembly proof. Signedslot producer/result/closure lineage stillpending; testsunexecuted,24/70 unchanged.

### 2026-10-17 — Signed slot lineage and current-attempt fence

Authored positive signedslot with parentexport, signerworkflow/task/result, provenance receipt, physical artifactattempt/fence and retainedclosure pins. Static source found downloadtarget missing signerresult↔currenttaskattempt/fence equality; forward-only02448 adds it without replacing otherauthority checks. Authored stalecurrenttaskfence, cancelledsigner, changedphysicalfence andexpiredpolicy post-sign refusal. Tests/migrations unexecuted; Download stillimplementing pending finalsource reconciliation/runtimeproof;24/70 unchanged.

### 2026-10-17 — Download post-sign infrastructure errors and catalog source

Portal post-sign authority lookup now returns infrastructure/cancellation error unchanged, never a URL; false authority still maps to normal denial. Added service scenarios asserting recheck was reached and noURL exposed, unexecuted. Canonical docs/implementation/contracts/operations.json now retains newautoops/masktimeout for futuregenerator; no generator execution. Residual24/70 unchanged.


### Интеграционный проход: automatic repair interchange — 2026-10-17

Добавлено строгое различение `defects.auto_repair` / `sensor.dust_auto_repair` по `conservative_auto_v1` и точному набору полей Params. Неизвестная policy, смешение automatic/reviewed полей и пропущенные обязательные поля отклоняются до generic fallback. Native operation schema whitelist расширен только на эти две версии v1; проверка установленной operation authority и текущего producer attempt/fence остаётся обязательной. Написаны сценарии распознавания и отклонения смешанных контрактов, не запускались.

Открыто: полный Go import remap/authority для DefectProposeReport, вложенных FaceLandmarks/MaskRef и диагностического PNG; worker closure согласуется параллельно. Частичное изменение не закрывает единицу реестра и не меняет 24/70.


### Продолжение automatic repair interchange — 2026-10-17

Добавлены Go-кодеки automatic Params и DefectProposeReport, подключение к typed router и проекция зависимостей V2. Params проходят исходную operation authority до resolve и целевую authority после remap. Отчёт переносит proposal, label_png, protection, permanent/additional/skin/manual masks, FaceLandmarks и согласованные object IDs; сохранена разница между текущим изображением и историческим источником landmarks. Подтверждения фотографа не создаются. Маски повторно квалифицируются при приёме подготовленного отчёта. Селектор не перехватывает посторонние metadata/provenance Params с полем policy.

Написаны сценарии типизированного remap, отказа исходной/целевой authority, недоступного PNG/report, неперенесённых ссылок, отмены и смешения с reviewed-контрактом. Сценарии не исполнялись; сборка не проверялась.

Осталось до закрытия этого пути: установить production scoped DefectProposeReport authority; закрыть prepared FaceLandmarks/FaceBoxes ancestry и физический DefectLabelPNG verifier/роль в Go; связать новые роли с каталогом и startup. Кодек отчёта включается только при установленной authority, иначе импорт отклоняется. Это промежуточная реализация, не готовый end-to-end interchange. Прогресс остаётся 24/70.


### Scoped report authority и prepared mask closure — 2026-10-17

Добавлена entry-scoped обёртка DefectProposeReport authority: исходный отчёт должен совпасть с проверенными bytes конкретного archive inventory; целевая проверка читает подготовленный DefectCandidates и master, проверяет защиту через общий review parser, вызывает независимую semantic/physical authority и повторно сверяет source/proposal после IO. Factory связывает эту обёртку с tenant/workflow/entry scope. Случай отсутствующей semantic authority не превращается в разрешение.

Go prepared reader/publisher/store/registry/router теперь знают JSON DefectProposeReport. Inline masks automatic Params/report включены в qualified witness inventory; геометрия повторно читает точный родительский документ. Добавлена forward-only 02451 для единственного JSON контракта в закрытом SQL prepared allowlist, без расширения EXECUTE grants. Написаны archive/source-binding и migration privilege сценарии, не исполнялись.

При независимом чтении 02449 выявлен SQL three-valued-logic разрыв: проверки отсутствующих JSON полей могли вернуть NULL вместо отказа. Владелец Apply исправляет required keysets/types, selection и lease guards до интеграции.

Не закрыто: production physical PNG verifier и FaceLandmarks/FaceBoxes ancestry receipts в Go; независимая production semantic authority; trusted renderer assembly и worker ancestry wiring. JSON registry support и mock-authority сценарии не доказывают закрытие этих требований. Учёт без изменения:24/70.


### Physical DefectLabelPNG transport — 2026-10-17

Добавлен interchange ExportDefectPNGQualifier поверх существующего bounded masknpy.VerifyDefectLabelPNG, без нового декодера. Сначала физический label-map проверяется против полного набора кандидатов, затем PNG16 потоково сравнивается с NPY (каждый pixel, SHA/bytes/EOF/CRC); исходная authority и object metadata перепроверяются после IO. Typed source привязывает PNG к конкретному immutable report, исходнику, proposal и label_map и требует независимую occurrence authority. В native qualifier router выделен отдельный PNG путь; generic image fallback запрещён.

Написаны сценарии положительного physical compare, несовпадения пикселей при корректном SHA, изменения объекта/authority, отсутствия доказательств, отмены, обязательной исходной report identity. Не запускались. Production SQL occurrence authority, archive/prepared PNG publication и полное подключение каталога остаются открытыми; это не закрытая единица.

Повторное чтение02449: ранее найденные required-key/type/lease guards исправлены в текущем файле. Apply sourcepass дополнительно обнаружил128-input разрыв series_balance для129..5000кадров; владелец внедряет boundedpacks вместо сокращения принятого лимита. Editor fullstyle-enabled recipe compiler также остаётся обязательным открытым кодом, передан отдельному агенту. Прогресс24/70 без изменения.


### Native export PNG installation — 2026-10-17

Добавлена SQLExportDocumentCatalog/SQLExportResultInputCatalog occurrence authority для DefectLabelPNG: она использует существующую semantic_proposal_defect_label_png_object с exact report/proposal/source/protection/params pins и проверяет неизменность catalogue metadata после IO. Typed PNG source дополнительно вызывает имеющуюся DefectLabel occurrence authority, сохраняя native/target preparation guards. Native graph discovery собирает report parents; resolver устанавливает PNG qualifier только при поддержке независимой authority, binary traversal принимает только image/png для этой роли.

Написаны сценарии обязательной SQL installation и отмены. Не запускались. Открыто: SQL export catalogue role allowlists/native JSON report admission, PNG archive/prepared publication, authenticated historical source ancestry и полная production automatic qualification. Кодовый путь ещё не end-to-end;24/70 не меняется.

### Продолжение параллельного кодирования — 2026-10-17

Основной план принят пользователем, параллельные агенты разрешены. Apply, Editor и worker/native assembly ведутся отдельными владельцами; миграции 02453 (Editor), 02454 (export catalogue), 02455 (Apply compatibility) разделены. Экспорт DefectProposeReport включён в обязательную семантическую квалификацию; автоматический dust Params извлекает полный bounded protected list через закрытую схему, добавлены unit-сценарии отказов. Проверки не запускались. Полная production-сборка conservative interchange и staged Editor остаётся открытой, новые единицы не засчитаны: 24/70 (34,285714%).

### Conservative runtime admission — 2026-10-17

Добавлена forward-only 02456: отдельный CPU qualification reader двух auto-repair операций, с сохранением source/policy/issuer/license/fixture/evidence/report/object-version gates; scheduler dispatch расширен явной веткой. Go entry-scoped installation добавляет measured-runtime authority до и после независимой semantic authority; её отсутствие не заменяется успешной runtime-проверкой. Написаны unit-сценарии порядка/отказов и SQL ACL/refusal-сценарии, без исполнения. Открыто: аутентифицированные historical image/FaceLandmarks ancestry receipts, физическая публикация импортированного PNG и report inline-mask export admission. Частичное закрытие не меняет 24/70.

### PNG copy и сообщение о совместимости Apply — 2026-10-17

Реализован отдельный DefectLabelPNG binary remapper: entry/source scope, реальная сверка PNG с proposal label map, повторная проверка immutable object/ACL, deterministic target identity и обязательный occurrence publisher. Generic image/NPY fallback запрещён. Полное подключение SQL occurrence publisher/archive source ещё открыто. Добавлено понятное сообщение фотографу для двух APPLY_PLAN_RESTART_REQUIRED отказов старых планов, без автоматического retry и без выдуманного маршрута к исходной коллекции. Unit-сценарии написаны, не запускались; 24/70 без новых целиком закрытых пунктов.

### Prepared PNG occurrence — 2026-10-17

Добавлены occurrence certificate, immutable streaming PNG publisher, SQL receipt serialization/catalog media и отдельный физически квалифицируемый prepared PNG resolver/router. Forward-only 02457 сохраняет exact archive PNG identity/report inventory/file receipts и current source/metadata guards при reserve/complete/read. Найден реальный дефект: 02016 перезаписывает plain scope без DefectLabelMap; 02457 восстанавливает роль через существующий occurrence guard. Добавлены unit/SQL scope-сценарии, без исполнения. Открыты archive/prepared PNG authority factories и полная production assembly, report inline masks и historical face ancestry. 24/70 без нового кредита.

### Archive PNG factory — 2026-10-17

Штатная SQLImportEntryRecipeAuthorityFactory теперь создаёт отдельный PNG remapper для каждого accepted entry. Archive source находит только typed report→proposal→label inventory, читает staged документы по exact SHA, вызывает независимые report/candidate authorities, повторяет source admission; streaming verifier сравнивает PNG с реальными labels. Publisher требует archived report occurrence certificate. Deployment qualification maps копируются при workflow binding. Unit-сценарии отсутствующей authority/cancellation/foreign entry написаны без запуска. Открыто: prepared PNG source/factory, полная report mask geometry admission, историческое face ancestry. 24/70.

### Export report mask geometry — 2026-10-17

Добавлено typed извлечение всех встроенных защитных масок DefectProposeReport, их включение в native geometry discovery, проверка canvas/transform против params.image и отдельная обязательная report-mask authority. SQL authority подтверждает исходный producer/report через существующий PNG occurrence guard, дополнительно проверяет exact mask membership и неизменность object metadata. Физическая mask verification остаётся штатной. Написан unit-сценарий schema/hash/contract/ownership отказов, без запуска. Scheduler report authority и historical ancestry пока открыты, новые пункты не засчитаны (24/70).

### Scheduler report/PNG authority — 2026-10-17

Добавлена 02460 и SQLSchedulerExportInputCatalog qualification для exact report/protection/PNG одного завершённого defects.propose producer. Проверяется current export claim, допустимый исходник и proposal/labels через existing guard, current attempt/fence/output membership, готовность immutable объектов; Go повторяет catalog metadata после SQL authority. Подключена scheduler report-mask authority. Authored unit cancellation/installation и SQL ACL/missingclaim cases; без исполнения. Historical/post-beauty ancestry и подготовленные import-origin экспортные receipts остаются открыты. 24/70.

### Независимый source review Apply — 2026-10-17

Найден дополнительный дефект совместимости: Go учитывает legacy top-level culling, SQL scanner только extensions.culling. Из-за этого ожидающий review старый план мог не отменяться, а Go запрос диагностической отмены отвергался SQL. В 02455 SQL effective style нормализует legacy fallback после graph precedence в соответствии с Go, без изменения принятого config_snapshot. Добавлен сценарий waiting_review legacy → штатная отмена с сохранением frozen config. Проверки не запускались; whole Apply review пока открыт, 24/70.

### Editor seal registry и продолжающийся source review — 2026-10-17

editor.effects_seal@1 добавлен в три checked-in operation catalog с CPU route/module/params/output/review policy, соответствующими worker и 02458. Реестры отредактированы напрямую, генераторы не запускались. 02457 теперь отдельно проверяет единственность media-branch anchor до изменения SQL-функции. Apply review подтверждает culling review gate, исходный immutable snapshot и adoption selection filter; итоговая сверка lifecycle ещё открыта. Editor coordinator/finalcompose и native ancestry продолжаются. 24/70, исполнения проверок не было.

### PNG end-to-end source reconciliation — 2026-10-17

Независимый source review нашёл два реальных разрыва: recursive dependency remapper не отправлял DefectLabelPNG в binary codec, а BoundImportPreparedRecipeQualifier отклонял binary PNG leaf. Оба allowlist исправлены без generic image fallback. Добавлены regression сценарии dispatch без JSON IO и qualified binary leaf/refusal через trusted resolver. Prepared PNG factory/accessor 02459 просмотрены; продолжается историческая ancestry authority и Editor finalcompose. Ничего не исполнялось, 24/70.

### Export catalog reconciliation — 2026-10-17

Найден ещё один production-разрыв: Go scheduler input catalog отклонял DefectProposeReport и DefectLabelPNG до новых SQL/physical authorities. Оба exact контракта добавлены в closed allowlist; unit vocabulary scenario расширен. JSON export reader теперь явно отклоняет DefectLabelPNG до IO, соответствующий regression authored. Source review worker PNG path подтвердил отдельную physical qualification ветку. Ничего не исполнялось; 24/70.

### Collection Apply v4 source closure — 2026-10-17

Независимый source review принят для Collection Apply: admission/durable requests, shared frozen planner, photographer culling barrier, bounded series fan-in до 5000, regional masks, quality target/adoption cardinality, atomic rejection rollback, cancellation и legacy plan diagnostics. Последний найденный SQL legacy culling mismatch исправлен, regression authored. P0.4.05 coding_closed_unverified; 25/70 = 35,714286%, +1/70 (1,428571 п.п.). Это не runtime PASS и не готовность релиза. Dedicated Editor fullstyle→seal→finalcompose, interchange ancestry/qualification и остальные открытые модули остаются в плане. Тесты, сборки, миграции не запускались.

### Download HTTP cancellation/cache gap — 2026-10-17

При whole Download source review найден HTTP-разрыв: redemption handler не проверял cancellation после возврата сервисного результата и не задавал no-store для bearer URL. Выделен общий handler с pre/post cancellation и private,no-store + Pragma:no-cache, штатный маршрут подключён. Authored сценарии early/late cancellation без выдачи URL и strict-body rejection до service. Проверки не запускались. Download ещё открыт; показатель 25/70 = 35,714286%.

### Portal Download response boundary — 2026-10-17

Guest volume/item download now validates bearer shape/lifetime before redemption and signed URL scheme, credentials, immutable version, lifetime, filename and integrity metadata before navigation. Unit scenarios authored, not executed. Whole Download remains open pending UI cancellation/recovery and complete job-path reconciliation; no progress credit.

### Guest Download cancellation — 2026-10-17

Item/volume intent and redemption share AbortSignal. Request checks cancellation before transport and after response/body decoding. Portal permits one active navigation, aborts on pagehide/hidden/session-scope cleanup and suppresses late URLs/errors. Cancellation unit scenarios authored, not run. Download job creation/polling/recovery still requires whole-path reconciliation; no closure credit.

### Guest archive preparation scope — 2026-10-17

Selection and archive-job creation now share an abortable preparation attempt; cancelled selection cannot start a job. Download polling aborts on job/session/phase changes. Reload clears previous collection/download state. Unit cases for cancellation before selection/start/poll transport authored; execution deferred. Full durable recovery and server/job reconciliation remain open; no closure credit.

### Guest Download recovery — 2026-10-17

Session storage retains only a validated job UUID, scoped by share; no signed URL or bearer is persisted. New secret exchange clears the prior job. Reload retrieves status through the current authenticated server route and removes inaccessible/missing/expired pointers. Bootstrap and recovery requests are abortable on share/unmount changes. Unit scenarios authored, unexecuted. Server archive lifecycle and durable creation recovery remain to reconcile; no whole-module credit.

### Download polling and cancellation — 2026-10-17

Transient poll errors now retry with bounded backoff instead of stopping permanently; access/missing/expired errors clear the saved job pointer. Job cancellation uses an abortable single-flight request, catches failures and updates only the same job. Session cleanup aborts cancellation too. Cancellation-before-transport scenario extended; tests not executed. Whole Download remains open pending complete server job-path reconciliation.

### Confirmed archive status mismatch — 2026-10-17

Current SQL finalizer sets download_jobs/download_volumes ready while runs become completed. Portal incorrectly waited for completed and never offered finished archive volumes. Portal now uses public ready for download buttons, navigation admission and terminal polling; cancelling remains pending. Regression scenarios authored, unexecuted. Evidence:00170_atomic_download_archive_finalize.sql and get_portal_download public projection. No full-module credit yet.

### Download public projection boundary — 2026-10-17

Status/cancellation responses validated against requested job ID, safe counters and unique bounded volume numbers. UI offers and admits only ready volumes, prevents duplicate busy clicks. Malformed or cross-job response cannot replace job state or navigate. Unit regressions authored, unexecuted. Full module reconciliation remains open.

### Download ambiguous creation retry — 2026-10-17

Preparation journals immutable revision/preset/selection, independent selection/start idempotency keys and accepted snapshot. Lost selection/start responses retry exact keys and payload instead of creating a duplicate job; explicit business refusal or fresh secret exchange clears pending preparation. Storage holds no signed URL/bearer and unavailable storage falls back to page memory. Authored lost-selection/lost-start scenarios, unexecuted. Reload resumes pending creation on the next explicit download action; full server lifecycle reconciliation remains open.

### Download creation receipt reconciliation — 2026-10-17

Reviewed current start_portal_download: authenticated session/link lock and policy check precede idempotent replay; replay precedes snapshot expiry check, allowing exact accepted-job recovery after snapshot expiry. Client now validates snapshot UUID/count/selected membership count before job creation and accepted job UUID/public status before persistence. Malformed receipts cannot start/expose a job. Authored regressions, not run; server archive producer/current-object path still under whole-module review.

### Archive producer current attempt — 2026-10-17

Confirmed finalizer accepted canonical result without current attempt/fencing equality. Forward-only02466 adds attempt/fencing/run/operation checks, live object expiry and producer/result/artifact row locks in both phases without replacing existing atomic validation. Atomicity scenarios now cover missing output, stale attempt, stale fence and expired archive/manifest; fixture pins actual current task attempt. Tests/migration not executed. Download still open until complete source reconciliation.

### Download volume post-sign producer — 2026-10-17

02466 also repeats current canonical producer attempt/fence/run/output membership after URL signing for archive and direct-encoded volumes. Archive manifest must remain live and belong to the same producer/canonical result. Existing session/policy/ACL/object-coordinate checks retained. Added positive finalized-volume plus stale-attempt/fence post-sign scenarios, unexecuted. Download source review remains open.

### Pending archive retry after reload — 2026-10-17

Closed UI link: restored immutable pending preparation now permits explicit Download retry even when current transient selection is empty after reload. Normal fresh selected-download still requires selection. Pending-state UI refreshes after success/refusal/failure; saved preparation tests cover availability and clearing. No execution or module credit.

### Direct download seal — 2026-10-17

Current direct-file seal bypassed archive finalization and could mark a job ready without rechecking current encode result.02466 now verifies each ready direct volume current encode attempt/fence/run/output membership and live physical metadata before seal, including mixed direct/ZIP jobs. Added isolated positive and stale-attempt/fence direct-seal fixtures. Authored only; no execution.

### Owner Download intent replay — 2026-10-17

Found owner legacy CreateIntent returned replay bearer without expiry/consumption recheck. Now validates dependencies and cancellation, requires live bounded receipt, verifies persisted unconsumed intent against exact user/job/volume/expiry within transaction and checks expiry/cancellation again after commit. Infrastructure errors remain errors. Lifetime/late-expiry unit cases authored, not run. Full Download review remains open.

### Guest intent dead replay — 2026-10-17

Guest item/volume intent responses now share final cancellation and bounded live-expiry validation.02466 prevents SQL replay of consumed/expired guest intents while retaining original idempotency and authorization. Authored response lifetime/cancellation unit cases and consumed/expired item replay SQL scenarios, unexecuted.

### Download encoded-source fence — 2026-10-17

02466 closes the upstream canonical-result gap too: download_encode_outcomes and download_archive_context now require current encode attempt/fence before packaging any direct/ZIP output. Outcomes reject expired encoded objects. Existing result membership, task operation, tenant, run and immutable physical coordinates retained. Migration authored, not executed; no runtime PASS claimed.

### Owner archive recovery after selection expiry — 2026-10-17

Owner Start previously checked snapshot expiry before idempotency, preventing recovery of an accepted job after a lost response. Expiry now gates only the new-work callback; exact replay retains current tenant/project/download authorization. Integration scenario authored for same-key recovery and new-key refusal after expiry. Tests unexecuted.

### Owner Export→Download missing UI — 2026-10-17

Found owner exports screen stopped at job-number notice. Added scoped job panel with foreground polling/backoff, status/progress, cancellation, ready-volume navigation and late-response suppression; OwnerAPI actual status/cancel/intent/redeem methods validate job DTO and signed response. Archive links admit multi-GiB files while Delivery retains its default512MiB cap. Authored DTO regression. Panel durable recovery and DOM scenarios remain open; no Download closure credit.

### Owner download job persistence — 2026-10-17

Exports restores only job UUID from existing AsyncStorage after current GET me. Scope includes API origin/user/tenant; auth logout clears records and invalidates old store instances, storage operations serialize against cleanup. Accepted job is persisted before UI acknowledgement. Account/tenant/logout unit scenario authored; exports DOM mocks updated for actual identity bootstrap. Panel DOM scenarios and durable pending creation key after full app restart remain open. No tests run or closure credit.

### Owner durable archive start — 2026-10-17

Owner Download start now persists immutable snapshot/preset/idempotency key before IO, restores pending attempt after restart and exposes explicit retry. Existing pending request cannot be silently replaced by a new selection; accepted job persistence precedes pending removal. Added restart/same-key/receipt-conflict unit scenario and updated DOM mocks. Execution deferred; panel DOM scenarios and final closure review remain open.

### Owner Download late start and panel scenarios — 2026-10-17

Start now has AbortSignal and single-flight controller; scope cleanup cancels transport, and response/storage completion cannot update a replaced store. Panel DOM scenarios authored for ready-volume navigation and delayed link after leaving foreground. Retry-key DOM expectation compares immutable request/key while allowing distinct per-attempt signals. No tests/builds run; Download closure review remains open.

### Owner Download definitive refusal — 2026-10-17

Durable pending start now clears only after an exact-key/payload classified API refusal (snapshot stale/validation/preset disallowed); transport and infrastructure failures retain immutable retry. Foreign/conflicting receipt cannot discard pending request. Authored ambiguous/503/conflicting/refused cases, unexecuted.

### Owner Download access refusal — 2026-10-17

Panel distinguishes definitive authorization/missing/expired/revoked errors from transient errors. Definitive refusal clears visible job/volume controls, aborts active navigation and stops automatic retry; explicit status Retry may reauthorize. Added DOM refusal scenario. Authored only, no execution.

### Download packaging cancellation — 2026-10-17

Added explicit cancellation before outcome IO, during row iteration, before planning publication/finalization and after finalizer DB response; authored no-IO cancellation unit case. A remaining source issue is explicit terminal handling of failed encoded items: coordinator currently returns a generic error; reconcile continue_items product policy before implementing final failure/partial publication. This is a real coding gap, not a test-only gate. No execution or closure credit.

### Download — текущая сверка барьера и восстановления

Реестр `/exports` дополнен фактическим `GET /api/v1/me`, используемым для scope durable recovery. Source outcome reader теперь отклоняет неправильный ordinal и отсутствующий asset version даже при незавершённых export tasks; ошибочный inventory не выдаётся за обычное ожидание. Написаны соответствующие unit-сценарии без запуска.

Открытый функциональный разрыв: frozen `continue_items` против текущего all-success `begin_download_packaging` и generic error для failed/cancelled/skipped encode. Нужна согласованная обработка успешного поднабора, счётчиков/manifest и нулевого успеха; нельзя ограничиться заменой ошибки на fail-all. Исторические PASS не использованы. Download не засчитан; остаточный показатель **25/70**.

### Download — terminal encode failure по нормативному контракту

Повторно прочитан photo_pipelines_spec.docx.md §8: отсутствующий файл означает failed со списком причин; молчаливый частичный архив запрещён. `continue_items` завершает остальные encode tasks, но не разрешает частичный архив. Добавлены typed terminal signal только после окончания всех encode tasks, SQL `fail_download_encoding` с независимыми live run/job/phase/inventory gates и блокировкой tasks, идемпотентный failed receipt и безопасный per-item report без внутренних worker exceptions. Миграция02470 и owner/portal projections сохраняют отчёт; оба клиента показывают item ordinal/reason. IO ошибки и повреждённые succeeded outputs остаются отдельно от terminal worker failure. Unit-сценарии написаны, ничего не запускалось. Полный Download пока не засчитан: требуется завершить current-source lifecycle audit и интеграционные сценарии нового SQL.

### Download — encoding failure integration inventory

Миграция02470 дополнена независимой сверкой полного snapshot inventory и обязательных asset_version_id. Workflow startup требует `fail_download_encoding`, чтобы старый schema deployment не принимался за готовый Download backend. Написаны SQL integration fixtures: terminal+exact report+replay без повторного revision, pending, cancellation, cross-tenant, missing plan/snapshot member, отсутствие volumes и private scheduler-only ACL. Сценарии не запускались; это authored coverage, не PASS. Остаточный показатель25/70.

### Download — progress до packaging

Owner и portal status сохраняют `files_total` из frozen snapshot во всех фазах. `files_done` учитывает terminal encode tasks, включая явные failures/skips, а не только сформированные download_job_items. Поэтому ранний сбой кодирования показывает обработанные/выбранные фото, а не0/0. Совместимый fallback оставлен для старых job items. Forward-only02470 меняет guest projection с source drift guard, без обхода authorize_portal_session. SQL fixture дополнен authored progress assertion; не запускался. Whole module credit не изменён25/70.

### Download — archive terminal report

02470 дополняет существующий atomic archive finalizer безопасным per-item отчётом `DOWNLOAD_ARCHIVE_FAILED`, с привязкой item→volume→canonical archive task и стабильными reason codes. Tasks блокируются до проверки barrier; failed transition очищает build lease. Успешные тома не публикуются частично. Оба клиента читают также archive failures. SQL fixture и parser scenarios написаны, не запускались. Независимый source audit Download идёт параллельно;25/70 без нового whole-module credit.

### Download — конкретная причина missing/hash в archive

Archive worker перехватывает только известные per-entry ошибки и передаёт стабильные artifact_id/SHA в TaskError.args; lease loss и cancellation не маскируются. Finalizer использует только error exact current attempt/fence и совпадение item artifact_id/SHA: лишь соответствующий файл получает ARTIFACT_MISSING/HASH_MISMATCH, остальные — affected-volume ARCHIVE_FAILED. В обоих клиентах добавлен bytes_written. Worker unit-сценарии написаны, не запускались. Независимый аудит также выявил succeeded encode с утраченным canonical artifact: это отдельный открытый code gap, требуется durable terminal report вместо endlessretry.25/70.

### Download — succeeded без доступного output

Закрыт backend code path endlessretry для succeeded encode с отсутствующим/expired/stale canonical output. Outcome SQL сохраняет planned row и проецирует недоступный artifact какNULL; некорректные bytes/object_version также проверяются в LEFT JOIN, не удаляя inventory row. Go дожидается остальных задач и вызывает независимый terminal barrier. SQL перечитывает availability и сохраняет EXPORT_OUTPUT_UNAVAILABLE только для соответствующих succeeded entries; если output восстановился до commit, terminal gate не срабатывает. Клиенты принимают стабильную причину. Unit wait/terminal scenarios написаны, не запускались.25/70 до whole-module closure.

### Download — configurable volume limits

Scheduler подключён к PHOTO_DOWNLOAD_VOLUME_BYTES/FILES с fail-fast bounds/default10GiB/5000. Planner принимает отдельный per-volume file limit, сохраняя весь snapshot и отдельную выдачу oversized файла. README и unit bounds/split scenarios написаны. Ничего не запускалось. Настройка TTL24h/60s ещё открыта, без ложного whole-module credit25/70.

### Текущее распределение целых модулей

Root ведёт Delivery и общую интеграцию; Metal-agent — Editor coordinator/finalrun/client recovery; Apply-agent — согласованную настройку Download TTL; Composition-agent — Export. Import, Session, Collection, Apply v4 и Review уже учтены в машинном реестре как coding_closed_unverified, повторного начисления нет. Заголовки checklist синхронизированы с реестром25/70; исторические записи ниже остаются журналом, не текущим процентом. Тестирование отложено согласно принятому порядку.

### Delivery — production factories reconciliation

Сверены CLI signingFactory→scheduler InstallDeliverySigningStage, API отдельный scheduler-role pool и SQL private ACL installation, Ordinary/Watermark child router, Metadata stage и independent adopter. Эти production branches существуют в текущем коде; старые strip-only/uninstalled записи не используются как текущие defects. Точная карта в audit/delivery-current-source-closure-2026-10-17.md. Child/adoption/publication whole-module pass ещё не закончен; никаких проверок не запускалось,25/70 без начисления.

### 2026-10-17 — Apply Glare: смена пользователя и поздний ответ

При различии пользователей до/после сохранения Apply блокируется и снимает текущую проекцию review через родительский callback. Написаны сценарии смены пользователя, размонтирования, смены решения и сохранения выбранного оригинала. Независимое ревью Delivery обнаружило и закрыло расхождение полного object descriptor между artifact и accepted upload в обычном и watermark stage reader (02477), написаны пять сценариев drift. Ничего не запускалось. Генеративная Glare issuance и независимое полное ревью модулей остаются открытыми; **25/70 без нового зачёта**.

### 2026-10-17 — P0.5.04: реальный разрыв local portrait review

Подтверждено по worker staging, общему settle и generic publisher/persistence: wrinkle correction может перейти в waiting_review без пригодного local portrait review publisher; generic creative publisher и direct-output attestation не подходят для V2 ImageResult root/child links. Полная обязательная цепочка authority → publisher → preview/decision → selected version/recovery внесена в audit/local-portrait-current-source-closure-2026-10-17.md. Тесты не запускались. P0.5.04 implementing, 25/70 без зачёта.

### 2026-10-17 — P0.5.04: document + physical adoption gates

Написаны local_portrait_adoption.py и local_portrait_pixels.py: immutable typed documents/source/masks/report, exact binary input/output membership, geometry/ICC/alpha preservation, budgeted hash-verified NPY и побитная неизменность protected/outside/strength-zero. Написаны positive и tampering/cancellation/changed-mask/signed-zero сценарии. Проверки не запускались. SQL publisher/service/selectedApply в02483 в работе; модуль открыт, 25/70 без зачёта.


### 2026-10-17 — Hair/clothing: целый модуль закрыт по кодированию

Root reconciled independent document/physical adoption, transactional current-authority publication, before/after preview, human and auto-result Apply, plain run-history discovery, cancellation/quota and retained GC references. Authored scenarios are present; none executed. Current audit: audit/local-portrait-current-source-closure-2026-10-17.md. P0.5.04 credited once:26/70 =37.142857% residual coding plan (+1.428571 percentage points). Whole system readiness and runtime quality are unproven.


### 2026-10-17 — Download coding closure

Root reconciled whole source path after independent owner/guest/direct/ZIP review and fixes: exact source read failure, Portal access withdrawal, terminal events,512 transport fan-in and current run→job recovery locks. Authored unit/DOM/SQL scenarios are present; full physical/concurrency/dispatch execution remains mandatory in deep testing. P0.4.09 credited once:27/70 =38.571429% (+1.428571pp). No runtime PASS or release readiness claimed.


### Editor: кодирование целого модуля — 2026-10-17

P0.4.04 закрыт по текущей полной source-сверке: immutable command/recovery, precompose→Apply→seal→final, exact typed receipt и physical input evidence, withdrawal/drain/quota, актуальность обоих observers. Evidence: `audit/editor-final-independent-review-2026-10-17.md`, `audit/editor-current-source-map-2026-10-17.json`. Nested P0.2.03 повторно не зачтён.28/70 =40% (+1.428571 процентного пункта). Написанные тесты не запускались; глубокие проверки и аппаратная квалификация остаются впереди.


### Relight: кодирование целого модуля — 2026-10-17

P0.5.06 принят по полной текущей source-сверке подготовки, immutable recovery, CPU worker, независимой adoption, before/after/viewed receipt, решения фотографа и selected Apply/version. Исправлены actual recorded consent, foreground/denial teardown и late transaction cancellation. Evidence: `audit/relight-current-source-closure-2026-10-17.md`.29/70 =41.428571% (+1.428571 процентного пункта). Тесты, миграции и quality qualification не выполнялись.


### Export и Delivery: кодирование целых модулей — 2026-10-17

P0.4.07/P0.4.08 приняты после root-сверки и независимого whole-source review. Полная конфигурация preset, immutable grouped recovery, physical exact source authority, presentation/assembly, publication/download, cancellation/drain/quota и startup связаны. Evidence: `audit/export-current-source-closure.md`, `audit/delivery-current-source-closure-2026-10-17.md`.31/70 =44.285714% (+2.857143 процентного пункта). Все выполнения и качество остаются непроверенными.


### Glare: кодирование целого модуля — 2026-10-17

P0.5.03 принят после независимой root-сверки текущей source/donor/adoption/preview/selection/Apply и consent withdrawal/recovery цепочки. Исправления02489/02491, actor/foreground fences и typed permanent witness связаны. Evidence: `audit/glare-current-source-closure-2026-10-17.md`.32/70 =45.714286% (+1.428571 процентного пункта), без исполнения тестов и миграций.


### OpenEyes: кодирование целого модуля — 2026-10-17

P0.5.02 принят после независимой root-сверки текущего UI/actor-scoped journal, donor worker/adoption,02486preview authority,02478selectedversion и02492originalproof withdrawal. Review route foreground unmount отменяет выбранный Apply; context и actor fences исправлены. Evidence: `audit/open-eyes-current-source-closure-2026-10-17.md`.33/70 =47.142857% (+1.428571 процентного пункта), без исполнения тестов/миграций.

### Current quota/lifecycle/GC coding acceptance

P0.6.04/P0.6.06/P0.6.07 accepted once by parent after whole-source inventories and current02535–02544 corrections/startup integration. **46/70 =65.714286%** residual coding; tests, migrations and physical/concurrent qualification remain unexecuted. Evidence: audit/storage-lifecycle-parent-acceptance-2026-10-17.md.

### Current whole role/tenant coding acceptance

P0.3.05 accepted once after independent complete registered-screen review and current boundary/actor/project/read-epoch source reconciliation. **47/70 =67.142857%** residual coding. Authored scenarios are not runtime PASS; no checks executed.

### Current source correction — Editor reopened

P0.4.04 reopened after confirmed cancellation and stale UI ownership defects. Current residual coding metric **46/70 =65.714286%**. Changes and authored scenarios are being completed; no tests/builds/migrations run. Earlier percentages are historical.

### Current navigation acceptance

P0.3.07 source accepted after parent and independent whole-route review, including localized new controls. **47/70 =67.142857%** residual coding; Editor remains reopened. No checks executed.

### Current baseline quality coding acceptance

P0.4.10 accepted once after whole source review; current metric **48/70 =68.571429%**. Editor remains reopened. Real photograph/hardware/encoder qualification deferred; no checks executed.

### Current API family acceptance

P0.3.02 accepted once after independent delivery/replacement/interchange contract review. **49/70 =70%** residual coding. Other screen API inventory remains open for confirmed Review/Export omissions. No checks executed.

### Editor reopened criteria accepted

Parent accepted current cancellation/recovery/storage ownership and composite CPU readiness corrections after independent source review. **50/70 =71.428571%** residual coding (+1.428571 percentage points). Scenarios authored, no execution. Capture durable-create journal remains a separate open screen-state criterion.

### SQL admission coding acceptance

Parent reconciled current common fences, activation amendments and independent operation-family maps. **51/70 =72.857143%** residual coding (+1.428571pp). Historical/declaration union is not the deployed enabled inventory; actual SQL and hardware executions remain deferred.

### Residual AI whole coding acceptance

Parent accepted generation resource correction and complete residual family map after independent whole Region/native reconciliation. **52/70 =74.285714%** residual coding (+1.428571pp). Quality, actual compatible model installation and all runtime tests remain unverified.


2026-10-17: P0.3.09 и P0.7.08 приняты coding_closed_unverified после323 уникальных source-карт и исправления финальных контрактных проекций. Учёт68/70=97,142857%. Основание: [полная приёмка endpoint-кодирования](audit/endpoint-whole-coding-parent-acceptance-2026-10-17.md). Исполненных проверок нет.
