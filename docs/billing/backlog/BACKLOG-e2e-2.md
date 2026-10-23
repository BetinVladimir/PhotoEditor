# Бэклог: e2e E01–E90 (разделы 08 и 10), часть 2

### E11 — SCA

- Положительный: Challenge success завершает purchase
- Отрицательный: Decline/canceled challenge сохраняют старый план/баланс
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e11_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E12 — Signatures

- Положительный: Signed real event durable сохранён и применён
- Отрицательный: Неверная signature/stale timestamp/raw-body modification400 без inbox grant
- Тема (подсказка по ключевым словам): `general`. Выход: функция `test_e12_*` в `tests/integration/test_billing_e2e_general.py`; UI-часть — `apps/photographer/src/billing-e2e-general.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E13 — Event dedup

- Положительный: Дубликат event и разные events об одном invoice дают один grant
- Отрицательный: Повтор с чужим customer/account/mode quarantined, нет cross-tenant изменения
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e13_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E14 — Out of order

- Положительный: subscription/update и invoice paid в обратном порядке сходятся к истине
- Отрицательный: Поздний старый canceled/update не откатывает более актуальную verified business state
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e14_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E15 — Lost webhook

- Положительный: Reconciliation находит реальный paid invoice и выдаёт missing grant
- Отрицательный: Повторный full reconciliation не повторяет grant/notifications
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e15_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E16 — Crash fulfillment

- Положительный: Crash после paid commit до grant восстанавливается
- Отрицательный: Crash после grant до ack не дублирует quota
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e16_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E17 — AI upgrade

- Положительный: Start→Photographer,60 used: gross1200,+200, available240, anchor прежний
- Отрицательный: Failure/SCA отказ не меняют plan; две вкладки не применяют две delta
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e17_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами. Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`).
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E18 — Renewal race

- Положительный: Оплаченный renewal рядом с upgrade согласует цену и quota по saga
- Отрицательный: Crash provider update/local apply восстанавливается без второго delta/charge
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e18_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E19 — Compensation

- Положительный: Paid upgrade с необратимым apply failure делает refund+correction
- Отрицательный: Нет бесконечного applying без alert и нет нового требования оплатить тот же upgrade
- Тема (подсказка по ключевым словам): `provider`. Выход: функция `test_e19_*` в `tests/integration/test_billing_e2e_provider.py`; UI-часть — `apps/photographer/src/billing-e2e-provider.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.

### E20 — Downgrade

- Положительный: Переход на границе сохраняет уже выданные packs до expiry
- Отрицательный: Immediate trim месячных/topup остатков запрещён
- Тема (подсказка по ключевым словам): `general`. Выход: функция `test_e20_*` в `tests/integration/test_billing_e2e_general.py`; UI-часть — `apps/photographer/src/billing-e2e-general.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`.
- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами.
- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.
