FROM docker.io/library/golang:1.25.6-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# .dockerignore excludes .git, so the toolchain cannot stamp the revision by
# itself. Pass it in: docker compose build --build-arg VERSION=$(git rev-parse --short HEAD)
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X github.com/cesargomez89/navidrums/internal/http.buildVersion=${VERSION}" \
    -o navidrums ./cmd/server

FROM docker.io/library/alpine:3.19

RUN apk --no-cache add ca-certificates tzdata ffmpeg
WORKDIR /app

COPY --from=builder /app/navidrums .

RUN mkdir /data && chmod 777 /data

ENV DB_PATH=/data/navidrums.db
ENV DOWNLOADS_DIR=/music

EXPOSE 8080
VOLUME ["/data", "/music"]

CMD ["./navidrums"]
