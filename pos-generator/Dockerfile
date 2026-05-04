# Stage 1: Build the Go binary
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY main.go ./

# Compile a statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o pos-generator .

# Stage 2: Create the minimal production image
FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/pos-generator .

# Run the binary
CMD ["./pos-generator"]
