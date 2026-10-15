# Подтверждённые разрывы статической трассировки

Срез кода: `54a35b8`. Метод: чтение исходников и сопоставление клиентского вызова, API route, backend installation, worker и UI с контрактами. Тесты и миграции не запускались. Найденные ниже дефекты подтверждены исходниками; отсутствие физической квалификации и неисполненный тест остаются отдельными статусами. Это не заключение о прохождении всех 714 ворот.

## P0 — устранить до начала глубокой тестовой фазы MVP

### D-01: semantic review предлагает Apply до просмотра кандидата

Требование [mvp-release-scope-2026-10-15.md](../mvp-release-scope-2026-10-15.md): фотограф решает принять/отклонить AI результат, а review показывает до/после, затронутую область, protected pixels и ограничения. В [semantic-review-decisions.tsx](../../../apps/photographer/src/semantic-review-decisions.tsx) для `proposed` рендерятся только короткий asset ID и кнопки approve/reject. В [semantic-target-screen.tsx](../../../apps/photographer/src/semantic-target-screen.tsx) `SemanticPreview` доступен лишь для `state==='applied'`, через `child_run_id`. Наличие `candidate_artifact_id`/`document` в [semantic.ts](../../../packages/api-client/src/semantic.ts) не сопровождается в этом UI предварительным изображением или quality/protection evidence. Следовательно, текущий путь позволяет отправить approval, не увидев кандидата.

Область: `semantic.transfer` (V27) и HTTP `GET /semantic-intents/{id}/targets`, `POST /semantic-intents/{id}/apply`; проверенная часть связей: endpoint объявлен, client вызывается, decision UI вызывается; разрыв — pre-Apply review presentation. Исправление: построить авторизованный источник исходного и кандидатного preview с pinned version/expiry, показать синхронное до/после и affected/protected область, статус/ограничения; блокировать approve, пока точный кандидат и доказательства не загружены и не совпали с текущим `plan_sha`/revision. Reject и недоступность должны оставаться доступными. Написать unit-сценарии stale candidate, истёкшего URL, отзыва доступа, разных target/asset и повторного решения. Критерий кодового закрытия: фотограф видит именно кандидата данной цели до approve, а запрос Apply несёт тот же fenced plan/revision.

## P1 — устранить до тестирования полного V20–V32, не блокирует первый MVP при выключенной capability

### D-02: отсутствует клиентский экран learning/identity report

[06-clients-integrations.md](../06-clients-integrations.md) требует `/learning/[id]` и `showIdentityReport` с changed/generated/protected overlays и честными состояниями pass/review/not measurable. В `apps/photographer/app` маршрута `/learning/[id]` нет; в клиентском исходнике нет реализации `showIdentityReport`. Backend/worker кандидаты для `identity.compare` присутствуют, но операция остаётся gated. Это разрыв полного клиентского контракта V23, а не доказательство, что выключенная операция обходит gate.

Исправление: маршрут и экран полного learning/identity review, API DTO для отчёта, проверка tenant/version/consent scope, overlays и explicit photographer decision; unit-сценарии revoke, not measurable, stale report и retry. Кодовое закрытие — пользовательский переход от доступного отчёта к review/отказу без автоматического принятия. В MVP capability остаётся `unavailable` до квалификации и активации.

## P0 — устранить до построения тестового охвата

### D-03: маршрутная карта клиента не охватывает новые экраны

Приоритет P0 **для подготовки полного тестового охвата**, не дефект исполнения экранов. [routes.ts](../../../apps/photographer/src/routes.ts) содержит 21 route capability, тогда как `apps/photographer/app` содержит 40 экранных маршрутов (без `_layout`). В карте отсутствуют 19 существующих экранов: `/`, `/collections/[id]/multi`, `/deliveries`, `/deliveries/[id]`, `/deliveries/new`, `/exports/sign`, `/interchange/[id]`, `/interchange/format`, `/interchange/import-format`, `/interchange/masks/[id]`, `/interchange/new`, `/interchange/prepare`, `/interchange/prepared/[id]`, `/interchange/upload`, `/multi-replacements/[original]`, `/perspective-replacements/[original]`, `/semantic/[id]`, `/sessions/[id]`, `/sessions/[id]/scenes`. `routes.ts` не импортируется production-кодом, поэтому этот факт не означает сломанную навигацию; он означает, что карта трасс не может служить полным списком экранов и API-связей. Исправление до тестовой фазы: синхронизировать маршрутную карту с файловыми маршрутами, указать API/role/capability для каждого и обеспечить статическое обнаружение новых экранов.

### D-04: выбранный Metal-хост не может квалифицировать все заявленные AI-ветки

Приоритет P0 **для тестовой стратегии**, не утверждение о повреждении CUDA production path. [runtime/service.py](../../../workers/photo_worker/runtime/service.py) отвергает `semantic_installation_factory` и `region_renderer`, если backend не `cuda`; V28 glare/reflection installation допускает только `cpu|cuda` GPU-class worker. [mvp-release-scope-2026-10-15.md](../mvp-release-scope-2026-10-15.md) задаёт текущий Metal с потолком 10 ГБ как среду проверки до RTX. Следовательно, на MPS невозможно провести positive physical QA для V27 semantic и region preview без нового backend adapter/qualification; V28 glare/reflection требуют отдельной CPU-ветки или CUDA-хоста. Рабочая граница: эти capability должны возвращать `unavailable`, пока нет квалифицированного backend; не считать их завершёнными по CPU/CUDA исходникам. До начала глубокой фазы зафиксировать матрицу `операция × backend × установленный model lock × доступность`, определить, какие операции квалифицируются на CPU/MPS, и оставить CUDA-only физическую проверку отдельным обязательным этапом на RTX. Если цель — проверить *все* включённые AI на Metal, сначала требуется код и квалификация MPS/CPU path без ослабления quality gates.

## Проверенные связи без выявленного дефекта

- Все 79 нормативных HTTP маршрутов имеют статическое объявление либо явную динамическую таблицу регистрации в `backend/internal/httpapi`; это подтверждает только кодовое mount declaration, не startup/authorization/response.
- `POST /semantic-intents/{id}/apply` вызывает `SemanticReviewApply.Apply` в HTTP handler; executable подставляет `semanticReviewApply` в `httpapi.Server`. Условная installation может вернуть 503 по отсутствующей provisioned конфигурации — это ожидаемый fail-closed gate, не D-01.
- `backend/cmd/photo/main.go` подключает scheduler interchange export factory; прежнее утверждение о непроведённом wiring снято. Provisioned authority и round-trip остаются непроверенными.
- Все строковые `router.push({pathname: ...})` в production TSX указывают на существующие файловые маршруты Expo; это не закрывает проверку параметров, полномочий и переходов.
- Прямые `Operation('id', resource_class, ...)` для контрактных операций не противоречат заявленному `resource_class`; четыре операции с динамической регистрацией (`optical.panorama`, `optical.focus_stack`, `v2.delivery.prepare`, `v2.delivery.assemble`) объявляют CPU. Это проверка деклараций, а не фактического dispatch.

Остальные переходы 30 операций и 79 endpoint всё ещё `not_verified`, если в [trace-inventory.json](trace-inventory.json) нет отдельного доказательства. При дальнейшем чтении кодовых связей добавлять дефекты с точным downstream переходом, а не переносить сюда подозрения из документации.
