# Изменение RAM существующих worker без изменения качества

Операторская процедура: `deploy/reconfigure-worker-capacity.sql`. Это не миграция и не автоматическое разрешение применить изменения к работающему серверу. Скрипт не создаёт роли или SECURITY DEFINER functions и требует существующий DBA/operator доступ. Worker LOGIN его исполнять не должен. Исходные reports, hashes, snapshots и receipts сохраняются.

## Постоянное распределение для RTX3060 / 32 GiB

Кандидат: CPU 8 GiB + media 8 GiB + GPU host RAM 12 GiB = 28 GiB. Для измеренного MemTotal 33,514,270,720 bytes остаётся 3,449,499,648 bytes ОС. Это распределение верхних границ процессов, а не обещание kernel-reservation памяти. CUDA VRAM остаётся 10,000,000,000 bytes; host RAM и VRAM — разные ресурсы. Scratch также проверяется отдельно, включая общую файловую систему и RAM, занятую tmpfs/page cache.

`configs/capacity-rtx3060-gpu-12g.json` и `configs/capacity-rtx3060-media-8g.json` — prospective policies. Наличие файлов не означает установку или qualification. Базовые политики и прежние evidence не заменять. Прежде чем принять GPU 12 GiB, измерить настоящее выполнение `pair.fit.color`: 24 Mp source/target, 65,536 samples, обычные три инициализации и полные шаги, также reliable-mask local-fit; проверить численные результаты независимым reference и подтвердить неизменность algorithm/runtime pins. Сравнение того же полного fixture с прежними 16 GiB полезно при наличии такого actual результата; маленький прежний smoke не является таким сравнением. Analytic reference для 24 Mp доказывает только измеренную ветку, не произвольные 200 Mp, маски или alignment. Маленький CUDA optimizer smoke не доказывает соответствие 12 GiB. Собирать cgroup memory.peak/events и VRAM peaks. Все применяемые операции должны оставаться в реальном квалифицированном allowlist.

Для RAW текущий conservative estimate 7,473,741,824 bytes не уменьшается. Он требует media не менее 8 GiB и также распространяется на RAW inspect/decode CPU. После изменения проверить реальные RAW через очередь и gateway до decode и далее по пользовательскому workflow. Прогон с exit 0 при `waiting_resource` не является приёмкой.

## Две фазы и граница внешнего доказательства

SQL выполняет две отдельные транзакции `stage` и `resume`. Между ними worker остаётся disabled, doctor и capacity freshness сброшены, доступная ёмкость равна нулю. Никакие task state, fences, reservation history, сертификаты, роли, model/runtime files или qualified_operations не переписываются. Slots сохраняют идентичности и число. Изменение concurrency этой процедурой не поддерживается.

SQL не может доказать остановку systemd, отсутствие сообщений Rabbit, физическую память, соответствие cgroup и корректность фотографического результата. Перед каждой фазой оператор самостоятельно проверяет эти условия, сохраняет evidence и передаёт его hashes в `preflight`. Это явная operator attestation; она не превращает синтетический report в qualification. Свежесть review ограничена десятью минутами.

1. Остановить scheduler/publishers, способные создавать worker dispatch, и проверить очереди ready/unacknowledged=0. Drain соответствующие nodes штатным механизмом. Дождаться завершения leases и reserved reservations; истечение expires_at само по себе не означает освобождение. Ничего не обнулять вручную, чтобы обойти активную работу.
2. Остановить соответствующие worker. Подтвердить `MainPID=0` и terminal inactive/failed, не только nonzero `is-active`; проверить пустой cgroup. Известные CPU/media/GPU и посторонние процессы учесть в общей host inventory. Свободную RAM и диск измерить сейчас. Подготовить recovery launcher до остановки.
3. Отдельным reviewed операторским действием отключить registration и node и обнулить advertised availability. Сохранить receipt этого действия. После остановки получить полные текущие `to_jsonb(worker_registrations)`, `to_jsonb(resource_nodes)` и `jsonb_agg(to_jsonb(resource_slots) ORDER BY slot_name)`. В файле inputs сохранить именно эти значения, включая старые policy, budgets, doctor, certificate, model, principal, allowlist, drain_generation. SQL сравнивает полные строки, а не отдельный выбранный набор полей.
4. Установить новую policy через `install_capacity_policy_timing` с canonical `CapacityPolicy` и независимо проверить raw file SHA/canonical policy SHA. SQL требует уже зарегистрированную policy с совпадающими hash, timing, RAM/VRAM и прежним количеством slots. Политика не устанавливается этим SQL.
5. Выполнить `stage`. Одной транзакцией меняются node budgets и registration policy pin, очищаются doctor pin, last_seen/capacity и reported-policy timestamps. Enabled остаются false. Снять новые полные snapshots и сохранить stage receipt. Ни одна старая фотографическая квалификация не «переименовывается» в новую.
6. Доставить тот же проверенный canonical policy file и установить реальные process limits. GPU compose использует `PHOTO_RTX_RAM_LIMIT`; `mem_limit` и `memswap_limit` равны, поэтому дополнительного swap нет. Default остаётся прежним 16 GiB до проверки 12 GiB. Вместе задать `PHOTO_RTX_CAPACITY_POLICY_FILE` (абсолютный путь reviewed canonical policy) и `PHOTO_RTX_RAM_LIMIT` (host RAM bytes): для кандидата GPU12 это policy gpu-12g и 12884901888. Default policy остаётся capacity-rtx3060.json с прежними 16 GiB. Перед запуском сверить rendered compose, bind source SHA/canonical pin, policy photo_ram_budget_bytes, DB node RAM и эффективный cgroup memory.max; все четыре значения RAM должны совпадать. Несовпадение — запрет старта, не разрешение рекламировать 16 GiB при физическом cap 12. Для CPU/media также нужны hard limits 8 GiB и swap=0. Проверить **эффективные** kernel/cgroup настройки; одного значения policy недостаточно. Не запускать consuming worker до resume.
7. Получить свежий target-host doctor и reviewed resource/quality evidence на этой конфигурации. Сверить models, CUDA provider, runtime lock, certificate и hashes; модельный doctor не заменяет качество фотографий. Сформировать `resume` с новыми stage snapshots, теми же target policy/budgets и новым doctor.
8. Выполнить `resume`. SQL сохраняет прежний allowlist, model/cert/principal/slots и drain_generation, закрывает drain через очистку drain_started_at, устанавливает reviewed doctor pin и включает registration/node. Доступная ёмкость остаётся stale/zero.
9. Запустить worker с подготовленными hard limits; проверить устойчивый PID, отсутствие OOM, свежий authenticated heartbeat с **новым** policy pin и реальные advertised capacities. Только после этого запустить scheduler/dispatch. Старые ready/retry_wait tasks могут быть заново оценены штатным scheduler; queued/running, в том числе привязанные только через dispatch_worker_id, запрещают реконфигурацию. Никаких ручных task resets.

## Входной JSON

Один psql literal variable `capacity_change_json` содержит объект:

```json
{
  "phase": "stage",
  "resource_class": "gpu",
  "backend": "cuda",
  "expected_worker": {"...": "полная фактическая строка registration"},
  "expected_node": {"...": "полная фактическая строка node"},
  "expected_slots": [{"...": "все фактические slots по slot_name"}],
  "new_policy_sha": "64 hex canonical policy SHA",
  "new_ram_bytes": 12884901888,
  "new_vram_bytes": 10000000000,
  "new_scratch_bytes": 12884901888,
  "preflight": {
    "status": "OPERATOR_REVIEWED_STOPPED_HOST_CAPACITY",
    "reviewed_at": "ISO8601 UTC actual review time",
    "worker_id": "фактический worker UUID",
    "new_policy_sha": "тот же target policy SHA",
    "scheduler_stopped": true,
    "worker_stopped": true,
    "rabbit_empty": true,
    "physical_ram_bytes": 33514270720,
    "total_worker_caps_bytes": 30064771072,
    "os_reserve_bytes": 3221225472,
    "host_evidence_sha256": "64 hex actual host inventory receipt SHA",
    "resource_measurement_sha256": "64 hex actual bounded measurements receipt SHA",
    "runtime_lock_sha256": "64 hex actual runtime lock SHA"
  }
}
```

Для `resume` обновить snapshots и review time; добавить `doctor_report_jcs` — точные UTF-8 bytes отчёта как JSON string, `doctor_report_file_sha` — SHA этих bytes. В preflight добавить `resource_and_quality_review_passed: true` и `qualification_evidence_sha256` с hash независимого review. Backend report должен совпадать с worker; model-dependent operations требуют passed model и lock_backend_status. Ни одна новая операция не добавляется.

Не передавать файлы через неэкранированную shell интерполяцию и не логировать весь private input. Использовать literal parameter binding через existing операторский psql runner; tests показывают безопасное `psycopg.sql.Literal` для одного JSON значения. Сохранять отдельно input SHA и безопасный итоговый receipt. SQL source использует `ON_ERROR_STOP`, lock timeout 5 s и statement timeout 30 s.

## Атомарность, ошибка и восстановление

Каждая фаза полностью атомарна: при CAS mismatch, конфликте идентичности, active dispatch, неверном policy/report/host review вся транзакция откатывается. Не повторять с устаревшими snapshots и не ослаблять guard. Повторно сверить состояние и причину.

Если stage применён, а доставка/измерение/doctor/resume не завершены, узлы **остаются disabled**. Это безопасная незавершённая реконфигурация, не повод автоматически включить старый launcher: его policy и hard limits уже могут расходиться с DB. Восстановить согласованный target deployment и получить новые evidence/snapshots; затем resume. Для возврата к прежнему распределению нужен отдельный reviewed operator change с фактическими snapshots, прежней сохранённой policy, ресурсным обоснованием и свежим doctor; не использовать Down и не редактировать applied ledger. Для узла без doctor после stage этот SQL специально не допускает повторный stage: возврат требует отдельного рассматриваемого recovery, а не автоматического обхода.

При ошибке после resume повторно отключить admission/drain, штатно завершить работу, проверить фактические процессы и восстановить согласованность. Не запускать два GPU unit одновременно. При неизвестном состоянии диагностического контейнера сначала доказать его остановку; лишь затем восстанавливать GPU.

## Проверки

`tests/integration/test_worker_capacity_reconfiguration.py`: реальный SQL в изолированной БД, outer rollback per case. Проверяются stage/resume четырёх классов, неизменность authority/slots и отсутствие fabricated capacity, stale full-row CAS, live nodes/slots, внешние missing/stale evidence, наследуемые привилегии, doctor/quality отказ, pending dispatch-only и reserved-expired. Синтетические fixtures предназначены только для проверки полномочий SQL. Эти tests подготовлены; факт их успешного PostgreSQL прогона должен фиксироваться отдельным actual receipt. На основной БД процедура здесь не применялась.
