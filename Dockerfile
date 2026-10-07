# syntax=docker/dockerfile:1

# 1. Frontend: Kotlin/JS -> web/build/dist/js/productionExecutable
FROM gradle:8.14.3-jdk21 AS web
WORKDIR /src/web
COPY web/ ./
RUN gradle --no-daemon -q jsBrowserDistribution

# 2. Backend: static Go binaries
FROM golang:1.26 AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

# 3. Runtime: one small image, run as "api" (default) or "worker"
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/api /out/worker /app/
COPY --from=web /src/web/build/dist/js/productionExecutable /app/web
ENV WEB_DIR=/app/web PORT=8080
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/app/api"]
