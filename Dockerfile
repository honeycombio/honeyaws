FROM golang:alpine

RUN apk add --update --no-cache git
RUN go get github.com/honeycombio/honeyaws/cmd/honeyelb
RUN go get github.com/honeycombio/honeyaws/cmd/honeycloudfront

FROM alpine

RUN apk add --update --no-cache ca-certificates
COPY --from=0 /go/bin/honeyelb /usr/bin/honeyelb
COPY --from=0 /go/bin/honeycloudfront /usr/bin/honeycloudfront
