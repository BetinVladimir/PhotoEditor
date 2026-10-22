# Локальный пакетный редактор

Python пакет `photoeditor`: обработка фотографий, CLI и настольный интерфейс PySide6. Код находится в `src/photoeditor`, зависимости — в `requirements.lock`, упаковка — в `pyproject.toml` этого каталога.

Из корня репозитория: `./run.sh --help`, `./run.sh studio`, `./run-e2e.sh`. Установка: `./scripts/setup.sh` (с `--studio` для PySide6). Модели, конфиги и фотографии продолжают использовать общие репозиторные каталоги; обработка и настройки качества сохранены. Для установленного CLI: `uv pip install --python .venv/bin/python --no-deps -e apps/local-editor`.

Корневой `pyproject.toml` задаёт общий pytest scope для приложений и workers, а не упаковку локального редактора. Перенос не создаёт новый облачный сервис.
