FROM golang:alpine
COPY . /go/src/github.com/honeycombio/honeyaws
WORKDIR /go/src/github.com/honeycombio/honeyaws
RUN go install ./...

FROM alpine
RUN apk add --update --no-cache ca-certificates
COPY --from=0 /go/bin/honeyelb /usr/bin/honeyelb
COPY --from=0 /go/bin/honeyalb /usr/bin/honeyalb
COPY --from=0 /go/bin/honeycloudfront /usr/bin/honeycloudfront
COPY --from=0 /go/bin/honeycloudtrail /usr/bin/honeycloudtrail
