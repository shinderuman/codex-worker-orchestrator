ARG GO_VERSION
FROM golang:${GO_VERSION}-trixie
ARG LINT_GO_VERSION
RUN apt-get update \
    && apt-get install -y --no-install-recommends git python3 sqlite3 curl ca-certificates xz-utils \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --shell /bin/bash worker \
    && GOTOOLCHAIN=go${LINT_GO_VERSION} go version \
    && mkdir -p /home/worker/.cache/go-build
COPY quality-tools.yml install-quality-tools.sh /tmp/quality-tools/
RUN HOME=/home/worker GOCACHE=/tmp/quality-build-cache sh /tmp/quality-tools/install-quality-tools.sh \
    && chown -R worker:worker /home/worker /go \
    && rm -rf /tmp/quality-tools /tmp/quality-build-cache
WORKDIR /workspace
CMD ["sleep", "infinity"]
