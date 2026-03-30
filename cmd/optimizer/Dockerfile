FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum first for layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy all source code.
COPY . .

# Build the optimizer binary.
RUN CGO_ENABLED=0 GOOS=linux go build -o /optimizer ./cmd/optimizer/

# ---
FROM alpine:3.20

RUN apk --no-cache add ca-certificates

COPY --from=builder /optimizer /optimizer

ENV GRPC_PORT=50052
EXPOSE 50052

ENTRYPOINT ["/optimizer"]
