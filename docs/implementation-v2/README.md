# PhotoEditor v2: спецификация доработки

Статус v2.2: готово к поэтапной реализации по результатам повторного ревью; известные блокирующие проектные разрывы закрыты. Это не свидетельство готовности приложения. База: миграции до 00195, существующие Go API/workflow, Python workers и Expo/Next клиенты. Дата: 2026-09-30.

## Объём

Закрываем предложения competitive-roadmap.md: production foundation, профессиональный редактор, Session Brain, обучение на подтверждённых коррекциях, on-set QA, сохранение личности, semantic batch, C2PA, локальный сервер студии, доставка, обмен с другими редакторами и перечисленные дополнительные фотооперации. «Все текущие предложения» означает этот конечный список, а не обещание превосходства над любым будущим конкурентом.

Принятая граница первого релиза и функции следующих версий: [решение по MVP от 2026-10-15](mvp-release-scope-2026-10-15.md). Оно включает весь доступный квалифицированный AI-функционал V27–V29 с решением фотографа по результату. Полный объём V20–V32 остаётся целью реализации.

## Порядок чтения и исполнения

1. [Архитектура и совместимость](01-architecture.md).
2. [Структуры и алгоритмы](02-contracts-algorithms.md).
3. [База и миграции](03-database.md).
4. [Сервисы и API](04-services-api.md).
5. [Фотооперации и модели](05-imaging.md).
6. [Редактор, доставка и интеграции](06-clients-integrations.md).
7. [План генерации и приёмка](07-execution-acceptance.md).
8. [Параметры операций](08-operation-params.md).
9. [Workflow и транзакции](09-workflow-transactions.md).
10. [Алгоритмы и калибровка](10-algorithms-calibration.md).
11. [Согласия и отзыв зависимостей](11-consent-lifecycle.md).
12. [Продукт и масштабирование](12-scale-product.md).
13. [Логика API и пользовательские цепочки](13-api-behavior.md).
14. [Закрытие замечаний и протокол кодогенерации](14-review-closure.md).
15. [Сквозные контракты исполнения](15-execution-contracts.md).
16. [Точные алгоритмы фотоопераций](16-imaging-algorithms.md).
17. [Интеграция, миграции и безопасность](17-integration-security.md).
18. [Реализуемое масштабирование](18-capacity-operations.md).
19. [Совместимость с worker/gateway protocol](19-runtime-protocol.md).
20. [Протокол повторного ревью](20-documentation-audit.md).
21. [Машинный каталог пакетов](work-packages.json) и [индекс функций](contracts/function-index.json).

Машинные контракты: [JSON Schema](contracts/schemas.json), [OpenAPI](contracts/openapi.json), [операции](contracts/operations.json), [база](contracts/database.json), [reference SQL](contracts/schema-reference.sql), [результат статической проверки](contracts/validation-report.json).

Версия2.2 согласует HTTP, БД, algorithms, artifact closure и реальные ограничения runtime. Актуальные количества и результаты проверок — в [общем отчёте](contracts/check-report.json).

Воспроизведение проверки из корня репозитория:

    PYTHONDONTWRITEBYTECODE=1 .venv-platform/bin/python docs/implementation-v2/check-docs.py

Генераторы создают только документы внутри этой директории. Не запускают БД и не изменяют production source.

Нормативные слова «должен», «запрещено» — требования. Численные SLA ниже являются целями до измерения. Параметры качества ML должны поставляться в версионированном calibration artifact. Отсутствующий calibration или approved model означает capability unavailable, а не успешный обход операции.

## Исправление исходной оценки

Предыдущая дорожная карта смешивает старый локальный pipeline и новую платформу. В текущем коде уже существуют WorkingImage/Canvas/TransformChain, ICC-aware decode, Apply v4, sensor dust review/repair, typed editor panels и undo/redo. Их нужно интегрировать и квалифицировать, а не создавать повторно. Статусы Pxx могут отставать от кода; отсутствие функции подтверждается трассировкой UI → API → compiler → worker → adoption, а не одной записью in_progress.

Проверенные точки расширения: backend/internal/workflow/apply_v4_plan.go; backend/internal/editor/service.go; workers/photo_worker/contracts/images.py; workers/photo_worker/contracts/style.py; workers/photo_worker/models/lock.py; workers/photo_worker/models/pool.py; apps/photographer/src/editor-store.ts; миграции 00192–00195.

## Что модель может и не может решить самостоятельно

Модель реализует зафиксированные ниже контракты, миграции и алгоритмы. Она не выдумывает веса, license approval, подписи C2PA, camera support, результаты benchmark или human acceptance. Доступ к железу, весам и корпусу является входом соответствующего пакета. Можно реализовать adapter и тесты, но пакет остаётся awaiting_evidence до реального прохода.

Файлы конкурентного анализа — исторический контекст. Имена и типы определяют contracts/*.json; транзакции и алгоритмы —03/09–19; дополнения15–19 обязательны и уточняют ранние главы. Версия2.2 имеет приоритет над исторической дорожной картой. Существующие security/integrity инварианты implementation v1 сохраняются.

Подробный меморандум по масштабированию в оформлении Legal Memorandum: [Word документ](PhotoEditor_scaling_memorandum.docx). Содержит подробный эксплуатационный контекст. Точные актуальные структуры, функции и persistence масштабирования —18; при расхождении нормативен18.
