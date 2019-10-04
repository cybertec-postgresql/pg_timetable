FROM golang:latest
RUN git config --global url."https://{github-user-token-here}:@github.com/".insteadOf "https://github.com/"
RUN go get github.com/cybertec-postgresql/pg_timetable/
WORKDIR /go/src/github.com/cybertec-postgresql/pg_timetable/
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=0 /go/src/github.com/cybertec-postgresql/pg_timetable/pg_timetable .
ENTRYPOINT ["./pg_timetable"]
