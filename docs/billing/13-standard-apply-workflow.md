# Этап 1Б.2: рабочая граница стандартного Apply

Историческая граница 02602/02603; актуальное расширение маршрутов 02604 описано в [14-routed-apply-workflow.md](14-routed-apply-workflow.md).

## Что подключено

- Стандартный `run.Service.Apply` / `Run.Create(kind=apply)` использует учёт standard_photo в существующей admission-транзакции.
- Создание run, снимка выбора, physical quota reservation, billing request/items/tickets и idempotency receipt атомарно.
- Scheduler продолжает вызывать существующие `ApplyStore.Adopt` и `ApplyStore.Fail`: новые SQL-обёртки соединяют прежнюю canonical authority с учётом funding.
- Окончательная отмена возвращает funding в транзакции существующего finalize_run_cancellation.

- Поддержаны обе реальные версии Apply v3/v4, первый успешный результат исходника, два бесплатных успешных повтора за 168 часов, платные последующие повторы, частичный skip без результата, технический retry, окончательная ошибка и отмена.
- Во время промежуточных попыток worker ничего повторно не оплачивает: billing ticket относится к пользовательскому запросу, а не к task attempt.

- Существующие студии автоматически не подключаются.
- Новый `billing_workflow_controls.enabled` по умолчанию false, миграция не создаёт ни одной строки opt-in.
- Оператор закрытого тестового cohort должен отдельно подготовить active shadow accounting control, один текущий accounting period и разрешённый bootstrap grant, затем включить standard Apply.
- Это не включение продаж или всего биллинга: тарифы остаются draft, Stripe accounts test-only/disabled.
- Публичного endpoint для включения нет.

- Граница текущего кода — только route `Run.Apply`, включая делегирующий ему Run.Create.
- `SessionAnalysis.Apply`, SaveEdits, dedicated Editor effects, V2 render, previews, самостоятельные операции и download/egress этим переключателем не тарифицируются.
- У них иные parent/child authority и success boundaries; подключение требует отдельных wrappers, а не повторного списания на каждом техническом узле.
- До покрытия этих entrypoints и остальных publication gates продажи остаются закрыты.
- UI в этом этапе не меняется; дальнейший UI только React Native Paper в apps/photographer.

## Источники

- `contracts/workflow-schema.sql`: точная типизированная схема, FK, CHECK, unique, FORCE RLS, ACL и immutable history.
- `contracts/workflow-tables.json`: версия и inventory таблиц, ссылка на авторитетный DDL source.
- `contracts/workflow-functions.sql`: реализации и reviewed insertion в существующий finalizer.
- `contracts/workflow-functions.json`: точные signatures и EXECUTE matrix.
- `scripts/generate-billing-workflow.py`: генерирует исключительно новые forward-only `02602_billing_apply_bindings.sql` и `02603_billing_apply_workflow.sql`, поддерживает `--check`.
- `backend/internal/billing/workflow.go`: адаптер существующей pgx.Tx, cold deadline и domain errors; никакого provider/object I/O.
- `backend/internal/run/apply.go`: preflight внутри idempotency.Use до collection/run/quota locks; binding после создания apply_build, но до admission commit и появления executable tasks.

Исторические миграции 1–02601 не переписываются. Ранее реализованные kernel signatures и их runtime-запреты сохраняются. Генераторы/источники каждого подэтапа остаются pinned к своей паре миграций; последующие изменения добавляются новыми версиями.

## Таблицы и поля

- Все связи включают tenant_id; cross-tenant IDs отвергаются FK и scoped queries.
- Runtime API/worker/scheduler не получают прямой доступ к таблицам биллинга.
- Владельцу SQL-функций выданы минимальные DML для внутренних переходов; history имеет trigger-запрет UPDATE/DELETE.
- UUID — внутренние FK, не внешние провайдерские IDs.
- Полная SQL схема является обязательным источником типов и nullable.

| Таблица | Поля/инварианты |
|---|---|
| billing_workflow_controls | tenant_id PK/FK,created_at,enabled=false,policy_version=1. Включает только standard Apply; не заменяет профиль или plan publication. |
| billing_photo_identities | id,tenant_id,created_at,digest_algorithm=sha256,source_digest bytea32,first_success_at?,window_ends_at?,successful_user_reruns>=0. Unique tenant/algorithm/digest; окно ровно first_success+168 hours, не календарная неделя с DST. Digest берётся из `assets.original_artifact_id→artifacts.sha256`, а не из меняющейся обработанной версии; одинаковые оригинальные bytes внутри tenant имеют одну identity. |
| billing_cost_quotes | id,tenant_id,actor_id,request_hash32,snapshot_id,policy_version=1,max_quantity>=0,expires_at,state=accepted. Immutable server-side ceiling для legacy Apply: maximum — число source items; фактическое списание может быть меньше из-за dedup/free slots. Это не денежный quote и не публичный DTO/экран подтверждения. Клиент не передаёт сумму или цены. Публичный cost-quotes flow и финансовые quotes остаются следующим этапом. |
| billing_usage_requests | id,tenant_id,created_at,actor_user_id,session_id,request_key,route=Run.Apply,canonical_request_hash32,workflow_kind=standard_apply,cost_quote_id,run_id,period_id,status accepted/settled/canceled. Unique tenant/actor/route/key и tenant/run. Session хранит первоначальный audit context; replay проверяет текущую действующую сессию того же actor, а не требует вечного сохранения прежней сессии входа. |
| billing_usage_charge_items | id,tenant_id,usage_request_id,ordinal,photo_identity_id,source_item_id,source_version_id,resource_code=standard_photo,operation_policy_id,policy_version=1,charge_kind initial/free/paid,max_quantity0/1,state accepted/succeeded/failed. Один item на distinct исходник запроса; quantity=0 только free. |
| billing_photo_free_slots | id,tenant_id,photo_identity_id,slot_kind first_result/rerun_1/rerun_2,usage_charge_item_id?,state available/held/consumed/expired,held_at?,window_ends_at?. Unique photo/slot. First_result — сериализующий маркер первоначального **платного** исходника, не free credit. |
| billing_usage_tickets | id,tenant_id,request_id,charge_item_id,resource_code,funding_claim_id?,free_slot_id?,quantity0/1,state accepted/settled/released. Composite FK гарантирует тот же request и quantity charge item. Paid требует уникальный claim; free — конкретный удержанный slot и отсутствие funding claim. |
| billing_usage_run_links | PK tenant/charge_item/run,relation=root,created_at; tenant FK к item и настоящему runs. Технические attempts остаются внутри того же run, не создают новые charge items. Иные child-run relations этим этапом не разрешаются. |
| billing_result_references | id,tenant_id,charge_item_id,canonical_run_id,final_artifact_id,adopted_result_id,asset_version_id,adopted_at,authority_kind=run_final. Immutable, unique charge item; composite FK к root run link, реальные artifacts/task_results/asset_versions. Произвольный client result UUID не принимается. |

- Добавлена одна included_standard operation policy для registry `image.local/1`, charge_context=standard_root, policy_version1, success_contract=apply_final_adoption_v1, rerun_policy=standard_window.
- Она описывает включённый шаг; units_per_success=0 **для отдельного operation debit**.
- Standard photo debit формируется root ticket.
- Платные operation offers или фиктивные benchmark/Stripe refs не создаются.

## Admission и повторы

1. Существующие tenant/session/project preflight и idempotency receipt lock выполняются первыми.
2. `billing_prepare_apply(tenant,actor,session,route)` проверяет transaction tenant scope, live authorize_tenant и роль owner/admin/editor. Для supported opt-in берёт control SHARE и kernel resource mutex до collection/run/storage locks. После ожидания повторяет проверку текущих прав. Unsupported route или не включённый tenant возвращает false; существующее поведение сохраняется.
3. Go cold query deadline — 200ms. Ошибка/таймаут откатывают admission и receipt, возвращая BILLING_UNAVAILABLE503. Произвольного «разрешить при недоступной БД» нет: DB transaction всё равно нужна для durable run/ticket. Уже выделенный allowance конечен и хранится в kernel, не подменяется локальным счётчиком.
4. После существующих проверок и создания ещё не executable Apply-build `billing_bind_apply` заново проверяет authority, source snapshot/project ACL, тип/run creator, queued/unsealed/not-cancelled, отсутствие tasks и текущий period. Source proof требует verified asset, ready/versioned/not-expired original и соответствие snapshot текущей source version.
5. В resource mutex принимается price decision каждого distinct оригинала. Пока initial in-flight, независимый request получает PHOTO_INITIAL_IN_PROGRESS409. Technical retry/adoption retry использует тот же run/item/ticket.
6. При действующем окне атомарно удерживаются rerun_1/rerun_2. После исчерпания slots quantity=1. Освободившийся позже slot не меняет цену уже accepted paid request.
7. Known batch deficit проверяется целиком до executable tasks: если paid demand>1 и доступного paid funding недостаточно, вся TX отказывает. Однофотографический недостаток может использовать существующий единый allowance периода. Free requests не требуют paid balance и проходят даже при overrun, если сохраняются остальные authority/safety gates.
8. Используется конечный process instance UUID. Перед доменными locks preflight восстанавливает до 16 истёкших active issuers по индексированному tenant/resource/expiry, включая предыдущие process instances. Возвращается только проверенный unissued funding; accepted claims переходят с lease в draining и сохраняются. Это не сбрасывает budget или settled overrun. Текущий неистёкший DB lease переиспользуется; exhausted/expired lease закрывается только по unissued funding, затем выдаётся новая generation. Leases другого instance не создают дополнительные credits. Это root-level admission; model steps не читают billing catalog и не создают новые списания.
9. Exact binding replay возвращает прежний request UUID; несовпадение canonical hash/run — conflict. Финансовая uniqueness живёт независимо от TTL обычного API receipt. На API прежний exact 202 response возвращается существующим idempotency механизмом после current authority gates.

- In-memory entitlement cache/cold network fallback не добавлены: оптимизация этого этапа — конечные DB-backed порции, reuse lease и отсутствие billing работы на DAG steps.
- Нельзя считать поле cacheTTL в исходном catalog доказательством реализованного cache.
- Перенос process-local оптимизаций должен сохранить durable issue и общий allowance bound.

## Canonical success и возврат

- `adopt_apply_results` переименован в закрытый `adopt_apply_results_before_billing`.
- Новая публичная scheduler-функция берёт billing locks до прежних build/run/task locks, вызывает старый adopter и при true вызывает settlement **в той же TX**.
- Вся прежняя цепочка, включая atomic refusal rollback и Editor/source/inventory/current-authority guards, сохранена; права исполнения прежнего bypass отозваны.
- Если старый adopter отказывает, нет ни частичных новых asset versions, ни billing debit.
- Ошибка settlement откатывает также adoption.

- `billing_settle_apply_run` не принимает result/proof/hash от клиента.
- После mutex заново читает request и run под locks; ищет реально adopted output collection item с нужным source item/parent version, `asset_versions.created_by_run_id`, ready/versioned/nonexpired final artifact и его canonical task_result/attempt/fence/outputs.
- Сохраняет typed result reference и только затем завершает kernel funding claim.
- Policy snapshot хранит SHA-256 canonical JSON schema_version1 (included_reruns,policy_version,rerun_window_hours,success_contract).
- Внутренний terminal digest — настоящий SHA-256, он лишь dedup receipt; authority получена из существующих typed rows и adopter, а не из digest.

- Первый success оплачивает одну единицу, фиксирует неизменное окно и открывает два slots.
- Free success потребляет held slot без финансового zero debit; paid rerun списывает заранее pinned quantity.
- Completed run с легитимно skipped item без adopted result возвращает funding этого item.
- Failure/cancel не расходуют успешный rerun; initial marker или free hold возвращаются, либо slot истекает после конца окна.

- `fail_apply_build` аналогично обёрнут с сохранением token/lease/current-run checks.
- Только окончательно failed run возвращает funding; промежуточные retryable task attempts этого не делают.
- Late intermediate artifacts после окончательной ошибки не создают пользовательского результата: старый adopter отвергает failed/cancelled run.

- Cancellation finalizer сохраняет все нынешние guards и SKIP LOCKED; reviewed source insertion добавляет billing lock **до** existing run lock и settlement перед return1.
- Если источник функции больше не имеет ожидаемых anchors, миграция отказывает.
- Не устанавливается AFTER-run trigger, приобретающий billing locks в обратном порядке.

- После подтверждённого terminal accounting `billing_guard_terminal_run` запрещает перевод billed terminal run обратно в executable status.
- Run.Retry преобразует этот guard в RUN_NOT_RETRYABLE409, откатывая также task resets.
- Для нового пользовательского запуска нужен новый Apply admission; технические retry до terminal сохраняют исходный funding.
- Это исключает возобновление run после уже выполненного возврата.

## Полномочия и lock graph

- photo_api: только billing_prepare_apply и billing_bind_apply; оба проверяют actual current user/session/tenant/project scope.
- photo_scheduler: существующие публичные adopt_apply_results/fail_apply_build, теперь wrappers.
- photo_scheduler/photo_maintenance: существующий cancellation finalizer, с прежними grants.
- billing_lock_apply_run, billing_settle_apply_run и terminal trigger: только доверенный photo_fn_owner, никаких runtime/PUBLIC EXECUTE.
- Kernel grant/finalize/release helpers остаются закрытыми; worker не может заявить success или выдать себе bootstrap.

- Lock graph: auth/receipt → tenant/control SHARE → balance standard_photo mutex → коллекция/snapshot/physical reservation → apply build/run/task/adoption.
- Внутренние повторные kernel locks уже принадлежат той же TX.
- SQL canonical wrappers первыми берут тот же resource mutex.
- Обычный API Cancel берёт только run locks и не ждёт billing; финализация отмены отдельной TX следует billing→run.
- Разные resource/tenant не используют общий mutex.

Новые billing функции нельзя вызывать из конца произвольного parent workflow, уже захватившего run/task locks. Именно поэтому SessionAnalysis/Editor/V2 root wiring оставлен отдельными этапами с собственным lock graph, а не притворяется покрытым стандартным Apply.

## Тесты и критерий завершённости

```sh
python3 scripts/generate-billing-catalog.py --check
python3 scripts/generate-billing-quota.py --check
python3 scripts/generate-billing-workflow.py --check
env PHOTO_TEST_DB_TEMPLATES=1 scripts/run-platform.sh .venv-platform/bin/python -m pytest -q tests/contracts/test_billing_catalog_contract.py tests/contracts/test_billing_quota_contract.py tests/contracts/test_billing_workflow_contract.py tests/integration/test_billing_catalog.py tests/integration/test_billing_quota.py tests/integration/test_billing_apply.py tests/integration/test_apply_adoption_atomicity.py tests/integration/test_apply_failure_current_run.py
scripts/run-platform.sh /usr/local/go/bin/go -C backend test ./internal/billing ./internal/run ./db
```

Каждый SQL test получает собственную одноразовую БД/клон. Стандартный Apply test применяет всю цепочку до 02603. Shared migration templates принадлежат существующему test harness и удаляются им; рабочая БД не меняется.

Проверки включают:

- настоящее создание Apply через Go Service под photo_api: 202, exact replay, один request/ticket, отказ независимому initial request, cold deadline на реально занятом accounting mutex, retry после rollback timeout, Cancel→finalizer→возврат, отказ revoked-session replay;
- canonical Apply v3/v4, отказ после потенциального первого partial output, retry adoption без двойного результата/списания;
- final failure, cancellation, невозможность восстановить refunded terminal run;
- initial marker, два бесплатных success за окно, release slot после неудачи, pinned paid decision при освобождении соседнего slot;
- единичный optimistic overrun, free rerun при отсутствии paid units, отказ следующему paid request после исчерпания budget;
- batch known deficit со всем rollback, исходные counters и их независимый rebuild из append-only journal;
- отдельные PostgreSQL sessions/barriers: duplicate adoption, adoption против failure, конкурентные initial requests; refill после process restart и возврат только unissued истёкшего lease;
- закрытие старых SQL bypass и отсутствие API/worker EXECUTE на settlement primitive; deterministic migration/ACL/hook-order contracts;
- прежние kernel/catalog, canonical adoption и failure/cancellation регрессии.

- Готовность этапа: новые миграции применимы до текущего полного head, сквозные tests реально исполняют API и SQL (не skip/mock), журнал и counters сходятся, runtime default-disabled, новый код не включает live Stripe.
- Полные коммерческие publication gates, legal/privacy/erasure, paid fulfillment/renewals/rollover, все остальные workflow boundaries и пользовательский Paper billing UI остаются отдельными следующими этапами.

> Результаты проверок этого этапа перенесены в [STATUS.md](STATUS.md).
