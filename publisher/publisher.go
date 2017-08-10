package publisher

import (
	"bufio"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/honeycombio/honeyelb/options"
	"github.com/honeycombio/honeytail/event"
	"github.com/honeycombio/honeytail/parsers/nginx"
	"github.com/honeycombio/libhoney-go"
	"github.com/honeycombio/urlshaper"
)

var (
	libhoneyInitialized = false
)

type Publisher interface {
	// Publish accepts an io.Reader and scans it line-by-line, parses the
	// relevant event from each line, and sends to the target (Honeycomb)
	Publish(r io.Reader) error
}

// HoneycombPublisher implements Publisher and sends the entries provided to
// Honeycomb
type HoneycombPublisher struct {
	APIHost      string
	ScrubQuery   bool
	SampleRate   int
	initialized  bool
	nginxParser  *nginx.Parser
	lines        chan string
	eventsToSend chan event.Event
}

func NewHoneycombPublisher(opt *options.Options, configFile string) *HoneycombPublisher {
	hp := &HoneycombPublisher{
		nginxParser: &nginx.Parser{},
	}

	// htflags is needed because we can't count on vendored honeyelb flags
	// lib to be the same as vendored ht flags lib to do the type
	// conversion :|
	hp.nginxParser.Init(&nginx.Options{
		ConfigFile:      configFile,
		TimeFieldName:   "timestamp",
		TimeFieldFormat: "2006-01-02T15:04:05.9999Z",
		LogFormatName:   "aws_elb",
		NumParsers:      runtime.NumCPU(),
	})

	if !libhoneyInitialized {
		libhoney.Init(libhoney.Config{
			MaxBatchSize:  500,
			SendFrequency: 100 * time.Millisecond,
			WriteKey:      opt.WriteKey,
			Dataset:       opt.Dataset,
			SampleRate:    uint(opt.SampleRate),
		})
		libhoneyInitialized = true
	}
	return hp
}

type requestShaper struct {
	pr *urlshaper.Parser
}

// Nicked directly from github.com/honeycombio/honeytail/leash.go
func (rs *requestShaper) Shape(field string, ev *event.Event) {
	if val, ok := ev.Data[field]; ok {
		// start by splitting out method, uri, and version
		parts := strings.Split(val.(string), " ")
		var path string
		if len(parts) == 3 {
			// treat it as METHOD /path HTTP/1.X
			ev.Data[field+"_method"] = parts[0]
			ev.Data[field+"_protocol_version"] = parts[2]
			path = parts[1]
		} else {
			// treat it as just the /path
			path = parts[0]
		}

		// next up, get all the goodies out of the path
		res, err := rs.pr.Parse(path)
		if err != nil {
			// couldn't parse it, just pass along the event
			logrus.WithError(err).Error("Couldn't parse request")
			return
		}
		ev.Data[field+"_uri"] = res.URI
		ev.Data[field+"_path"] = res.Path
		if res.Query != "" {
			ev.Data[field+"_query"] = res.Query
		}
		ev.Data[field+"_shape"] = res.Shape
		if res.QueryShape != "" {
			ev.Data[field+"_queryshape"] = res.QueryShape
		}
	}
}

func sendEvents(eventsCh <-chan event.Event) {
	shaper := requestShaper{&urlshaper.Parser{}}
	for ev := range eventsCh {
		shaper.Shape("request", &ev)
		libhEv := libhoney.NewEvent()
		libhEv.Timestamp = ev.Timestamp
		if err := libhEv.Add(ev.Data); err != nil {
			logrus.WithFields(logrus.Fields{
				"event": ev,
				"error": err,
			}).Error("Unexpected error adding data to libhoney event")
		}
		// sampling is handled by the nginx parser
		if err := libhEv.SendPresampled(); err != nil {
			logrus.WithFields(logrus.Fields{
				"event": ev,
				"error": err,
			}).Error("Unexpected error event to libhoney send")
		}
	}
}

func (hp *HoneycombPublisher) Publish(r io.Reader) error {
	linesCh := make(chan string)
	eventsCh := make(chan event.Event)
	scanner := bufio.NewScanner(r)
	go hp.nginxParser.ProcessLines(linesCh, eventsCh, nil)
	go sendEvents(eventsCh)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		linesCh <- line
	}

	return scanner.Err()
}

// Close flushes outstanding sends
func (hp *HoneycombPublisher) Close() {
	libhoney.Close()
}
