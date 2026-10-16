# syntax=docker/dockerfile:1
FROM golang:1.27.2-alpine AS build
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
    -o /out/bump ./cmd/bump

FROM alpine:3.23
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 bump \
    && mkdir -p /workspace /state \
    && chown bump:bump /workspace /state
COPY --from=build /out/bump /usr/local/bin/bump
ENV BUMP_STATE_DIR=/state
USER bump
WORKDIR /workspace
ENTRYPOINT ["bump"]
CMD []
