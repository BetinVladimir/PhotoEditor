# Бэклог: Paper UI (BILL-10), часть 3

### UI-00 — Навигация и routes

- Вход: раздел «Структура навигации» из 06 ниже; образцы — `apps/photographer/src/`.
- Выход: Пункты настроек, planned routes, роли owner/admin/editor/viewer, legacy redirects.
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

### UI-14 — Привязка к существующему Paper shell

- Вход: раздел «Привязка к существующему Paper shell» из 06 ниже; образцы — `apps/photographer/src/`.
- Выход: Использование `paper-provider`, `paper-theme`, `locale-context`, `localized-router`, `owner-api`, `use-cas-mutation`, workspace boundary; onboarding вне workspace-only gate; scoped endpoint статуса удаления.
- Критерий: A. Все перечисленные модули переиспользованы (`apps/photographer/src/`). B. Onboarding работает у пользователя без tenant. C. Статус удаления не зависит от уже невалидного access token. D. Native payment capability=false до отдельной квалификации.
- Запреты: R13, R7.
- Из 06:

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

### UI-15 — Приёмка Paper и проверка платформ

- Вход: раздел «Приёмка Paper» из 06 ниже; образцы — `apps/photographer/src/`.
- Выход: Тесты на фактических Paper-экранах, web desktop, узкий mobile web, native target.
- Критерий: A. DOM-тесты запускают настоящие экраны. B. `make test-web` PASS. C. Hosted Checkout на native квалифицируется отдельно и не считается пройденным по web e2e.
- Запреты: R13, R7.
- Из 06:

- Ревью всех новых экранов подтверждает `react-native-paper` components и отсутствие второго UI framework.
- DOM/browser tests запускают фактические Paper screens, а не тестовую HTML копию.
- Проверить web desktop, узкий mobile web и поддерживаемый native target с теми же backend contracts.
- Открытие/возврат Hosted Checkout на native квалифицируется отдельно; browser e2e не считается доказательством deep link на устройстве.
