# Этап 1Б.1: учёт квот, журнал и ограниченный optimistic allowance

## Реализованная граница

- Добавлен внутренний PostgreSQL kernel коммерческого funding: grant → allocation lease → durable funding claim → terminal consumption/release.
- Его можно вызывать только из доверенного SQL-контекста владельца функций; рабочие API, scheduler, worker и maintenance пока не получают EXECUTE.
- Все изменения остаются в транзакции вызывающей стороны.

- Это завершённый слой учёта, но не включение коммерческого enforcement в существующие workflows.
- `billing_funding_claims` — обязательства выделенного funding, **не** `billing_usage_tickets` из целевой модели.
- Они не доказывают права пользователя, наличие cost quote, запуск workflow или adoption результата.
- Следующий слой создаст typed request/charge item/ticket/run/result связи и вызовет kernel внутри общей admission/adoption транзакции.
- Нет orphan UUID, обозначающих несуществующий run или платёж.

- Начальная миграция не создаёт control rows, периоды, grants или allowance budgets для существующих студий.
- Явный opt-in через `billing_accounting_controls` допускает только `mode=shadow`, с lifecycle active/deleting/closed.
- CHECK не разрешает enforce.
- Продажи, Stripe-подключения, HTTP endpoints и существующие задачи не меняются.

## Источники и миграции

- `contracts/quota-tables.json`: schema_version1; типы, nullable/default, PK/FK, CHECK, индексы и ACL 11 новых таблиц.
- `contracts/quota-functions.json`: 11 точных signatures, owner=photo_fn_owner, execute_roles=[] (никаких runtime grants).
- `contracts/quota-functions.sql`: проверяемый источник SQL-примитивов, без сетевых side effects.
- `scripts/generate-billing-quota.py`: генерирует исключительно 02600/02601, `--check` обнаруживает drift. Исторические миграции и каталог 02598/02599 не перегенерируются. SQL expressions и тела функций — доверенный source code, не принимаемый пользовательский ввод.
- `02600_billing_quota_accounting.sql`: таблицы, FORCE RLS, trusted-function policies, индексы.
- `02601_billing_quota_functions.sql`: SECURITY DEFINER функции с фиксированным search_path, REVOKE PUBLIC, immutable/pinned-budget triggers.

Миграции forward-only; Down отказывает. Применять их к рабочей БД в этой задаче не требуется. В тестах используется отдельная создаваемая и удаляемая БД.

## Таблицы и поля

- Все таблицы содержат created_at по серверному времени.
- Таблицы с surrogate ID имеют UUID PK; tenant-scoped references содержат tenant_id и resource_code, а lease/claim references также period_id.
- Межтенантное смешение даже настоящих UUID блокируется FK.
- Даты UTC, интервалы [starts_at,expires_at).
- Null expiry не означает unlimited.

| Таблица | Назначение и поля |
|---|---|
| billing_accounting_controls | tenant_id PK/FK; mode shadow; lifecycle; revision>0. Временный gate закрытого kernel, не замена будущего tenant_billing_profiles и не публичная настройка пользователя. |
| billing_periods | id,tenant_id,starts_at,ends_at,anchor_kind trial_calendar/subscription,state open/closing/closed. ends_at>starts_at; не более одного open period на tenant. Будущий period-boundary конструктор отвечает за непрерывность и отсутствие пересечения entitlement периодов; сейчас такого публичного конструктора нет. Ссылка на подписку добавится вместе с реальной subscription-таблицей. |
| billing_resource_balances | PK(tenant_id,resource_code),overrun_quantity>=0,revision>0. Строка — mutex всех accounting mutations данного ресурса студии; не копия storage quota. |
| billing_allowance_budgets | PK(tenant_id,resource_code,period_id); configured_quantity,outstanding_quantity,settled_quantity,policy_version,revision. outstanding+settled<=configured; configured/policy/identity immutable; settled не уменьшается. |
| billing_bootstrap_grants | id,tenant_id,resource_code,reason trial_once/beta_transition,quantity,starts_at,expires_at,expiry_policy fixed/never,policy_snapshot_hash32. Append-only; UNIQUE(tenant,reason,resource). trial_once допускает только standard_photo,20,never. Это только разрешённый оператором bootstrap, не подтверждение оплаты. |
| billing_credit_buckets | id,tenant_id,resource_code,bootstrap_grant_id; original/remaining/leased/consumed/revoked/transferred quantities; starts_at,expires_at,expiry_policy,source_type trial/beta_transition,state. UNIQUE(tenant,bootstrap_grant). Все составляющие >=0, original=remaining+leased+consumed+revoked+transferred. Never только для Trial standard_photo. paid entitlement FK, rollover origin и платные source_type добавятся вместе с соответствующей authority. |
| billing_admission_leases | id,tenant_id,resource_code,period_id,instance_id,generation,fence_token,request_key,policy_version,allocated_quantity (paid),allowance_quantity,issued_quantity,returned_quantity,expires_at,state active/draining/closed. Unique request и generation в tenant/resource/period; unique fence; issued+returned<=paid+allowance. |
| billing_lease_allocations | id,tenant_id,resource_code,period_id,lease_id,bucket_id?,source paid/allowance,quantity,issued_quantity,returned_quantity. Paid требует bucket, allowance запрещает bucket. Unique NULLS NOT DISTINCT(tenant,lease,bucket,source); issued+returned<=quantity. |
| billing_funding_claims | id,tenant_id,resource_code,period_id,lease_id,request_key,request_hash32,quantity,state accepted/settled/released,confirmed_quantity?,terminal_proof_hash32?. Unique(tenant,resource,request_key), не по техническому retry. Accepted не имеет terminal fields; terminal всегда имеет итог и proof digest. |
| billing_funding_claim_allocations | id,tenant_id,resource_code,period_id,lease_id,claim_id,lease_allocation_id,quantity>0. Append-only split funding, одна строка на claim/allocation; composite FK связывает их с одним lease/ресурсом/периодом. |
| billing_resource_ledger | id,tenant_id,resource_code,bucket_id?,lease_id?,period_id?,claim_id?,event_type,quantity>0,direction,business_key. Append-only, UNIQUE(tenant,business_key), индекс для cursor по tenant/resource/created_at/id. Направление согласовано с типом события. |

- Для policy/resource catalog добавлены только SELECT policies для photo_fn_owner на billing_resource_types и billing_admission_policies.
- Каталог plans остаётся закрытым.
- Runtime-роли не получают прямого доступа к новым таблицам.
- Глобальный trusted-function owner — доверенный внутренний контекст, а не tenant membership; его нельзя выдавать приложению как роль подключения.

## Функции

- Точные типы аргументов/результатов находятся в SQL source и functions JSON.
- Все ошибки kernel имеют SQLSTATE P0001 и стабильное имя BILLING_*; структурные CHECK/FK/unique ошибки используют стандартные SQLSTATE.
- Будущий transport adapter преобразует их в существующий domain error envelope, не возвращая сырые SQL ошибки клиенту.

| Функция | Контракт |
|---|---|
| billing_quota_lock(tenant,resource) → void | Берёт SHARE lock активного tenant и shadow-control; запрещает deleting/closed/no-control. Принимает только consumable integer resource, не storage capacity. Создаёт при необходимости balance row и берёт её FOR UPDATE. Повторная проверка lifecycle выполняется и при replay. |
| billing_append_quota_event(...) → void | Внутренний append helper. Его нельзя вызывать в обход доменной мутации и mutex; отдельный runtime EXECUTE отсутствует. Duplicate key не игнорируется: ошибка откатывает неверный повтор. |
| billing_grant_bootstrap(tenant,grant_id) → bucket UUID | Только существующий immutable bootstrap данной студии, с текущим сроком. Повтор возвращает тот же bucket. Создаёт grant event; покрывает имеющийся overrun из нового remaining→consumed в той же TX; settled allowance не меняет. |
| billing_allocate_admission_lease(tenant,resource,period,instance,generation,policy_version,key) → lease UUID | Проверяет current open period, pinned policy, local lease count и отсутствие overrun. Читает доступные buckets при refill, сначала ближайший expiry. Paid funding уменьшает remaining и увеличивает leased. Только недостаток chunk может резервироваться из configured-outstanding-settled allowance этого периода. При нуле paid и allowance — BILLING_QUOTA_EXCEEDED. Exact replay возвращает прежний lease без выдачи нового funding; несовпадение параметров — conflict. |
| billing_issue_funding_claim(tenant,resource,lease,fence,key,hash,quantity) → claim UUID | Проверяет текущую lifecycle, tenant/resource/fence, активность/expiry lease, локальный остаток и request replay. Создаёт durable claim и split allocations; увеличивает issued counters. Берёт funding по expiry, paid до allowance. Не перечитывает весь каталог и не агрегирует всю историю claims при каждом issue. |
| billing_finalize_funding_claim(tenant,resource,claim,confirmed,proof_hash) → claim UUID | Доверенный terminal primitive. confirmed=0 — release; standard_photo/operation — только 0 либо весь claim; outgoing_bytes поддерживает частичный terminal flush. Paid leased→consumed для использованной части, неиспользованная часть возвращается в исходный remaining либо revoked после expiry. Allowance outstanding уменьшается, settled и overrun увеличиваются только на использованную часть. Exact replay не меняет счётчики; другой итог/digest отклоняется. |
| billing_close_admission_lease(tenant,resource,lease,fence) → lease UUID | Сверяет issued counters с durable claim/allocation rows. Возвращает только quantity-issued-returned. Принятые claims остаются funded, state=draining; после их terminal state=closed. Повтор не возвращает funding снова. |
| billing_expire_quota_buckets(tenant,resource,limit) → count | limit1..1000, server clock. Только remaining истёкших buckets→revoked, без снятия leased и без затрагивания never Trial. Повтор не создаёт расхода. |
| billing_project_quota(tenant,resource) → typed row | В одном snapshot возвращает available,allocated,consumed,revoked,overrun,revision только для текущего активного tenant/control. available=max(current remaining-overrun,0); allocated=paid leased; consumed=sum(bucket consumed)+uncovered overrun. Поэтому cover не удваивает и не меняет факт потребления. Отсутствующий scope возвращает пустой результат. Это внутренний projection, не окончательный API DTO. |
| billing_quota_reject_immutable() / billing_quota_pin_budget() | Trigger helpers: запрет UPDATE/DELETE journal/bootstrap/claim-allocation history; запрет изменения identity/configuration бюджета и уменьшения settled allowance. |

- `terminal_proof_hash` обеспечивает неизменяемость/replay, **не проверяет** canonical adoption, состояние run, отмену или получение файла.
- До появления typed authority wrapper нет EXECUTE для рабочих ролей и нет подключения kernel к endpoint/worker.
- Произвольный UUID или hash от клиента не должен превращаться в proof.

## Состояния и ограничение превышения

- Бюджет allowance создаётся один раз для tenant/resource/accounting period из pinned policy.
- Исходная политика standard_photo: chunk5, lease10s, cacheTTL5s, allowance1, max_local_leases1, cold_timeout200ms.
- Для outgoing_bytes chunk/allowance16MiB.
- Строки политики остаются disabled; в закрытом shadow kernel операторский opt-in разрешает их тестировать.
- In-process cache, cold timeout/fallback и production routing будут реализованы в admission adapter; эти поля ещё не означают наличие такого адаптера.

- Outstanding включает весь зарезервированный allowance, даже пока claim не выдан.
- Close возвращает только unissued, terminal failure — только подтверждённо неиспользованный claim.
- Использованный allowance переходит в settled и overrun.
- Lease restart, новая generation, новый instance и cover не уменьшают settled.
- Смена policy в пределах того же resource/period отклоняется.
- Новый период может получить отдельный бюджет только после закрытия старого и явного создания maintenance/paid-period функцией будущего этапа; request path новые периоды не создаёт.

Истечение lease запрещает новые claims; ранее выданные не исчезают. Lease expiry ограничен серверным временем + lease_seconds, концом accounting period и ближайшим expiry выделенного bucket. Never Trial funding всё равно использует конечный lease и accounting period для allowance.

- max_local_leases считает только active issuers данного instance/resource (включая старые периоды): перед refill исчерпанный lease закрывают.
- Draining lease не выдаёт новые claims, хранит funding прежних claims и не блокирует новый refill из оставшегося paid баланса.
- Полный объём funded obligations по-прежнему вычтен из paid buckets и единого allowance budget.

Оплаченные новые grants в будущем обязаны использовать тот же resource mutex и cover rule. Сейчас реализованы только типизированные bootstrap grants: здесь нет фиктивного payment proof или временной возможности выдать paid grant через owner API.

## Порядок блокировок и стоимость

- Текущий kernel: tenant SHARE → control SHARE → balance(tenant,resource) FOR UPDATE → allowance/buckets → lease/claim.
- Balance row сериализует все операции данного ресурса, включая пути, которые после неё обращаются к lease и bucket; обратные вложенные пути для той же пары не выполняются параллельно.
- Между студиями и ресурсами общего mutex нет.
- При выдаче используются только текущий lease и его allocations; суммирование durable claims делается при close/recovery.

- Будущий много-ресурсный wrapper берёт billing resource locks в resource_code order **до** run/task/adoption locks, сохраняя существующие auth/idempotency preflight.
- Нельзя подключать finalize в конце уже захватившего task lock кода.
- При недостатке известной batch-квоты wrapper отклоняет весь batch до создания executable tasks; low-level allocation chunk не является такой проверкой.

Все времена сравниваются с серверным clock после получения accounting mutex. Долгий внешний вызов внутри этих транзакций запрещён. SQL row accounting нужен для durable issue/terminal; задачи сетевого cache/cold-fallback и benchmarking не подменяются этой реализацией.

## Проверки и критерий завершённости

```sh
python3 scripts/generate-billing-catalog.py --check
python3 scripts/generate-billing-quota.py --check
scripts/run-platform.sh .venv-platform/bin/python -m pytest -q tests/contracts/test_billing_catalog_contract.py tests/contracts/test_billing_quota_contract.py tests/integration/test_billing_catalog.py tests/integration/test_billing_quota.py
scripts/run-platform.sh /usr/local/go/bin/go -C backend test ./db
```

- Тесты создают случайные БД и удаляют их.
- Обычно применяются prerequisites1–17 и новые billing migrations; test_quota_full_migration_head проверяет **всю** цепочку до 02601, отсутствие автоматического opt-in и полный grant→lease→claim→settlement→close цикл после всех текущих schema/security изменений.
- Конкурентные тесты используют отдельные PostgreSQL sessions, barriers, конечные lock/statement timeouts и роль photo_fn_owner; последовательные mock-вызовы за concurrency не выдаются.

- Положительные: grant/lease/issue/success, failure/release, duplicate replay, partial egress, mixed funding, expiry/late success/failure, pending recovery, refill при draining, overrun cover без повторного допуска.
- Баланс каждого bucket и resource overrun независимо пересобираются из journal и сверяются со счётчиками.

- Отрицательные: межтенантные ссылки, replay с другим payload/outcome, stale fence/generation, local lease cap, period/policy mismatch, storage вместо consumable, некорректные/null/overflow-sized quantities, попытки уменьшить settled allowance, активация enforce, ledger UPDATE/DELETE, отсутствие runtime/PUBLIC EXECUTE и отсутствие автоматических grants существующим студиям.
- Откат внешней admission TX удаляет все её lease/claim/ledger mutations.

- Гонки: за последние paid units и общий allowance; конкурентная выдача разных claims; exact issue replay; bootstrap/lease replay; success против failure вместе с close; duplicate terminal success; expiry/close/terminal без deadlock.
- Допустимые исходы проверяются по суммам и unique business events, не по тому, какой поток победил.

- Критерий 1Б.1: все команды выше проходят, journal reconstruction совпадает, aggregate allowance bound сохраняется при гонках, полный migration head применим, runtime остаётся закрыт.
- Пользовательские billing/e2e, профили/financial subjects, paid allocation/renewal/rollover/refund, free rerun slots, controlled delivery receipts и actual workflow admission/adoption ещё не реализованы.
- Следующий этап должен добавить их authority bindings до первого разрешённого runtime вызова.

Cursor API журнала пока не подключён. Пара created_at/id служит индексом просмотра, но не обещает commit-order change feed: будущий incremental projector должен использовать согласованный snapshot/sequence protocol, не пропускать поздно committed транзакции по wall-clock cursor.

> Результаты проверок этого этапа перенесены в [STATUS.md](STATUS.md).
