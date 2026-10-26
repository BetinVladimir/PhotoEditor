# Бэклог: Paper UI (BILL-10), часть 1

### UI-01 — экран «Экран»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): отдельного route в 06 нет: диалог/секция внутри другого экрана; не создавать параллельный routing stack.
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Тесты (в этой же задаче, R33): `apps/photographer/src/<имя>.dom.test.tsx` — по одному кейсу на состояние из UI-12 и проверка ключей bg/en/ru; helper — `TST-04`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Экран | Компоненты Paper | Содержимое, действия и критерий |

### UI-02 — экран «Биллинг»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): settings/billing.
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Тесты (в этой же задаче, R33): `apps/photographer/src/<имя>.dom.test.tsx` — по одному кейсу на состояние из UI-12 и проверка ключей bg/en/ru; helper — `TST-04`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Биллинг | Appbar, Card, Text, Button, Banner | Название current plan, статус, gross recurring total с налоговым пояснением/net, period dates, next payment, AI/storage отдельными строками, pending change. Действия «Сменить план», «Докупка», «Карта и счета». Custom показывается как текущий индивидуальный план с полными условиями, но не в каталоге |

### UI-03 — экран «Использование»

- Статус: экран в `apps/photographer/screens/` отсутствует (кроме «Использование»).
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): settings/quotas (экран уже существует: `screens/settings/quotas.tsx`, 20 строк, сейчас показывает storage_bytes/1073741824 с подписью GiB).
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Тесты (в этой же задаче, R33): `apps/photographer/src/<имя>.dom.test.tsx` — по одному кейсу на состояние из UI-12 и проверка ключей bg/en/ru; helper — `TST-04`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Использование | Card, ProgressBar, List, Text, Banner | Отдельно фото, операции каждого resource, storage и egress. Фото: использовано, допущено в работу, доступно, overrun, expiry buckets. Storage: included/additional/total, committed/reserved/free, категории original/result/preview/archive. Egress: период, фактически отдано, available/allocated. updated_at и признак stale |

### UI-04 — экран «Выбор плана»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): settings/billing/plans.
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Тесты (в этой же задаче, R33): `apps/photographer/src/<имя>.dom.test.tsx` — по одному кейсу на состояние из UI-12 и проверка ключей bg/en/ru; helper — `TST-04`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Выбор плана | Card, RadioButton, Button, HelperText | Public published каталог, current выделен, net/gross, включённые ресурсы, перенос, дата действия. Studio Pro disabled «Скоро» до gate. Custom полностью скрыт. Selected draft без published limits не купить |

### UI-05 — экран «Докупить ресурсы»

- Статус: экран в `apps/photographer/screens/` отсутствует.
- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.
- Маршрут (по 06): settings/billing/resources.
- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.
- Тесты (в этой же задаче, R33): `apps/photographer/src/<имя>.dom.test.tsx` — по одному кейсу на состояние из UI-12 и проверка ключей bg/en/ru; helper — `TST-04`.
- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.
- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.
- Из 06:

| Докупить ресурсы | RadioButton/SegmentedButtons при наличии, List, TextInput, HelperText | Выбор фото/операции/трафика/диска; только eligible offers current plan. Integer pack count, серверный max. До Checkout quote: quantity/expiry/tax/total. Storage явно recurring; фото/операции разово. Сравнить с подходящим upgrade; не приравнивать разные operation codes |
