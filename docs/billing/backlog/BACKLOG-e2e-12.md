# Бэклог: e2e E01–E90 (разделы 08 и 10), часть 12

### E85 — Source generation

- Положительный: Новые catalog/migrations additive, baseline hashes unchanged
- Отрицательный: Перегенерация billing не меняет DDL1–9, frozen source hashes и существующие parameter identities
- Тема (подсказка по ключевым словам): `catalog`. Выход: функция `test_e85_*` в `tests/integration/test_billing_e2e_catalog.py`; UI-часть — `apps/photographer/src/billing-e2e-catalog.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E86 — Single subscription

- Положительный: Storage add на existing AI и AI resume на storage-only используют тот же sub/anchor
- Отрицательный: Второй subscription_checkout запрещён; incomplete/failed invoice не даёт capacity
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e86_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E87 — Anchor conversion

- Положительный: Trial использовал 5GB,переход Start выдаёт 100GB paid и закрывает leftover Trial
- Отрицательный: Paid grant не уменьшается до 85/80GB, Trial20GB не остаётся вторым активным base entitlement
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e87_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E88 — Upgrade entitlements

- Положительный: Полная AI delta даёт photo/storage/egress10×storage delta один раз
- Отрицательный: Open renewal invoice/<=24h window не создают charge; unavoidable race компенсирует paid upgrade без двойного renewal
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e88_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E89 — Closed scopes

- Положительный: Sandbox BG synthetic20% pass,live flag off; unsupported country fails before Stripe
- Отрицательный: Fixture price/operation limits не публикуются live; account/scope/currency подмена отвергается
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e89_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E90 — Multi-resource admission

- Положительный: Batch с free и paid items сохраняет all tickets и root stubs до seal
- Отрицательный: Late shortage одного ресурса не публикует половину DAG; abort/releases идемпотентны; active old tickets не украдены
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e90_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.
