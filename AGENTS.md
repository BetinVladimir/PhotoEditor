# AGENTS

Подсистема billing (`docs/billing`, `backend/internal/billing`, `backend/db/migrations/026*`, `scripts/generate-billing-*.py`, тесты `tests/*/test_billing_*`):

1. Прочитай `docs/billing/START.md` и `docs/billing/RULES.md`. Больше ничего не читай целиком.
2. Работай по одной задаче: собери пакет `python3 docs/billing/tools/build-pack.py --task <ID> --window 50000 --budget 24000 --reserve 14000 --out /tmp/pack.md` и читай только его.
3. Тесты пишутся вместе с кодом в той же задаче (R33).
4. Нет значения (цена, лимит, срок, налог, сигнатура) — остановись и задай вопрос, не угадывай (`docs/billing/OPEN.md`).
5. Сгенерированные файлы не правь руками.

Вне billing этот файл ничего не требует.
