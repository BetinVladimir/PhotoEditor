# GLOSSARY — словарь терминов биллинга

Минимальные определения; подробности — в указанных разделах. Ссылки на правила RULES.md вида `R16`.

## Основные понятия

**admission** — проверка и резервирование ресурсов перед запуском операции. Включает authority check, cohort guard, optimistic allowance выдачу и создание lease. Подробнее: `12-quota-kernel.md`, `13-standard-apply-workflow.md`.

**allowance (optimistic allowance)** — локальная временная квота, выдаваемая без ожидания полной блокировки. Ограничена `optimistic_allowance` в `billing_admission_policies` и `outstanding_quantity` в `billing_allowance_budgets`. Не то же самое, что paid bucket: при исчерпании не блокирует, а переключает на paid allocation. Раздел `02` (оптимистичные квоты), `R23`.

**bucket** (`billing_credit_buckets`) — конечный запас единиц ресурса (фото, байты и т.д.) с датой истечения. Источники: trial bootstrap, подписка (invoice line), докупка (topup), rollover. Остатки: `remaining`, `leased`, `consumed`. Никакого null-expiry без явной policy `never` (R8).

**canonical result** — единственный авторитетный успешный результат операции, признанный billing; именно он даёт право на settlement. Определяется per-operation success contract в `billing_operation_policies`. Техническое retry не порождает второй canonical result. Раздел `09`.

**cohort** — именованная группа billing-функциональности (например, `v2_apply`, `standalone_operations`). Включается оператором явно; по умолчанию выключена (R14). Cohort guard — первая проверка после authority check в SQL-функции.

**debit** — расход единиц из bucket или allowance. Один пользовательский запрос порождает ровно один debit (R19). Технические retry, дочерние run, форматы экспорта второй debit не создают.

**fence_token** — UUID, привязанный к конкретному lease; защищает от повторного использования устаревшего lease при конкурентном обновлении. В `billing_admission_leases`.

**funded run** — run, для которого admission завершился успешно: создан ticket (или claim) в состоянии `accepted`. Дальнейшие шаги — settlement при canonical result или release при отмене/провале.

**grant** — зачисление единиц в balance/bucket. Источники: план (monthly grant), покупка (topup), bootstrap (trial/beta), rollover. Событие в `billing_resource_ledger` с `event=grant, direction=credit`.

**idempotency key** — детерминированный ключ запроса; точный повтор (тот же hash) возвращает прежний результат, тот же ключ с другим hash — конфликт (R20).

**lease** (`billing_admission_leases`) — временное удержание ресурсов под конкретный instance/run. Поля: `allocated_quantity` (paid из bucket), `allowance_quantity` (optimistic), `issued_quantity` (реально выдано tickets), `expires_at`. Истёкший lease запрещает новые выдачи, не отменяет уже выданные.

**operation policy** (`billing_operation_policies`) — billing-класс, единица списания, success contract и rerun policy для конкретной пары (operation_code, operation_version). Immutable per policy_version. Классы: `included_standard`, `paid_operation`, `free_beta`, `disabled`.

**period** (`billing_periods`) — временной интервал начислений для tenant. Источники: trial (calendar UTC) или subscription anchor (Stripe). Egress и rollover привязаны к периоду. Одновременно только один open period.

**photo identity** (`billing_photo_identities`) — SHA-256 от оригинальных байтов файла + алгоритм, уникально идентифицирующий исходник в рамках tenant. Используется для бесплатных повторов и window. Является PII — стирается при erasure.

**proof hash** (`terminal_proof_hash32`, `request_hash`) — байтовый digest, доказывающий конкретный canonical result. Settlement принимается только с matching proof hash. Клиентский proof hash — не полномочие (R12).

**release** — возврат зарезервированных единиц из lease/ticket при отмене или неуспехе операции. Единицы возвращаются в `remaining` bucket или сбрасывают `allowance_quantity`.

**resource_code** — строковый идентификатор вида `standard_photo`, `storage_bytes`, `outgoing_bytes`, `operation.<code>`. Тип ресурса определяет единицу измерения и семантику.

**rollover** — перенос свободных единиц (`generation=0`) в следующий период (`generation=1`) при подтверждённой непрерывной оплате. Только для `rollover_periods=1`. Leased и принятые tickets не переносятся. Раздел `02`.

**settlement** — финальное списание после canonical result: ticket переходит в `settled`, lease allocation обновляется, ledger получает `event=settle`. Инициируется только `photo_scheduler` с доказанным result (R17, R25).

**tenant** — изолированный аккаунт (студия). Все billing-данные tenant-scoped; FK с `tenant_id` обязателен для каждой таблицы; cross-tenant смешение блокируется (R18, R22).

**ticket** (`billing_usage_tickets`) — расчётная единица одного принятого запроса на один ресурс. Состояния: `accepted → settled | released`. Zero-ticket — подтверждённый free-slot (бесплатный повтор). Раздел `09`.

## Аббревиатуры

| Аббревиатура | Значение |
|---|---|
| PII | Personally Identifiable Information — персональные данные |
| DML | Data Manipulation Language (INSERT/UPDATE/DELETE) |
| RLS | Row-Level Security (PostgreSQL) |
| EXECUTE grant | разрешение вызывать SQL-функцию |
| XOR | ровно одно из двух полей должно быть заполнено (CHECK) |
| FK | Foreign Key |
| TTL | Time-to-Live |
| ACL | Access Control List |
| DTO | Data Transfer Object (wire-формат API) |
| minor units | наименьшая денежная единица (для EUR: центы) |

## Связанные файлы

- Точные поля и инварианты таблиц: `03-data-model.md`
- Точные signatures функций: `contracts/*-functions.json`
- Продуктовые правила (тарифы, повторы, rollover): `02-product-and-quotas.md`
- Запреты: `RULES.md` R7–R15
