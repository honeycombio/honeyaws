package publisher

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"runtime"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/honeycombio/dynsampler-go"
	"github.com/honeycombio/honeyelb/options"
	"github.com/honeycombio/honeytail/event"
	"github.com/honeycombio/honeytail/parsers/nginx"
	"github.com/honeycombio/libhoney-go"
	"github.com/honeycombio/urlshaper"
)

const (
	AWSElasticLoadBalancerFormatV2 = "aws_elbv2"
	AWSElasticLoadBalancerFormat = "aws_elb"
)

var (
	// 2017-07-31T20:30:57.975041Z spline_reticulation_lb 10.11.12.13:47882 10.3.47.87:8080 0.000021 0.010962 0.000016 200 200 766 17 "PUT https://api.simulation.io:443/reticulate/spline/1 HTTP/1.1" "libhoney-go/1.3.3" ECDHE-RSA-AES128-GCM-SHA256 TLSv1.2
	logFormatV2           = []byte(fmt.Sprintf(`log_format %s '$type $timestamp $elb $client_authority $backend_authority $request_processing_time $backend_processing_time $response_processing_time $elb_status_code $backend_status_code $received_bytes $sent_bytes "$request" "$user_agent" $ssl_cipher $ssl_protocol $target_group_arn "$trace_id" $domain_name $chosen_cert_arn';`, AWSElasticLoadBalancerFormatV2))
	logFormat             = []byte(fmt.Sprintf(`log_format %s '$timestamp $elb $client_authority $backend_authority $request_processing_time $backend_processing_time $response_processing_time $elb_status_code $backend_status_code $received_bytes $sent_bytes "$request" "$user_agent" $ssl_cipher $ssl_protocol';`, AWSElasticLoadBalancerFormat))
	libhoneyInitialized = false
	formatFileName      string
	formatFileNameV2      string
)

func init() {
	// Set up the log format file for parsing in the future.
	formatFile, err := ioutil.TempFile("", "honeytail_fmt_file")
	if err != nil {
		logrus.Fatal(err)
	}

	if _, err := formatFile.Write(logFormat); err != nil {
		logrus.Fatal(err)
	}

	if err := formatFile.Close(); err != nil {
		logrus.Fatal(err)
	}

	formatFileName = formatFile.Name()

	// Set up the log format file for parsing in the future.
	formatFileV2, err := ioutil.TempFile("", "honeytail_fmt_file_v2")
	if err != nil {
		logrus.Fatal(err)
	}

	if _, err := formatFileV2.Write(logFormatV2); err != nil {
		logrus.Fatal(err)
	}

	if err := formatFileV2.Close(); err != nil {
		logrus.Fatal(err)
	}

	formatFileNameV2 = formatFileV2.Name()
}

type Publisher interface {
	// Publish accepts an io.Reader and scans it line-by-line, parses the
	// relevant event from each line, and sends to the target (Honeycomb)
	Publish(r io.Reader) error
}

// HoneycombPublisher implements Publisher and sends the entries provided to
// Honeycomb. Publisher allows us to have only one point of entry to sending
// events to Honeycomb (if desired), as well as isolate line parsing, sampling,
// and URL sub-parsing logic.
type HoneycombPublisher struct {
	APIHost      string
	SampleRate   int
	nginxParser  *nginx.Parser
	lines        chan string
	eventsToSend chan event.Event
	sampler      dynsampler.Sampler
}

func NewHoneycombPublisher(opt *options.Options, logFormatName string) *HoneycombPublisher {
	hp := &HoneycombPublisher{
		nginxParser: &nginx.Parser{},
	}

	var logFormatFilename = formatFileName
	if (logFormatName == AWSElasticLoadBalancerFormatV2) {
		logFormatFilename = formatFileNameV2
	}

	hp.nginxParser.Init(&nginx.Options{
		ConfigFile:      logFormatFilename,
		TimeFieldName:   "timestamp",
		TimeFieldFormat: "2006-01-02T15:04:05.9999Z",
		LogFormatName:   logFormatName,
		NumParsers:      runtime.NumCPU(),
	})

	if !libhoneyInitialized {
		libhoney.Init(libhoney.Config{
			MaxBatchSize:  500,
			SendFrequency: 100 * time.Millisecond,
			WriteKey:      opt.WriteKey,
			Dataset:       opt.Dataset,
			SampleRate:    uint(opt.SampleRate),
			APIHost:       opt.APIHost,
		})
		libhoneyInitialized = true
	}

	hp.sampler = &dynsampler.AvgSampleRate{
		ClearFrequencySec: 300,
		GoalSampleRate:    opt.SampleRate,
	}

	if err := hp.sampler.Start(); err != nil {
		logrus.Error(err)
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

func (h *HoneycombPublisher) dynSample(eventsCh <-chan event.Event, sampledCh chan<- event.Event) {
	for ev := range eventsCh {
		// use backend_status_code and elb_status_code to set sample rate
		var key string
		if backendStatusCode, ok := ev.Data["backend_status_code"]; ok {
			if bsc, ok := backendStatusCode.(int64); ok {
				key = fmt.Sprintf("%d", bsc)
			} else {
				key = "0"
			}
		}
		if elbStatusCode, ok := ev.Data["elb_status_code"]; ok {
			if esc, ok := elbStatusCode.(int64); ok {
				key = fmt.Sprintf("%s_%d", key, esc)
			}
		}

		// Make sure sample rate is per-ELB
		if elbName, ok := ev.Data["elb"]; ok {
			if name, ok := elbName.(string); ok {
				key = fmt.Sprintf("%s_%s", key, name)
			}
		}

		rate := h.sampler.GetSampleRate(key)
		if rate <= 0 {
			logrus.WithField("rate", rate).Error("Sample should not be less than zero")
			rate = 1
		}
		if rand.Intn(rate) == 0 {
			ev.SampleRate = rate
			sampledCh <- ev
		}
	}
}

func (h *HoneycombPublisher) sample(eventsCh <-chan event.Event) chan event.Event {
	sampledCh := make(chan event.Event, runtime.NumCPU())
	go h.dynSample(eventsCh, sampledCh)
	return sampledCh
}

func dropNegativeTimes(ev *event.Event) {
	timeFields := []string{
		"response_processing_time",
		"request_processing_time",
		"backend_processing_time",
	}
	for _, f := range timeFields {
		if t, present := ev.Data[f]; present {
			if tfloat, isFloat := t.(float64); isFloat && tfloat < 0 {
				delete(ev.Data, f)
			}
		}
	}
}

func sendEvents(eventsCh <-chan event.Event) {
	shaper := requestShaper{&urlshaper.Parser{}}
	for ev := range eventsCh {
		shaper.Shape("request", &ev)
		libhEv := libhoney.NewEvent()
		libhEv.Timestamp = ev.Timestamp
		libhEv.SampleRate = uint(ev.SampleRate)
		dropNegativeTimes(&ev)
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
	linesCh := make(chan string, runtime.NumCPU())
	eventsCh := make(chan event.Event, runtime.NumCPU())
	scanner := bufio.NewScanner(r)
	go hp.nginxParser.ProcessLines(linesCh, eventsCh, nil)
	sampledCh := hp.sample(eventsCh)
	go sendEvents(sampledCh)
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
