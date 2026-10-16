# syntax=docker/dockerfile:1
FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
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

FROM alpine:3.23@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0
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
