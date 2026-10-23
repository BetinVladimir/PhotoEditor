# CODE-REVIEW — ревью текущего кода биллинга (23.10.2026)

Объём: `backend/internal/billing` (5 файлов, 301 строка Go), SQL биллинга в миграциях 02598–02609 и в `contracts/`, 9 генераторов, 9 контрактных и 9 integration-тестов, места вызова в `run`, `workflow`, `v2input`, `v2render`, `v2sessions`.

## Что реально выполнено

| Проверка | Результат |
|---|---|
| 9 генераторов `scripts/generate-billing-*.py --check` | PASS, расхождений нет |
| `tests/contracts/test_billing_*.py` | 42 PASS (без БД) |
| Сверка ACL по миграциям 02598–02609 (52 функции) | у всех `SECURITY DEFINER` и фиксированный `search_path`; EXECUTE у `PUBLIC` и `photo_worker` не выдан; роли: `photo_api` 8, `photo_scheduler` 10 |
| Код ошибок SQL против `MapError` | см. находку 2 |

## Что НЕ выполнено (причина)

- `tests/integration/test_billing_*.py`: в среде ревью нет PostgreSQL.
- `go build`, `go vet`, `go test`: нет Go 1.26, загрузка заблокирована прокси.
- Не-биллинговые контрактные тесты (`test_authored_render*`): нужен `psycopg`, которого в среде нет.
- Проверка ACL грубая: разбирает явные `GRANT`/`REVOKE` в тексте миграций; права, выданные динамически в `DO`-блоках, она не видит.
- Рабочее дерево содержит чужие незакоммиченные изменения (`gateway/*`, `workflow/apply*.go`, `workflow/service.go`); результаты относятся к текущему состоянию дерева.

## Находки

| № | Важность | Находка | Доказательство | Что сделано / задача |
|---|---|---|---|---|
| 1 | Средняя | Контрактные и drift-тесты биллинга не входили ни в один `make`-таргет | `Makefile`: `test-python` → `tests/platform`, `test-db` → `tests/integration`; `tests/contracts` нигде не вызывался | Добавлен `test-billing-contracts` (`check-static.sh`), подключён к `test` и `test-unit`. Откат: `git diff -- Makefile` |
| 2 | Средняя | 45 из 56 SQL-кодов ошибок биллинга не обработаны в `MapError` и уйдут клиенту как `500 INTERNAL_ERROR` | `ERROR-CODES.md` | Решение владельца 23.10.2026 (`OPEN-11`): 26 внутренних остаются 500; 19 получают 422/409/503; задача T-02 |
| 3 | Низкая | Документация расходилась с кодом | head миграций 02656, а не 02609; в `contracts/README.md` не было генератора 02608; 38 функций из 04 названы иначе, чем реализованы (совпало 1) | Исправлено в `EXAMPLES.md`, `RULES.md`, `contracts/README.md`; бэклог предупреждает о дублях |
| 4 | Низкая | Логика «холодное ожидание 200 мс → `BILLING_UNAVAILABLE` 503» скопирована 4 раза | `workflow.go`, `editor.go`, `render.go`, `operations.go` | T-03 |
| 5 | Низкая | У пакета `billing` нет unit-тестов; единственный integration-тест выполняет `t.Skip` без переменной окружения | `operations_integration_test.go` | T-04; предупреждение в `EXAMPLES-TESTS.md` |
| 6 | Закрыто | `PrepareOperations` в отличие от остальных Prepare* не использует флаг `enabled` | `operations.go`, `v2input/admission.go:97`; `billing_bind_operations` повторяет `billing_prepare_operations` сам | `OPEN-12`: проверено по SQL, намеренно. Добавлен комментарий в `operations.go` |

## Что в коде выглядит хорошо

- Go-слой тонкий: вся логика в SQL, нет сетевых и object-store вызовов (комментарий пакета подтверждён).
- Все Prepare берут блокировки биллинга до run/quota locks и ограничивают холодное ожидание.
- Ошибки SQL не просачиваются клиенту: `writeError` отдаёт только код, `message_key`, `args`, `request_id`.
- Генераторы, контракты и миграции синхронны; повторные изменения оформляются новыми миграциями.

## Что осталось вне ревью

Тела SQL-функций (67 штук, 11 длиннее 2K токенов) на корректность блокировок и атомарность я не читал построчно: это требует PostgreSQL и конкурентных тестов (`T-01`). Paper UI и Stripe-интеграция в коде ещё не существуют.
