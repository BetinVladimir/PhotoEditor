# Бэклог: e2e E01–E90 (разделы 08 и 10), часть 6

### E51 — Tenant isolation

- Положительный: Switching tenant показывает только новую summary
- Отрицательный: Foreign UUID/order/quote/bucket/result404/403, no cross-tenant RLS bypass
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e51_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E52 — Old settings

- Положительный: Технические настройки меняются только разрешённым процессом
- Отрицательный: Старый tenant PATCH/прямой SQL role не повышает paid entitlement
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e52_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E53 — Paper screens

- Положительный: Все billing/legal/deletion экраны используют реальные Paper components
- Отрицательный: Нет HTML substitute/нового framework; portal recipient не получает payment controls
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e53_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E54 — UI states

- Положительный: Mobile, keyboard, dark mode, bigint, timezone и decimal GB корректны
- Отрицательный: Loading/offline/stale/failed не выглядят successful/zero balance; focus восстанавливается
- Тема (подсказка по ключевым словам): `ui`. Выход: функция `test_e54_*` в `tests/integration/test_billing_e2e_ui.py`; UI-часть — `apps/photographer/src/billing-e2e-ui.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E55 — Resume purchase

- Положительный: Reload/return from Stripe восстанавливает тот же order и batch selection
- Отрицательный: Tampered deep link/success param не дает платный доступ, новый charge не создаётся
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e55_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E56 — Notifications

- Положительный: 80/95/100% один раз на resource/period
- Отрицательный: Re-render/replayed outbox не спамят; новая квота пересчитывает threshold корректно
- Тема (подсказка по ключевым словам): `general`. Выход: функция `test_e56_*` в `tests/integration/test_billing_e2e_general.py`; UI-часть — `apps/photographer/src/billing-e2e-general.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E57 — Onboarding

- Положительный: Явное принятие актуальных Terms/DPA и Privacy acknowledgement активирует account
- Отрицательный: Unchecked/подменённая версия/locale/hash/изменившийся текст не создают фиктивный consent
- Тема (подсказка по ключевым словам): `legal`. Выход: функция `test_e57_*` в `tests/integration/test_billing_e2e_legal.py`; UI-часть — `apps/photographer/src/billing-e2e-legal.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E58 — Optional consent

- Положительный: Reject optional позволяет core service; accepted SDK только после выбора
- Отрицательный: До consent нет optional network/storage; closing banner не accept
- Тема (подсказка по ключевым словам): `legal`. Выход: функция `test_e58_*` в `tests/integration/test_billing_e2e_legal.py`; UI-часть — `apps/photographer/src/billing-e2e-legal.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E59 — Withdrawal

- Положительный: Отзыв analytics/marketing применяется и документируется
- Отрицательный: Не отнимает paid core service и не открывает matching/learning consent
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e59_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E60 — Existing legal

- Положительный: Existing account явно принимает новую требуемую версию
- Отрицательный: Backfill не записывает accepted задним числом; obsolete version не покупает новый план
- Тема (подсказка по ключевым словам): `legal`. Выход: функция `test_e60_*` в `tests/integration/test_billing_e2e_legal.py`; UI-часть — `apps/photographer/src/billing-e2e-legal.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.
