# OPEN — реестр открытых параметров

Статус `OPEN` = **не реализовывать и не подставлять значение** (RULES R5, R7). Безопасное поведение — в колонке «Пока». Статус меняется только владельцем продукта, с датой и источником решения.

| ID | Параметр | Статус | Пока (безопасное поведение) | Что нужно для закрытия | Источник |
|---|---|---|---|---|---|
| OPEN-01 | Цены тяжёлых операций, allowlist standard registry | OPEN | Неизвестная policy отклоняется в billing-enforced cohort; beta — отдельная конечная policy; существующий `v2_enabled` не активировать миграцией | Перечень context/canonical charge + benchmark | 09 §9, 17, 18 |
| OPEN-02 | Max file / output bytes, per-tenant concurrency / rate | OPEN | Переиспользовать текущий qualified safety profile; черновик плана не обещает непроверенных значений; новых unlimited/NULL нет | Опубликованные конечные лимиты по планам + evidence | 08, 09 §9 |
| OPEN-03 | Юридические сроки, legal scope PhotoEditor, approved backup retention, legal financial years | OPEN | Draft retention, синтетические fixtures; удаление необязательной PII не откладывать | Утверждённые сроки, тексты, архив версий, privacy review | 08, 09 §9 |
| OPEN-04 | Налоги вне BG test fixture | OPEN | Ответ `422` до создания Stripe order | Утверждённый TaxResolver + acceptance-тест по стране и статусу покупателя | 09 §9 |
| OPEN-05 | Native purchase distribution | OPEN | Capability `false`; Paper UI показывает read-only объяснение покупки | Реализация и квалификация канала + проверка правил распространения | 09 §9 |
| OPEN-06 | Долгая неоплата архива | OPEN | Overquota-состояние, без auto age purge | Опубликованные уведомления, read-export grace, ручной процесс закрытия | 09 §9, 08 |
| OPEN-07 | Выбор `transition_at`, уведомлений, beta entitlement по категориям существующих tenant | OPEN | Backfill только в режиме report-only; старый tenant не отключать молча при переходе на Trial 2GB | Решение по каждой категории после отчёта backfill | 08 |
| OPEN-08 | Приёмка 1Б.7 (миграция 02609) | BLOCKED | Cohort выключены; цены и production-qualified policies не публикуются | Совместный PostgreSQL/Go прогон (команды — в `TASKS.md` T-01) после исправления open-eyes consent authority и интеграции paid 02608 | 11, 18 |
| OPEN-09 | Capacity gate Studio Pro, benchmark тяжёлых операций | OPEN | Live-продажи закрыты | Результат capacity-проверки | README |
| OPEN-10 | Native pixel execution и fresh public compiler Editor | NOT QUALIFIED | Не заявлять как проверенные | Отдельный прогон | 11 (1Б.5) |
| OPEN-11 | Какие из 45 необработанных SQL-кодов ошибок биллинга пользовательские и какой HTTP-статус им дать | DECIDED 23.10.2026 | Решение владельца: группа A (26, внутренние) — остаётся 500; B (8, неверный запрос) — 422 `VALIDATION_FAILED`; C (8, конфликт состояния) — 409 новый код `BILLING_STATE_CONFLICT`; D (3, доказательство недоступно) — 503 `BILLING_UNAVAILABLE`. Источник: `contracts/error-classes.json` | Реализация в Go — задача T-02 | ERROR-CODES.md |
| OPEN-12 | Намеренно ли `billing.PrepareOperations` отбрасывает флаг `enabled` | RESOLVED 23.10.2026 | Намеренно. `billing_bind_operations` внутри SQL-обёртки seal сам первой строкой повторно вызывает `billing_prepare_operations` и возвращает NULL при выключенной когорте или уже покрытом run. Go-вызов нужен для порядка блокировок и раннего отказа. Поведение не меняется; добавлен комментарий в `operations.go` | — | operations-workflow.sql, selected-workflow.sql |
| OPEN-13 | Версия Stripe API и список событий с payload paths | OPEN | Версию не выдумывать; код читает `STRIPE_API_VERSION` из конфигурации | Integration spike на реальном sandbox (нужны ключи) | 05 |
| OPEN-14 | Stripe sandbox-аккаунты (dev, CI), ключи, webhook secrets, реквизиты продавца | WAITING owner | Stripe выключен, capability=false; код проверяется на fake-провайдере | Владелец добавляет значения в конфиг после кода; порядок — `STRIPE-CONFIG.md` | 05, STRIPE-CONFIG.md |

Как добавлять строку: ID по порядку, один параметр на строку, колонка «Пока» обязательна и конкретна («вернуть 422», «capability=false»), не «будет решено».
