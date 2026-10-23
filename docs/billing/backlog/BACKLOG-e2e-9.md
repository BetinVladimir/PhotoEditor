# Бэклог: e2e E01–E90 (разделы 08 и 10), часть 9

### E80 — Fresh proof/epoch

- Положительный: Новая purpose-bound OTP proof в 5min принимается один раз
- Отрицательный: Refresh/new access token,чужой purpose,старый auth_epoch,OTP recovery после erasure не открывают deletion/auth
- Тема (подсказка по ключевым словам): `legal`. Выход: функция `test_e80_*` в `tests/integration/test_billing_e2e_legal.py`; UI-часть — `apps/photographer/src/billing-e2e-legal.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E81 — Controlled URL

- Положительный: Actual photographer и portal validators принимают controlled URL с единственным bound versionId, исходным DTO и finite TTL
- Отрицательный: New enforced link не выдаёт direct object URL; bucket public access/expired capability/revoked share отвергнуты
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e81_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E82 — Crash byte ambiguity

- Положительный: Подтверждённые chunks settled once; unresolved bounded hold виден и reconciled
- Отрицательный: Crash после write до DB не списывает весь ZIP и не возвращает unlimited allocation; HEAD0/no duplicate receipt
- Тема (подсказка по ключевым словам): `general`. Выход: функция `test_e82_*` в `tests/integration/test_billing_e2e_general.py`; UI-часть — `apps/photographer/src/billing-e2e-general.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E83 — Outbox isolation

- Положительный: Одно событие user result порождает нужный task/finance bridge atomic
- Отрицательный: Task publisher не запускает financial grant, billing consumer не повторяет DAG activation
- Тема (подсказка по ключевым словам): `general`. Выход: функция `test_e83_*` в `tests/integration/test_billing_e2e_general.py`; UI-часть — `apps/photographer/src/billing-e2e-general.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E84 — Retention migration

- Положительный: Manual cohort хранит retained results/previews/archive дольше 30 дней, temporary cleanup работает
- Отрицательный: Legacy DTO0/NULL не включает manual; ни один age-GC не удаляет клиентский архив в обход policy
- Тема (подсказка по ключевым словам): `catalog`. Выход: функция `test_e84_*` в `tests/integration/test_billing_e2e_catalog.py`; UI-часть — `apps/photographer/src/billing-e2e-catalog.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E85 — Source generation

- Положительный: Новые catalog/migrations additive, baseline hashes unchanged
- Отрицательный: Перегенерация billing не меняет DDL1–9, frozen source hashes и существующие parameter identities
- Тема (подсказка по ключевым словам): `catalog`. Выход: функция `test_e85_*` в `tests/integration/test_billing_e2e_catalog.py`; UI-часть — `apps/photographer/src/billing-e2e-catalog.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E86 — Single subscription

- Положительный: Storage add на existing AI и AI resume на storage-only используют тот же sub/anchor
- Отрицательный: Второй subscription_checkout запрещён; incomplete/failed invoice не даёт capacity
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e86_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E87 — Anchor conversion

- Положительный: Trial использовал 5GB,переход Start выдаёт 100GB paid и закрывает leftover Trial
- Отрицательный: Paid grant не уменьшается до 85/80GB, Trial20GB не остаётся вторым активным base entitlement
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e87_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E88 — Upgrade entitlements

- Положительный: Полная AI delta даёт photo/storage/egress10×storage delta один раз
- Отрицательный: Open renewal invoice/<=24h window не создают charge; unavoidable race компенсирует paid upgrade без двойного renewal
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e88_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.
