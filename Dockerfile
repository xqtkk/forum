ARG GO_VERSION=1.27.3
FROM golang:${GO_VERSION}-alpine AS build
RUN apk add --no-cache build-base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /forum ./cmd && CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /forum-manage ./cmd/manage

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -g 10001 forum && adduser -D -u 10001 -G forum forum
WORKDIR /app
COPY --from=build /forum /app/forum
COPY --from=build /forum-manage /app/forum-manage
COPY static /app/static
RUN mkdir -p /data /app/uploads /app/.local && chown -R forum:forum /data /app/uploads /app/.local
USER forum
ENV FORUM_ADDR=0.0.0.0:4000 FORUM_DB_PATH=/data/forum.db FORUM_BACKUP_DIR=/data/backups
EXPOSE 4000
CMD ["/app/forum"]
