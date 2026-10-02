FROM quay.io/minio/minio@sha256:14cea493d9a34af32f524e538b8346cf79f3321eff8e708c1e2960462bd8936e
COPY minio-server /usr/bin/minio
COPY LICENSE /usr/share/licenses/photoeditor-minio/LICENSE
COPY source-manifest.json /usr/share/licenses/photoeditor-minio/source-manifest.json
LABEL org.opencontainers.image.source="https://github.com/minio/minio" \
      org.opencontainers.image.revision="9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a" \
      org.opencontainers.image.licenses="AGPL-3.0"
ENTRYPOINT ["/usr/bin/minio"]
CMD ["server", "/data", "--console-address", ":9001"]
