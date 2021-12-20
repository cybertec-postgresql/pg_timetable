FROM golang:alpine AS builder

# Set necessary environmet variables needed for our image
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

# Move to working directory /build
WORKDIR /build

# Update certificates
RUN apk update && apk upgrade && apk add --no-cache ca-certificates
RUN update-ca-certificates

# Copy and download dependency using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy the code into the container
COPY . .

# Build the application
RUN go build -o pg_timetable .

FROM scratch

# Copy the binary and certificates into the container
COPY --from=builder /build/pg_timetable /
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Command to run the executable
ENTRYPOINT ["/pg_timetable"]

ENV PGTT_HTTPPORT 8088
# Exposing port 8088 (you should specify your value!)
EXPOSE 8088
