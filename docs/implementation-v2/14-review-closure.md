# Закрытие замечаний ревью

Дата: 2026-09-30. Проверяется проект документации, не наличие реализации.

| Замечание | Принятое решение | Приёмка кода |
|---|---|---|
| R1: неоднозначный run kind и два состояния | kind=v2, runs.status sole authority, NOT NULL snapshot;09 | consumer matrix, cancel/adopt/finalizer race |
| R2: registry только в V32 создаёт цикл | каждый пакет владеет registry/output policies; work-packages.json,03 | package integration до следующего пакета |
| R3: несовместимый CorrectionEvent | единые CorrectionCreate/Event schemas, серверные actor/diff, точные nullable FK;03,10,11 | body/DB round-trip, fake actor, cross-session source |
| R4: невозможный holdout и identity split | минимум40 events/6 sessions, минимум2 holdout и4 train sessions; разные режимы;10 | недостаточные данные, lineage leakage, CV train-only |
| R5: неполная ревокация | grants/epochs, explicit example/use links, version eligibility, invalidation и cleanup;11 | revoke против train/adopt/activate/rollback |
| R6: неопределённые структуры/алгоритмы | JSON schemas, точные feature order, grouping, color deltas, ridge/loss, calibration;10 | cross-language fixtures + реальные quality gates |
| R7: неполная БД, array/object, конфликт draft links | database catalog/reference SQL, versioned documents, per-document refs, explicit grants;03 | actual migration, CAS, history GC, project FK |
| R8: неполные пользовательские сценарии/API | OpenAPI, handler behavior, consent/person/shot-list/draft/reference flows;13 | browser/native E2E и denied-role tests |

## Что значит «пригодно к кодогенерации»

Нормативные решения R1–R8 зафиксированы; дополнительно закрыты R09–R24, см. [повторное ревью](20-documentation-audit.md). Актуальная версия2.2 включает обязательные15–19; прежний проход2.1 сам по себе не был достаточной проверкой. Генерация выполняется пакетами с контрактными и интеграционными проверками, а не одним запросом «написать всё». Reference SQL необходимо превратить в реальные миграции с функциями/триггерами из03, а затем проверить на PostgreSQL. Наличие SQL файла не доказывает корректность upgrade.

Исходные ML веса, лицензии, calibration corpus, реальные камеры, C2PA key/certificate и оборудование являются входами квалификации. До получения evidence соответствующая capability unavailable, пакет awaiting_evidence; placeholder не засчитывается. Документация не превращает неизвестное качество модели в доказанный результат.

## Протокол генерации пакета

1. Проверить baseline и migrations head, сохранить regression evidence текущего quality pipeline.
2. Выбрать следующий пакет из work-packages.json; реализовать только его contracts/functions/tables/UI, вместе с registry/lifecycle integration.
3. Генерировать DTO из schemas; strict semantic validation по10/13/15–19; схема первична для имён и типов.
4. Прогнать upgrade/role/concurrency tests и feature-specific tests. Любое расхождение спецификации оформить явным изменением docs до исправления кода, не догадкой.
5. Приложить evidence manifest: commit, migration head, contract SHA, test commands/results, dataset/model/device hashes, unresolved external prerequisites.
6. accepted только при наличии всех required evidence. awaiting_evidence не означает implemented-and-qualified.

## Статическая проверка этого набора

Единый check-docs.py: build-database.py → build-spec.py → build-indexes.py → validate-spec.py → semantic-spec.py → повторная генерация/hash comparison. Последний использует jsonschema из существующего окружения проекта; ничего не устанавливает и не запускает БД. Проверяет schemas/refs, handlers, output policies, dependency order, migrations, FK target names, purge coverage, локальные ссылки, Markdown tables, structural witnesses и несколько численных golden checks.

Результат сохраняется в contracts/validation-report.json. structural-fixtures.json — только синтаксические witnesses; повтор UUID намеренный, это не training corpus и не доказательство semantic validity.
