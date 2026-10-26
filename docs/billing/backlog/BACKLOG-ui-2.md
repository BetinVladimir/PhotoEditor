# Бэклог: Paper UI (BILL-10), часть 2

### UI-06 — экран «Подтверждение изменения»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): отдельного route в 06 нет: диалог/секция внутри другого экрана; не создавать параллельный routing stack.
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Тесты (в этой же задаче, R33): `apps/photographer/src/<имя>.dom.test.tsx` — по одному кейсу на состояние из UI-12 и проверка ключей bg/en/ru; helper — `TST-04`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Подтверждение изменения | Portal, Dialog, Text, Button | Текущий и новый план, точная доплата, delta ресурсов, сохранённые остатки, anchor и next recurring total. Downgrade/storage decrease — будущая дата и projected overquota; явное подтверждение без скрытой отмены storage |

### UI-07 — экран «Статус заказа»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): settings/billing/orders/[id].
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Тесты (в этой же задаче, R33): `apps/photographer/src/<имя>.dom.test.tsx` — по одному кейсу на состояние из UI-12 и проверка ключей bg/en/ru; helper — `TST-04`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Статус заказа | Card, ActivityIndicator, Banner, Button | created/processing/success/failed/canceled. После возврата из Stripe до fulfillment «Проверяем оплату». Poll только своего order, bounded backoff и остановка при unfocus. Reload восстанавливает order. «Продолжить оплату» повторно использует тот же order |

### UI-08 — экран «Платёжные данные»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): отдельного route в 06 нет: диалог/секция внутри другого экрана; не создавать параллельный routing stack.
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Тесты (в этой же задаче, R33): `apps/photographer/src/<имя>.dom.test.tsx` — по одному кейсу на состояние из UI-12 и проверка ключей bg/en/ru; helper — `TST-04`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Платёжные данные | TextInput, HelperText, Button, List | Billing email, имя/компания, country/address/tax details по tax policy. Card management и invoice history через owner-only Portal. До передачи Stripe объяснён recipient и назначение данных |

### UI-09 — экран «Batch перед запуском»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): отдельного route в 06 нет: диалог/секция внутри другого экрана; не создавать параллельный routing stack.
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Тесты (в этой же задаче, R33): `apps/photographer/src/<имя>.dom.test.tsx` — по одному кейсу на состояние из UI-12 и проверка ключей bg/en/ru; helper — `TST-04`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Batch перед запуском | Dialog, List, Button, HelperText | Фото/operations/storage cost по resources, known deficit, expiry/free rerun пояснение; нет скрытого частичного старта при заранее известной нехватке. После покупки выбор batch сохранён и перед запуском свежий quote/admission |

### UI-10 — экран «Cookies и privacy»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): settings/privacy.
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Тесты (в этой же задаче, R33): `apps/photographer/src/<имя>.dom.test.tsx` — по одному кейсу на состояние из UI-12 и проверка ключей bg/en/ru; helper — `TST-04`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Cookies и privacy | Banner, Dialog, Checkbox, Button | Необходимые cookies описаны; «Отклонить необязательные», «Принять необязательные», «Настроить» доступны одинаково. Optional по умолчанию false; настройки можно открыть позже |
