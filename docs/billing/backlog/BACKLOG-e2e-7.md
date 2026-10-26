# Бэклог: e2e E01–E90 (разделы 08 и 10), часть 7

### E48 — Refund

- Положительный: Unused remainder полного/частичного refund отзывается один раз
- Отрицательный: Consumed history не удаляется; active tickets не создают negative bucket
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e48_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E49 — Dispute

- Положительный: Verified dispute отражает опубликованный доступ и history
- Отрицательный: Чужой refund/dispute не блокирует другой tenant; deletion остаётся доступным
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e49_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E50 — Roles

- Положительный: Owner purchase и member read-only usage
- Отрицательный: Member POST checkout/portal/change/custom assignment403; invoice URL не выдаётся
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e50_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E51 — Tenant isolation

- Положительный: Switching tenant показывает только новую summary
- Отрицательный: Foreign UUID/order/quote/bucket/result404/403, no cross-tenant RLS bypass
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e51_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E52 — Old settings

- Положительный: Технические настройки меняются только разрешённым процессом
- Отрицательный: Старый tenant PATCH/прямой SQL role не повышает paid entitlement
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e52_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E53 — Paper screens

- Положительный: Все billing/legal/deletion экраны используют реальные Paper components
- Отрицательный: Нет HTML substitute/нового framework; portal recipient не получает payment controls
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e53_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E54 — UI states

- Положительный: Mobile, keyboard, dark mode, bigint, timezone и decimal GB корректны
- Отрицательный: Loading/offline/stale/failed не выглядят successful/zero balance; focus восстанавливается
- Тема (подсказка по ключевым словам): `ui`. Выход: функция `test_e54_*` в `tests/integration/test_billing_e2e_ui.py`; UI-часть — `apps/photographer/src/billing-e2e-ui.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.
