# Этап 1Б.4: квоты V2 render

## Граница и включение

- Миграция 02605 добавляет закрытый opt-in `billing_render_controls.enabled=false`.
- Она не создаёт control rows, не включает runtime operations/qualification и не открывает продажи.
- Для cohort нужны active shadow accounting, текущий accounting period, разрешённые bootstrap credits и явная строка render control.
- Существующий Apply opt-in не включает render автоматически.
- Stripe остаётся test-only/disabled;
- UI не меняется.

- Подключены `SQLRenderDraftStore.BeginBatch` (пользовательский batch, в том числе submission Queue/recovery) и `Begin` (отдельный render).
- Новый request имеет `route=V2.Render`, `workflow_kind=v2_render`, `run_id` исполняющего standalone либо batch-родителя.
- `request_key` — UUID root run, который в публичном submission детерминированно связан с tenant/actor/исходным idempotency key; canonical hash — сохранённый config hash.
- В SQL admission, canonical source validation, billing records, claims, blocked tasks, physical quota и run сохраняются одной транзакцией.
- Ошибка любого шага откатывает всю транзакцию.
- Object-store IO остаётся снаружи.

- `PrepareRender` вызывается до существующего claim/begin/run/quota staging.
- Для render прежний staging требует эксклюзивной блокировки tenant: её берём до общего billing resource mutex, без опасного повышения SHARE→UPDATE после ожидания mutex.
- Холодная проверка ограничена 200ms и возвращает BILLING_UNAVAILABLE.
- Авторизация проверяется после ожидания; disabled cohort сохраняет старую обработку ошибок.

- `BindRender` вызывается после создания всех children, до commit.
- Связанные nodes/params hash, snapshot/version/master artifact, project, original verified asset и blocked task проверяются из SQL, не из клиентского billing proof.
- Source inventory материализуется один раз на admission; frames и child links связываются пакетно, без повторного чтения source цепочки для каждого исходника.
- Стандартная единица — один успешный AI-результат исходника SHA256 в tenant.
- Повторяющиеся snapshot entries/версии одного исходника дают один charge item, но каждый task получает frame link.
- Whole-batch known deficit отвергается целиком; ограниченный allowance разрешён только по прежним kernel правилам.
- Два бесплатных успешных повтора за 168ч и их удержание разделяются со стандартным Apply через общие photo identities/free slots.
- Повторы без результата не расходуют slot.

## Дочерние workflow и success authority

- Batch children ссылаются на те же charge items с relation=technical_child.
- Самостоятельный `billing_bind_render` для такого child отвергается BILLING_CHILD_NOT_ROOT.
- Root link не является дополнительным ticket.
- Retry/task attempt, input recovery, review derivatives, cache и preview не создают новой покупки или списания.
- Дополнительные operation credits здесь не вводятся: render.compose/1 зарегистрирован как included_standard, отдельная операция списывает 0; standard_photo ticket списывается отдельно один раз.

- Технический final render из `editor_effect_builds.final_run_id` остаётся частью существующего Editor Apply authority.
- Его SQL-вызов begin_v26_render_draft не проходит пользовательский Go Begin и не создаёт render ticket.
- Seal исключение возможно только по этому typed parent link.
- Это не подключение billing к dedicated Editor effects: их собственная success boundary остаётся отдельным этапом.

Seal wrapper требует frame funding для **каждого** render.compose task при включённом render control. Нельзя обойти billing прямыми legacy begin/seal SQL вызовами scheduler. Старый seal переименован, EXECUTE runtime/PUBLIC закрыт; все прежние qualification/source/activation guards сохраняются.

## Durable settlement и возврат

- Canonical adoption и review могут завершаться разными транзакциями.
- Funding уже удержан до исполнения;
- `RenderBatchFinalizer` выполняет bounded `billing_settle_next_render()` отдельной DB транзакцией.
- Это восстановимый отложенный settlement, а не атомарный debit вместе с adoption: до него projection показывает удержанные units, не дополнительный available balance.
- Политика optimistic allowance от задержки не расширяется. 168-часовое окно начинается с canonical adoption/decided review timestamp, а не с позднего прохода billing scanner.
- После рестарта scanner обнаруживает durable accepted request без необходимости памяти прежнего процесса.

- Settlement ждёт terminal root **и всех связанных runs**; pending review/worker retry не дают ни debit, ни refund.
- Затем в одной транзакции проверяются current canonical task result/attempt/fence, verified unexpired/versioned root, v2-adopt inbox, root/WorkingImage artifact links и exact decided selection.
- Только candidate selection либо не требующий review adopted результат считается успехом.
- Reject→base и skip возвращают funding; pending review сохраняет hold.
- Partial success по разным исходникам списывает только успешные items; несколько frames одного исходника дают один успех, если доступен хотя бы один принятый результат.

- Завершённый успешный child может сохранить доступный результат даже при последующей отмене batch-родителя; сам факт отмены родителя его не обнуляет.
- Отменённые children и failed/skipped tasks без опубликованного результата освобождают funding.
- Сам статус failed у child не отменяет уже доступный canonical task result: при частичном отказе такой результат сохраняет свой debit.
- Expired/неполный/несогласованный success proof **не трактуется как доказанный refund**: request остаётся accepted для повторной проверки/операторского разбора.
- API/worker не могут предоставить settlement hash или вызвать primitives.

- Scanner рассматривает до 16 подходящих requests, пропускает занятые tenant locks, откладывает неопределённый proof на 30с и продолжает другие requests.
- Ошибка BILLING_RENDER_PROOF_UNAVAILABLE изолирована savepoint: частичный финансовый settlement кандидата откатывается.
- Остальные SQL/инвариантные ошибки не скрываются.
- Terminal guard запрещает возвращать оплаченный/освобождённый child в активное состояние.
- Generic Apply cancellation settler отфильтровывает route V2.Render: он не должен преждевременно вернуть его funding.

## Таблицы, поля и ограничения

Авторитетные DDL, bodies и ACL находятся в `contracts/render-workflow.sql`; полный signature inventory — `contracts/render-functions.json`. Генератор `scripts/generate-billing-render.py --check` создаёт только 02605, исторические миграции и generators не переписываются.

| Структура | Поля и смысл |
|---|---|
| billing_render_controls | tenant_id PK/FK tenants; enabled boolean defaultfalse; policy_version int CHECK1. Настраивается только trusted operator, без публичного endpoint |
| billing_render_frames | tenant_id; charge_item_id; run_id; task_id; source_version_id; ordinal bigint>=0. PK(tenant,task); composite FK к usage_run_links(item,run), tasks и asset_versions. Все frames immutable, один task не принадлежит двум root requests |
| billing_render_results | tenant_id; charge_item_id; task_id; result_id; root_artifact_id; image_artifact_id; adopted_at (canonical adoption/review time). PK(tenant,item), composite FK к frames/tasks result/artifacts. Immutable typed proof фактического результата, не клиентский digest |
| billing_usage_requests | CHECK дополнен парой V2.Render/v2_render. Новое nullable render_reconcile_after timestamptz — due time повторной проверки; partial queue index по due/created/id для accepted render requests |
| billing_usage_charge_items | source_item_id становится nullable только для V2.Render, поскольку V2 snapshot не обязан содержать collection item. BEFORE trigger запрещает NULL у остальных маршрутов. source_version_id остаётся обязательным FK; operation_policy_id render policy с success_contract=render_adopted_selection_v1 |
| billing_usage_run_links | CHECK relation root/technical_child; существующий composite PK/FK сохранён |

Все три новые таблицы FORCE RLS, trusted owner policies, без прямого runtime DML. Results и frames защищены immutable triggers. Финансовые buckets, ledger, claims, tickets, budgets, free slots повторно используются без нового mutable баланса.

## Функции

| Сигнатура | Контракт / EXECUTE |
|---|---|
| billing_prepare_render(uuid tenant,uuid actor,uuid session,uuid project) → boolean | Проверка render cohort/current authority, tenant UPDATE→resource mutex, bounded expired issuer recovery; scheduler only |
| billing_render_source_frames(uuid tenant,uuid root) → TABLE(run_id,task_id,asset_version_id,ordinal,sha256) | Typed source inventory, один row/task даже при повторе версии в snapshot; owner only |
| billing_bind_render(uuid tenant,uuid root,uuid actor,uuid session,text instance) → uuid | Root request/quote/items/frames/tickets и конечное funding; exact hash replay возвращает прежний UUID; scheduler only |
| billing_settle_render(uuid tenant,uuid root) → boolean | Проверенный canonical результат→append proof + kernel finalize + slot/item/request status одной транзакцией; owner only |
| billing_settle_next_render() → boolean | Bounded durable reconciliation, authority после ожидания и сохранение неопределённого funding; scheduler only |
| seal_v2_graph(uuid tenant,uuid run) → boolean | Funding gate перед неизменённым прежним seal; scheduler only |
| billing_guard_render_terminal() → trigger | Запрет воскресить уже settled/released child; owner only |
| billing_guard_source_item() → trigger | Nullable source_item разрешён только V2.Render; owner only |
| billing_settle_apply_run(uuid tenant,uuid run) → void | Прежний алгоритм с исключением render requests; owner only |

SECURITY DEFINER функции используют fixed search_path и photo_fn_owner, PUBLIC revoked. Kernel finalize остаётся закрытым для scheduler/API/worker. Render submission HTTP/DTO сохраняются; доменные ошибки quota409 и unavailable503 проходят через существующий adapter.

## Проверка и критерии завершения

- Новый SQL набор проверяет standalone/batch success/failure/cancel, child dedup, exact replay, free reruns, finite allowance, whole-batch deficit, revoked authority, direct seal bypass, terminal resurrection, missing/expired/stale-fence proof, отсутствие starvation, review accept/reject/skip/pending.
- Независимые PostgreSQL connections проверяют duplicate bind/settlement, конкурирующие initial roots и finalization/cancellation/settlement race.
- Counters сверяются с append-only ledger conservation.

- Go integration test использует существующий полный typed source/style/frozen recipe fixture и настоящий SQLRenderDraftStore под photo_scheduler: deadline на реальном mutex, atomic staging, шесть concurrent replays, отказ независимому initial с rollback runs, реальный cancellation finalizer, отсутствие преждевременного Apply refund, render refund и revoked replay.
- Это DB/service integration с контролируемым object reader и synthetic binary provenance;
- GPU pixel execution/MinIO/Stripe не выдаются за проверенные.

- Завершение требует PASS нового contract/SQL/Go набора, прежних Apply/routed quota регрессий и V2 state/cancellation/manifest tests, всех пяти generator checks и Go migration bundle tests.
- При ошибках постороннего пакета полный Go build нельзя объявлять успешным: фиксируется конкретный blocker отдельно от проверенных пакетов.

```sh
python3 scripts/generate-billing-render.py --check
env PHOTO_TEST_DB_TEMPLATES=1 scripts/run-platform.sh .venv-platform/bin/python -m pytest -q --tb=short tests/contracts/test_billing_render_contract.py tests/integration/test_billing_render.py tests/integration/test_billing_render_go.py
scripts/run-platform.sh /usr/local/go/bin/go -C backend test ./internal/billing ./internal/v2render ./internal/workflow ./db
```

- Перед rollout применить 02605 при выключенном render control, обновить все API/scheduler процессы и проверить closed-cohort smoke cases; затем включить только тестовый cohort.
- Отключение control останавливает новые billing admissions, но **не отменяет durable funding**: scanner продолжает settlement принятых requests при active tenant/accounting.
- Downgrade миграций, удаление journal/claims и автоматический live Stripe недопустимы.
- Рабочую БД тесты не мигрируют.

> Результаты проверок этого этапа перенесены в [STATUS.md](STATUS.md).
