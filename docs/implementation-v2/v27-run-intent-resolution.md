# V27: разрешение parent run в semantic intent

`GET /api/v1/tenants/{t}/runs/{id}/semantic-intent` — read-only resolver для перехода от принятого JobTicket к review. Он не создаёт задания, документы или intent и не запускает повторную обработку.

Ответ 200 содержит ровно `run_id`, `workflow_id`, `intent_id`, `revision`, `state`. UUID ненулевые; `intent_id` допускает null. Revision — положительная decimal int64 строка. State: queued, running, waiting_review, cancelling, cancelled, completed, failed.

`ReadRunIntent` выполняет tenant-транзакцию с текущей session и ролью editor, выбирает только kind=v2, sealed semantic workflow с parent config v27_semantic. Join intent выполняется по tenant/project/workflow. После чтения повторно проверяется штатный project.Authorize(editor), включая restricted ACL и archived project. Ничего не возвращается до успешного завершения транзакции. Неизвестный parent или неподходящий child возвращает 404; ошибки авторизации сохраняют штатный 401/403/404. Недоступная БД не заменяется пустым успешным результатом.

`semantic_intents.id` создаётся отдельно при review initialization. До этого resolver возвращает null, а не workflow UUID. После initialization возвращается сохранённый resource ID. Отдельных миграций или расширений прав API не требуется.

SDK `decodeSemanticRunIntent` отвергает неизвестные/пропущенные поля, malformed IDs, revision overflow и неизвестный state. `readSemanticRunIntent` проверяет совпадение run_id с запросом и передаёт AbortSignal. Метод ApiClient: semanticRunIntent.

Текущий охват проверок: SDK pending/independent identity/malformed response/wrong parent; сервер invalid identity/storage и unauthenticated route; physical create-service запускает настоящий API executable и проверяет pending response с revision=2 после единственного sealed admission, 401 без token и 404 для неизвестного run. Этот тест не доказывает post-assembly non-null resolution, cross-tenant/restricted ACL matrix, browser/native navigation, restart recovery или приёмку всего V27.

Клиент: SemanticCreate передаёт в маршрут задания semanticWorkflow из принятого JobTicket. RunScreen монтирует SemanticRunLink только для этого маршрута и загруженного run. Компонент последовательно опрашивает resolver каждые 3 секунды до terminal state; фиксирует run/workflow и монотонную int64 revision, после первого non-null ID запрещает его замену или исчезновение даже после ошибки/отзыва доступа. Ошибка скрывает ссылку; явный Refresh повторяет только чтение. Background отменяет flight/timer и скрывает ссылку, foreground заново проверяет доступ. Смена client/tenant/run/workflow перемонтирует scope и игнорирует late results. Кнопка открывает /semantic/{intent_id}; null никогда не подставляется workflow ID. Terminal без intent отображает результат вместо бесконечного ожидания подготовки. EN/RU/BG строки добавлены.

DOM-тест маршрута подтверждает настоящий intent ID в navigation. Component tests проверяют pending→review, подмену run/workflow/intent, bigint regression, background/cancel/late scope, access loss/read retry и terminal stop. Это не доказательство работы в реальном браузере/native runtime.

Физические fresh-assembly и fresh-unsupported сценарии теперь вызывают ReadRunIntent реальной ролью photo_api после worker/adopter assembly и committed review initialization: exact run/workflow, independently generated non-null intent ID, waiting_review и revision из планового fence. Неизвестные tenant/run/session и реально отозванная сессия не возвращают ресурс. Fixture восстанавливает свою сессию до дальнейших проверок. Это не полный restricted ACL/foreign-existing-tenant matrix и не реальный browser/native flow.

SemanticRunIntent и ReadRunIntent добавлены в существующий spec_v22.py генератор; JSON Schema, OpenAPI/endpoints и function-index перегенерированы. Исполняемая документация проверяет nullable/non-null identity, malformed response и editor GET operation.

Обновление общего schema SHA до 4092c7b6caacbfc2e6552f73a8e31a4cd53de3d5ca4ab165625d8423748d1d71 учтено в worker contract_lock. Предыдущий 91d32b65844eb40cb4d145901bc4ea737ac514a0c0a8ce64ad725d2f1858d849 сохранён как RUN_INTENT_PREVIOUS_SCHEMAS_SHA256 и принимается V2InputIndex для старых jobs. Добавление response DTO не меняет operation params/output; test_prior_run_intent_schema_pin_keeps_existing_jobs_readable проверяет старый pin через настоящий disk-backed input index. Это изменение schema pin не изменяет исторический source-baseline-lock.

Дополнительный physical ACL probe использует отдельного editor с собственной session/membership, сохраняя последнего owner tenant. Restricted project без grant и с viewer grant не раскрывает intent; editor grant разрешает чтение; archived project снова закрывает доступ. Fixture удаляет только созданные ею записи и восстанавливает исходную visibility/archived state. Проверка существующего чужого tenant с действующей membership и полного role matrix ещё не закрыта.

Общий V2 run URL теперь монтирует read-only semantic resolver и без `semanticWorkflow`: первая успешная выдача закрепляет workflow ID, последующие выдачи не вправе его менять. Первый 404 означает обычный не-semantic run и не показывает ошибку; потеря доступа после уже увиденного semantic intent скрывает ссылку и показывает отказ. Явный workflow hint по-прежнему сверяется с backend. Component/route unit-сценарии написаны, но не запускались в implementation-first фазе.

Следующий шаг: полная cross-tenant/role матрица и browser/native flow. Native producer orchestration и полный acceptance V27 также остаются открытыми.
