# RULES — правила для любого исполнителя (человека или модели)

Читать целиком перед любой задачей из `TASKS.md`. Правила пронумерованы: в тестах, PR и ревью ссылайтесь как `R7`.
Эти правила — выжимка из README, 09, 12, 14. При расхождении побеждает исходный документ, а расхождение нужно записать в `OPEN.md`.

## A. Что считать истиной (по убыванию приоритета)

- **R1.** Последние требования владельца: оптимистичная коммерческая блокировка; весь UI только в `apps/photographer` на React Native Paper.
- **R2.** Реальный код: SQL, Go, клиентские контракты. Новый код встраивается в них и не заменяет authorization, consent epoch, purge, idempotency, task adoption.
- **R3.** `contracts/*.sql` и `contracts/*.json` + раздел 09 (точные поля, FK, DTO). Если пример в другом файле менее подробен — действует 09.
- **R4.** Остальные docs — контекст. Их текст не доказывает, что что-то реализовано или что тесты прошли. Состояние смотреть только в `STATUS.md`.
- **R5.** Не нашли ответ в R1–R4 → не угадывать. Записать вопрос в `OPEN.md` и остановить задачу.

## B. Запреты (нарушение = задача не принята)

- **R6.** Никакого live Stripe: ни ключей, ни переключения. Только test/sandbox.
- **R7.** Не придумывать цены, лимиты, налоги, сроки хранения, юридические тексты. Всё, что в `OPEN.md` со статусом `OPEN`, — не реализовывать и не заполнять «разумным значением».
- **R8.** Не создавать бесконечные или NULL-лимиты. Не добавлять «unlimited fallback».
- **R9.** Не править существующие миграции (весь `backend/db/migrations/`; head сейчас 02656). Только новая forward-only миграция + новая версия source. Down-миграции не писать.
- **R10.** Не вызывать сеть и не делать внешних эффектов из SQL. Не держать долгий внешний вызов внутри транзакции с accounting mutex.
- **R11.** Не давать прямой DML на финансовые таблицы. Не выдавать `photo_worker` самостоятельный grant/settle.
- **R12.** `billing_finalize_funding_claim` и settlement-примитивы закрыты для API и worker. Клиентский proof hash — не полномочие.
- **R13.** Не добавлять UI биллинга в `apps/portal`. Hosted Checkout и Customer Portal Stripe — внешние страницы, открываются из Paper UI.
- **R14.** Не включать cohorts и `billing_workflow_controls.enabled` автоматически. Все cohorts выключены по умолчанию; включает оператор.
- **R15.** Не считать готовым то, что только сгенерировано (цены операций, юридические настройки, продажи).

## C. Инварианты функций (SQL)

- **R16.** Каждая новая функция: `SECURITY DEFINER`, `SET search_path=pg_catalog,photo`, `OWNER TO photo_fn_owner`, `REVOKE ALL … FROM PUBLIC`, затем `GRANT EXECUTE` только перечисленным ролям. Образец — `EXAMPLES.md` §1.
- **R17.** Роли EXECUTE: `photo_api` — quote/order/change/summary, issue admission; `photo_auth` — onboarding/profile privacy; `photo_scheduler` — settlement с доказанным canonical result и recovery; `photo_maintenance` — inbox/outbox/reconcile/expiry/erasure. Helpers — без EXECUTE (`execute_roles: []`).
- **R18.** Первой строкой тела: сверить `app.tenant_id` с `p_tenant`, затем `authorize_tenant(actor, session, tenant)`. Иначе `RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID'`.
- **R19.** Один пользовательский запрос = ровно один debit. Дочерние/технические run не создают второй debit.
- **R20.** Идемпотентность: точный повтор (тот же hash) возвращает прежний результат; тот же ключ с другим hash — конфликт. Связи tenant/user проверяются **до** выдачи сохранённого replay body.
- **R21.** Все времена — серверные (`clock_timestamp()`), сравнение после взятия accounting mutex.
- **R22.** Все количества и байты — `bigint >= 0`. PK новых таблиц — `uuid`. `*_at` — `timestamptz`. Период — только `starts_at/ends_at`.

## D. Порядок блокировок (нарушение даёт deadlock)

- **R23.** Один ресурс: `tenant SHARE → control SHARE → balance(tenant,resource) FOR UPDATE → allowance/buckets → lease/claim`.
- **R24.** Несколько ресурсов: брать billing resource locks в порядке `resource_code` **до** любых run/task/adoption locks.
- **R25.** Нельзя вызывать finalize/settle из кода, который уже держит task lock.
- **R26.** Известный дефицит квоты в batch → отклонить весь batch до создания задач.

## E. Миграции и генерация

- **R27.** Источник правды — файлы в `contracts/`. Генераторы `scripts/generate-billing-*.py` запускаются с `--check`; drift = ошибка.
- **R28.** Сгенерированный SQL вручную не править. Менять source и перегенерировать.
- **R29.** Перед заменой существующей функции миграция проверяет body hash, signature, owner, язык, security definer, search_path, ACL (см. 18). Drift прерывает транзакцию.
- **R30.** Миграции применять только к одноразовым тестовым БД. Рабочую БД не обновлять.

- **R31.** Новая SQL-функция не длиннее ~80 строк. Длиннее — делить на helper-функции с `execute_roles: []`.
- **R32.** Один файл документации — секции до ~4K токенов. Больше — делить (линтер проверяет).
- **R33.** Тесты пишутся вместе с кодом, в той же задаче и коммите (`TESTING.md`, T1). Задача без тестов не принята. Skip в приёмочном прогоне = провал (T5).

## F. Чек-лист перед коммитом

1. `python3 docs/billing/tools/lint-docs.py` → exit 0 (пробелы, ссылки, размеры).
   Если менялись `contracts/`, затем `python3 docs/billing/tools/gen-briefs.py` и `--check`.
2. Все затронутые генераторы: `python3 scripts/generate-billing-<name>.py --check` → PASS.
3. Тесты задачи написаны в этом же коммите и проходят (команды — в задаче), без skip.
4. Для новой функции: запись в `*-functions.json` совпадает с `GRANT/REVOKE` в SQL (R16, R17).
5. Новых открытых вопросов нет, либо они в `OPEN.md`.
6. В `STATUS.md` внесён результат (дата, команда, число PASS). Числа из разных прогонов не суммировать.
