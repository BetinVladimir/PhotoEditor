# Бэклог: e2e E01–E90 (разделы 08 и 10), часть 7

### E61 — Legal links

- Положительный: Все ссылки legal.beeloy.org, version archive/hash и translations проверены
- Отрицательный: Missing PhotoEditor scope/неподдержанный B2C блокируют live; loyalty terms не маскируют пробел
- Тема (подсказка по ключевым словам): `legal`. Выход: функция `test_e61_*` в `tests/integration/test_billing_e2e_legal.py`; UI-часть — `apps/photographer/src/billing-e2e-legal.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E62 — Delete member

- Положительный: Fresh-auth member удаляет PII/сессии, общая студия сохраняется
- Отрицательный: Stale auth/CSRF/чужой request не удаляют профиль/tenant
- Тема (подсказка по ключевым словам): `legal`. Выход: функция `test_e62_*` в `tests/integration/test_billing_e2e_legal.py`; UI-часть — `apps/photographer/src/billing-e2e-legal.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E63 — Delete owner

- Положительный: Transfer ownership или явный tenant close корректно завершает erasure
- Отрицательный: Sole owner не стирает чужую shared студию молча; запрос не висит без deadline
- Тема (подсказка по ключевым словам): `legal`. Выход: функция `test_e63_*` в `tests/integration/test_billing_e2e_legal.py`; UI-часть — `apps/photographer/src/billing-e2e-legal.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E64 — Delete payments

- Положительный: Deleting блокирует Checkout/grant и отменяет будущие charges по scope
- Отрицательный: Late webhook/reconcile не восстанавливают account и subscription; paid orphan компенсируется
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e64_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E65 — PII inventory

- Положительный: Canary PII очищена из auth/contact/invites/events/cache/local storage
- Отрицательный: Очистка только users.email не проходит; full IP/email в logs/raw payload выявляются
- Тема (подсказка по ключевым словам): `legal`. Выход: функция `test_e65_*` в `tests/integration/test_billing_e2e_legal.py`; UI-часть — `apps/photographer/src/billing-e2e-legal.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E66 — Photo erasure

- Положительный: Все purge-scope object versions/multipart/EXIF/embeddings удалены с proof
- Отрицательный: Shared refs требуют явного retention reason, failed object DELETE не completed
- Тема (подсказка по ключевым словам): `legal`. Выход: функция `test_e66_*` в `tests/integration/test_billing_e2e_legal.py`; UI-часть — `apps/photographer/src/billing-e2e-legal.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E67 — Stripe erasure

- Положительный: Customer PII redacted/deleted где применимо, invoices минимально retained
- Отрицательный: Stripe outage→external_pending+retry, не ложный completed
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e67_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E68 — Backup restore

- Положительный: Restore до serving применяет tombstones, данные не воскресают
- Отрицательный: Старый login/photo/hosted customer связь не становятся активными после restore
- Тема (подсказка по ключевым словам): `quota`. Выход: функция `test_e68_*` в `tests/integration/test_billing_e2e_quota.py`; UI-часть — `apps/photographer/src/billing-e2e-quota.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E69 — Legal retention

- Положительный: Retained record имеет approved basis/expiry/restricted access
- Отрицательный: Нет blanket «GDPR всё удалить» для statutory invoice и нет бессрочного retain all
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e69_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E70 — Repeated erasure

- Положительный: Повтор запроса/worker restart возвращает тот же progress
- Отрицательный: Дубли jobs не отменяют чужую subscription и не посылают повторный refund
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e70_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.
