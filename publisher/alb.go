package publisher

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path"
	"regexp"
	"runtime"
	"strings"

	dynsampler "github.com/honeycombio/dynsampler-go"
	"github.com/honeycombio/honeyaws/options"
	"github.com/honeycombio/honeyaws/sampler"
	"github.com/honeycombio/honeyaws/state"
	"github.com/honeycombio/honeytail/event"
	"github.com/honeycombio/honeytail/parsers"
	"github.com/honeycombio/honeytail/parsers/nginx"
	"github.com/sirupsen/logrus"
)

type ALBEventParser struct {
	sampler                 dynsampler.Sampler
	samplerFields           []string
	connectionSampler       dynsampler.Sampler
	connectionSamplerFields []string
	keep4xx                 bool
}

func NewALBEventParser(opt *options.Options) *ALBEventParser {
	s, err := sampler.NewSamplerFromOptions(opt)
	if err != nil {
		logrus.WithField("err", err).Fatal("couldn't build sampler from arguments")
	}

	ep := &ALBEventParser{sampler: s, samplerFields: opt.SamplerFields, keep4xx: opt.Keep4xx,
		connectionSamplerFields: opt.ConnectionSamplerFields}

	if err := ep.sampler.Start(); err != nil {
		logrus.WithField("err", err).Fatal("Couldn't start dynamic sampler")
	}
	// A dynamic target of 1 can still downsample skewed keys after adjustment.
	// Leave the connection sampler unset to retain every connection at rate 1.
	if (opt.ALBLogType == "connection" || opt.ALBLogType == "both") && opt.ConnectionSampleRate > 1 {
		connectionOptions := *opt
		connectionOptions.SampleRate = opt.ConnectionSampleRate
		ep.connectionSampler, err = sampler.NewSamplerFromOptions(&connectionOptions)
		if err != nil {
			logrus.WithError(err).Fatal("Couldn't build connection sampler")
		}
		if err := ep.connectionSampler.Start(); err != nil {
			logrus.WithError(err).Fatal("Couldn't start connection sampler")
		}
	}

	return ep
}

func (ep *ALBEventParser) ParseEvents(obj state.DownloadedObject, out chan<- event.Event) error {
	f, err := os.Open(obj.Filename)
	if err != nil {
		return err
	}

	defer f.Close()

	r, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer r.Close()

	scanner := bufio.NewScanner(r)
	connectionLog := strings.HasPrefix(path.Base(obj.Object), "conn_log_")
	var accessLines chan string
	if connectionLog {
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	} else {
		np := &nginx.Parser{}
		if err := np.Init(&nginx.Options{
			ConfigFile:      formatFileName,
			TimeFieldName:   "timestamp",
			TimeFieldFormat: "2006-01-02T15:04:05.9999Z",
			LogFormatName:   AWSApplicationLoadBalancerFormat,
			NumParsers:      runtime.NumCPU(),
		}); err != nil {
			return err
		}
		accessLines = make(chan string)
		defer close(accessLines)
		parsed := make(chan event.Event)
		// Keep the label in the capture so nginx preserves opaque IDs as strings.
		prefix := &parsers.ExtRegexp{Regexp: regexp.MustCompile(`^(?P<conn_trace_id>conn_trace_id=\S+) `)}
		go func() {
			np.ProcessLines(accessLines, parsed, prefix)
			close(parsed)
		}()
		go func() {
			for ev := range parsed {
				ev.Data["log_type"] = "access"
				if id, ok := ev.Data["conn_trace_id"].(string); ok {
					ev.Data["conn_trace_id"] = strings.TrimPrefix(id, "conn_trace_id=")
				}
				out <- ev
			}
		}()
	}

	lineNumber := 0
	var parseErrors int

	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !connectionLog {
			accessLines <- accessLogWithConnectionID(line)
			continue
		}
		ev, err := parseALBConnectionLine(line, obj.Object)
		if err != nil {
			parseErrors++
			logrus.WithError(err).WithFields(logrus.Fields{"object": obj.Object, "line": lineNumber}).Error("Cannot parse ALB log line")
			continue
		}
		out <- ev
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if parseErrors > 0 {
		return fmt.Errorf("could not parse %d ALB log lines in %s", parseErrors, obj.Object)
	}
	return nil
}

func (ep *ALBEventParser) samplingKey(ev event.Event) (string, error) {
	fields := ep.samplerFields
	if ev.Data["log_type"] == "connection" {
		fields = ep.connectionSamplerFields
		if len(fields) == 0 {
			fields = []string{"tls_verify_status", "tls_protocol", "elb"}
		}
	}
	if len(fields) > 0 {
		values := make([]interface{}, len(fields))
		for i, field := range fields {
			values[i] = ev.Data[field]
		}
		key, err := json.Marshal(values)
		return string(key), err
	}
	var key string
	if backendStatusCode, ok := ev.Data["backend_status_code"]; ok {
		if bsc, ok := backendStatusCode.(int64); ok {
			key = fmt.Sprintf("%d", bsc)
		} else {
			key = "0"
			logrus.WithFields(logrus.Fields{
				"field":       "backend_status_code",
				"intended":    "int64",
				"actual_val":  backendStatusCode,
				"actual_type": fmt.Sprintf("%T", backendStatusCode),
			}).Error("Did not cast field from access log correctly")
		}
	}
	if elbStatusCode, ok := ev.Data["elb_status_code"]; ok {
		if esc, ok := elbStatusCode.(int64); ok {
			key = fmt.Sprintf("%s_%d", key, esc)
		} else {
			logrus.WithFields(logrus.Fields{
				"field":       "elb_status_code",
				"intended":    "int64",
				"actual_val":  elbStatusCode,
				"actual_type": fmt.Sprintf("%T", elbStatusCode),
			}).Error("Did not cast field from access log correctly")
		}
	}

	if elbName, ok := ev.Data["elb"]; ok {
		if name, ok := elbName.(string); ok {
			key = fmt.Sprintf("%s_%s", key, name)
		}
	}

	return key, nil
}

func (ep *ALBEventParser) DynSample(in <-chan event.Event, out chan<- event.Event) {
	for ev := range in {
		if ep.keepError(ev) {
			ev.SampleRate = 1
			out <- ev
			continue
		}
		s := ep.sampler
		if ev.Data["log_type"] == "connection" {
			s = ep.connectionSampler
		}
		key, err := ep.samplingKey(ev)
		rate := 1
		if err != nil {
			logrus.WithError(err).Error("Could not encode ALB sampling key; retaining event")
		} else if s != nil {
			rate = s.GetSampleRate(key)
		}
		if rate <= 0 {
			logrus.WithField("rate", rate).Error("Sample should not be less than zero")
			rate = 1
		}
		if rand.Intn(rate) == 0 {
			ev.SampleRate = rate
			out <- ev
		}
	}
}

func (ep *ALBEventParser) keepError(ev event.Event) bool {
	if ev.Data["log_type"] == "connection" {
		status, _ := ev.Data["tls_verify_status"].(string)
		return strings.HasPrefix(status, "Failed:")
	}
	for _, field := range []string{"elb_status_code", "backend_status_code"} {
		status, _ := ev.Data[field].(int64)
		if status >= 500 && status < 600 || ep.keep4xx && status >= 400 && status < 500 {
			return true
		}
	}
	if reason, _ := ev.Data["error_reason"].(string); reason != "" && reason != "-" {
		return true
	}
	for _, field := range []string{"request_processing_time", "backend_processing_time", "response_processing_time"} {
		switch value := ev.Data[field].(type) {
		case int64:
			if value < 0 {
				return true
			}
		case float64:
			if value < 0 {
				return true
			}
		}
	}
	return false
}
