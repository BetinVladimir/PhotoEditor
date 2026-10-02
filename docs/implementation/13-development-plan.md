# 13. План автоматической разработки

## Работа по пакетам

Пакеты P00–P18 — проверяемые единицы, зависимости в `contracts/work-packages.json`. Реализация пакета ограничена указанными контрактами, но включает обязательные тесты/миграции/схемы. UI shell можно создавать раньше backend, но закрывать пакет с mock-only backend запрещено. Независимые пакеты допускают отдельные ветки; параллельная работа не отменяет contract gates.

Каждый пакет декомпозируется по публичным методам из04/06/07/08/09/10. Для каждого метода: DTO/schema→validation/negative cases→implementation→unit test→integration at service boundary. Таблицы/constraints из02 обязательны до repository; нельзя сначала писать API с произвольными dict и потом «добавить типы».

## Шаблон задания модели

```text
Реализуй пакет <ID> из docs/implementation/contracts/work-packages.json.
Прочитай README, нормативные документы пакета, source inventory и актуальный код.
Сохрани все инварианты ADR; не меняй выбранные модели и пороги.
Создай предусмотренные функции, методы, структуры, миграции и проверки.
Запусти acceptance пакета и contract drift check; исправь найденные ошибки.
Обнови docs/implementation-progress/<ID>.json доказательствами.
Не объявляй неисполненные аппаратные/визуальные проверки пройденными.
```

Progress schema: `{package_id,status:not_started|in_progress|blocked_external|done,source_commit,changed_files,checks:[{id,command,exit_code,evidence}],blocked_gates,decisions}`. `done` только при всех required checks; аппаратно заблокированные проверки могут быть вынесены в зависимый release gate, но запись clearly states functional implemented / quality not accepted, не общий green release.

## Порядок

### P00. Основа monorepo и contract tooling

Зависимости: нет. Файлы: `backend/,workers/,apps/,packages/,tests/,Makefile`. Документы: 00 01 15.

Критерий завершения: contracts-check; legacy22 остаются зелёными; no placeholder implementation.

### P01. Совместимость, locks, модели и ICC

Зависимости: P00. Файлы: `resources/,deploy/images/,workers/photo_worker/cli/doctor.py`. Документы: 06 07 11.

Критерий завершения: all required model CPU smoke; MPS/CUDA reports; exact hashes licenses locks; blocked hardware explicit.

### P02. PostgreSQL и RLS

Зависимости: P00. Файлы: `backend/db/migrations/00001..00011,backend/db/queries`. Документы: 02 03.

Критерий завершения: DB01 DB02 DB03; real role negative tests; sqlc generation.

### P03. Auth, tenants, projects, ACL, SMTP

Зависимости: P02. Файлы: `backend/internal/auth,tenant,project,mail`. Документы: 03 04.

Критерий завершения: AUTH01 AUTH02; owner transfer race; SMTP unknown-result retry same OTP.

### P04. Каталог, snapshots, shares и публикации

Зависимости: P03. Файлы: `backend/internal/collection,share,selection`. Документы: 02 04 09.

Критерий завершения: IMM constraints; SHARE01 SHARE02; revision conflicts.

### P05. Outbox и scheduler runtime

Зависимости: P02, P03. Файлы: `backend/internal/queue,workflow,workers/photo_worker/runtime`. Документы: 05 06.

Критерий завершения: all fault boundaries; single canonical output; queue topology.

### P06. Multipart ingest и media

Зависимости: P04, P05, P01. Файлы: `backend/internal/upload,storage; workers/operations/asset,media`. Документы: 06 09.

Критерий завершения: UP01 UP02 UP03; thumb profiles; GC leases.

### P07. ZIP и download jobs

Зависимости: P04, P05, P06. Файлы: `backend/internal/download; workers/operations/archive`. Документы: 04 09.

Критерий завершения: DL01 DL02 DL03; no partial ready; intent policy.

### P08. Гостевой портал

Зависимости: P04, P06, P07. Файлы: `apps/portal,packages/locales,design-tokens`. Документы: 10.

Критерий завершения: SHARE flows; UI02 UI03 portal matrix; real uploads/downloads.

### P09. Цветовое ядро и StyleV2

Зависимости: P01, P05, P06. Файлы: `workers/algorithms/color,workers/contracts/style`. Документы: 07 15.

Критерий завершения: N01 N04 N05 N06; fixed/adaptive schemas, canonical bytes cross-language.

### P10. Analyze пар и версии профиля

Зависимости: P09. Файлы: `workers/operations/pair,style; backend/internal/style`. Документы: 04 07.

Критерий завершения: STYLE01 STYLE02; one and multi pairs; aggregate equal weights; provenance.

Статус: graph v3 полностью материализует регистрацию, optional geometry, color fit, face/skin признаки, beauty/defect evidence, aggregate и optional независимые holdout validate/package эпохи. Старые v1/v2 переходы сохранены; candidate/version сохраняют exact canonical provenance.

### P11. Apply DAG и результат новой коллекции

Зависимости: P09, P10, P04. Файлы: `backend/internal/run,workflow; workers/operations/image`. Документы: 04 05 07.

Критерий завершения: IMM01 IMM02 IMM03; partial/retry/cancel; fixed no hidden WB.

Статус: fixed и adaptive color/local ветви материализуются и исполняются sealed epoch-ами с exact canonical predecessor binding. Adaptive ветвь включает version-locked `mask.person` и `color.stats`; результат принимается в новую collection транзакционно. P11 остаётся незавершённым до включения обязательных geometry/series/beauty/defects/generation/composition/quality/export ветвей и проверок partial/cancel/retry всего полного графа.

### P12. Маски, отбор, серия, композиция

Зависимости: P01, P09, P05. Файлы: `workers/operations/mask,face,pose,collection,series,composition`. Документы: 06 08.

Критерий завершения: cull/group stable permutation; mask transforms; N08 corresponding metrics.

### P13. Beauty, defects, body, review

Зависимости: P11, P12. Файлы: `workers/operations/portrait,defects,geometry; backend/internal/review`. Документы: 04 08.

Критерий завершения: N07 REVIEW01; protected marks; strength0; no duplicate color fit.

### P14. Restoration, background, generation, HDR

Зависимости: P11, P12, P13. Файлы: `workers/operations/background,hdr,image/{denoise,generate,inpaint,outpaint,upscale}`. Документы: 06 08.

Критерий завершения: N08 heavy modules; all generation acceptance; budgets/fallbacks.

### P15. Полное приложение фотографа

Зависимости: P03, P04, P10, P11, P13, P14. Файлы: `apps/photographer,packages/api-client,locales`. Документы: 10.

Критерий завершения: all routes/panels web/iOS/Android; UI01 UI03; no mock-only paths.

### P16. Эксплуатация и deploy

Зависимости: P05, P07, P14. Файлы: `deploy/,backend/internal/maintenance,telemetry`. Документы: 09 11.

Критерий завершения: all profiles health checks; RESTORE01 GC01; secrets/ports; peak budgets.

### P17. Legacy importer и обновления

Зависимости: P06, P10, P11, P16. Файлы: `workers/cli/migrate; backend/internal/importer; tests/migration`. Документы: 00 02.

Критерий завершения: dry-run then resume import; original bytes unchanged; unsupported config fails explicitly.

### P18. Итоговая приёмка и выпуск

Зависимости: P08, P15, P16, P17. Файлы: `tests/,release/,docs/user/,docs/ops/`. Документы: 12 14.

Критерий завершения: all functional/fault/security/numerical/visual/performance/native gates; SBOM/locks/full delivery.

## Уточнение атомарных задач внутри пакетов

P02: каждая goose migration отдельно→constraints/RLS tests→security fn grants→sqlc. P05: persist task/outbox→publisher confirms→claim/heartbeat→fenced commit→event dedup→retry/cancel→reconcile→fair scheduling. Порядок важен: нельзя тестировать «resume» только наличием выходного файла.

P09: ICC/raw decode→reference float transforms→curves/LUT→tiling→StyleV2 schema/JCS→fixed renderer→Stats20/KNN/OOD. P10: pair register→holdout sampling→body fit hook disabled until P13→color fit→aggregate→validation/package→accept version. До P13 geometry-enabled analyze явно unavailable, а не silently ignored; release требует включённый путь.

P11: snapshot transaction→new output collection→compiled graph→per-item accepted commit→partial result→selection scopes→cancellation/retry. P12/P13/P14 расширяют registry+schema+DAG+UI capabilities одновременно; не включать поле schema, которому нет операции. P15 закрывает каждый экран с реальными сервисами, масками и review. P18 проверяет весь объём ТЗ, а не только базовый ColorProgram.

## Запрещённые сокращения

Не заменять PostgreSQL SQLite, Rabbit in-memory queue, MinIO local filesystem, Expo desktop GUI, реальные workers HTTP mocks. Не переносить whole run_batch в одну task. Не пропускать manual review для generation. Не писать latest checkpoints/не скачивать runtime weights. Не выдавать «всё готово» при отсутствующих модели/лицензии/аппаратной приёмке. Уменьшение объёма ТЗ требует отдельного явного изменения требований, не решения генератора.
