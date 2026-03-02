FROM golang:1.22-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /gh-webhook-handler ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ca-certificates sqlite
WORKDIR /app
COPY --from=builder /gh-webhook-handler .
COPY configs/ configs/

EXPOSE 8080
ENTRYPOINT ["./gh-webhook-handler"]
