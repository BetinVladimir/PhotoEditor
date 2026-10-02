# Local HTTP-only integration runtime for the already source-pinned static binary.
# Production uses deploy/images/minio/Dockerfile and separate qualification.
FROM scratch
COPY minio-server /minio
COPY LICENSE /licenses/minio/LICENSE
COPY source-manifest.json /licenses/minio/source-manifest.json
LABEL photoeditor.scope="development" photoeditor.purpose="isolated-objectstore-integration"
ENTRYPOINT ["/minio"]
CMD ["server", "/data"]
