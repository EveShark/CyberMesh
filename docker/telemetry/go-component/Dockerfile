# syntax=docker/dockerfile:1

FROM mirror.gcr.io/library/golang:1.25 AS builder

ARG MODULE_DIR
ARG CMD_PATH
ARG BIN_NAME
ARG BUILD_TAGS=

WORKDIR /src

COPY telemetry-layer ./telemetry-layer

WORKDIR /src/telemetry-layer/${MODULE_DIR}

RUN go mod download

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64
RUN go build \
    -trimpath \
    -ldflags="-s -w" \
    -tags="${BUILD_TAGS}" \
    -o /out/${BIN_NAME} \
    ${CMD_PATH}

FROM gcr.io/distroless/static-debian12:nonroot

ARG BIN_NAME

WORKDIR /app
COPY --from=builder /out/${BIN_NAME} /app/telemetry-component

USER nonroot:nonroot

ENTRYPOINT ["/app/telemetry-component"]
