# Бэклог: Тестовая инфраструктура (TST), часть 1

### TST-01 — общая e2e-фикстура

- Цель: одна фикстура для всех `test_billing_e2e_*.py`: одноразовая БД, tenant, роли, активный профиль, fake-провайдер, управляемые часы.
- Вход: `tests/integration/billing_editor_fixture.py` (образец формы), `tests/integration/conftest.py` (фикстура `database`), `TESTING.md`.
- Выход: `tests/integration/billing_e2e_fixture.py` + тест самой фикстуры `tests/integration/test_billing_e2e_fixture.py`.
- Критерий: A. Фикстура создаёт и удаляет одноразовую БД, рабочую не трогает (R30). B. Два tenant для проверки RLS. C. Без БД — skip, как у `database`. D. Секретов нет (T8).
- Тесты (R33): `test_billing_e2e_fixture.py` — создание tenant, изоляция, откат.

### TST-02 — контрактный набор провайдера (fake и real)

- Цель: один набор проверок поведения провайдера (идемпотентность, timeout после успеха, неизвестный объект), запускаемый на fake и позже на real.
- Вход: результат `STR-01`, `05-stripe-test-setup.md`, `TESTING.md` T7.
- Выход: `contract_test.go` в пакете провайдера (путь — из `STR-01`): функция `RunProviderContract(t, factory)`; вызов для fake без условий, для real — только при наличии ключей.
- Критерий: A. Fake PASS без сети. B. Real пропускается явно, и это записано как «не запускалось» в `STATUS.md`. C. Один и тот же набор — T7.
- Тесты (R33): сам набор; проверка, что fake нарушает контракт при сломанной идемпотентности (негативный тест набора).

### TST-03 — команды `make test-billing-integration` и `make test-billing-e2e`

- Цель: единые команды приёмки, падающие при skip и при сценарии E01–E90 без теста.
- Вход: `Makefile` (цель `test-billing-contracts` как образец), `TESTING.md`.
- Выход: цели в `Makefile` и скрипт `docs/billing/tools/check-e2e-coverage.py` (каждый E-ID из 08/10 есть как `test_eNN_*` и имеет docstring с «Положительный» и «Отрицательный»).
- Критерий: A. Команда падает при `skipped>0`. B. `check-e2e-coverage.py` падает при пропущенном E-ID. C. Рабочая БД не используется.
- Тесты (R33): negative-проверка скрипта на временном каталоге с пропущенным сценарием.

### TST-04 — helper для dom-тестов Paper UI

- Цель: общий helper рендера экрана с провайдерами Paper, локалью, tenant и подменой API; проверка ключей локалей.
- Вход: `apps/photographer/src/quotas-locale.dom.test.tsx` (образец), `paper-provider`, `locale-context`, `owner-api`.
- Выход: `apps/photographer/src/billing-test-utils.tsx` и тест `billing-locale-keys.dom.test.tsx` (одинаковый набор ключей биллинга в bg/en/ru).
- Критерий: A. `cd apps/photographer && npm test -- --run` PASS. B. Лишний или отсутствующий ключ в одной локали роняет тест.
- Тесты (R33): `billing-locale-keys.dom.test.tsx`.

### TST-05 — запись доказательства приёмки

- Цель: не переписывать числа руками: скрипт принимает вывод pytest/go test и дописывает блок в `STATUS.md`.
- Вход: формат блока из `TESTING.md`, `STATUS.md`.
- Выход: `docs/billing/tools/record-evidence.py <ACC-id> <файл вывода>`; unit-тест `tests/contracts/test_billing_record_evidence.py`.
- Критерий: A. Из строки `12 passed, 2 skipped` получается `skipped=2` и пометка «приёмка не пройдена». B. Числа разных запусков не суммируются (T6). C. Повторная запись не затирает старые блоки.
- Тесты (R33): unit-тест на три формата вывода (чистый, со skip, с failed).
