# syntax=docker/dockerfile:1
ARG VERSION=dev

FROM golang:1.27-alpine AS go
ARG VERSION
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /seedkit ./cmd/seedkit

# ffmpeg comes from the alpine package; the binary itself has no other deps.
FROM alpine:3.21
RUN apk add --no-cache ffmpeg ca-certificates tzdata font-wqy-zenhei
COPY --from=go /seedkit /usr/local/bin/seedkit
LABEL org.opencontainers.image.source="https://github.com/wuyaos/seedkit" \
      org.opencontainers.image.licenses="GPL-3.0-only"
COPY LICENSE THIRD_PARTY_NOTICES.md /usr/share/seedkit/
COPY licenses/ /usr/share/seedkit/licenses/
ENV SEEDKIT_DATA=/data SEEDKIT_SERVER_LISTEN=:8080
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["seedkit"]
CMD ["serve"]
