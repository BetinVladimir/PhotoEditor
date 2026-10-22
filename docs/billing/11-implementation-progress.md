# Ход реализации биллинга

## Этап 1А — контракты каталога и начальные миграции (21.10.2026)

Реализован закрытый каркас каталога. Это часть этапа контрактов/миграций, а не завершение всего B1 или биллинга. HTTP API, UI, покупки, начисления и enforcement ещё не подключены.

### Источники и результат

- `contracts/tables.json`: schema_version=1, 10 таблиц в схеме `photo`, типы, nullable/default, PK, FK, CHECK, индексы, ACL и режим RLS.
- `contracts/functions.json`: явный пустой список. На этом этапе нет новых публичных SQL-функций и EXECUTE grants; бизнес-функции добавляются вместе с проверкой полномочий и runtime.
- `contracts/seed.json`: стабильные UUIDv5 по логической идентичности, суммы в EUR minor units без налога, bytes в десятичной системе. Это внутренний DB contract; wire DTO с bigint-строками будут отдельным этапом.
- `scripts/generate-billing-catalog.py`: отдельный детерминированный генератор; baseline-генератор и исторические миграции не меняются. `--check` завершается ошибкой при расхождении SQL и источников. SQL expressions в checks/constraints/indexes — доверенный код репозитория, не пользовательский ввод.
- `02598_billing_catalog_scaffold.sql`: планы, версии, цены, ресурсы, лимиты, предложения, аккаунты/цены провайдера, operation/admission policies.
- `02599_billing_catalog_seed.sql`: 7 plans, 7 draft versions, 6 prices, 8 resource types, 36 limits, 45 inactive offers, 2 disabled admission policies. Ноль provider accounts/bindings и operation policies. Повтор seed допускает только совпадающие строки; конфликт значений вызывает ошибку и rollback транзакции миграции.

Trial: 20 фото однократно, egress ежемесячно, storage 2 GB. Месячные фото платных планов имеют rollover_periods=1. Seed сохраняет все суммы из раздела 02; для Custom не создаётся неявных лимитов/цен. Неизвестные max_file_bytes и gpu_concurrency не подменяются придуманными значениями.

### Временные закрывающие ограничения

На этом этапе CHECK допускает только `status='draft'`, `self_service_enabled=false`, inactive offers, disabled admission policies/provider accounts. Эти ограничения намеренно строже полной целевой схемы раздела 03. Следующая **additive** миграция должна заменить их на публикационные guards после реализации полного policy snapshot, неизменяемости опубликованных строк, согласованности default prices/интервалов и runtime. Нельзя просто удалить CHECK для открытия продаж.

`catalog_scope` в версиях/ценах/offers допускает sandbox/live для будущих контрактов, но начальные данные только sandbox. Provider accounts сейчас допускают только `environment=test`, `catalog_scope=sandbox`, enabled=false. Live Stripe не реализован. Binding имеет XOR price/offer и composite FK по scope, local account namespace и уникальность external price. Провайдерские суммы/валюта/interval сверяются будущим provisioning до разрешения enabled; поле verified_at само по себе не доказывает обращение к Stripe.

FORCE RLS включён на всех 10 глобальных таблицах, policies пока отсутствуют. Runtime-роли не имеют DML; photo_fn_owner имеет SELECT privilege, но RLS возвращает пустой набор. Миграции применяет существующий привилегированный migration connection. Будущие ограниченные функции чтения/публикации вводятся вместе с точечными policies, проверками доступа и тестами; выдача runtime прямого SELECT не является следующим шагом.

Ни одна существующая студия не получает новый профиль, trial или квоту от этих миграций. Существующие tenant_usage, quota_reservations, auth и operation_registry не меняются. Каталог operation policies лишь ссылается на точную существующую пару operation/version; runtime операции этим не включаются.

### Проверки и критерий завершённости 1А

Команды из корня репозитория:

```sh
python3 scripts/generate-billing-catalog.py --check
scripts/run-platform.sh .venv-platform/bin/python -m pytest -q tests/contracts/test_billing_catalog_contract.py tests/integration/test_billing_catalog.py
scripts/run-platform.sh /usr/local/go/bin/go -C backend test ./db
```

SQL-тесты используют существующий harness с созданием/удалением отдельной случайной PostgreSQL БД. Большинство проверок применяют prerequisites 1–17 и новые миграции; отдельный test_catalog_at_full_migration_head применяет **всю** цепочку до 02599. Рабочая БД не мигрируется.

Проверяются воспроизводимость SQL, числа/цены/периоды seed, повтор seed и конфликт, CHECK/FK/unique/XOR, невозможность live Stripe и активации продаж, sandbox account namespace с NULLS NOT DISTINCT, typed enum/integer limits, точная ссылка на operation registry, read/write запреты runtime-ролей. Это DB integration и contract tests; пользовательских e2e оплаты/анбординга пока нет.

Результат проверки 21.10.2026: `--check` без расхождений; 38 contract/SQL tests прошли за 40.19s, отдельный full-head SQL test прошёл за 14.43s (всего 39); `go -C backend test ./db` — PASS. Этап 1А завершён. Интеграционные тесты не заменяют будущие проверки concurrency, ledger conservation, replay/settlement, RLS tenant isolation и real Stripe test-mode webhook lifecycle.

## Следующие этапы

1. **1Б: entitlement и ledger contracts/migrations.** Профиль/subject, периоды, beta/trial bootstrap, buckets, allocation/ledger, leases/tickets, durable optimistic allowance и cumulative overrun. Tenant composite FK/RLS, conservation и идемпотентный bootstrap. Совместимость с физическим storage учётом. Нужны реальные конкурентные DB tests до runtime wiring.
2. **1В: финансы, legal/privacy contracts/migrations.** Quote/order/invoice/outbox/inbox и erasure/consent модели, retention/encryption boundaries. Реализация auth/onboarding и удаление профиля отдельными проверяемыми изменениями.
3. **2: функции и backend.** Authorized summary/catalog, pricing/quotes, leases и success/failure settlement, optimistic блокировка, контролируемый egress. Подключать endpoints только с работающим handler и executable DTO.
4. **3: Stripe test-mode.** Test-only provisioning, secret-store refs, tax fixture scope, checkout/webhook/reconciliation/refunds, owner authority. Никаких live keys или переключения в live.
5. **4: UI и полные e2e.** Только `apps/photographer` и React Native Paper, локализация/auth/theme/workspace boundaries. Выбор тарифа, usage/лимиты, top-ups, orders/portal, legal/onboarding/privacy/deletion. Положительные и отрицательные сценарии раздела 08.

Для каждого этапа сохраняются отдельные миграции и автоматические тесты. Продажи остаются закрыты до всех evidence gates разделов 05, 07–10.

## Этап 1Б.1 — quota kernel

Добавлены внутренний учёт consumable квот, append-only journal, периодный optimistic allowance, lease/claim funding и конкурентные тесты. Точное покрытие, временные gates, источники и остающаяся интеграция: [12-quota-kernel.md](12-quota-kernel.md). В отличие от исходного состояния 1А, photo_fn_owner теперь имеет SELECT policies только для resource/admission catalogs; планы и продажи остаются закрыты. Весь этап 1Б пока не завершён: профили, paid allocations, rollover и workflow authority wrappers следуют отдельными изменениями.

Проверка 1Б.1 от 21.10.2026: **77 billing contract/SQL tests PASS**, включая реальные PostgreSQL concurrency и full-head02601 с grant/issue/settlement. Оба generator drift checks и Go migration bundle tests PASS. Рабочая БД не изменялась.

## Этап 1Б.2 — стандартный Apply workflow

Фактические Go/SQL admission и canonical adoption/failure/cancellation связаны с quota kernel для явного opt-in standard Apply. Добавлены requests, server cost ceiling, photo identities/free slots, charge items/tickets/root links/result refs; возврат, idempotency, cold deadline, restart recovery и конкурентные проверки. Подробная граница и оставшиеся workflow: [13-standard-apply-workflow.md](13-standard-apply-workflow.md). Нельзя считать глобальный enforcement/публикационные gates завершёнными по одному стандартному Apply.

Проверки 1Б.2: regression **106 PASS**, финальный прогон workflow **20 PASS** (counts пересекаются), действующий Go API photo_api проверен без skip. Go billing/run/workflow/taskrecovery/db и три generator drift checks PASS. Миграции02602/02603 применялись только к одноразовым тестовым БД; Stripe и production cohort не включались.

## Этап 1Б.3 — SessionAnalysis.Apply и SaveEdits

Единый route-aware binding пользовательского запроса и исполняющего Apply, без второго child debit; manual SaveEdits не использует billing funding/free slots. Миграция02604 расширяет opt-in, исторические миграции сохранены. Контракты, границы проверки и критерии завершения: [14-routed-apply-workflow.md](14-routed-apply-workflow.md).

Проверки 1Б.3: финальные **29 PASS** нового этапа, **51 PASS** непересекающихся прежних regression cases; Go billing/run/v2sessions/workflow/taskrecovery/db и четыре generator drift checks PASS. Настоящий SessionAnalysis parent commit, rollback после observed lock wait и concurrent receipt replay проверены. Stripe остаётся test-only; рабочая БД не обновлялась.

## Этап 1Б.4 — V2 render

Подключены атомарный staging и durable canonical settlement для standalone/batch V2 render, общие photo/free slots и единый ticket на исходник между children. Отдельный default-disabled render opt-in, seal bypass guard, terminal recovery и bounded reconciliation. Контракты, ограничения и тесты: [15-v2-render-workflow.md](15-v2-render-workflow.md).

Проверки 1Б.4 от21.10.2026: **30 PASS** нового render этапа; **68 PASS** прежних billing checks; **18 PASS** V2 state/manifest/cancel regressions. Это116 различных сценариев, проверенных отдельными прогонами. Обычные Go billing/v2render/workflow/run/db и пять generator checks PASS. Двойной child debit, partial-failure доступный результат, review routes, finite allowance, restart/proof recovery и concurrent bind/settlement/cancel покрыты. Отдельный render opt-in остаётся default-disabled; Stripe test-only; рабочая БД не менялась.


## Этап 1Б.5 — Editor effects

Миграция02606 подключает Editor.Effects к единому root ticket и shared standard_photo identity/free slots/finite allowance. Промежуточный Apply не завершает финансы; seal/final render — technical children, второй debit запрещён. Успех проверяется по принятому canonical final result, возврат — после durable failed + drain всех stage runs. Default-disabled отдельный Editor cohort, proof hold/recovery и bounded scheduler reconciliation. Подробные поля/функции/rollout: [16-editor-effects-workflow.md](16-editor-effects-workflow.md).

Реальный Go photo_api admission выявил и исправил отсутствующую SQL-функцию lock_editor_effect_identity, преждевременную заморозку Editor snapshot и несовместимость public replay с JSONB форматированием. Эти исправления покрыты реальным admission/replay тестом; native pixel execution и fresh public compiler этим тестом не квалифицируются.

Проверки этапа от21.10.2026: общий прогон **116 PASS** (billing contracts, Editor, Apply/routed и V2 render, включая реальные Go admission harnesses) на актуальном head02606; после последнего исправления public replay финальный прогон **30 PASS** (17 Editor integration scenarios,2 Editor contract tests,11 V2 state/manifest/cancel regressions). Прогоны пересекаются; числа не суммируются. Go run/workflow/v2render/db PASS после последнего изменения; billing package компилируется, собственных unit tests нет. Все6 generator drift checks PASS; whitespace и Python syntax checks PASS.

Миграция применялась только в одноразовых тестовых БД. Рабочая БД и Stripe configuration не менялись; Stripe остаётся test-only. Перед включением Editor cohort нужно завершить старые активные builds без tickets; общий коммерческий launch/остальные billing entrypoints остаются отдельными этапами.


## Этап 1Б.6 — отдельные фото-операции

Миграция02607 добавляет отдельный default-disabled operation cohort, context-specific paid_operation/free_beta accounting, pinned original/policy/resource/units, deferred claim scope и immutable financial identity. Рабочий V2 SealGraph preflight и общий SQL seal связывают funding с существующей native authority; отказ seal откатывает все новые claims. Внутренние prerequisite tasks и standard/Editor/Render children не получают второго debit; private trusted child link закрывает новые технические run. Success contract generic photo root требует canonical current-attempt result, exact verified/versioned task-root artifact link, v2-adopt и matching review. Финансовое завершение ждёт drain, частичный успех учитывается по leaf, неизвестный proof удерживает funding и не блокирует другие requests.

Внутри batch переиспользуется chunk одного resource; разные ресурсы блокируются в стабильном порядке, известный дефицит отклоняет весь batch. Одиночная единица использует только finite allowance kernel; beta имеет отдельный operation.beta.* resource и конечный per-policy/period budget. Current authority проверяется после quota/native seal ожиданий. Цены, operation offers и benchmark не выдумываются; специализированные selected/global/multi-input success contracts не квалифицируются generic policy автоматически. Подробная граница и release gates: [17-standalone-operations.md](17-standalone-operations.md).

Проверки от21.10.2026: общий billing regression **151 PASS** на02607 (все7 contract suites, operations/Editor/Apply/routed/V2 render, включая Go admission harnesses). После последнего post-wait authority check и расширения native Cancel harness финальный прогон **46 PASS**:33 operation integration scenarios,2 operation contract tests,11 V2 state/manifest/cancel regressions. Прогоны пересекаются, числа не суммируются. Go billing/v2input/workflow/run/v2render/db PASS;7 generator drift checks PASS; whitespace и Python syntax checks PASS. Go OperationColdAdmission запускается явно без skip и проверяет photo_scheduler preflight, cold deadline, actual seal refusal, настоящий photo_api Run.Cancel/replay/native drain/единственный возврат.

Миграция применялась только в isolated test databases. Рабочая БД, live Stripe, operation prices и публичные продажи не включались. Весь дальнейший UI по-прежнему только React Native Paper в apps/photographer.

## 1Б.7 — selected/final (подготовлен, runtime-приёмка не завершена)

Миграция02609 и native authority adapters шести операций, immutable selections, trusted preview children и local review finalizer: [18-selected-final-operations.md](18-selected-final-operations.md). Первичные native local/open-eyes проверки прошли; расширенный прогон обнаружил отсутствие повторной open-eyes consent authority, исправлено в source. Финальный совместный PostgreSQL/Go прогон после исправления и интеграции paid02608 обязателен. Статические проверки не снимают этот блокер; cohorts выключены, Stripe не менялся.
