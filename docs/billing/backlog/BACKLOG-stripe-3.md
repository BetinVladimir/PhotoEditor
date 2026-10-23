# Бэклог: Stripe-провижининг (BILL-07/08/09), часть 3

### STR-12 — TaxScope v1 и интерфейс TaxResolver

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Синтетический BG customer, EUR, 20% tax exclusive, округление half-up построчно; иное → 422 до создания provider object.
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
- Критерий: A. Смена тарифа вне нашей saga невозможна. B. Нет гранта на unpaid invoice. C. CI не шлёт production emails.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

| Customer Portal | Карта, billing info, invoices; смена планов выключена | Попытка сменить тариф вне нашей saga отсутствует/отвергнута; удаление профиля координируется отдельно |
| Retry/dunning | Политика test renewal failure и recovery; нет гранта на unpaid invoice | Test Clock overdue/recovery evidence |
| Notifications | Только тестовые адреса/маршруты; не реальные пользователи CI | CI не отправляет production emails |

### STR-14 — Реальные sandbox-проверки перед тестовым включением

- Группа: Stripe (BILL-07/08/09). **ЖДЁТ КЛЮЧЕЙ (`OPEN-14`).** Порядок добавления — `STRIPE-CONFIG.md`.
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Чек-лист из 05 на реальном Stripe Sandbox.
- Критерий: A. Каждый пункт чек-листа пройден и записан в `STATUS.md` (дата, что именно, результат). B. Synthetic trigger из Stripe CLI без реальной покупки не считается доказательством fulfillment. C. Live mapping отсутствует.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

- Реальные sandbox покупки первой подписки, photo/operation/egress pack и storage;
- SCA успех/отказ; declined card; delayed payment; duplicate/out-of-order webhook; restart между paid и grant;
- Test Clock renewal/downgrade/cancel; refund частично использованного пакета; потерянный webhook с reconciliation.
- Stripe CLI synthetic trigger без реальной связанной покупки не является достаточным доказательством fulfillment.
- Live mapping отсутствует до отдельного выпуска.
- Требуются account verification, seller/tax configuration, published PhotoEditor legal addendum, утверждённые operation prices, retention matrix, capacity evidence и весь раздел 08. Эта спецификация не утверждает настройку Stripe аккаунта или налоговую применимость 20% за пределами fixture.

### STR-15 — Операторские шаги настройки аккаунта (не код)

- Группа: Stripe (BILL-07/08/09). **ЖДЁТ КЛЮЧЕЙ (`OPEN-14`).** Не задача для модели.
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Выполняет человек; результат — записи в закрытом deployment manifest без секретов.
- Критерий: A. Для каждой строки есть доказательство из колонки «Доказательство завершения». B. Ни один секрет не попал в git, docs, логи.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

| Workspace | Изолированные dev/CI sandboxes; live disabled в приложении | Account IDs и environment в закрытом deployment manifest, без секретов |
| Seller | Юридическое имя, страна, валюта EUR, support contact, statement descriptor, branding | Проверено на test invoice/Checkout; реквизиты сверены с актуальным legal entity |
| Access | MFA для людей, минимальные роли, отдельные service credentials | Журнал настройки; секреты только secret store |
| Keys | Secret API credential, public key только если реально нужен, webhook signing secret | Никаких sk/whsec в git/docs/logs; startup env guard |

### STR-16 — Структура подписки и lifecycle

- Группа: Stripe (BILL-07/08/09). Ключи не нужны: проверяется на fake-провайдере (`STR-01`).
- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.
- Выход: Правила одной recurring subscription, общий month anchor, Hosted Checkout только без открытой subscription, блокировка по lifecycle финансового субъекта.
- Критерий: A. Один активный account на billing profile. B. Все items одной subscription делят anchor. C. Storage add-on не создаёт вторую recurring subscription. D. Удаление пользователя не закрывает чужой работающий billing profile transferred tenant.
- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.
- Из 05:

- Только один sandbox/account активен для конкретного tenant billing profile.
- Все AI/storage items одной recurring subscription имеют общий month anchor.
- Начало storage-only subscription из Trial создаёт новый полный monthly period; добавление к существующей — только paid prorated adjustment.
- Не создавать вторую recurring subscription ради Checkout storage add-on: существующий customer/subscription меняется серверной invoice saga с hosted invoice/SCA action.
- Hosted Checkout subscription используется только когда незакрытой subscription ещё нет.
- Возобновление AI на storage-only anchor следует той же отдельной AI-change saga, без второго subscription.
- Операции account deletion и purchase fulfillment блокируются по одному financial subject lifecycle: для transferred tenant deleting user не закрывает чужой продолжающий работать billing profile.
