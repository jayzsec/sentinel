# Stage 1: Build Environment
FROM golang:1.26.2-alpine AS builder
LABEL authors="jayzsec"
WORKDIR /app

# Copy the Go module files and download dependencies
COPY go.mod ./
# If you eventually add external packages (like Azure SDK), uncomment the line below
# COPY go.sum ./
# RUN go mod download

# Copy the source code (including your provider folder)
COPY . .

# Compile the static binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o finops-bot .

# Stage 2: Minimal Runtime
FROM alpine:latest
WORKDIR /root/

# Copy the compiled binary from the builder stage
COPY --from=builder /app/finops-bot .

# Because this is a CronJob, it doesn't listen on a port, so we don't need EXPOSE.
# It simply wakes up, executes the binary, and shuts down.
CMD ["./finops-bot"]