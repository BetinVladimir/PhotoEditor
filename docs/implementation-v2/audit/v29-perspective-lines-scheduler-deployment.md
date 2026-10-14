# V29: scheduler recovery детектора линий

Статус: код и unit tests написаны; tests/build/typecheck НЕ запускались. Миграции01127–01128 НЕ применены. Operation registry остаётся disabled. Этот документ не является evidence запуска или приёмки.

## Конфигурация

- `PHOTO_SCHEDULER_PERSPECTIVE_LINES_ENABLED`: unset/`false` выключает цикл; только`true` включает. Другие значения останавливают startup с ошибкой.
- `PHOTO_SCHEDULER_PERSPECTIVE_LINES_RUNTIME_SHA`: обязательный lowercase SHA256 независимого runtime lock детектора, совпадающий с worker/adopter installation. Это deployment pin, не qualification качества.
- `PHOTO_SCHEDULER_PERSPECTIVE_LINES_TIMEOUT_SECONDS`:1..300, default300; включает загрузку recovery descriptor, source/NPY IO и activation.
- Working ICC берётся из существующего `PHOTO_ICC_ROOT` через LoadWorkSHA. Policy включает CPU/float32/v2.2/max200MP/proxy1600/max128lines.

## Жизненный цикл

RunScheduler использует тот же photo_scheduler pool: existing source/array readers V29 требуют именно эту роль. Дополнительный API-pool не создаётся. Startup до loop проверяет текущую роль, наличие, SECURITY DEFINER, owner photo_fn_owner и EXECUTE пяти нужных функций. Timeout startup10sec. Функции01127/01128 должны быть установлены заранее; этот код не применяет миграции.

Цикл использует RunExpansionLoop, idle/backoff5sec, один candidate на ProcessOne, keyset cursor по run UUID. Ошибка candidate продвигает cursor; пустая страница сбрасывает cursor. Выборка исключает disabled registry, cancelled/terminal/неактивных tenants, незапечатанные drafts. Это read-only discovery, не lease. Несколько scheduler instances могут повторить IO; existing01121 locked activation обеспечивает idempotency/outbox semantics.

ResumeSealed строго реконструирует сохранённый intent/task/source/identity и сравнивает saved execution policy с текущей. Затем заново проверяет байты исходника и RGB/alpha, и вызывает tenant-scoped activation с транзакционной SQL authority. Исторический receipt не заменяет fresh authorization. Уже активированный graph получает существующий acknowledgement без повторного dispatch.

Readiness учитывает состояние recovery loop только когда он включён. Ошибка снимает Ready, следующий успешный проход восстанавливает. Shutdown отменяет и ожидает loop до закрытия общего pool. Registry activation, submission и terminal lifecycle требуют отдельного завершения; этот цикл их не подменяет.

## Terminal lifecycle

При включённом scheduler lines flag запускается также PerspectiveLinesLifecycle. Startup требует01129 finalize/release функции. Successful producer завершается только при единственной succeeded task и совпадении durable adoption receipt/inbox/event; unsupported является завершённым результатом детекции. Отдельный pass освобождает reservation только для terminal run после settlement tasks/output uploads/backend documents через01106. Lifecycle loop имеет собственный Ready; shutdown отменяет и ожидает его. Failed/cancelled run transitions и abandoned drafts требуют отдельного завершения и приёмки.01129 не применялась, unit tests не запускались.
