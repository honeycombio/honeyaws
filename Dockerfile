FROM golang:alpine

RUN apk add --update --no-cache git
RUN go get github.com/honeycombio/honeycomb-aws/cmd/honeyelb
RUN go get github.com/honeycombio/honeycomb-aws/cmd/honeycloudformation

FROM alpine

RUN apk add --update --no-cache ca-certificates
COPY --from=0 /go/bin/honeyelb /usr/bin/honeyelb
COPY --from=0 /go/bin/honeycloudformation /usr/bin/honeycloudformation
