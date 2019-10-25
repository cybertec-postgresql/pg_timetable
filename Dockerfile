FROM golang:latest
RUN go get github.com/cybertec-postgresql/pg_timetable/
WORKDIR /go/src/github.com/cybertec-postgresql/pg_timetable/
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
RUN mkdir sql
COPY --from=0 /go/src/github.com/cybertec-postgresql/pg_timetable/sql/* sql/
COPY --from=0 /go/src/github.com/cybertec-postgresql/pg_timetable/pg_timetable .
ENTRYPOINT ["./pg_timetable"]
