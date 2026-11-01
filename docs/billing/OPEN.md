# OPEN — реестр открытых параметров

Статус `OPEN` = **не реализовывать и не подставлять значение** (RULES R5, R7). Безопасное поведение — в колонке «Пока». Статус меняется только владельцем продукта, с датой и источником решения.

| ID | Параметр | Статус | Пока (безопасное поведение) | Что нужно для закрытия | Источник |
|---|---|---|---|---|---|
| OPEN-01 | Цены тяжёлых операций, allowlist standard registry | OPEN | Неизвестная policy отклоняется в billing-enforced cohort; beta — отдельная конечная policy; существующий `v2_enabled` не активировать миграцией | Результат OPEN-09 capacity benchmark, затем прайс-лист операций | 09 §9, 17, 18 |
| OPEN-02 | Max file / output bytes, per-tenant concurrency / rate | OPEN | Переиспользовать текущий qualified safety profile; черновик плана не обещает непроверенных значений; новых unlimited/NULL нет | Владелец предоставит конкретные лимиты по планам | 08, 09 §9 |
| OPEN-05 | Native purchase distribution | OPEN | Capability `false`; Paper UI показывает read-only объяснение покупки; квалификация запланирована — ждём параметры App Store / Google Play | Параметры канала распространения от владельца | 09 §9 |
| OPEN-07 | Выбор `transition_at`, уведомлений, beta entitlement по категориям существующих tenant | OPEN | Backfill только в режиме report-only; старый tenant не отключать молча при переходе на Trial 2GB | Владелец предоставит правила по каждой категории | 08 |
| OPEN-09 | Capacity gate Studio Pro, benchmark тяжёлых операций | OPEN | Live-продажи закрыты; benchmark запускается — ждём параметры тестового окружения | Параметры capacity-теста от владельца | README |
| OPEN-10 | Native pixel execution и fresh public compiler Editor | NOT QUALIFIED (следующий спринт) | Не заявлять как проверенные | Тестовое окружение GPU/NPU — запланировано на следующий спринт | 11 (1Б.5) |
| OPEN-14 | Stripe sandbox-аккаунты (dev, CI), ключи, webhook secrets, реквизиты продавца | IN PROGRESS | Ключи записываются в файлы вне репозитория (`~/.photo/secrets/`); env-ссылки — `STRIPE_SECRET_KEY_REF`, `STRIPE_WEBHOOK_SECRET_REF`; нужны ещё `STRIPE_ACCOUNT_ID`, `STRIPE_PORTAL_CONFIG_ID` | Владелец добавляет значения; код тестируется на fake-провайдере до завершения | 05, STRIPE-CONFIG.md |

Как добавлять строку: ID по порядку, один параметр на строку, колонка «Пока» обязательна и конкретна («вернуть 422», «capability=false»), не «будет решено».

---

## Закрытые вопросы (история решений — не реализовывать ничего нового)

| ID | Параметр | Статус | Итог | Источник |
|---|---|---|---|---|
| OPEN-03 | Юридические сроки, legal scope PhotoEditor, approved backup retention, legal financial years | DECIDED 01.11.2026 | Draft-значения на основе GDPR: финансовые данные 3650 д (10 лет), персданные 1095 д (3 года), системные логи 365 д (1 год), backup 90 д. Все функции помечаются `-- draft: pending privacy review`. Финальный privacy review до продакшна. | owner 01.11.2026 |
| OPEN-04 | Налоги вне BG test fixture | DECIDED 01.11.2026 | EU-страны (ISO 3166-1 alpha-2 в списке): draft 20% VAT, фиктура до TaxResolver. Остальные юрисдикции — 422 до Stripe order. Помечать как draft до утверждения TaxResolver. | owner 01.11.2026 |
| OPEN-06 | Долгая неоплата архива | DECIDED 01.11.2026 | Overquota-состояние без auto age purge. Уведомления и grace window — отдельная задача. Auto-purge не реализовывать. | owner 01.11.2026 |
| OPEN-08 | Приёмка 1Б.7 (миграция 02609) | READY 01.11.2026 | Consent authority исправлен. PostgreSQL+Go joint run готов к запуску (команды — `TASKS.md` T-01). Cohorts остаются выключены до прохождения acceptance. | owner 01.11.2026 |
| OPEN-13 | Версия Stripe API и список событий с payload paths | DECIDED 01.11.2026 | Версию не хардкодить; читать `STRIPE_API_VERSION` из конфигурации. Список событий — зафиксировать при integration spike с реальным sandbox (OPEN-14). | owner 01.11.2026 |
| OPEN-11 | HTTP-статусы для 45 SQL-кодов ошибок биллинга | DECIDED 23.10.2026 | Группа A (26, внутренние) — 500; B (8, неверный запрос) — 422 `VALIDATION_FAILED`; C (8, конфликт состояния) — 409 `BILLING_STATE_CONFLICT`; D (3, proof недоступен) — 503 `BILLING_UNAVAILABLE`. Источник: `contracts/error-classes.json`. Реализация — задача T-02. | ERROR-CODES.md |
| OPEN-12 | Намеренно ли `billing.PrepareOperations` отбрасывает флаг `enabled` | RESOLVED 23.10.2026 | Намеренно. `billing_bind_operations` внутри SQL-обёртки первой строкой повторно вызывает `billing_prepare_operations` и возвращает NULL при выключенной когорте или уже покрытом run. Go-вызов нужен для порядка блокировок и раннего отказа. Добавлен комментарий в `operations.go`. | operations-workflow.sql, selected-workflow.sql |
