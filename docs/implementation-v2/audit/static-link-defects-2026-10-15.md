# Подтверждённые разрывы статической трассировки

Срез кода: `54a35b8`. Метод: чтение исходников и сопоставление клиентского вызова, API route, backend installation, worker и UI с контрактами. Тесты и миграции не запускались. Найденные ниже дефекты подтверждены исходниками; отсутствие физической квалификации и неисполненный тест остаются отдельными статусами. Это не заключение о прохождении всех 714 ворот.

## P0 — устранить до начала глубокой тестовой фазы MVP

### D-01: semantic review предлагает Apply до просмотра кандидата

Актуализация рабочего дерева 2026-10-15: клиентский approve теперь требует отображённые exact before/after/affected/protected preview и повторный parent fence; transport DTO/GET route добавлены. Без production provider preview возвращает 503 и approve закрыт. Producer предварительного рендера, SQL receipt/authority и server-side gate/adoption **ещё отсутствуют**, поэтому D-01 не закрыт. Детали и следующий обязательный этап: [semantic-preapply-preview-contract.md](../semantic-preapply-preview-contract.md). Unit-сценарии написаны, не выполнялись.

Требование [mvp-release-scope-2026-10-15.md](../mvp-release-scope-2026-10-15.md): фотограф решает принять/отклонить AI результат, а review показывает до/после, затронутую область, protected pixels и ограничения. В [semantic-review-decisions.tsx](../../../apps/photographer/src/semantic-review-decisions.tsx) для `proposed` рендерятся только короткий asset ID и кнопки approve/reject. В [semantic-target-screen.tsx](../../../apps/photographer/src/semantic-target-screen.tsx) `SemanticPreview` доступен лишь для `state==='applied'`, через `child_run_id`. Наличие `candidate_artifact_id`/`document` в [semantic.ts](../../../packages/api-client/src/semantic.ts) не сопровождается в этом UI предварительным изображением или quality/protection evidence. Следовательно, текущий путь позволяет отправить approval, не увидев кандидата.

Область: `semantic.transfer` (V27) и HTTP `GET /semantic-intents/{id}/targets`, `POST /semantic-intents/{id}/apply`; проверенная часть связей: endpoint объявлен, client вызывается, decision UI вызывается; разрыв — pre-Apply review presentation. Исправление: построить авторизованный источник исходного и кандидатного preview с pinned version/expiry, показать синхронное до/после и affected/protected область, статус/ограничения; блокировать approve, пока точный кандидат и доказательства не загружены и не совпали с текущим `plan_sha`/revision. Reject и недоступность должны оставаться доступными. Написать unit-сценарии stale candidate, истёкшего URL, отзыва доступа, разных target/asset и повторного решения. Критерий кодового закрытия: фотограф видит именно кандидата данной цели до approve, а запрос Apply несёт тот же fenced plan/revision.

## P1 — устранить до тестирования полного V20–V32, не блокирует первый MVP при выключенной capability

### D-02: отсутствует клиентский экран learning/identity report

[06-clients-integrations.md](../06-clients-integrations.md) требует `/learning/[id]` и `showIdentityReport` с changed/generated/protected overlays и честными состояниями pass/review/not measurable. В `apps/photographer/app` маршрута `/learning/[id]` нет; в клиентском исходнике нет реализации `showIdentityReport`. Backend/worker кандидаты для `identity.compare` присутствуют, но операция остаётся gated. Это разрыв полного клиентского контракта V23, а не доказательство, что выключенная операция обходит gate.

Исправление: маршрут и экран полного learning/identity review, API DTO для отчёта, проверка tenant/version/consent scope, overlays и explicit photographer decision; unit-сценарии revoke, not measurable, stale report и retry. Кодовое закрытие — пользовательский переход от доступного отчёта к review/отказу без автоматического принятия. В MVP capability остаётся `unavailable` до квалификации и активации.

## P0 — устранить до построения тестового охвата

### D-03: маршрутная карта клиента не охватывает новые экраны

Актуализация 2026-10-15 (рабочее дерево поверх `1787516`): `routes.ts` дополнен 19 маршрутами; статический сценарий `routes.test.ts` теперь сравнивает карту со всеми файловыми экранами Expo. Статическое сравнение исходников: 40 экранов = 40 записей, без пропущенных и дубликатов. Это закрывает только обнаружение новых экранов. Проверка точных API-связей, требуемых ролей и capability каждого нового экрана остаётся открытой частью D-03; тесты по принятому порядку не запускались. Старые числа 21/40 ниже описывают состояние до исправления.

Приоритет P0 **для подготовки полного тестового охвата**, не дефект исполнения экранов. [routes.ts](../../../apps/photographer/src/routes.ts) содержит 21 route capability, тогда как `apps/photographer/app` содержит 40 экранных маршрутов (без `_layout`). В карте отсутствуют 19 существующих экранов: `/`, `/collections/[id]/multi`, `/deliveries`, `/deliveries/[id]`, `/deliveries/new`, `/exports/sign`, `/interchange/[id]`, `/interchange/format`, `/interchange/import-format`, `/interchange/masks/[id]`, `/interchange/new`, `/interchange/prepare`, `/interchange/prepared/[id]`, `/interchange/upload`, `/multi-replacements/[original]`, `/perspective-replacements/[original]`, `/semantic/[id]`, `/sessions/[id]`, `/sessions/[id]/scenes`. `routes.ts` не импортируется production-кодом, поэтому этот факт не означает сломанную навигацию; он означает, что карта трасс не может служить полным списком экранов и API-связей. Исправление до тестовой фазы: синхронизировать маршрутную карту с файловыми маршрутами, указать API/role/capability для каждого и обеспечить статическое обнаружение новых экранов.

### D-04: CUDA-квалификация обязательна для MVP на выделенном RTX-сервере

Приоритет P0 **как release gate**, не утверждение о повреждении CUDA production path. [runtime/service.py](../../../workers/photo_worker/runtime/service.py) отвергает `semantic_installation_factory` и `region_renderer`, если backend не `cuda`; V28 glare/reflection installation допускает только `cpu|cuda` GPU-class worker. Пользователь уточнил аппаратную границу: выделенный сервер RTX 3060 12 ГБ / RAM 32 ГБ / Ryzen 5600 входит в MVP. Поэтому поддержка MPS для этих веток **не является требованием MVP**; Metal остаётся предварительной средой без фиксированного лимита 10 ГБ. Реальный разрыв до выпуска — отсутствие подтверждённой CUDA qualification и сквозного quality/Apply прогона на выделенном RTX. Для каждой включённой CUDA-операции зафиксировать точные model/runtime/device pins, доступность worker/scheduler, потолок 10 000 000 000 байт VRAM при проверке, реальный фото-корпус, время/память, protected pixels и решение фотографа. До положительного результата capability отвечает `unavailable`; ветку нельзя объявлять квалифицированной по Metal, CPU или одной статической регистрации. Отсутствие RTX-сервера/положительной CUDA-квалификации остаётся блокером активации заявленных CUDA-возможностей MVP, а не основанием разрабатывать непроверенный MPS fallback.

## Проверенные связи без выявленного дефекта

- Все 79 нормативных HTTP маршрутов имеют статическое объявление либо явную динамическую таблицу регистрации в `backend/internal/httpapi`; это подтверждает только кодовое mount declaration, не startup/authorization/response.
- `POST /semantic-intents/{id}/apply` вызывает `SemanticReviewApply.Apply` в HTTP handler; executable подставляет `semanticReviewApply` в `httpapi.Server`. Условная installation может вернуть 503 по отсутствующей provisioned конфигурации — это ожидаемый fail-closed gate, не D-01.
- `backend/cmd/photo/main.go` подключает scheduler interchange export factory; прежнее утверждение о непроведённом wiring снято. Provisioned authority и round-trip остаются непроверенными.
- Все строковые `router.push({pathname: ...})` в production TSX указывают на существующие файловые маршруты Expo; это не закрывает проверку параметров, полномочий и переходов.
- Прямые `Operation('id', resource_class, ...)` для контрактных операций не противоречат заявленному `resource_class`; четыре операции с динамической регистрацией (`optical.panorama`, `optical.focus_stack`, `v2.delivery.prepare`, `v2.delivery.assemble`) объявляют CPU. Это проверка деклараций, а не фактического dispatch.

Остальные переходы 30 операций и 79 endpoint всё ещё `not_verified`, если в [trace-inventory.json](trace-inventory.json) нет отдельного доказательства. При дальнейшем чтении кодовых связей добавлять дефекты с точным downstream переходом, а не переносить сюда подозрения из документации.


## 2026-10-16 — D-05: конкурентный отзыв output metadata выбранного retouch preview

Приоритет P0 до тестирования. Статически подтверждено:02373 блокировал preview run и producer task, но02363 bundle readiness лишь читал mutable verified_at/expires_at восьми output artifacts. Между проверкой и commit Apply отдельная транзакция могла отозвать метаданные результата.

Исправление написано в02399_v27_retouch_selection_output_locks.sql: стабильные FOR SHARE блокировки всех output rows данного tenant/task/attempt/fence и canonical result перед повторной readiness-проверкой, сохранены owner/ACL/прочие проверки. Сценарии двух соединений — tests/integration/test_v27_retouch_selection_concurrency.py; только изолированная временная БД,14 row-contention и2 writer-first сценария. Статус: исправлено в коде, миграция не применена, тесты не запускались; многотаргетная квалификация остаётся открытой в .07.5.4.
