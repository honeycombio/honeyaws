FROM golang:alpine

RUN apk add --update --no-cache git
RUN go get github.com/honeycombio/honeyelb

FROM alpine

RUN apk add --update --no-cache ca-certificates
COPY --from=0 /go/bin/honeyelb /usr/bin/honeyelb
ENTRYPOINT ["honeyelb"]
