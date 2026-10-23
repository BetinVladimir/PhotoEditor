# Бэклог: Stripe-провижининг (BILL-07/08/09), часть 2

### STR-06 — Fulfillment: подтверждение оплаты и выдача (grant)

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Проверка актуального provider-объекта, invoice line allocation, атомарный grant, обновление summary.
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
- Критерий: A. Open renewal invoice или менее 24 часов до period_end → 409 `BILLING_CHANGE_WINDOW_CLOSED`, деньги не списываются. B. Гонка после quote → delta не применяется, платёж возвращается. C. Запрет по времени не заменяет compensation.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

- Граница renewal сериализуется локальным pending change и provider checks.
- Если invoice renewal уже создан по старой цене, spike обязан выбрать и протестировать один путь корректировки invoice/credit или переноса upgrade; запрещено произвольно брать delta дважды.
- Для AI upgrade, пока не реализована и не квалифицирована invoice-boundary correction, запрос допустим лишь если provider snapshot не имеет open renewal invoice и до period_end больше 24 часов.
- В остальных случаях сервер возвращает 409 BILLING_CHANGE_WINDOW_CLOSED, UI показывает дату возобновления/плановую смену, денег не списывает.
- Гонка после quote и перед оплатой всё равно разрешается persisted saga: если renewal уже создал неподходящий invoice, не применять delta, вернуть upgrade payment и сохранить действующий renewal.
- Этот путь обязателен в sandbox; запрет по времени не заменяет compensation.

### STR-09 — Storage add-on

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Policy proration остатка месяца, grant после оплаты, egress пропорционально оплаченной доле; AI и storage в одной subscription.
- Критерий: A. Storage-only переживает отмену AI. B. Отмена только AI не отменяет subscription. C. Egress пропорционален оплаченной доле.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

- Storage add-on имеет отдельную policy: proration только оставшейся части месяца с invoice preview, grant после оплаты, egress пропорционален оплаченной доле.
- Одна subscription может содержать AI и несколько storage items; storage-only переживает отмену AI.
- Не отменять всю subscription, когда пользователь отменяет только AI.

### STR-10 — Refund и dispute

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Связь refund с order lines и quantity, отзыв неиспользованного остатка, политика dispute.
- Критерий: A. Consumed не исчезает, неиспользованный остаток отзывается компенсирующим событием. B. Partial refund без однозначного quantity требует явного распределения оператора. C. Dispute закрывает новые risky admissions, но не удаление и права на данные. D. Invoice и журналы не удаляются.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

- Full/partial refund связывается с конкретными order lines и quantity.
- Неиспользованный paid остаток отзывается компенсирующим событием; уже consumed не исчезает.
- При partial monetary refund без однозначного quantity оператор принимает явное распределение, автоматическая догадка запрещена.
- Dispute закрывает новые risky commercial admissions по опубликованной policy, но не мешает deletion/data rights.
- Возврат не удаляет invoice и не очищает финансовые журналы.

### STR-11 — Reconciliation и восстановление после сбоя

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Сверка paid invoice allocations после restore и потерянного webhook; tombstones применяются до выдачи.
- Критерий: A. Потерянный webhook восстанавливается сверкой. B. После restore missing grants восстанавливаются, erasure tombstones сохраняются. C. Удалённый аккаунт не воскресает.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

| Retry/dunning | Политика test renewal failure и recovery; нет гранта на unpaid invoice | Test Clock overdue/recovery evidence |
- После DB restore сверка по paid invoice allocations восстанавливает missing grants, сохраняя erasure tombstones; deleted account не воскресает.
- Test Clock renewal/downgrade/cancel; refund частично использованного пакета; потерянный webhook с reconciliation.
