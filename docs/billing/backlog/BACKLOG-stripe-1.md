# Бэклог: Stripe-провижининг (BILL-07/08/09), часть 1

### STR-00 — Конфигурация Stripe и проверка при старте

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`). Контракт: `STRIPE-CONFIG.md`.
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: backend/internal/config/config.go: поля и валидация; backend/internal/config/config_test.go; deploy/env/production.env.example (значения REPLACE).
- Критерий: A. `BILLING_STRIPE_ENABLED` не true → ошибок нет. B. true без любой переменной → ошибка без значений секретов. C. `STRIPE_ENVIRONMENT` не `test` → ошибка. D. Относительный путь `*_REF` → ошибка. E. `go test ./internal/config` PASS.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

| Keys | Secret API credential, public key только если реально нужен, webhook signing secret | Никаких sk/whsec в git/docs/logs; startup env guard |
- Secrets refs: STRIPE_SECRET_KEY_REF, STRIPE_WEBHOOK_SECRET_REF, STRIPE_ACCOUNT_ID, STRIPE_ENVIRONMENT, STRIPE_API_VERSION, STRIPE_PORTAL_CONFIG_ID.
- Это целевые имена конфигурации, не действующие env vars.
- Не создавать public key, если Hosted Checkout redirect обходится без него.
- Startup сравнивает configured environment/account с server response и price mappings; mismatch закрывает создание Checkout.

### STR-01 — Интерфейс провайдера и fake-реализация

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Новый пакет Go (имя выбрать по соседним: `backend/internal/billingprovider`, проверить отсутствие конфликта): интерфейс (Product, Price, Customer, Checkout Session, Invoice, Refund, чтение объектов и событий), in-memory fake с идемпотентностью по `provider_request_key`, real-адаптер-заглушка, возвращающий `BILLING_UNAVAILABLE`, пока нет конфигурации.
- Критерий: A. Один набор контрактных тестов прогоняется на fake (позже на real). B. В тестах нет сети. C. Повтор того же `provider_request_key` возвращает прежний ответ. D. Timeout после успеха не разрешается новым ключом, а чтением объекта.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

- API version фиксируется после integration spike реальной доступной версии аккаунта; не брать выдуманный номер из документации.
- Использовать стабильный поддерживаемый SDK и согласованную webhook version.
- Базовый план использует Customer API; доступность и преимущества Accounts v2 отдельно оценить в spike, не делать preview зависимость обязательной для оплаты.
- Каждый provider effect имеет persisted provider_request_key, request hash и response refs.
- Timeout после Stripe success не разрешается новым key.

### STR-02 — Provisioning Products и Prices

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Функция/команда provisioning по catalog version и account ref; запись mapping в БД (таблицы из группы `finance`/`provider`); повтор без дублей.
- Критерий: A. Повторный запуск не создаёт Product/Price. B. Изменение цены → новая Stripe Price и новая local price version. C. Trial без paid price. D. Локальные amount/interval/currency совпадают с провайдером.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

| Products/Prices | Реальные Product/Price для published AI plans и offers; test mappings отдельно от live | Provisioning повторяется без дублей; local amount/interval/currency совпадают |
- Provisioning принимает catalog version и account ref, читает действующие продукты/цены по локальному immutable mapping key, создаёт отсутствующие, проверяет совпадение immutable price.
- Изменённая цена создаёт новую Stripe Price и новую local price version.
- Нельзя обновлять цену под историческим invoice snapshot.
- Trial не требует paid Stripe price.

### STR-03 — Создание Customer

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Сервис создания Customer один раз на tenant/account с durable idempotency key; неопределённый ответ разрешается чтением.
- Критерий: A. Двойной вызов → один Customer. B. Неизвестный результат → retrieval/reconciliation, не второй create.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

- Customer создаётся один раз tenant/account с durable idempotency key; unknown response разрешается retrieval/reconciliation, не слепым вторым create.

### STR-04 — Quote → order → Hosted Checkout (intent)

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Outbox-запись о создании Checkout, серверные price IDs, возврат action URL через GET order; суммы с клиента не принимаются.
- Критерий: A. Цена не берётся из запроса клиента. B. Повтор Idempotency-Key возвращает тот же order. C. URL возврата не доказывает оплату.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

| Checkout | Hosted, subscription для первой recurring покупки, payment для prepaid | Allowed return URLs; server-only price IDs; no client amount |
- Quote → order → durable Checkout create intent → Stripe Hosted Checkout → signed inbox → authoritative payment verification → invoice line allocation → atomic grant → summary update.
- URL возврата не доказывает оплату.
- `checkout.session.completed` сам по себе при отложенной оплате не гарантирует paid.

### STR-05 — Webhook ingress и inbox

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`). Реальная подпись Stripe — в `STR-14`.
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: HTTP-приём с проверкой подписи по сырому телу, запись в inbox до 2xx, worker с retry lease.
- Критерий: A. Неверная подпись → отказ, inbox пуст. B. Дубликат и out-of-order не дают двойного эффекта. C. Неожиданный customer/order/mode/currency/price → quarantine без entitlement. D. Список событий и payload paths — по `OPEN-13`, не выдумывать.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

| Webhooks | HTTPS endpoint конкретного sandbox; raw-body signature; выбранный payload format и API version | Signed реальное событие проходит inbox и grant; неверный secret отвергнут |
- Поддержать lifecycle Checkout completed/expired/async success/failure, invoices paid/payment_failed/finalization_failed, subscription created/updated/deleted, refunds и disputes.
- Точный список и payload paths закрепить с реальной API version в spike.
- Не определять порядок только по event timestamp: получать актуальный provider object и проверять business state.
- Неожиданный customer/order/mode/currency/price переводит событие в quarantine без entitlement.
- Ingress проверяет исходное тело и timestamp штатным SDK, сохраняет inbox до 2xx.
- Worker имеет retry lease и обрабатывает событие повторяемо.
