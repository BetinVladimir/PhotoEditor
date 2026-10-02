# syntax=docker/dockerfile:1.7
ARG UV_IMAGE
ARG PYTHON_BASE_IMAGE
FROM ${UV_IMAGE} AS uv
FROM ${PYTHON_BASE_IMAGE}
RUN addgroup --system photo && adduser --system --uid 10001 --ingroup photo photo
COPY --from=uv /uv /uvx /bin/
WORKDIR /opt/photo
COPY workers/pyproject.toml workers/uv.lock ./workers/
COPY workers/photo_worker ./workers/photo_worker
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen --no-dev --extra ml --extra media --project workers
COPY resources/icc ./resources/icc
ENV PATH="/opt/photo/workers/.venv/bin:${PATH}" PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1
USER 10001:10001
ENTRYPOINT ["photo-worker"]
CMD ["run"]
