# syntax=docker/dockerfile:1.7
ARG NODE_BUILD_IMAGE
ARG STATIC_IMAGE
FROM ${NODE_BUILD_IMAGE} AS build
WORKDIR /src
COPY packages/api-client/package.json ./packages/api-client/
COPY apps/photographer/package.json apps/photographer/package-lock.json ./apps/photographer/
RUN npm --prefix packages/api-client ci && npm --prefix apps/photographer ci
COPY packages/api-client ./packages/api-client
COPY apps/photographer ./apps/photographer
RUN npm --prefix apps/photographer exec expo export -- --platform web --output-dir dist

FROM ${STATIC_IMAGE}
COPY --from=build --chown=10001:10001 /src/apps/photographer/dist /srv
USER 10001:10001
CMD ["caddy","file-server","--listen",":8080","--root","/srv"]
