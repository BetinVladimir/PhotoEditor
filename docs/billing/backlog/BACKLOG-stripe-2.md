# Бэклог: Stripe-провижининг (BILL-07/08/09), часть 2

### STR-05 — Webhook ingress и inbox

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`). Реальная подпись Stripe — в `STR-14`.
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: HTTP-приём с проверкой подписи по сырому телу, запись в inbox до 2xx, worker с retry lease.
- Тесты (в этой же задаче, R33, `TESTING.md` T7): Go-тесты рядом с кодом на fake-провайдере (`STR-01`); общий контрактный набор провайдера `TST-02` расширяется кейсами этой задачи; секретов и сети нет. Без них задача не принята.
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

### STR-06 — Fulfillment: подтверждение оплаты и выдача (grant)

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Проверка актуального provider-объекта, invoice line allocation, атомарный grant, обновление summary.
- Тесты (в этой же задаче, R33, `TESTING.md` T7): Go-тесты рядом с кодом на fake-провайдере (`STR-01`); общий контрактный набор провайдера `TST-02` расширяется кейсами этой задачи; секретов и сети нет. Без них задача не принята.
- Критерий: A. Одна покупка → один grant при любом числе событий. B. Recurring привязан к оплаченному invoice/line/period. C. Рестарт между paid и grant восстанавливается.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

- Quote → order → durable Checkout create intent → Stripe Hosted Checkout → signed inbox → authoritative payment verification → invoice line allocation → atomic grant → summary update.
- Для recurring выдача привязана к оплаченному invoice/line/period, для one-time — к подтверждённой успешной оплате order line.
- Разные события о той же покупке не начисляют несколько grants.

### STR-07 — Upgrade saga (состояния и компенсация)

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`). Положительный real-sandbox тест обязателен до открытия upgrade (`STR-14`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Машина состояний quoted → payment_pending → paid → provider_price_applying → entitlement_applying → applied, retry, compensation.
- Тесты (в этой же задаче, R33, `TESTING.md` T7): Go-тесты рядом с кодом на fake-провайдере (`STR-01`); общий контрактный набор провайдера `TST-02` расширяется кейсами этой задачи; секретов и сети нет. Без них задача не принята.
- Критерий: A. Каждое состояние и переход покрыты тестом. B. Оплаченный invoice без provider update остаётся paid/applying и не просит платить снова. C. Невозможность применить upgrade → возврат доплаты и compensating events. D. До нового upgrade закрыт предыдущий.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

- Состояния: quoted → payment_pending → paid → provider_price_applying → entitlement_applying → applied.
- При error — retry либо compensating → compensated/manual_review.
- До нового upgrade закрыть предыдущий.
- Отключить automatic AI proration; создать invoice на полную разницу; после оплаты обновить recurring price с прежним anchor; после подтверждённого provider update локально выдать delta и сменить plan.
- Оплаченный invoice без provider update остаётся paid/applying, UI сообщает обработку и не просит платить снова.
- Восстановление повторяет тот же idempotent step.
- Граница renewal сериализуется локальным pending change и provider checks.
- Если invoice renewal уже создан по старой цене, spike обязан выбрать и протестировать один путь корректировки invoice/credit или переноса upgrade; запрещено произвольно брать delta дважды.
- Если не удаётся безопасно применить upgrade, возврат подтверждённой доплаты и compensating events, issued usage остаётся в истории.
- До положительного real sandbox теста этого случая upgrade не открывается.

### STR-08 — Окно смены плана: BILLING_CHANGE_WINDOW_CLOSED

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Серверное правило допустимости upgrade и ответ 409; compensation при гонке после quote.
- Тесты (в этой же задаче, R33, `TESTING.md` T7): Go-тесты рядом с кодом на fake-провайдере (`STR-01`); общий контрактный набор провайдера `TST-02` расширяется кейсами этой задачи; секретов и сети нет. Без них задача не принята.
- Критерий: A. Open renewal invoice или менее 24 часов до period_end → 409 `BILLING_CHANGE_WINDOW_CLOSED`, деньги не списываются. B. Гонка после quote → delta не применяется, платёж возвращается. C. Запрет по времени не заменяет compensation.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

- Граница renewal сериализуется локальным pending change и provider checks.
- Если invoice renewal уже создан по старой цене, spike обязан выбрать и протестировать один путь корректировки invoice/credit или переноса upgrade; запрещено произвольно брать delta дважды.
- Для AI upgrade, пока не реализована и не квалифицирована invoice-boundary correction, запрос допустим лишь если provider snapshot не имеет open renewal invoice и до period_end больше 24 часов.
- В остальных случаях сервер возвращает 409 BILLING_CHANGE_WINDOW_CLOSED, UI показывает дату возобновления/плановую смену, денег не списывает.
- Гонка после quote и перед оплатой всё равно разрешается persisted saga: если renewal уже создал неподходящий invoice, не применять delta, вернуть upgrade payment и сохранить действующий renewal.
- Этот путь обязателен в sandbox; запрет по времени не заменяет compensation.
