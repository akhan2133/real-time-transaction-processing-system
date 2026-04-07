FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/worker ./cmd/worker

FROM alpine:3.21 AS runtime

WORKDIR /app

RUN adduser -D -g '' appuser

COPY --from=builder /out/api /app/api
COPY --from=builder /out/worker /app/worker
COPY migrations /app/migrations

USER appuser

EXPOSE 8080

CMD ["/app/api"]
