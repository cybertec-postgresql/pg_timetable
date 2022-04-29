# When building an image it is recommended to provide version arguments, e.g.
# docker build --no-cached -t <tagname> \
#     --build-arg COMMIT=`git show -s --format=%H HEAD` \
#     --build-arg VERSION=`git describe --tags --abbrev=0` \
#     --build-arg DATE=`git show -s --format=%cI HEAD` .
FROM golang:alpine AS builder

ARG COMMIT
ARG VERSION
ARG DATE
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
RUN go build -buildvcs=false -ldflags "-X main.commit=${COMMIT} -X main.version=${VERSION} -X main.date=${DATE}" -o pg_timetable .

FROM alpine

# Install psql client
RUN apk --no-cache add postgresql-client

# Copy the binary and certificates into the container
COPY --from=builder /build/pg_timetable /
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Command to run the executable
ENTRYPOINT ["/pg_timetable"]

# Expose REST API if needed
ENV PGTT_RESTPORT 8008
EXPOSE 8008
