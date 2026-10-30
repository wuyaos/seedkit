# seedkit

Single-binary helper for private-tracker uploads on seedboxes: file manager,
MediaInfo / BDInfo, screenshots with image-host upload, torrent creation and
BBCode templating. Go backend with an embedded Vue 3 UI; only external
dependency is ffmpeg/ffprobe for screenshots.

## Run

```sh
# binary
SEEDKIT_DATA=./data ./seedkit serve        # http://localhost:8080
./seedkit doctor                            # show detected ffmpeg, config path

# docker
docker compose -f build/docker-compose.example.yml up -d
```

First visit creates the admin account. Media roots, image hosts and torrent
defaults are set in Settings or in `$SEEDKIT_DATA/config.yaml`; every key has
an env override `SEEDKIT_<SECTION>_<KEY>`.

## Build

```sh
make build       # frontend (npm) + backend
make release     # cross-compile into dist/
make docker
```

## Layout

```
cmd/seedkit/      entrypoint (serve | doctor | version)
internal/
  config/         yaml + env config
  auth/           bcrypt login, cookie sessions, rate limit
  task/           queue, SSE events, persistence
  fsroot/         os.Root sandboxed file access
  tools/          ffmpeg/ffprobe discovery
  media/          mediainfo (go-mediainfo), bdinfo (go-bdinfo)
  screenshot/     frame picking, subtitles, HDR tonemap, PNG quantize
  upload/         image hosts (picgo, pixhost)
  torrent/        mkbrr wrapper
  bbcode/         templates
  server/         HTTP handlers
web/              Vue 3 + Vite + Naive UI, embedded via web/embed.go
build/            Dockerfile, compose example
scripts/          release cross-compile
```
