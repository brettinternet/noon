# syntax=docker/dockerfile:1
FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
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

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
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
