# Stage 1: Build the GO binary
FROM golang:1.22-alpine AS builder
LABEL authors="jayzsec"
WORKDIR /app
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o sentinel-soc .

# Stage 2: Create the minimal production image
FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/sentinel-soc .

# Expose port 8080 for our new HTTP API
EXPOSE 8080

CMD ["./sentinel-soc"]