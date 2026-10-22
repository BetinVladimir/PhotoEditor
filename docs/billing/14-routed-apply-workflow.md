# Этап 1Б.3: SessionAnalysis.Apply и SaveEdits

## Контракт реализации

Миграция `02604_billing_routed_apply.sql` расширяет opt-in `billing_workflow_controls.enabled` на `Run.Apply`, `SessionAnalysis.Apply` и SaveEdits со стилем. Перед обновлением оператор учитывает расширение cohort: отдельного автоматического включения или создания control rows нет. Accounting остаётся shadow, Stripe только test/disabled. Продажи закрыты.

Каждый пользовательский запрос получает ровно один `billing_usage_requests` с исходным route и соответствующим workflow_kind: `Run.Apply/standard_apply`, `SessionAnalysis.Apply/session_analysis_apply`, `Run.SaveEdits/save_edits_style`. Сочетания защищены одним CHECK. Новых таблиц и полей нет; прежние FK, RLS, immutable history и unique `(tenant_id,run_id)` сохранены. Unique `(tenant_id,actor_user_id,route,request_key)` разделяет receipt namespaces. Уже существующие строки сохраняют прежний смысл.

`SessionAnalysis.Apply` вызывает `ApplyWithCommit` с route родителя. Дочерний Apply — исполнитель этого запроса, а не новый платный запрос. `billing_usage_requests.run_id`, root run links и result references указывают на исполняющий Apply, существующий `v22_analysis_decisions` связывает его с analysis. Отдельной billing-записи для scene-analysis run не создаётся. Расчёт/preview анализа и стоимость самостоятельных операций этим этапом не подключены.

Порядок в одной транзакции: авторизация → idempotency receipt → billing prepare/mutex → source snapshot/run/build → binding/funding → существующий `finish_v22_analysis_apply` через commit hook → receipt commit. Hook проверяет revision/approved groups/recipe и typed parent-child authority; ошибка откатывает также билеты, claims и удержания. На exact replay hook не вызывается. Возврат происходит по окончательному состоянию исполняющего Apply, а не по статусу review-родителя. Mutex берётся до parent/run/task locks.

`SaveEdits` без StyleVersionID использует существующую identity pipeline и не создаёт billing request, photo identity, funding или free slot. Отсутствие доступного billing accounting не мешает ручному сохранению; существующая авторизация, physical quotas и валидация overrides сохраняются. SaveEdits со StyleVersionID использует общий standard_photo admission/settlement и два бесплатных успешных повтора за 168 часов того же исходника. Стиль, overrides и исходник проверяются существующим сервисом; сам факт route не позволяет объявить стилевую обработку бесплатной.

## Функции и права

Авторитетный additive SQL: `contracts/routed-workflow.sql`; генератор `scripts/generate-billing-routed-workflow.py --check`. Исторические источники 02602/02603 остаются pinned.

- `billing_prepare_apply(uuid tenant, uuid actor, uuid session, text route) → boolean`: прежняя проверка live authority, cohort, resource mutex и bounded recovery; allowlist расширен на три маршрута. Неизвестный маршрут не подключается.
- `billing_bind_routed_apply(uuid tenant, uuid actor, uuid session, uuid run, text key, bytea hash, text instance, text route) → uuid`: единый admission алгоритм для трёх маршрутов. Точные повторы возвращают прежний UUID; изменение hash/run конфликтует. Route записывается вместе с workflow_kind. Свежий SessionAnalysis child требует server-stored analysis_decision_sha; другие маршруты не могут забрать такой child. SaveEdits требует stored overrides_id и identity_style=false. Повторная fresh binding уже связанного run отвергается. Сохраняются kernel budget, batch known-deficit, lease fencing и pinned slot decisions.
- `billing_bind_apply(uuid,uuid,uuid,uuid,text,bytea,text) → uuid`: совместимая обёртка старого Run.Apply, делегирует единому алгоритму. Не создаёт второй механизм расхода.

Все три функции SECURITY DEFINER, fixed search_path, владелец photo_fn_owner, PUBLIC revoked, EXECUTE только photo_api. Settlement/finalize primitives по-прежнему недоступны API и worker. Существующие adopter/failure/cancellation wrappers используются без изменения результата и атомарности.

## Проверки и критерии завершения

`tests/integration/test_billing_routed_apply.py` проверяет оба маршрута на миграции02604: canonical adoption, отсутствие второго child debit, exact binding replay и изменённый hash, возвраты failure/cancellation, batch отказ с rollback, частичный adoption refusal, free reruns и возврат slot. Конкурентные тесты используют независимые PostgreSQL соединения: шесть adopter, adopter против failure, конкурирующие initial requests.

Go harness под photo_api исполняет настоящий SaveEdits и ApplyWithCommit: admission202, receipt replay, cold mutex timeout, retry, один ticket, отказ второму initial, cancellation refund, revoked-session replay. Проверяются шесть конкурентных exact receipt replays для обоих маршрутов. Для SessionAnalysis child-admission проверяется rollback при отказе тестового commit hook и отсутствие повторного commit на replay. Дополнительный полный Service.ApplyAnalysis test использует typed scene/plan/correction/recipe fixture с in-memory object store и настоящий finish_v22_analysis_apply: один request/ticket/claim, replay без object reads, возврат при отмене child. В двух сценариях отзыва сессии во время ожидания parent/event cursor locks проверяется rollback parent/child и ноль tickets/claims. Для manual SaveEdits проверяются успешное сохранение и replay при закрытом accounting, ноль billing tickets. Канонические output fixtures не исполняют обработку пикселей.

Завершение этапа требует PASS новых SQL/API/concurrency/contract tests, регрессий standard Apply и существующей V22 replay boundary, всех generator drift checks и Go billing/run/v2sessions/db tests. Миграция применяется только к одноразовым БД во время проверки. Рабочая БД не обновляется автоматически. Полный billing UI, checkout/webhooks, paid provisioning, deletion/PII и остальные workflows остаются отдельными этапами.

## Порядок развёртывания

Перед миграцией отключить billing_workflow_controls.enabled у тестового cohort, если он уже включён. Применить02604, обновить все API процессы до route-aware адаптера, проверить smoke cases, затем включить cohort обратно. Во время смешанного развёртывания старого/нового API этот флаг не включать: старый Bind использует namespace Run.Apply и не передаёт исходный route. Миграция forward-only; операционный rollback — отключение workflow control, а не удаление journal или downgrade SQL. До готовности публикационных gates допускается только закрытый shadow/test cohort.

Воспроизводимая проверка нового этапа:

```sh
python3 scripts/generate-billing-routed-workflow.py --check
scripts/run-platform.sh .venv-platform/bin/python -m pytest -q --tb=short tests/contracts/test_billing_routed_workflow_contract.py tests/integration/test_billing_routed_apply.py
scripts/run-platform.sh /usr/local/go/bin/go -C backend test ./internal/billing ./internal/run ./internal/v2sessions ./internal/workflow ./internal/taskrecovery ./db
```

При ошибке template isolation использовать обычные одноразовые БД без PHOTO_TEST_DB_TEMPLATES; это не причина ослаблять assertions или изменять shared PostgreSQL.

## Проверено 21.10.2026

- Финальный набор нового этапа: **29 PASS за128.14s**, включая реальные Go API и PostgreSQL races.
- Прежние workflow/contract/adoption/failure/V22 regression cases: **51 PASS**. Это непересекающийся с новым набором поднабор общего regression прогона; результаты нового набора взяты из финального повторного прогона после исправления admission fixture.
- Go billing/run/v2sessions/workflow/taskrecovery/db: PASS. Все четыре generator --check: PASS.
- Миграция применялась только в одноразовых тестовых БД. Реальное исполнение пиксельных worker и Stripe network calls в эти результаты не входят.
