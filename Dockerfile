FROM node:20-alpine AS frontend
WORKDIR /fe
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install
COPY frontend/ ./
RUN npm run build   # outputs to /web/static (see vite.config.ts outDir)

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
COPY --from=frontend /web/static ./web/static
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/cachenova ./cmd/cachenova

FROM alpine:3.20
RUN adduser -D -u 10001 app && mkdir -p /data && chown app /data
WORKDIR /app
COPY --from=build /out/cachenova /app/cachenova
COPY --from=build /src/web /app/web
USER app
VOLUME ["/data"]
EXPOSE 6380 8080
ENTRYPOINT ["/app/cachenova", "--resp", ":6380", "--http", ":8080", "--aof", "/data/cache.aof"]
