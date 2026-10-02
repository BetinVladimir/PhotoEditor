# syntax=docker/dockerfile:1.7
ARG GO_BUILD_IMAGE
ARG RUNTIME_IMAGE
FROM ${GO_BUILD_IMAGE} AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./backend/
RUN --mount=type=cache,target=/go/pkg/mod go -C backend mod download
COPY backend ./backend
RUN --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 go -C backend build -trimpath -ldflags="-s -w" -o /out/photo ./cmd/photo \
 && CGO_ENABLED=0 go -C backend build -trimpath -ldflags="-s -w" -o /out/photo-healthcheck ./cmd/photo-healthcheck \
 && CGO_ENABLED=0 go -C backend build -trimpath -ldflags="-s -w" -o /out/photo-v2-qualifications ./cmd/photo-v2-qualifications

FROM ${RUNTIME_IMAGE}
RUN addgroup -S photo && adduser -S -G photo -u 10001 photo
COPY --from=build --chown=photo:photo /out/photo /usr/local/bin/photo
COPY --from=build --chown=photo:photo /out/photo-healthcheck /usr/local/bin/photo-healthcheck
COPY --from=build --chown=photo:photo /out/photo-v2-qualifications /usr/local/bin/photo-v2-qualifications
COPY --chown=photo:photo resources/icc /opt/photo/icc
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/photo"]
