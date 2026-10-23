# Бэклог: Paper UI (BILL-10), часть 2

### UI-07 — экран «Статус заказа»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): settings/billing/orders/[id].
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Статус заказа | Card, ActivityIndicator, Banner, Button | created/processing/success/failed/canceled. После возврата из Stripe до fulfillment «Проверяем оплату». Poll только своего order, bounded backoff и остановка при unfocus. Reload восстанавливает order. «Продолжить оплату» повторно использует тот же order |

### UI-08 — экран «Платёжные данные»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): отдельного route в 06 нет: диалог/секция внутри другого экрана; не создавать параллельный routing stack.
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Платёжные данные | TextInput, HelperText, Button, List | Billing email, имя/компания, country/address/tax details по tax policy. Card management и invoice history через owner-only Portal. До передачи Stripe объяснён recipient и назначение данных |

### UI-09 — экран «Batch перед запуском»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): отдельного route в 06 нет: диалог/секция внутри другого экрана; не создавать параллельный routing stack.
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Batch перед запуском | Dialog, List, Button, HelperText | Фото/operations/storage cost по resources, known deficit, expiry/free rerun пояснение; нет скрытого частичного старта при заранее известной нехватке. После покупки выбор batch сохранён и перед запуском свежий quote/admission |

### UI-10 — экран «Cookies и privacy»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): settings/privacy.
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Cookies и privacy | Banner, Dialog, Checkbox, Button | Необходимые cookies описаны; «Отклонить необязательные», «Принять необязательные», «Настроить» доступны одинаково. Optional по умолчанию false; настройки можно открыть позже |

### UI-11 — экран «Онбординг»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): limited onboarding routes вне workspace-only gate (06, «Привязка к существующему Paper shell»).
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Онбординг | Card, Checkbox, Text, HelperText, Button | Terms принятие, Privacy ознакомление, B2B DPA включён в договор; отдельные optional purposes. Version/locale фиксирует server. Не ставить consent за пользователя и не прятать условия под общей кнопкой |

### UI-12 — экран «Удаление профиля»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): settings/profile/delete.
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Удаление профиля | Card, Banner, Dialog, TextInput при нужном подтверждении, Button | Объяснить user vs studio, shared data, subscription effects, export, retained legal records. Fresh auth. Для sole owner варианты transfer/закрыть студию с правами. После submit limited progress, безопасный signout и очистка local storage |
