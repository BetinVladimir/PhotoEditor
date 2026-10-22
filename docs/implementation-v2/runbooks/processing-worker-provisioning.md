# Отключённая регистрация CPU/media/archive worker

Operator SQL: `deploy/provision-processing-worker.sql`. Исполняется DBA через psql, не worker LOGIN. Backend всегда `cpu`, resource_class только `cpu`, `media`, `archive`. Существующие identities не перезаписываются. Скрипт не создаёт роли, ключи, сертификаты или qualification.

Обязательные psql variables: `tenant_id`, `worker_id`, `node_id`, `db_principal`, `model_lock_sha`, `certificate_sha`, `capacity_policy_sha`, `ram_budget_bytes`, `scratch_budget_bytes`, `resource_class`. `certificate_sha` — SHA256 DER клиентского сертификата; worker_id должен соответствовать CN и отдельному LOGIN. Сам пароль задаётся отдельным защищённым способом и не входит в SQL variables.

До вызова установите точную canonical capacity policy и создайте ограниченный LOGIN, имеющий только допустимое членство photo_worker. Скрипт проверяет существование LOGIN, отсутствие superuser/BYPASSRLS/CREATEROLE/CREATEDB/REPLICATION, опасных наследуемых ролей, владения объектами/SECURITY DEFINER и CREATE в photo. RAM должен совпадать с control_ram_budget_bytes для cpu, photo_ram_budget_bytes для media/archive; VRAM node равен0. Node identity должна быть новой.

После COMMIT registration и node остаются disabled; qualified_operations пуст, slots отсутствуют, doctor report отсутствует. Heartbeat не должен превращать эту заготовку в dispatchable worker. Это НЕ завершённое развёртывание.

Следующий отдельный этап: проверить actual target-host operations и их provenance/quality requirements, выпустить и доставить индивидуальные mTLS/queue/database credentials, проверить суммарный ресурсный бюджет процессов на физическом сервере, затем выполнить reviewed `deploy/activate-processing-worker.sql`. Готовность model doctor сама по себе не означает quality qualification. Не копируйте GPU activation script с заменой класса: его правила и slots рассчитаны на CUDA.

Проверки: `tests/integration/test_processing_worker_provisioning.py`, actual PostgreSQL receipt `work/processing-worker-provisioning-live-r2/result.json`:11 PASS, source unchanged, outer force rollback per case. R1 сохранён: его autocommit fixture не соответствовал контракту capacity installation и не является ошибкой production SQL. В live стенде CPU/media/archive этим прогоном не создавались и не запускались.


## Initial activation

`deploy/activate-processing-worker.sql` принимает те же identity/pin variables (без ram/scratch arguments), плюс `resource_class`, `doctor_report_jcs`, `doctor_report_file_sha`, `qualified_operations_json`. Report должен быть получен на целевом хосте и независимо рассмотрен; SHA относится к исходным UTF-8 bytes. Backend report — cpu. Allowlist — JSON object operation→["cpu"],1..128 зарегистрированных version1 операций именно выбранного класса. Для model-dependent operation требуется passed doctor entry и lock_backend_status passed; это не заменяет прикладную quality qualification.

Скрипт допускает только initial disabled registration/node без slots, doctor pin, drain state или другого worker на том же node. Проверяет неизменность LOGIN privileges, class/backend, model/certificate/capacity policy и node RAM относительно policy. CPU получает cpu_concurrency slots; media/archive — один slot. Available RAM/VRAM/scratch обнуляются, reported_at=NULL, last_seen=epoch, reported policy pin очищен. Только последующий настоящий heartbeat может предоставить измеренную ёмкость. Повторную активацию running/drained/shared node этим скриптом делать нельзя.

До активации требуется отдельно проверить суммарный RAM/scratch бюджет всех процессов на одном физическом сервере; новые node IDs не создают новую физическую память. Синтетические reports из tests запрещено использовать для реального развёртывания.

Actual PostgreSQL integration: `tests/integration/test_processing_worker_activation.py`,14 PASS, receipt `work/processing-worker-activation-live-r1/result.json`. Проверены3класса/числоslots/staleavailability,7несовпаденийscope,3privilegedrolesafterprovision иRAMdrift. Outer rollback каждого case; no live activation. Tests используют синтетический relational report исключительно для проверки authority.


Для текущего32GiBRTXстенда подготовлена отдельная `configs/capacity-rtx3060-media.json` сmediaRAM4GiB. Общаяpolicy остаётсядляCPU8/GPU16. Нацелевомmediaenv должна бытьименноэтаpolicy, еёSHA иnodeRAM должнысовпадатьвDB. Сумма28GiB —план, неkernelreservation: доодновременногозапуска обеспечитьhardprocesslimits иучестьостальныепроцессыхоста.
