# 00. Аудит проекта и стратегия переноса

## Зафиксированное состояние

В репозитории есть 9 Python-модулей `src/photoeditor`, один файл из 22 тестов, две YAML-конфигурации, схема legacy-конфига, shell setup/run, CodeFormer script, шесть JPEG fixtures и шесть Markdown-документов плюс README. `docs/implementation/` на момент анализа пуст. Собственные исходники и документы зафиксированы SHA-256 в `contracts/source-inventory.json`; окружения, веса и кэши считаются установленными внешними/генерируемыми ресурсами, а не исходниками продукта. Git содержит изменённые и неотслеживаемые файлы; документация не предполагает чистый checkout и не предписывает reset/clean.

Повторная проверка 20.09.2026: `PYTHONDONTWRITEBYTECODE=1 .venv/bin/python -m pytest -q -p no:cacheprovider` — **22 passed**. Это подтверждает существующие тесты CLI, не выполнение ТЗ 4.1. Полный рендер, реальный CodeFormer inference, CPU/MPS/CUDA benchmark и визуальное утверждение в рамках аудита не выполнялись. Старые числа времени/качества в `docs/implementation.md` — история автора, не новые замеры.

## Карта исходников и решений

| Источник / символы | Факт | Целевое действие |
|---|---|---|
| `config.py`: Config, load, for_image, env_paths | strict legacy YAML schema integer 1, per-file overrides, запрет aliases | оставить legacy; новые Pydantic StyleV2/JobV1/OverrideV1 отдельно; integer 1 не равен style 1.0.0 |
| `io.py`: digest, atomic_json | потоковый SHA, local replace | переносить после выделения pure helpers; local replace не заменяет S3+SQL commit |
| `io.py`: read_rgb, write_tiff, write_jpeg | Pillow/tifffile, sRGB, uint16 TIFF; полный буфер | новый libvips/LittleCMS/RAW слой; не переносить ICC detection по имени профиля |
| `analysis.py`: Analyzer.analyze | MediaPipe FaceLandmarker + SelfieMulticlass CPU; 478 точек, карты proxy | заменить обязательными SFD/FAN68/LR-ASPP и остальными адаптерами; типы legacy landmarks несовместимы |
| `jobs.py`: scan, make_plan, ensure_plan | один каталог, метеринг и frozen EV, JSON/NPZ cache | importer + ingest DAG + immutable snapshots; это не analyze пар |
| `jobs.py`: job_lock, run_batch | fcntl одного output, последовательные кадры, local resume | заменить DB leases/fencing/outbox, планировщик Go; fcntl не координирует кластер |
| `render.py`: sample_map | глобальные pixel-center координаты | переносить идею и тесты в TileReader, уточнить transform contract |
| `render.py`: process_tile/prepare_maps | эвристическая коррекция света/кожи и WB | только материал для явных операторов; fixed ColorProgram не включает эти эвристики автоматически |
| `render.py`: geometry_field | горизонтальный dx; body boxes; масштабирование градиента | переписать BodyWarp 2D, pose/silhouette, det(J), фон, единый remap |
| `render.py`: render | держит full input/output/warp; circles через uint8 | memmap/region IO; Telea float32 поканально; geometry до color |
| `jobs.py`: restore_soft_faces/cleanup_all_face_highlights | постобработка JPEG отдельно от master | legacy-only, в renderer_v2 нет скрытого отдельного JPEG-тракта |
| `render.py`: post_white_balance | действует на весь кадр | вопреки общему описанию «внутри skin-mask» в истории: нельзя считать локальной кожной функцией |
| `scripts/codeformer_arcface_gate.py` | генеративный face restore, ArcFace gate | сохранить как отдельный legacy experiment; не обязательная модель нового ТЗ |
| `scripts/setup-codeformer.sh` | clone без commit, pip без lock, patch site-packages | запретить в новой поставке; не выдавать за reproducible installer |
| `models.py`: ensure_models | 2 MediaPipe веса с SHA, скачивание setup-only | сохранить подход; новый полный models.lock, offline inference |
| `studio.py`: Studio, QProcess | YAML редактор, proxy, cancel; нет полноценного mask UI | legacy остаётся; продуктовые клиенты Expo и Next.js новые |
| `cli.py` | argparse analyze=plan, render, preview | legacy CLI отдельно; новый Typer — клиент Go + doctor/benchmark/migrate |
| `tests/test_pipeline.py` | unit/local batch tests; mocks анализа в части сценариев | сохранить regression suite; добавить сервисные/численные/fault tests |

## Разрывы, влияющие на проектирование

* Нет Go, PostgreSQL/RLS, RabbitMQ, MinIO, OTP/SMTP, tenancy, HTTP, Expo/Next.js, контейнеров и CI целевого продукта.
* Нет парного fit, ColorProgram LUT, StyleKNN и переносимого StyleV2. `make_plan` не переименовывать в новый analyze.
* Legacy default принимает отсутствие ICC как sRGB; целевой ingest требует явный assume_profile. Legacy `read_rgb` допускает профилированный CMYK, целевой продукт его отклоняет.
* Master и finished JPEG могут иметь разные изменения. В новой системе derivatives одного version обязаны представлять одну принятую правку.
* Legacy `publish_delivery` копирует даже needs_review в output. Это не серверная публикация и не может стать её реализацией.
* `test_tiles_match_full_reference` допускает 7 уровней uint16; новая приёмка — 2. Нельзя переносить старый допуск.
* Устаревшие masks после warp недопустимы для следующих стадий. Новый transform chain — часть каждого артефакта.
* Утверждённых golden references нет; 6 увеличенных JPEG не заменяют native 45 MP, реальные пары и закрытый test.

## Перенос без потери данных

Сохранить существующий пакет и скрипты рабочими до P17. Новые директории `backend`, `workers`, `apps`, `packages`, `deploy` независимы. Не выполнять автообновление `.venv`/`.models` и не удалять старые output.

Importer создаёт исходники и отдельные draft коллекции legacy-результатов по проверенным manifest/hash, связывает parent только при доказанной связи. Master/delivery/finished с разными правками импортируются разными asset/version, а не derivatives. `visual_status=not_reviewed` остаётся pending_review. Нет связи — `legacy_unlinked`, без выдуманного parent.

Legacy конфигурации сохраняются как provenance JSON, не преобразуются автоматически в ColorProgram: функции и цветовые пространства различны. Для переноса look предложить явные before/after пары через новый analyze. Подробности миграции — 02.
