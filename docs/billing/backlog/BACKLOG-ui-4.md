# Бэклог: Paper UI (BILL-10), часть 4

### UI-14 — Привязка к существующему Paper shell

- Вход: раздел «Привязка к существующему Paper shell» из 06 ниже; образцы — `apps/photographer/src/`.
- Выход: Использование `paper-provider`, `paper-theme`, `locale-context`, `localized-router`, `owner-api`, `use-cas-mutation`, workspace boundary; onboarding вне workspace-only gate; scoped endpoint статуса удаления.
- Тесты (в этой же задаче, R33): dom-тесты в `apps/photographer/src/`; helper — `TST-04`.
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
- Тесты (в этой же задаче, R33): dom-тесты в `apps/photographer/src/`; helper — `TST-04`.
- Критерий: A. DOM-тесты запускают настоящие экраны. B. `make test-web` PASS. C. Hosted Checkout на native квалифицируется отдельно и не считается пройденным по web e2e.
- Запреты: R13, R7.
- Из 06:

- Ревью всех новых экранов подтверждает `react-native-paper` components и отсутствие второго UI framework.
- DOM/browser tests запускают фактические Paper screens, а не тестовую HTML копию.
- Проверить web desktop, узкий mobile web и поддерживаемый native target с теми же backend contracts.
- Открытие/возврат Hosted Checkout на native квалифицируется отдельно; browser e2e не считается доказательством deep link на устройстве.
