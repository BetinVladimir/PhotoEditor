# syntax=docker/dockerfile:1.7
ARG NODE_BUILD_IMAGE
ARG NODE_RUNTIME_IMAGE
FROM ${NODE_BUILD_IMAGE} AS build
WORKDIR /src
COPY packages/api-client/package.json ./packages/api-client/
COPY apps/portal/package.json apps/portal/package-lock.json ./apps/portal/
RUN npm --prefix packages/api-client ci && npm --prefix apps/portal ci
COPY packages/api-client ./packages/api-client
COPY apps/portal ./apps/portal
RUN mkdir -p apps/portal/public
RUN npm --prefix apps/portal run build

FROM ${NODE_RUNTIME_IMAGE}
ENV NODE_ENV=production HOSTNAME=0.0.0.0 PORT=3000
WORKDIR /app
COPY --from=build --chown=10001:10001 /src/apps/portal/.next/standalone ./
COPY --from=build --chown=10001:10001 /src/apps/portal/.next/static ./apps/portal/.next/static
COPY --from=build --chown=10001:10001 /src/apps/portal/public ./apps/portal/public
USER 10001:10001
CMD ["node","apps/portal/server.js"]
