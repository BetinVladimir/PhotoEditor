# TESTING — тесты вместе с кодом, описание e2e и приёмка модулей

Правила пронумерованы (T1…T8), в задачах и PR ссылайтесь как `T4`.

## Правила

- **T1.** Тест пишется в той же задаче и в том же коммите, что и код. Задача без тестов не принята (`R33`).
- **T2.** У каждого модуля есть слои: contract (форма: JSON ↔ SQL, ACL, DTO), integration (PostgreSQL), Go (handler или harness под ролью БД), UI (dom-тесты), e2e (сценарии E01–E90).
- **T3.** Для e2e сначала пишется описание теста (шаблон ниже), потом код и тест. Описание живёт в docstring теста, отдельного документа нет.
- **T4.** Для каждой SQL-функции: успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках — ещё конкурентный тест на независимых соединениях.
- **T5.** `skip` = провал приёмки. Go-harness делает `t.Skip` без переменной окружения, Python-фикстура `database` делает skip без запущенной БД; такой прогон не засчитывается.
- **T6.** Числа разных прогонов не суммируются (наборы пересекаются). В `STATUS.md` каждый набор записывается отдельно.
- **T7.** Провайдер Stripe проверяется одним контрактным набором на fake и на real. Событие из Stripe CLI без реальной покупки не доказывает fulfillment.
- **T8.** В тестах, фикстурах и логах нет секретов.

## Что обязательно тестировать (по типу артефакта)

| Артефакт | Обязательные тесты | Где | Образец |
|---|---|---|---|
| Таблица | contract: колонки, PK, FK, CHECK из JSON; integration: нарушение каждого CHECK/FK, RLS по tenant | `tests/contracts/test_billing_<g>_contract.py`, `tests/integration/test_billing_<g>.py` | `test_billing_quota_contract.py`, `test_billing_quota.py` |
| SQL-функция | contract: `execute_roles` = GRANT; integration: T4 | те же | `test_billing_routed_apply.py` |
| API endpoint | Go: успех, 422, роли, идемпотентность; contract: endpoints и DTO в обоих реестрах | `backend/internal/httpapi/*_test.go` | `EXAMPLES-GO.md`, `EXAMPLES-API.md` |
| Go-обёртка над SQL | harness под ролью БД, холодная блокировка, MapError | `backend/internal/billing/*_test.go` | `operations_integration_test.go` |
| Stripe-код | Go-тесты на fake + общий контрактный набор провайдера | рядом с кодом | задачи `STR-*`, `TST-02` |
| Экран Paper | dom-тест: состояния, локали bg/en/ru, смена tenant | `apps/photographer/src/*.dom.test.tsx` | `quotas-locale.dom.test.tsx` |
| Сценарий E01–E90 | положительный и отрицательный тест + описание | `tests/integration/test_billing_e2e_<тема>.py` | шаблон ниже |

## Шаблон описания e2e (docstring теста)

```
"""E10 Async pay
Модули: provider, usage
Предусловия: tenant с активным профилем; Stripe — fake (потом sandbox); роли и флаги: <перечислить>
Положительный:
  Дано: заказ создан, оплата отложена
  Когда: приходит событие об успешной отложенной оплате
  Тогда: ровно один grant, summary обновлён
Отрицательный:
  Дано: оплата не завершена или tenant в состоянии deleting
  Когда: приходит событие
  Тогда: grant не выдан, аккаунт не воскрес
Инварианты: R19, R20 (ссылки на RULES.md)
Данные: фикстуры <имена>; секретов нет
"""
```

Описание обязано содержать оба сценария. Строки сценария берутся из 08 и 10 дословно, детали — из задачи `Eнн` в `backlog/`.

## Приёмка модуля

1. Предпосылки: `make dev-infra` (или `PHOTO_TEST_NATIVE_INFRA=1`), миграции применяются к одноразовым БД. Рабочая БД не трогается (`R30`).
2. Запуск: команды из задачи `ACC-*` модуля.
3. Условие: `failed=0` и `skipped=0` (`T5`).
4. Доказательство — блок в `STATUS.md`:

```
### ACC-<id> — <дата> — <git rev-parse --short HEAD>
- Команда: <точная команда>
- Результат: passed=N failed=0 skipped=0
- Не покрыто: <что не запускалось и почему>
```

5. Модуль принят, когда есть блок для каждого набора задачи `ACC-*` и закрыты все задачи группы с их тестами.

## Команды (статические проверены в песочнице; с PostgreSQL и Go — не проверялись там)

- Без БД: `bash docs/billing/tools/check-static.sh`.
- Integration: `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -p no:cacheprovider tests/integration/test_billing_*.py` (нужна БД).
- Go: `scripts/run-platform.sh go -C backend test ./internal/billing ./internal/run ./internal/v2sessions ./internal/workflow ./internal/v2input ./internal/v2render ./db`.
- UI: `cd apps/photographer && npm test -- --run && npm run typecheck`; веб-сборка `npx expo export --platform web` (`make test-web`).
