FROM golang:alpine

RUN apk add --update --no-cache git
RUN go get github.com/honeycombio/honeyaws/cmd/honeyelb
RUN go get github.com/honeycombio/honeyaws/cmd/honeyalb
RUN go get github.com/honeycombio/honeyaws/cmd/honeycloudfront
RUN go get github.com/honeycombio/honeyaws/cmd/honeycloudtrail

FROM alpine

RUN apk add --update --no-cache ca-certificates
COPY --from=0 /go/bin/honeyelb /usr/bin/honeyelb
COPY --from=0 /go/bin/honeyalb /usr/bin/honeyalb
COPY --from=0 /go/bin/honeycloudfront /usr/bin/honeycloudfront
COPY --from=0 /go/bin/honeycloudtrail /usr/bin/honeycloudtrail
