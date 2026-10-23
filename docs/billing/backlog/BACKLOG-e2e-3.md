# Бэклог: e2e E01–E90 (разделы 08 и 10), часть 3

### E21 — Cancel AI

- Положительный: Storage-only и купленные packs продолжают по условиям
- Отрицательный: Cancel AI не отменяет storage items и не выдаёт новый Trial20
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e21_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E22 — Rollover

- Положительный: Generation0 остаток переносится один раз после paid renewal
- Отрицательный: Generation1 повторно не переносится; unpaid gap не создаёт rollover
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e22_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E23 — Rollover pending

- Положительный: Late success расходует только исходный leased funding, late failure переносит освободившееся ровно один раз при eligible paid next period
- Отрицательный: Funding accepted tickets не входит в rollover и не создаёт искусственный overrun
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e23_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E24 — Photo topup

- Положительный: Пакеты 100/500/1000 даются на 90 суток после оплаты
- Отрицательный: Trial/foreign-plan offer/custom без assignment нельзя купить
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e24_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E25 — Expiry order

- Положительный: Расходуется ближайший expiry; до expiry accepted result завершается позже
- Отрицательный: Release после expiry не оживляет истёкший остаток
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e25_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E26 — Lease chunks

- Положительный: A1/A2 получают конечные разные paid allocations
- Отрицательный: Два instances не расходуют один paid bucket; allowance bound не превышен
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e26_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E27 — Optimistic overrun

- Положительный: Один разрешённый небольшой overrun виден отдельно без автоматического денежного списания
- Отрицательный: Дальнейшие requests после allowance rejected; no negative paid balance/no auto invoice
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e27_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E28 — Cache stale

- Положительный: Уже committed runs завершаются после entitlement update
- Отрицательный: Expired lease/stale fence/new generation не выдаёт новые valid tickets
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e28_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E29 — Lease crash

- Положительный: После crash recovery возвращает только unissued часть
- Отрицательный: TTL не возвращает credits accepted run и не допускает второго использования
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e29_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E30 — Billing outage

- Положительный: Принятые задачи завершаются, cached summary помечен stale
- Отрицательный: После finite lease expiry новые admissions503, не unlimited fail-open
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e30_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.
