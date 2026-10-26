# Бэклог: Paper UI (BILL-10), часть 3

### UI-11 — экран «Онбординг»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): limited onboarding routes вне workspace-only gate (06, «Привязка к существующему Paper shell»).
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Тесты (в этой же задаче, R33): `apps/photographer/src/<имя>.dom.test.tsx` — по одному кейсу на состояние из UI-12 и проверка ключей bg/en/ru; helper — `TST-04`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Онбординг | Card, Checkbox, Text, HelperText, Button | Terms принятие, Privacy ознакомление, B2B DPA включён в договор; отдельные optional purposes. Version/locale фиксирует server. Не ставить consent за пользователя и не прятать условия под общей кнопкой |

### UI-12 — экран «Удаление профиля»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): settings/profile/delete.
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Тесты (в этой же задаче, R33): `apps/photographer/src/<имя>.dom.test.tsx` — по одному кейсу на состояние из UI-12 и проверка ключей bg/en/ru; helper — `TST-04`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Удаление профиля | Card, Banner, Dialog, TextInput при нужном подтверждении, Button | Объяснить user vs studio, shared data, subscription effects, export, retained legal records. Fresh auth. Для sole owner варианты transfer/закрыть студию с правами. После submit limited progress, безопасный signout и очистка local storage |

### UI-00 — Навигация и routes

- Вход: раздел «Структура навигации» из 06 ниже; образцы — `apps/photographer/src/`.
- Выход: Пункты настроек, planned routes, роли owner/admin/editor/viewer, legacy redirects.
- Тесты (в этой же задаче, R33): dom-тесты в `apps/photographer/src/`; helper — `TST-04`.
- Критерий: A. Роли видят ровно то, что указано. B. Нет параллельного routing stack. C. Deep link к чужому order/invoice не раскрывает финансов.
- Запреты: R13, R7.
- Из 06:

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

### UI-12 — Состояния каждого экрана

- Вход: раздел «Состояния каждого экрана» из 06 ниже; образцы — `apps/photographer/src/`.
- Выход: Общий компонент/хук состояний и правила (busy state, восстановление order по ID, expired quote, сброс при смене tenant).
- Тесты (в этой же задаче, R33): dom-тесты в `apps/photographer/src/`; helper — `TST-04`.
- Критерий: A. Double click блокируется busy state и backend key. B. Неизвестный outcome заказа восстанавливается по сохранённому order ID. C. Смена tenant сбрасывает sensitive данные.
- Запреты: R13, R7.
- Из 06:

- Loading с ActivityIndicator; empty не выглядит как нулевой платёж;
- API error с localized HelperText/Banner и retry; cached/stale read помечен датой; conflict предлагает refresh, сохраняя ввод; forbidden скрывает действия и обрабатывает серверный отказ; deleting закрывает покупку; offline не рисует ложную оплату; pending payment не рисует success; empty offers объясняет, почему покупки нет.
- Double click блокируется busy state и backend key, а не одним disabled Button.
- Неизвестный outcome заказа восстанавливается по сохранённому order ID, не создаёт второй.
- Expired quote показывает обновлённый итог и требует нового явного подтверждения.
- Цена не берётся из route params.
- Переключение tenant сбрасывает sensitive данные и запросы старой студии.
- Deep link к чужому invoice/order не раскрывает финансов.

### UI-13 — Числа, доступность, уведомления 80/95/100%

- Вход: раздел «Отображение чисел и доступность» из 06 ниже; образцы — `apps/photographer/src/`.
- Выход: Форматирование EUR/GB/GiB/bigint, ProgressBar, доступность, дедупликация уведомлений.
- Тесты (в этой же задаче, R33): dom-тесты в `apps/photographer/src/`; helper — `TST-04`.
- Критерий: A. GB (decimal) и GiB показаны раздельно. B. Bigint без потери точности. C. ProgressBar 0..1, превышение текстом. D. Уведомления дедуплицированы по resource/period/threshold. E. PII не уходит в analytics.
- Запреты: R13, R7.
- Из 06:

- EUR форматируется по locale, даты в timezone пользователя с понятным expiry; server time остаётся истиной.
- GB/TB decimal, GiB отдельно; старое деление quota на 1073741824 не использовать с подписью GB.
- Bigint parsing/formatting без потери точности.
- ProgressBar ограничивается 0..1, превышение показывается отдельным текстом, а не обрезается молча.
- Общая доступность вычисляется сервером и не равна просто plan limit минус used, поскольку есть packs, leases, expiry и overrun.
- Клавиатура, screen reader labels/hints, доступные touch targets, контраст light/dark, масштабирование текста, error announcements.
- Dialog удерживает/восстанавливает фокус на web, native использует доступный modal lifecycle.
- Не передавать PII через analytics.
- Уведомления 80/95/100% dedup по resource/period/threshold; всплывающее сообщение не повторяется при каждом render.
