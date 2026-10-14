# V28: измерение local CPU preparation

`tools/benchmark_v28_local.py` выполняет существующий `prepare_local` для `portrait.hair` или `portrait.clothing`. Алгоритмы обработки не заменяются. Измерение не создаёт trusted qualification, не включает операции и не заменяет human QA или полный worker/adoption цикл.

## Синтетический прогон

Из корня репозитория:

```sh
PYTHONPATH=workers .venv-platform/bin/python tools/benchmark_v28_local.py --operation portrait.hair --width 3000 --height 2000
PYTHONPATH=workers .venv-platform/bin/python tools/benchmark_v28_local.py --operation portrait.clothing --width 3000 --height 2000
```

`--pattern waves` включает защищённую полосу и область с нулевой маской. `constant` предназначен для отдельного диагностического прогона; при нулевом числе защищённых/отключённых пикселей соответствующий инвариант не считается проверенным на корпусе.

## Подготовленный корпус

Каждый случай — один manifest JSON и семь файлов внутри репозитория. Пути считаются от корня репозитория; абсолютные пути разрешены только внутри него. Ссылки за пределы корня отклоняются. Manifest ограничен 64 KiB; SHA manifest передаётся независимо через CLI.

```json
{
  "schema_version": 1,
  "operation": "portrait.hair",
  "working_image": {"path": "work/corpus/case/image.json", "sha256": "<64 hex>", "artifact_id": "<UUID>"},
  "params": {"path": "work/corpus/case/params.json", "sha256": "<64 hex>", "artifact_id": "<UUID>"},
  "target_ref": {"path": "work/corpus/case/target.json", "sha256": "<64 hex>", "artifact_id": "<UUID>"},
  "protected_ref": {"path": "work/corpus/case/protected.json", "sha256": "<64 hex>", "artifact_id": "<UUID>"},
  "rgb": {"path": "work/corpus/case/rgb.npy", "sha256": "<64 hex>", "artifact_id": "<UUID>"},
  "target": {"path": "work/corpus/case/target.npy", "sha256": "<64 hex>", "artifact_id": "<UUID>"},
  "protected": {"path": "work/corpus/case/protected.npy", "sha256": "<64 hex>", "artifact_id": "<UUID>"}
}
```

Значения `<...>` заменяются фактическими pins. `working_image` содержит действующий WorkingImage float32 без alpha. `params` содержит соответствующий PortraitHairParams/PortraitClothingParams. `target_ref` и `protected_ref` — MaskRef с `protected=false/true`. Массивы — little-endian float32 NPY: RGB `(H,W,3)`, маски `(H,W)` со значениями `[0,1]`. Не передавайте JPEG непосредственно: необходимы подготовленные working pixels и правильный color contract. Текущий инструмент не измеряет alpha-путь.

ICC identity WorkingImage дополнительно сверяется с фактическими bytes/SHA профиля из `resources/icc/profiles.lock.json` через действующий ICCRegistry. Это проверка identity уже подготовленных working pixels; инструмент не выполняет JPEG→working color conversion и не доказывает правильность предшествующего преобразования.

Проверяются source ref SHA/ID и canvas hash в Params, pixels ref в WorkingImage, ссылки MaskRef в Params, ссылки массивов, canvas и transform chain в MaskRef. Документы и массивы сначала копируются с проверкой SHA в отдельный снимок внутри репозитория; обработка читает только снимок. NPY загружается без pickle, read-only mmap. Временный снимок и выходы удаляются после прогона; исходники не изменяются.

```sh
PYTHONPATH=workers .venv-platform/bin/python tools/benchmark_v28_local.py --operation portrait.hair --fixture work/corpus/case/manifest.json --fixture-sha256 ACTUAL_MANIFEST_SHA
```

## Результат

JSON stdout содержит manifest SHA, операцию, размеры, время подготовки, process peak RSS, ресурсную оценку, SHA сырых выходных float32 пикселей и счётчики инвариантов. Обнаружение nonfinite output, изменения защищённых/отключённых пикселей или несоответствия changed_mask завершает прогон ошибкой до вывода успешного JSON.

`seconds` включает подготовку и проверку scratch reservation, исключает последующую проверку выходов. `process_peak_rss_bytes` — максимум всего процесса, включая импорты, подготовку входов и верификацию; это не отдельный peak только алгоритма. Выходной digest относится к raw RGB bytes в row-major порядке, а не к NPY-файлу.

Для квалификации нужны отдельные реальные corpus/human approval, deployment runtime/device/model/dataset pins, повторы и распределение времени, полный IO/worker/adoption цикл и существующий trusted importer. `production_acceptance` всегда false. Успешные технические инварианты не доказывают визуальную приемлемость результата.
