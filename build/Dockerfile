# syntax=docker/dockerfile:1
ARG VERSION=dev

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS go
ARG VERSION
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /seedkit ./cmd/seedkit

# ffmpeg comes from the alpine package; the binary itself has no other deps.
FROM alpine:3.21
RUN apk add --no-cache ffmpeg ca-certificates tzdata
COPY --from=go /seedkit /usr/local/bin/seedkit
ENV SEEDKIT_DATA=/data SEEDKIT_SERVER_LISTEN=:8080
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["seedkit"]
CMD ["serve"]
