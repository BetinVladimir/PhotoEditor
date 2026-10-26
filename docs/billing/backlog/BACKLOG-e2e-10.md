# Бэклог: e2e E01–E90 (разделы 08 и 10), часть 10

### E71 — Transport parity

- Положительный: Registry/HTTP/OwnerAPI schemas совпадают с /api/v1 identities и existing envelope
- Отрицательный: Неверный alias{tenant},unknown fields,unquoted/missing If-Match отвергаются до effects
- Тема (подсказка по ключевым словам): `general`. Выход: функция `test_e71_*` в `tests/integration/test_billing_e2e_general.py`; UI-часть — `apps/photographer/src/billing-e2e-general.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E72 — Receipt lifecycle

- Положительный: Replay возвращает исходные stable202 bytes/status; GET выдаёт current action
- Отрицательный: Истёкший URL не извлекается из receipt; revoke/re-role не раскрывает старый owner response; expired API receipt не позволяет второй grant
- Тема (подсказка по ключевым словам): `general`. Выход: функция `test_e72_*` в `tests/integration/test_billing_e2e_general.py`; UI-часть — `apps/photographer/src/billing-e2e-general.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E73 — Cumulative allowance

- Положительный: Один allowance1 принят, следующий после grant/correct cover не переоткрывает period budget
- Отрицательный: Lease expiry/restart/другой instance/новый topup не получают ещё allowance1; сумма outstanding+settled<=configured
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e73_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E74 — Rollover held credit

- Положительный: Bucket100,consumed60,leased10 переносит 30; late success10 остаётся original
- Отрицательный: При переносе 40 тест падает; late failure10 добавляет child ровно один раз только пока paid next period eligible
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e74_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E75 — Free rerun zero balance

- Положительный: При available0 и свободном slot accepted free result без paid lease
- Отрицательный: Третий/expired slot отказывает без credit; initial same digest in-flight409; no quote price change от другого failure
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e75_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E76 — Finance FK scope

- Положительный: Invoice с двумя ресурсами создаёт два typed allocations и один денежный payment
- Отрицательный: Другой sandbox/subject/tenant/line ID не связывается; orphan polymorphic refs не дают grant
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e76_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E77 — One user charge

- Положительный: Один standard user request с несколькими child runs и retries списывает 1
- Отрицательный: Каждый adopted model step не создаёт ещё credit debit; task-only result без видимого final output не charged
- Тема (подсказка по ключевым словам): `general`. Выход: функция `test_e77_*` в `tests/integration/test_billing_e2e_general.py`; UI-часть — `apps/photographer/src/billing-e2e-general.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.
