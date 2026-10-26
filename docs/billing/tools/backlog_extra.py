"""Задачи Stripe / Paper UI / e2e для gen-backlog.py. Текст требований берётся дословно из 05, 06, 08, 10."""
import os, re

def build(DOCS, est, chunk):
    rd = lambda n: open(os.path.join(DOCS, n), encoding="utf-8").read()
    d5, d6, d8, d10 = rd("05-stripe-test-setup.md"), rd("06-paper-ui.md"), rd("08-delivery-migrations-e2e.md"), rd("10-audit-and-traceability.md")
    def lines(doc, subs, exclude=()):
        out = []
        for ln in doc.split("\n"):
            s = ln.strip()
            if s and (s.startswith("-") or s.startswith("|")) and any(x in s for x in subs) and not any(x in s for x in exclude) and s not in out: out.append(s)
        return out
    def section(doc, head):
        m = re.search(r"^## %s\n(.*?)(?=^## |\Z)" % re.escape(head), doc, re.S | re.M)
        return [l.strip() for l in (m.group(1) if m else "").split("\n") if l.strip().startswith(("-", "|")) and not re.match(r"^\|[\s\-|:]+\|$", l.strip()) and not l.strip().startswith("| Настройка |")]
    files, idx = {}, []
    def pack(group, title, items):
        chunks, cur = [], []
        for it in items:
            if cur and est("\n".join(cur + it)) > chunk: chunks.append(cur); cur = []
            cur = cur + it
        if cur: chunks.append(cur)
        names = []
        for k, ch in enumerate(chunks, 1):
            nm = "BACKLOG-%s-%d.md" % (group, k); names.append(nm); files[nm] = "\n".join(["# Бэклог: %s, часть %d" % (title, k), ""] + ch)
        return names
    # ---------------- Stripe ----------------
    NOKEY = "Ключи не нужны: проверяется на fake-провайдере (`STR-01`)."
    S = [
     ("STR-00", "Конфигурация Stripe и проверка при старте", "backend/internal/config/config.go: поля и валидация; backend/internal/config/config_test.go; deploy/env/production.env.example (значения REPLACE).", ["Secrets refs", "целевые имена", "Не создавать public key", "Startup сравнивает", "Никаких sk/whsec"], [], "A. `BILLING_STRIPE_ENABLED` не true → ошибок нет. B. true без любой переменной → ошибка без значений секретов. C. `STRIPE_ENVIRONMENT` не `test` → ошибка. D. Относительный путь `*_REF` → ошибка. E. `go test ./internal/config` PASS.", NOKEY + " Контракт: `STRIPE-CONFIG.md`."),
     ("STR-01", "Интерфейс провайдера и fake-реализация", "Новый пакет Go (имя выбрать по соседним: `backend/internal/billingprovider`, проверить отсутствие конфликта): интерфейс (Product, Price, Customer, Checkout Session, Invoice, Refund, чтение объектов и событий), in-memory fake с идемпотентностью по `provider_request_key`, real-адаптер-заглушка, возвращающий `BILLING_UNAVAILABLE`, пока нет конфигурации.", ["API version фиксируется", "стабильный поддерживаемый SDK", "Customer API", "provider_request_key", "Timeout после Stripe success"], [], "A. Один набор контрактных тестов прогоняется на fake (позже на real). B. В тестах нет сети. C. Повтор того же `provider_request_key` возвращает прежний ответ. D. Timeout после успеха не разрешается новым ключом, а чтением объекта.", NOKEY),
     ("STR-02", "Provisioning Products и Prices", "Функция/команда provisioning по catalog version и account ref; запись mapping в БД (таблицы из группы `finance`/`provider`); повтор без дублей.", ["| Products/Prices |", "Provisioning принимает", "Изменённая цена", "Нельзя обновлять цену", "Trial не требует"], [], "A. Повторный запуск не создаёт Product/Price. B. Изменение цены → новая Stripe Price и новая local price version. C. Trial без paid price. D. Локальные amount/interval/currency совпадают с провайдером.", NOKEY),
     ("STR-03", "Создание Customer", "Сервис создания Customer один раз на tenant/account с durable idempotency key; неопределённый ответ разрешается чтением.", ["Customer создаётся один раз"], [], "A. Двойной вызов → один Customer. B. Неизвестный результат → retrieval/reconciliation, не второй create.", NOKEY),
     ("STR-04", "Quote → order → Hosted Checkout (intent)", "Outbox-запись о создании Checkout, серверные price IDs, возврат action URL через GET order; суммы с клиента не принимаются.", ["Quote → order", "URL возврата", "checkout.session.completed", "| Checkout |"], [], "A. Цена не берётся из запроса клиента. B. Повтор Idempotency-Key возвращает тот же order. C. URL возврата не доказывает оплату.", NOKEY),
     ("STR-05", "Webhook ingress и inbox", "HTTP-приём с проверкой подписи по сырому телу, запись в inbox до 2xx, worker с retry lease.", ["| Webhooks |", "Ingress проверяет", "Worker имеет retry lease", "Поддержать lifecycle", "Не определять порядок", "Неожиданный customer", "Точный список и payload paths"], [], "A. Неверная подпись → отказ, inbox пуст. B. Дубликат и out-of-order не дают двойного эффекта. C. Неожиданный customer/order/mode/currency/price → quarantine без entitlement. D. Список событий и payload paths — по `OPEN-13`, не выдумывать.", NOKEY + " Реальная подпись Stripe — в `STR-14`."),
     ("STR-06", "Fulfillment: подтверждение оплаты и выдача (grant)", "Проверка актуального provider-объекта, invoice line allocation, атомарный grant, обновление summary.", ["Для recurring выдача", "Разные события о той же покупке", "authoritative payment verification"], [], "A. Одна покупка → один grant при любом числе событий. B. Recurring привязан к оплаченному invoice/line/period. C. Рестарт между paid и grant восстанавливается.", NOKEY),
     ("STR-07", "Upgrade saga (состояния и компенсация)", "Машина состояний quoted → payment_pending → paid → provider_price_applying → entitlement_applying → applied, retry, compensation.", [], ["Storage add-on", "Одна subscription может", "Не отменять всю"], "A. Каждое состояние и переход покрыты тестом. B. Оплаченный invoice без provider update остаётся paid/applying и не просит платить снова. C. Невозможность применить upgrade → возврат доплаты и compensating events. D. До нового upgrade закрыт предыдущий.", NOKEY + " Положительный real-sandbox тест обязателен до открытия upgrade (`STR-14`)."),
     ("STR-08", "Окно смены плана: BILLING_CHANGE_WINDOW_CLOSED", "Серверное правило допустимости upgrade и ответ 409; compensation при гонке после quote.", ["BILLING_CHANGE_WINDOW_CLOSED", "open renewal invoice", "Гонка после quote", "Этот путь обязателен", "Граница renewal сериализуется", "Если invoice renewal уже создан"], [], "A. Open renewal invoice или менее 24 часов до period_end → 409 `BILLING_CHANGE_WINDOW_CLOSED`, деньги не списываются. B. Гонка после quote → delta не применяется, платёж возвращается. C. Запрет по времени не заменяет compensation.", NOKEY),
     ("STR-09", "Storage add-on", "Policy proration остатка месяца, grant после оплаты, egress пропорционально оплаченной доле; AI и storage в одной subscription.", ["Storage add-on", "Одна subscription может", "Не отменять всю"], [], "A. Storage-only переживает отмену AI. B. Отмена только AI не отменяет subscription. C. Egress пропорционален оплаченной доле.", NOKEY),
     ("STR-10", "Refund и dispute", "Связь refund с order lines и quantity, отзыв неиспользованного остатка, политика dispute.", [], [], "A. Consumed не исчезает, неиспользованный остаток отзывается компенсирующим событием. B. Partial refund без однозначного quantity требует явного распределения оператора. C. Dispute закрывает новые risky admissions, но не удаление и права на данные. D. Invoice и журналы не удаляются.", NOKEY),
     ("STR-11", "Reconciliation и восстановление после сбоя", "Сверка paid invoice allocations после restore и потерянного webhook; tombstones применяются до выдачи.", ["После DB restore", "потерянный webhook", "Test Clock overdue", "Data restore"], [], "A. Потерянный webhook восстанавливается сверкой. B. После restore missing grants восстанавливаются, erasure tombstones сохраняются. C. Удалённый аккаунт не воскресает.", NOKEY),
     ("STR-12", "TaxScope v1 и интерфейс TaxResolver", "Синтетический BG customer, EUR, 20% tax exclusive, округление half-up построчно; иное → 422 до создания provider object.", ["TaxScope v1", "BILLING_TAX_SCOPE_UNSUPPORTED", "Это не политика", "TaxResolver", "| Taxes |"], [], "A. Другая страна/режим → 422 `BILLING_TAX_SCOPE_UNSUPPORTED` без объектов у провайдера. B. Округление half-up построчно. C. Интерфейс TaxResolver сохранён для будущей конфигурации. D. 20% не применяется ко всему миру (`OPEN-04`).", NOKEY),
     ("STR-13", "Customer Portal, dunning, уведомления", "Конфигурация Portal без смены плана, политика неуспешного renewal, только тестовые адреса.", ["| Customer Portal |", "| Retry/dunning |", "| Notifications |"], [], "A. Смена тарифа вне нашей saga невозможна. B. Нет гранта на unpaid invoice. C. CI не шлёт production emails.", NOKEY),
     ("STR-14", "Реальные sandbox-проверки перед тестовым включением", "Чек-лист из 05 на реальном Stripe Sandbox.", [], [], "A. Каждый пункт чек-листа пройден и записан в `STATUS.md` (дата, что именно, результат). B. Synthetic trigger из Stripe CLI без реальной покупки не считается доказательством fulfillment. C. Live mapping отсутствует.", "**ЖДЁТ КЛЮЧЕЙ (`OPEN-14`).** Порядок добавления — `STRIPE-CONFIG.md`."),
     ("STR-15", "Операторские шаги настройки аккаунта (не код)", "Выполняет человек; результат — записи в закрытом deployment manifest без секретов.", ["| Workspace |", "| Seller |", "| Access |", "| Keys |"], [], "A. Для каждой строки есть доказательство из колонки «Доказательство завершения». B. Ни один секрет не попал в git, docs, логи.", "**ЖДЁТ КЛЮЧЕЙ (`OPEN-14`).** Не задача для модели."),
     ("STR-16", "Структура подписки и lifecycle", "Правила одной recurring subscription, общий month anchor, Hosted Checkout только без открытой subscription, блокировка по lifecycle финансового субъекта.", ["Только один sandbox/account", "общий month anchor", "Начало storage-only", "Не создавать вторую recurring", "Hosted Checkout subscription используется", "Возобновление AI на storage-only", "Операции account deletion"], [], "A. Один активный account на billing profile. B. Все items одной subscription делят anchor. C. Storage add-on не создаёт вторую recurring subscription. D. Удаление пользователя не закрывает чужой работающий billing profile transferred tenant.", NOKEY),
    ]
    sec_whole = {"STR-10": "Refund и dispute", "STR-14": "Проверки перед тестовым включением", "STR-07": "Upgrade saga"}
    items = []
    for sid, title, out, subs, excl, crit, note in S:
        ls = section(d5, sec_whole[sid]) if sid in sec_whole else lines(d5, subs)
        if excl: ls = [l for l in ls if not any(x in l for x in excl)]
        items.append(["### %s — %s" % (sid, title), "", "- Группа: Stripe (BILL-07/08/09). %s" % note, "- Вход: строки 05 ниже; `STRIPE-CONFIG.md`; зависит от таблиц и функций групп `finance`, `provider`, `usage` из `backlog/README.md`.", "- Выход: %s" % out, "- Тесты (в этой же задаче, R33, `TESTING.md` T7): Go-тесты рядом с кодом на fake-провайдере (`STR-01`); общий контрактный набор провайдера `TST-02` расширяется кейсами этой задачи; секретов и сети нет. Без них задача не принята.", "- Критерий: %s" % crit, "- Запреты: R6, R7, R10; секреты не логировать; live-режима нет.", "- Из 05:", ""] + ls + [""])
    str_names = pack("stripe", "Stripe-провижининг (BILL-07/08/09)", items); str_count = len(S)
    # ---------------- UI ----------------
    ROUTES = {"Биллинг": "settings/billing", "Использование": "settings/quotas (экран уже существует: `screens/settings/quotas.tsx`, 20 строк, сейчас показывает storage_bytes/1073741824 с подписью GiB)", "Выбор плана": "settings/billing/plans", "Докупить ресурсы": "settings/billing/resources", "Статус заказа": "settings/billing/orders/[id]", "Cookies и privacy": "settings/privacy", "Удаление профиля": "settings/profile/delete", "Онбординг": "limited onboarding routes вне workspace-only gate (06, «Привязка к существующему Paper shell»)"}
    rows = [l for l in section(d6, "Экраны и формы")]
    U = []
    for i, r in enumerate(rows, 1):
        name = [c.strip() for c in r.strip("|").split("|")][0]
        route = ROUTES.get(name, "отдельного route в 06 нет: диалог/секция внутри другого экрана; не создавать параллельный routing stack")
        U.append(["### UI-%02d — экран «%s»" % (i, name), "", "- Статус: экран в `apps/photographer/screens/` отсутствует" + (" (кроме «Использование»)" if name == "Использование" else "") + ".", "- Вход: строка 06 ниже; `EXAMPLES-API.md` (DTO и endpoint ещё не существуют — группа `api`, работать против типов из DTO 09 §7); образец экрана и теста: `screens/settings/quotas.tsx`, `src/quotas-locale.dom.test.tsx`.",
                  "- Маршрут (по 06): %s." % route,
                  "- Выход: экран в `apps/photographer/screens/settings/`, маршрут в `app/[locale]/settings/` (+ legacy-редирект в `app/settings/` по образцу quotas), ключи в `locales/bg|en|ru/common.json`, тест `src/<имя>.dom.test.tsx`.",
                  "- Тесты (в этой же задаче, R33): `apps/photographer/src/<имя>.dom.test.tsx` — по одному кейсу на состояние из UI-12 и проверка ключей bg/en/ru; helper — `TST-04`.",
                  "- Критерий: A. Только компоненты `react-native-paper`, второго UI-фреймворка нет. B. Все состояния из UI-12 (loading, empty, error, stale, conflict, forbidden, deleting, offline). C. Ключи есть во всех трёх локалях. D. `cd apps/photographer && npm test -- --run && npm run typecheck` PASS. E. Цена и tenant не берутся из route params.",
                  "- Запреты: R13 (не `apps/portal`), R7; не придумывать тексты юридических документов и цены.", "- Из 06:", "", r, ""])
    shared = [("UI-00", "Навигация и routes", "Структура навигации", "Пункты настроек, planned routes, роли owner/admin/editor/viewer, legacy redirects.", "A. Роли видят ровно то, что указано. B. Нет параллельного routing stack. C. Deep link к чужому order/invoice не раскрывает финансов."),
              ("UI-12", "Состояния каждого экрана", "Состояния каждого экрана", "Общий компонент/хук состояний и правила (busy state, восстановление order по ID, expired quote, сброс при смене tenant).", "A. Double click блокируется busy state и backend key. B. Неизвестный outcome заказа восстанавливается по сохранённому order ID. C. Смена tenant сбрасывает sensitive данные."),
              ("UI-13", "Числа, доступность, уведомления 80/95/100%", "Отображение чисел и доступность", "Форматирование EUR/GB/GiB/bigint, ProgressBar, доступность, дедупликация уведомлений.", "A. GB (decimal) и GiB показаны раздельно. B. Bigint без потери точности. C. ProgressBar 0..1, превышение текстом. D. Уведомления дедуплицированы по resource/period/threshold. E. PII не уходит в analytics."),
              ("UI-14", "Привязка к существующему Paper shell", "Привязка к существующему Paper shell", "Использование `paper-provider`, `paper-theme`, `locale-context`, `localized-router`, `owner-api`, `use-cas-mutation`, workspace boundary; onboarding вне workspace-only gate; scoped endpoint статуса удаления.", "A. Все перечисленные модули переиспользованы (`apps/photographer/src/`). B. Onboarding работает у пользователя без tenant. C. Статус удаления не зависит от уже невалидного access token. D. Native payment capability=false до отдельной квалификации."),
              ("UI-15", "Приёмка Paper и проверка платформ", "Приёмка Paper", "Тесты на фактических Paper-экранах, web desktop, узкий mobile web, native target.", "A. DOM-тесты запускают настоящие экраны. B. `make test-web` PASS. C. Hosted Checkout на native квалифицируется отдельно и не считается пройденным по web e2e.")]
    for sid, title, head, out, crit in shared:
        U.append(["### %s — %s" % (sid, title), "", "- Вход: раздел «%s» из 06 ниже; образцы — `apps/photographer/src/`." % head, "- Выход: %s" % out, "- Тесты (в этой же задаче, R33): dom-тесты в `apps/photographer/src/`; helper — `TST-04`.", "- Критерий: %s" % crit, "- Запреты: R13, R7.", "- Из 06:", ""] + section(d6, head) + [""])
    ui_names = pack("ui", "Paper UI (BILL-10)", U); ui_count = len(U)
    # ---------------- e2e ----------------
    E = []; seen = 0
    pat = re.compile(r"^\|\s*(E\d{2}) ([^|]+)\|([^|]*)\|([^|]*)\|\s*$")
    THEMES = [("provider", r"Stripe|webhook|Checkout|SCA|Test Clock|Portal|dispute|refund|invoice|provider|payment|paid|Async pay"), ("legal", r"erasure|delet|consent|legal|cookie|onboarding|privacy|PII"), ("ui", r"\bUI\b|Paper|screen|экран"), ("quota", r"quota|lease|ticket|overrun|allowance|bucket|slot|photo|egress|storage"), ("catalog", r"seed|catalog|migration|backfill|Trial|plan")]
    for src in (d8, d10):
        for ln in src.split("\n"):
            m = pat.match(ln.strip())
            if not m: continue
            eid, name, pos, neg = m.group(1), m.group(2).strip(), m.group(3).strip(), m.group(4).strip()
            txt = name + " " + pos + " " + neg
            theme = next((t for t, rx in THEMES if re.search(rx, txt, re.I)), "general")
            stripe = bool(re.search(r"Stripe|webhook|Checkout|SCA|Test Clock|Portal|dispute|payment|card|Async pay", txt, re.I))
            E.append(["### %s — %s" % (eid, name), "", "- Положительный: %s" % pos, "- Отрицательный: %s" % neg,
                      "- Тема (подсказка по ключевым словам): `%s`. Выход: функция `test_%s_*` в `tests/integration/test_billing_e2e_%s.py`; UI-часть — `apps/photographer/src/billing-e2e-%s.dom.test.tsx`. Образцы — `EXAMPLES-TESTS.md`." % (theme, eid.lower(), theme, theme),
                      "- Описание теста: сначала docstring по шаблону из `TESTING.md` (модули, предусловия, положительный и отрицательный Дано/Когда/Тогда, инварианты), затем код; описание и тест — в одном коммите (R33).",
                      "- Критерий: оба сценария (положительный и отрицательный) обязательны, один без другого не принимается (08). Гонки и recovery — отдельными тестами." + (" Stripe-зависимо: сначала на fake-провайдере (`STR-01`), затем повтор на sandbox после ключей (`OPEN-14`, `STR-14`)." if stripe else ""),
                      "- Блокеры: функциональность из групп бэклога (`finance`, `provider`, `usage`, `legal`, `api`, `stripe`, `ui`) — сценарий можно начинать, когда соответствующие задачи закрыты.", ""])
    e2e_names = pack("e2e", "e2e E01–E90 (разделы 08 и 10)", E); e2e_count = len(E)
    idx = [("stripe", "BILL-07/08/09", "—", "%d задач" % str_count, str_names), ("ui", "BILL-10", "—", "%d задач" % ui_count, ui_names), ("e2e", "08, 10", "—", "%d сценариев" % e2e_count, e2e_names)]
    return files, idx, dict(stripe=str_count, ui=ui_count, e2e=e2e_count)
