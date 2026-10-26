# Бэклог: Stripe-провижининг (BILL-07/08/09), часть 3

### STR-09 — Storage add-on

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Policy proration остатка месяца, grant после оплаты, egress пропорционально оплаченной доле; AI и storage в одной subscription.
- Тесты (в этой же задаче, R33, `TESTING.md` T7): Go-тесты рядом с кодом на fake-провайдере (`STR-01`); общий контрактный набор провайдера `TST-02` расширяется кейсами этой задачи; секретов и сети нет. Без них задача не принята.
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
- Тесты (в этой же задаче, R33, `TESTING.md` T7): Go-тесты рядом с кодом на fake-провайдере (`STR-01`); общий контрактный набор провайдера `TST-02` расширяется кейсами этой задачи; секретов и сети нет. Без них задача не принята.
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
- Тесты (в этой же задаче, R33, `TESTING.md` T7): Go-тесты рядом с кодом на fake-провайдере (`STR-01`); общий контрактный набор провайдера `TST-02` расширяется кейсами этой задачи; секретов и сети нет. Без них задача не принята.
- Критерий: A. Потерянный webhook восстанавливается сверкой. B. После restore missing grants восстанавливаются, erasure tombstones сохраняются. C. Удалённый аккаунт не воскресает.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

| Retry/dunning | Политика test renewal failure и recovery; нет гранта на unpaid invoice | Test Clock overdue/recovery evidence |
- После DB restore сверка по paid invoice allocations восстанавливает missing grants, сохраняя erasure tombstones; deleted account не воскресает.
- Test Clock renewal/downgrade/cancel; refund частично использованного пакета; потерянный webhook с reconciliation.

### STR-12 — TaxScope v1 и интерфейс TaxResolver

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Синтетический BG customer, EUR, 20% tax exclusive, округление half-up построчно; иное → 422 до создания provider object.
- Тесты (в этой же задаче, R33, `TESTING.md` T7): Go-тесты рядом с кодом на fake-провайдере (`STR-01`); общий контрактный набор провайдера `TST-02` расширяется кейсами этой задачи; секретов и сети нет. Без них задача не принята.
- Критерий: A. Другая страна/режим → 422 `BILLING_TAX_SCOPE_UNSUPPORTED` без объектов у провайдера. B. Округление half-up построчно. C. Интерфейс TaxResolver сохранён для будущей конфигурации. D. 20% не применяется ко всему миру (`OPEN-04`).
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

| Taxes | EUR tax exclusive, сбор необходимых country/address/tax details | Fixture BG 20%; неподдержанная страна/налоговый режим блокируют quote до настройки |
- TaxScope v1 в dev/CI: только синтетический domestic BG customer, EUR, explicit tax exclusive20%, округление half-up построчно.
- Self-service иных country/tax combinations возвращает 422 BILLING_TAX_SCOPE_UNSUPPORTED до создания provider object.
- Это не политика реальных продаж и не разрешение применять 20% всему миру.
- Технический интерфейс TaxResolver сохраняется для последующей утверждённой конфигурации.

### STR-13 — Customer Portal, dunning, уведомления

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Конфигурация Portal без смены плана, политика неуспешного renewal, только тестовые адреса.
- Тесты (в этой же задаче, R33, `TESTING.md` T7): Go-тесты рядом с кодом на fake-провайдере (`STR-01`); общий контрактный набор провайдера `TST-02` расширяется кейсами этой задачи; секретов и сети нет. Без них задача не принята.
- Критерий: A. Смена тарифа вне нашей saga невозможна. B. Нет гранта на unpaid invoice. C. CI не шлёт production emails.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

| Customer Portal | Карта, billing info, invoices; смена планов выключена | Попытка сменить тариф вне нашей saga отсутствует/отвергнута; удаление профиля координируется отдельно |
| Retry/dunning | Политика test renewal failure и recovery; нет гранта на unpaid invoice | Test Clock overdue/recovery evidence |
| Notifications | Только тестовые адреса/маршруты; не реальные пользователи CI | CI не отправляет production emails |
