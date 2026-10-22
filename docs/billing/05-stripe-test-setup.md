# Stripe в тестовом окружении

## Объём настройки

В этой задаче настройки описываются, аккаунт не создаётся и не изменяется. Первый этап — только тестовые оплаты. Предпочтительный target — отдельный Stripe Sandbox для dev и отдельный для CI. Официальная [инструкция по тестированию Billing](https://docs.stripe.com/billing/testing) рекомендует sandboxes для новых интеграций и поддерживает Test Clocks для проверки временных переходов. Локальный test/live флаг недостаточен: сохранять реальный Stripe account/sandbox identity в каждом mapping.

API version фиксируется после integration spike реальной доступной версии аккаунта; не брать выдуманный номер из документации. Использовать стабильный поддерживаемый SDK и согласованную webhook version. Базовый план использует Customer API; доступность и преимущества Accounts v2 отдельно оценить в spike, не делать preview зависимость обязательной для оплаты.

## Настройки тестового аккаунта

| Настройка | Требование | Доказательство завершения |
|---|---|---|
| Workspace | Изолированные dev/CI sandboxes; live disabled в приложении | Account IDs и environment в закрытом deployment manifest, без секретов |
| Seller | Юридическое имя, страна, валюта EUR, support contact, statement descriptor, branding | Проверено на test invoice/Checkout; реквизиты сверены с актуальным legal entity |
| Access | MFA для людей, минимальные роли, отдельные service credentials | Журнал настройки; секреты только secret store |
| Keys | Secret API credential, public key только если реально нужен, webhook signing secret | Никаких sk/whsec в git/docs/logs; startup env guard |
| Products/Prices | Реальные Product/Price для published AI plans и offers; test mappings отдельно от live | Provisioning повторяется без дублей; local amount/interval/currency совпадают |
| Taxes | EUR tax exclusive, сбор необходимых country/address/tax details | Fixture BG 20%; неподдержанная страна/налоговый режим блокируют quote до настройки |
| Checkout | Hosted, subscription для первой recurring покупки, payment для prepaid | Allowed return URLs; server-only price IDs; no client amount |
| Customer Portal | Карта, billing info, invoices; смена планов выключена | Попытка сменить тариф вне нашей saga отсутствует/отвергнута; удаление профиля координируется отдельно |
| Webhooks | HTTPS endpoint конкретного sandbox; raw-body signature; выбранный payload format и API version | Signed реальное событие проходит inbox и grant; неверный secret отвергнут |
| Retry/dunning | Политика test renewal failure и recovery; нет гранта на unpaid invoice | Test Clock overdue/recovery evidence |
| Notifications | Только тестовые адреса/маршруты; не реальные пользователи CI | CI не отправляет production emails |

Secrets refs: STRIPE_SECRET_KEY_REF, STRIPE_WEBHOOK_SECRET_REF, STRIPE_ACCOUNT_ID, STRIPE_ENVIRONMENT, STRIPE_API_VERSION, STRIPE_PORTAL_CONFIG_ID. Это целевые имена конфигурации, не действующие env vars. Не создавать public key, если Hosted Checkout redirect обходится без него. Startup сравнивает configured environment/account с server response и price mappings; mismatch закрывает создание Checkout.

Provisioning принимает catalog version и account ref, читает действующие продукты/цены по локальному immutable mapping key, создаёт отсутствующие, проверяет совпадение immutable price. Изменённая цена создаёт новую Stripe Price и новую local price version. Нельзя обновлять цену под историческим invoice snapshot. Trial не требует paid Stripe price. Customer создаётся один раз tenant/account с durable idempotency key; unknown response разрешается retrieval/reconciliation, не слепым вторым create.

## Оплата и выдача

Quote → order → durable Checkout create intent → Stripe Hosted Checkout → signed inbox → authoritative payment verification → invoice line allocation → atomic grant → summary update. URL возврата не доказывает оплату. `checkout.session.completed` сам по себе при отложенной оплате не гарантирует paid. Для recurring выдача привязана к оплаченному invoice/line/period, для one-time — к подтверждённой успешной оплате order line. Разные события о той же покупке не начисляют несколько grants.

Поддержать lifecycle Checkout completed/expired/async success/failure, invoices paid/payment_failed/finalization_failed, subscription created/updated/deleted, refunds и disputes. Точный список и payload paths закрепить с реальной API version в spike. Не определять порядок только по event timestamp: получать актуальный provider object и проверять business state. Неожиданный customer/order/mode/currency/price переводит событие в quarantine без entitlement.

[Stripe Webhooks](https://docs.stripe.com/webhooks) описывает подписи, дубликаты и доставку не по порядку. Ingress проверяет исходное тело и timestamp штатным SDK, сохраняет inbox до 2xx. Worker имеет retry lease и обрабатывает событие повторяемо. После DB restore сверка по paid invoice allocations восстанавливает missing grants, сохраняя erasure tombstones; deleted account не воскресает.

## Upgrade saga

Полная разница месячных AI цен — требование прежнего плана. Это отличается от автоматической time proration Stripe. [Prorations](https://docs.stripe.com/billing/subscriptions/prorations) использовать как источник механики, но алгоритм реализовать через отдельный зафиксированный quote.

Состояния: quoted → payment_pending → paid → provider_price_applying → entitlement_applying → applied. При error — retry либо compensating → compensated/manual_review. До нового upgrade закрыть предыдущий. Отключить automatic AI proration; создать invoice на полную разницу; после оплаты обновить recurring price с прежним anchor; после подтверждённого provider update локально выдать delta и сменить plan. Оплаченный invoice без provider update остаётся paid/applying, UI сообщает обработку и не просит платить снова. Восстановление повторяет тот же idempotent step.

Граница renewal сериализуется локальным pending change и provider checks. Если invoice renewal уже создан по старой цене, spike обязан выбрать и протестировать один путь корректировки invoice/credit или переноса upgrade; запрещено произвольно брать delta дважды. Если не удаётся безопасно применить upgrade, возврат подтверждённой доплаты и compensating events, issued usage остаётся в истории. До положительного real sandbox теста этого случая upgrade не открывается.

Storage add-on имеет отдельную policy: proration только оставшейся части месяца с invoice preview, grant после оплаты, egress пропорционален оплаченной доле. Одна subscription может содержать AI и несколько storage items; storage-only переживает отмену AI. Не отменять всю subscription, когда пользователь отменяет только AI.

## Refund и dispute

Full/partial refund связывается с конкретными order lines и quantity. Неиспользованный paid остаток отзывается компенсирующим событием; уже consumed не исчезает. При partial monetary refund без однозначного quantity оператор принимает явное распределение, автоматическая догадка запрещена. Dispute закрывает новые risky commercial admissions по опубликованной policy, но не мешает deletion/data rights. Возврат не удаляет invoice и не очищает финансовые журналы.

## Проверки перед тестовым включением

Реальные sandbox покупки первой подписки, photo/operation/egress pack и storage; SCA успех/отказ; declined card; delayed payment; duplicate/out-of-order webhook; restart между paid и grant; Test Clock renewal/downgrade/cancel; refund частично использованного пакета; потерянный webhook с reconciliation. Stripe CLI synthetic trigger без реальной связанной покупки не является достаточным доказательством fulfillment.

Live mapping отсутствует до отдельного выпуска. Требуются account verification, seller/tax configuration, published PhotoEditor legal addendum, утверждённые operation prices, retention matrix, capacity evidence и весь раздел 08. Эта спецификация не утверждает настройку Stripe аккаунта или налоговую применимость 20% за пределами fixture.

## Детерминированные решения первого тестового выпуска

Только один sandbox/account активен для конкретного tenant billing profile. Все AI/storage items одной recurring subscription имеют общий month anchor. Начало storage-only subscription из Trial создаёт новый полный monthly period; добавление к существующей — только paid prorated adjustment. Не создавать вторую recurring subscription ради Checkout storage add-on: существующий customer/subscription меняется серверной invoice saga с hosted invoice/SCA action. Hosted Checkout subscription используется только когда незакрытой subscription ещё нет. Возобновление AI на storage-only anchor следует той же отдельной AI-change saga, без второго subscription.

Для AI upgrade, пока не реализована и не квалифицирована invoice-boundary correction, запрос допустим лишь если provider snapshot не имеет open renewal invoice и до period_end больше24 часов. В остальных случаях сервер возвращает409 BILLING_CHANGE_WINDOW_CLOSED, UI показывает дату возобновления/плановую смену, денег не списывает. Гонка после quote и перед оплатой всё равно разрешается persisted saga: если renewal уже создал неподходящий invoice, не применять delta, вернуть upgrade payment и сохранить действующий renewal. Этот путь обязателен в sandbox; запрет по времени не заменяет compensation.

TaxScope v1 в dev/CI: только синтетический domestic BG customer, EUR, explicit tax exclusive20%, округление half-up построчно. Self-service иных country/tax combinations возвращает422 BILLING_TAX_SCOPE_UNSUPPORTED до создания provider object. Это не политика реальных продаж и не разрешение применять20% всему миру. Технический интерфейс TaxResolver сохраняется для последующей утверждённой конфигурации.

Каждый provider effect имеет persisted provider_request_key, request hash и response refs. Timeout после Stripe success не разрешается новым key. Операции account deletion и purchase fulfillment блокируются по одному financial subject lifecycle: для transferred tenant deleting user не закрывает чужой продолжающий работать billing profile.
