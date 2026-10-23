# Бэклог: e2e E01–E90 (разделы 08 и 10), часть 1

### E01 — Seed

- Положительный: Fresh DB исходного sandbox seed имеет 7 plans/6 prices/45 offers, точные bytes/цены
- Отрицательный: Неполный plan version не публикуется; Custom не попадает в public catalog
- Тема (подсказка по ключевым словам): `catalog`. Выход: функция `test_e01_*` в `tests/integration/test_billing_e2e_catalog.py`; UI-часть — `apps/photographer/src/billing-e2e-catalog.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E02 — Upgrade DB

- Положительный: Existing beta сохраняет архив/reservations и согласованный access
- Отрицательный: Новый Trial limit не уничтожает >2GB архив; no historical charge
- Тема (подсказка по ключевым словам): `catalog`. Выход: функция `test_e02_*` в `tests/integration/test_billing_e2e_catalog.py`; UI-часть — `apps/photographer/src/billing-e2e-catalog.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E03 — Replay миграции

- Положительный: Повтор backfill/provisioning не добавляет profile/grant/Price
- Отрицательный: Прерванный batch продолжает cursor, без дублирующего Trial
- Тема (подсказка по ключевым словам): `catalog`. Выход: функция `test_e03_*` в `tests/integration/test_billing_e2e_catalog.py`; UI-часть — `apps/photographer/src/billing-e2e-catalog.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E04 — Trial

- Положительный: Новый tenant один раз получает 20 фото без карты
- Отрицательный: Relogin/rejoin/new member не дают 20 снова
- Тема (подсказка по ключевым словам): `catalog`. Выход: функция `test_e04_*` в `tests/integration/test_billing_e2e_catalog.py`; UI-часть — `apps/photographer/src/billing-e2e-catalog.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E05 — Catalog

- Положительный: Публикуемые планы и eligible offers отображены Paper cards
- Отрицательный: Подставленный Custom/closed Studio Pro ID не создаёт Checkout
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e05_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E06 — VAT fixture

- Положительный: Start900+180=1080, Photographer1900+380=2280
- Отрицательный: Client amount/tax/currency подмена отвергается; неподдержанный tax scope не получает безусловные 20%
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e06_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E07 — Quote

- Положительный: Актуальный quote создаёт immutable order
- Отрицательный: Expired quote/changed price/membership создаёт 409/403 без charge
- Тема (подсказка по ключевым словам): `general`. Выход: функция `test_e07_*` в `tests/integration/test_billing_e2e_general.py`; UI-часть — `apps/photographer/src/billing-e2e-general.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E08 — Idempotency

- Положительный: Двойной Paper click и retry возвращают один order/session
- Отрицательный: Тот же key с другим payload409, нет второго платежа
- Тема (подсказка по ключевым словам): `ui`. Выход: функция `test_e08_*` в `tests/integration/test_billing_e2e_ui.py`; UI-часть — `apps/photographer/src/billing-e2e-ui.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E09 — Subscription

- Положительный: Реальная sandbox оплата выдаёт одну месячную квоту
- Отрицательный: Success URL без webhook/paid proof не даёт entitlement
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e09_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E10 — Async pay

- Положительный: Delayed successful payment позднее выдаёт grant
- Отрицательный: Pending/failure не дают grant; после deleting поздняя оплата не оживляет account
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e10_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.
