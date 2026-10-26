# Бэклог 1В–4: задачи для ещё не реализованного

Сгенерировано `tools/gen-backlog.py` из строк 03/04/09 и сверки с `backend/db/migrations`. Вручную не править.
ВНИМАНИЕ: названия функций в 04 — целевые. Этапы 1Б реализовали часть из них под другими именами (см. 12–18 и `contracts/*-functions.json`). Перед каждой задачей `*-F*` проверить «возможно покрыта»: если функция уже есть, задачу не делать, а записать в `STATUS.md`, чем она покрыта.
Тесты пишутся вместе с кодом (`TESTING.md`, R33); приёмка модулей — группы `acceptance`, инфраструктура тестов — `testinfra`.
Порядок: сначала `*-00` (каркас группы), затем таблицы, затем функции, затем API. Зависимости — по пакетам BILL в `08-delivery-migrations-e2e.md`.

| Группа | Пакеты | Таблиц | Функций | Файлы |
|---|---|---|---|---|
| finance | BILL-04 | 15 | 11 | [BACKLOG-finance-1.md](BACKLOG-finance-1.md), [BACKLOG-finance-2.md](BACKLOG-finance-2.md), [BACKLOG-finance-3.md](BACKLOG-finance-3.md), [BACKLOG-finance-4.md](BACKLOG-finance-4.md), [BACKLOG-finance-5.md](BACKLOG-finance-5.md), [BACKLOG-finance-6.md](BACKLOG-finance-6.md) |
| provider | BILL-07/08 | 4 | 3 | [BACKLOG-provider-1.md](BACKLOG-provider-1.md), [BACKLOG-provider-2.md](BACKLOG-provider-2.md) |
| usage | BILL-05/06/09 | 7 | 20 | [BACKLOG-usage-1.md](BACKLOG-usage-1.md), [BACKLOG-usage-2.md](BACKLOG-usage-2.md), [BACKLOG-usage-3.md](BACKLOG-usage-3.md), [BACKLOG-usage-4.md](BACKLOG-usage-4.md), [BACKLOG-usage-5.md](BACKLOG-usage-5.md), [BACKLOG-usage-6.md](BACKLOG-usage-6.md) |
| legal | BILL-11/12 | 4 | 8 | [BACKLOG-legal-1.md](BACKLOG-legal-1.md), [BACKLOG-legal-2.md](BACKLOG-legal-2.md), [BACKLOG-legal-3.md](BACKLOG-legal-3.md) |
| api | BILL-10 | — | 16 операций | [BACKLOG-api-1.md](BACKLOG-api-1.md), [BACKLOG-api-2.md](BACKLOG-api-2.md) |
| stripe | BILL-07/08/09 | — | 17 задач | [BACKLOG-stripe-1.md](BACKLOG-stripe-1.md), [BACKLOG-stripe-2.md](BACKLOG-stripe-2.md), [BACKLOG-stripe-3.md](BACKLOG-stripe-3.md), [BACKLOG-stripe-4.md](BACKLOG-stripe-4.md) |
| ui | BILL-10 | — | 17 задач | [BACKLOG-ui-1.md](BACKLOG-ui-1.md), [BACKLOG-ui-2.md](BACKLOG-ui-2.md), [BACKLOG-ui-3.md](BACKLOG-ui-3.md), [BACKLOG-ui-4.md](BACKLOG-ui-4.md) |
| e2e | 08, 10 | — | 90 сценариев | [BACKLOG-e2e-1.md](BACKLOG-e2e-1.md), [BACKLOG-e2e-2.md](BACKLOG-e2e-2.md), [BACKLOG-e2e-3.md](BACKLOG-e2e-3.md), [BACKLOG-e2e-4.md](BACKLOG-e2e-4.md), [BACKLOG-e2e-5.md](BACKLOG-e2e-5.md), [BACKLOG-e2e-6.md](BACKLOG-e2e-6.md), [BACKLOG-e2e-7.md](BACKLOG-e2e-7.md), [BACKLOG-e2e-8.md](BACKLOG-e2e-8.md), [BACKLOG-e2e-9.md](BACKLOG-e2e-9.md), [BACKLOG-e2e-10.md](BACKLOG-e2e-10.md), [BACKLOG-e2e-11.md](BACKLOG-e2e-11.md), [BACKLOG-e2e-12.md](BACKLOG-e2e-12.md) |
| acceptance | ACC | — | 16 задач | [BACKLOG-acceptance-1.md](BACKLOG-acceptance-1.md), [BACKLOG-acceptance-2.md](BACKLOG-acceptance-2.md) |
| testinfra | TST | — | 5 задач | [BACKLOG-testinfra-1.md](BACKLOG-testinfra-1.md) |

Stripe-задачи не требуют ключей: проверяются на fake-провайдере (`STR-01`); `STR-14` и `STR-15` ждут ключей (`OPEN-14`, порядок — `STRIPE-CONFIG.md`).
e2e: каждый сценарий — один положительный и один отрицательный тест; сценарии зависят от остальных групп.
