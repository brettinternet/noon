# syntax=docker/dockerfile:1
FROM golang:1.27.3-alpine@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.buildVersion=${VERSION} -X main.buildCommit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /out/noon ./cmd/noon

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 noon \
    && mkdir -p /workspace /state \
    && chown noon:noon /workspace /state
COPY --from=build /out/noon /usr/local/bin/noon
ENV NOON_STATE_DIR=/state
USER noon
WORKDIR /workspace
ENTRYPOINT ["noon"]
CMD []
