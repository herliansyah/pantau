FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=${VERSION}" -o pantau .

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/pantau /app/pantau
VOLUME ["/app/data"]
EXPOSE 8080
ENTRYPOINT ["/app/pantau", "-db", "/app/data/pantau.db", "-port", "8080"]
