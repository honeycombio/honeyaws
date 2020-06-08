FROM golang:1.9-alpine
COPY . /go/src/github.com/honeycombio/honeyaws
WORKDIR /go/src/github.com/honeycombio/honeyaws
RUN go install ./...

FROM golang:1.9-alpine
RUN apk add --update --no-cache ca-certificates
COPY --from=0 /go/bin/honeyelb /usr/bin/honeyelb
COPY --from=0 /go/bin/honeyalb /usr/bin/honeyalb
