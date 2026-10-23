# Интерфейсы только на React Native Paper

## Обязательная платформа

- Все новые экраны, формы, ошибки, диалоги, баннеры и onboarding flow реализуются только в `/Users/freelancer/Documents/PhotoEditor/apps/photographer` средствами React Native Paper с существующими theme, locale, auth, routing и CAS primitives проекта.
- Это прямое уточнение владельца от 21.10.2026. Не использовать отдельный HTML dashboard, shadcn, MUI, raw web form или новую дизайн-систему.
- Базовые React Native View/SafeAreaView/ScrollView и существующие navigation wrappers допустимы как инфраструктура layout; интерактивные controls и визуальные компоненты — Paper.

- Основные компоненты: Appbar, Card, Text, Button, ProgressBar, List, DataTable для широкого экрана, TextInput, HelperText, Checkbox, RadioButton, Banner, Snackbar, ActivityIndicator, Portal/Dialog.
- Для узкого экрана строки DataTable преобразуются в Paper List/Card.
- Точная доступность компонентов сверяется с установленной версией react-native-paper, без обязательного обновления зависимости ради документа.

- Hosted Checkout и Customer Portal открываются как внешние страницы Stripe из Paper Button.
- PAN/CVC в PhotoEditor не вводятся.
- На web возврат через allowlisted route, на native через безопасный browser/deep link; параметры возврата не доказательство оплаты.
- Никаких новых billing management screens в apps/portal.

## Структура навигации

- В существующих настройках фотографа: «Биллинг», «Использование», «Способы оплаты и счета», «Конфиденциальность», «Профиль».
- Сохранить localized routes и legacy redirects.
- Планируемые routes: settings/billing, settings/quotas, settings/billing/plans, settings/billing/resources, settings/billing/orders/[id], settings/privacy, settings/profile/delete.
- Это proposal для существующего роутера; реализация не должна создавать параллельный routing stack.

- В текущей модели роли owner/admin/editor/viewer; отдельной роли member нет.
- Owner меняет финансы и видит PII/счета.
- Admin читает полную ресурсную сводку без billing contacts/payment links; editor/viewer видят только capability reasons и доступные ресурсы в контексте разрешённого задания, без истории заказов.
- Термин member в тестах означает участника с явно заданной одной из этих ролей.
- Если появится billing-admin, сначала реализовать роль и backend permissions.
- Обычный recipient клиентской галереи не видит кабинет оплаты студии; quota error на публичном скачивании сообщает недоступность и возможность обратиться к владельцу без раскрытия его финансов.

## Экраны и формы

| Экран | Компоненты Paper | Содержимое, действия и критерий |
|---|---|---|
| Биллинг | Appbar, Card, Text, Button, Banner | Название current plan, статус, gross recurring total с налоговым пояснением/net, period dates, next payment, AI/storage отдельными строками, pending change. Действия «Сменить план», «Докупка», «Карта и счета». Custom показывается как текущий индивидуальный план с полными условиями, но не в каталоге |
| Использование | Card, ProgressBar, List, Text, Banner | Отдельно фото, операции каждого resource, storage и egress. Фото: использовано, допущено в работу, доступно, overrun, expiry buckets. Storage: included/additional/total, committed/reserved/free, категории original/result/preview/archive. Egress: период, фактически отдано, available/allocated. updated_at и признак stale |
| Выбор плана | Card, RadioButton, Button, HelperText | Public published каталог, current выделен, net/gross, включённые ресурсы, перенос, дата действия. Studio Pro disabled «Скоро» до gate. Custom полностью скрыт. Selected draft без published limits не купить |
| Докупить ресурсы | RadioButton/SegmentedButtons при наличии, List, TextInput, HelperText | Выбор фото/операции/трафика/диска; только eligible offers current plan. Integer pack count, серверный max. До Checkout quote: quantity/expiry/tax/total. Storage явно recurring; фото/операции разово. Сравнить с подходящим upgrade; не приравнивать разные operation codes |
| Подтверждение изменения | Portal, Dialog, Text, Button | Текущий и новый план, точная доплата, delta ресурсов, сохранённые остатки, anchor и next recurring total. Downgrade/storage decrease — будущая дата и projected overquota; явное подтверждение без скрытой отмены storage |
| Статус заказа | Card, ActivityIndicator, Banner, Button | created/processing/success/failed/canceled. После возврата из Stripe до fulfillment «Проверяем оплату». Poll только своего order, bounded backoff и остановка при unfocus. Reload восстанавливает order. «Продолжить оплату» повторно использует тот же order |
| Платёжные данные | TextInput, HelperText, Button, List | Billing email, имя/компания, country/address/tax details по tax policy. Card management и invoice history через owner-only Portal. До передачи Stripe объяснён recipient и назначение данных |
| Batch перед запуском | Dialog, List, Button, HelperText | Фото/operations/storage cost по resources, known deficit, expiry/free rerun пояснение; нет скрытого частичного старта при заранее известной нехватке. После покупки выбор batch сохранён и перед запуском свежий quote/admission |
| Cookies и privacy | Banner, Dialog, Checkbox, Button | Необходимые cookies описаны; «Отклонить необязательные», «Принять необязательные», «Настроить» доступны одинаково. Optional по умолчанию false; настройки можно открыть позже |
| Онбординг | Card, Checkbox, Text, HelperText, Button | Terms принятие, Privacy ознакомление, B2B DPA включён в договор; отдельные optional purposes. Version/locale фиксирует server. Не ставить consent за пользователя и не прятать условия под общей кнопкой |
| Удаление профиля | Card, Banner, Dialog, TextInput при нужном подтверждении, Button | Объяснить user vs studio, shared data, subscription effects, export, retained legal records. Fresh auth. Для sole owner варианты transfer/закрыть студию с правами. После submit limited progress, безопасный signout и очистка local storage |

## Состояния каждого экрана

- Loading с ActivityIndicator; empty не выглядит как нулевой платёж;
- API error с localized HelperText/Banner и retry; cached/stale read помечен датой; conflict предлагает refresh, сохраняя ввод; forbidden скрывает действия и обрабатывает серверный отказ; deleting закрывает покупку; offline не рисует ложную оплату; pending payment не рисует success; empty offers объясняет, почему покупки нет.

- Double click блокируется busy state и backend key, а не одним disabled Button.
- Неизвестный outcome заказа восстанавливается по сохранённому order ID, не создаёт второй.
- Expired quote показывает обновлённый итог и требует нового явного подтверждения.
- Цена не берётся из route params.
- Переключение tenant сбрасывает sensitive данные и запросы старой студии.
- Deep link к чужому invoice/order не раскрывает финансов.

## Отображение чисел и доступность

- EUR форматируется по locale, даты в timezone пользователя с понятным expiry; server time остаётся истиной.
- GB/TB decimal, GiB отдельно; старое деление quota на 1073741824 не использовать с подписью GB.
- Bigint parsing/formatting без потери точности.
- ProgressBar ограничивается 0..1, превышение показывается отдельным текстом, а не обрезается молча.
- Общая доступность вычисляется сервером и не равна просто plan limit минус used, поскольку есть packs, leases, expiry и overrun.

- Клавиатура, screen reader labels/hints, доступные touch targets, контраст light/dark, масштабирование текста, error announcements.
- Dialog удерживает/восстанавливает фокус на web, native использует доступный modal lifecycle.
- Не передавать PII через analytics.
- Уведомления 80/95/100% dedup по resource/period/threshold; всплывающее сообщение не повторяется при каждом render.

## Приёмка Paper

- Ревью всех новых экранов подтверждает `react-native-paper` components и отсутствие второго UI framework.
- DOM/browser tests запускают фактические Paper screens, а не тестовую HTML копию.
- Проверить web desktop, узкий mobile web и поддерживаемый native target с теми же backend contracts.
- Открытие/возврат Hosted Checkout на native квалифицируется отдельно; browser e2e не считается доказательством deep link на устройстве.

## Привязка к существующему Paper shell

- Переиспользовать `src/paper-provider.tsx`, `src/paper-theme.ts`, `src/locale-context.tsx`, `src/localized-router.tsx`, `src/owner-api.ts`, `src/use-cas-mutation.ts` и workspace access boundary.
- Зафиксированная в package.json зависимость react-native-paper — ^5.15.3; установленный lock проверяется перед генерацией, обновление не требуется по умолчанию.

- Limited onboarding routes располагаются вне workspace-only gate, иначе новый пользователь без tenant не сможет принять Terms.
- Deletion status после отзыва обычной сессии использует отдельный scoped capability endpoint, а не OwnerAPI с уже невалидным access token.
- Ownership transfer завершается до irreversible revoke.
- Recent-auth action является новым proof flow, не сравнивает дату token refresh.
- Все эти экраны — Paper.

- Native payment channel не открывается автоматически после web qualification: соответствующая capability=false до проверки safe browser/deep link и применимых правил целевого магазина распространения цифровых услуг.
- Наличие Expo native route не доказывает готовность такого канала.
- Development/sandbox Paper UI можно генерировать сейчас.
