package publisher

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strings"

	dynsampler "github.com/honeycombio/dynsampler-go"
	"github.com/honeycombio/honeyaws/options"
	"github.com/honeycombio/honeyaws/sampler"
	"github.com/honeycombio/honeyaws/state"
	"github.com/honeycombio/honeytail/event"
	"github.com/honeycombio/honeytail/parsers/nginx"
	"github.com/sirupsen/logrus"
)

type ALBEventParser struct {
	sampler dynsampler.Sampler
}

func NewALBEventParser(opt *options.Options) *ALBEventParser {
	s, err := sampler.NewSamplerFromOptions(opt)
	if err != nil {
		logrus.WithField("err", err).Fatal("couldn't build sampler from arguments")
	}

	ep := &ALBEventParser{sampler: s}

	if err := ep.sampler.Start(); err != nil {
		logrus.WithField("err", err).Fatal("Couldn't start dynamic sampler")
	}

	return ep
}

func (ep *ALBEventParser) ParseEvents(obj state.DownloadedObject, out chan<- event.Event) error {
	np := &nginx.Parser{}
	err := np.Init(&nginx.Options{
		ConfigFile:      formatFileName,
		TimeFieldName:   "timestamp",
		TimeFieldFormat: "2006-01-02T15:04:05.9999Z",
		LogFormatName:   AWSApplicationLoadBalancerFormat,
		NumParsers:      runtime.NumCPU(),
	})
	if err != nil {
		logrus.Fatal("Can't initialize the nginx parser")
	}

	linesCh := make(chan string)

	go np.ProcessLines(linesCh, out, nil)

	f, err := os.Open(obj.Filename)
	if err != nil {
		return err
	}

	defer f.Close()

	r, err := gzip.NewReader(f)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		linesCh <- line
	}

	close(linesCh)

	return scanner.Err()
}

func (ep *ALBEventParser) DynSample(in <-chan event.Event, out chan<- event.Event) {
	for ev := range in {
		// use backend_status_code and elb_status_code to set sample rate
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

		// Make sure sample rate is per-ELB
		if elbName, ok := ev.Data["elb"]; ok {
			if name, ok := elbName.(string); ok {
				key = fmt.Sprintf("%s_%s", key, name)
			}
		}

		rate := ep.sampler.GetSampleRate(key)
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
