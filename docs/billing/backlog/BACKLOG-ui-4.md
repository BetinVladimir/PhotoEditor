# Бэклог: Paper UI (BILL-10), часть 4

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
