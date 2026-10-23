# Бэклог: e2e E01–E90 (разделы 08 и 10), часть 5

### E41 — Storage reserve

- Положительный: Upload declared/actual bytes и output reservations сходятся
- Отрицательный: Full disk/oversized object блокируют admission, GPU не стартует без safety reserve
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e41_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E42 — Storage decrease

- Положительный: Next-boundary decrease даёт overquota и понятный read grace
- Отрицательный: Ни cancel, ни возраст 30 дней автоматически не purges client originals/results
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e42_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E43 — Storage dedup

- Положительный: Одна physical version shared внутри tenant counted once
- Отрицательный: Отдельная ZIP/export copy counted; ссылка не удваивает bytes
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e43_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E44 — Purge bytes

- Положительный: После verified physical deletion committed bytes уменьшаются
- Отрицательный: Delete requested/failed object removal не освобождают место заранее
- Тема (подсказка по ключевым словам): `legal`. Выход: функция `test_e44_*` в `tests/integration/test_billing_e2e_legal.py`; UI-часть — `apps/photographer/src/billing-e2e-legal.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E45 — Egress

- Положительный: Body/Range/preview/repeat download/CDN feed counts реальные bytes
- Отрицательный: HEAD/304/no-body=0; internal worker I/O не billable; invalid Range не тратит full object
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e45_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E46 — Egress bound

- Положительный: Active streams тратят выделенные chunks, bounded tail after cutoff
- Отрицательный: Большой ZIP/direct URL не обходят quota; превышение не равно неограниченному full stream
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e46_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E47 — Egress restart

- Положительный: Partial flush receipt replay без double consumption
- Отрицательный: Потеря метрики после crash не скрывает неограниченные bytes; ambiguous scope закрыт/reconciled
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e47_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E48 — Refund

- Положительный: Unused remainder полного/частичного refund отзывается один раз
- Отрицательный: Consumed history не удаляется; active tickets не создают negative bucket
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e48_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E49 — Dispute

- Положительный: Verified dispute отражает опубликованный доступ и history
- Отрицательный: Чужой refund/dispute не блокирует другой tenant; deletion остаётся доступным
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e49_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E50 — Roles

- Положительный: Owner purchase и member read-only usage
- Отрицательный: Member POST checkout/portal/change/custom assignment403; invoice URL не выдаётся
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e50_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.
