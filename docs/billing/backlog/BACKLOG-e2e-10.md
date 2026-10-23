# Бэклог: e2e E01–E90 (разделы 08 и 10), часть 10

### E89 — Closed scopes

- Положительный: Sandbox BG synthetic20% pass,live flag off; unsupported country fails before Stripe
- Отрицательный: Fixture price/operation limits не публикуются live; account/scope/currency подмена отвергается
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e89_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E90 — Multi-resource admission

- Положительный: Batch с free и paid items сохраняет all tickets и root stubs до seal
- Отрицательный: Late shortage одного ресурса не публикует половину DAG; abort/releases идемпотентны; active old tickets не украдены
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e90_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.
