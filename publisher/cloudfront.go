package publisher

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strings"

	"github.com/Sirupsen/logrus"
	dynsampler "github.com/honeycombio/dynsampler-go"
	"github.com/honeycombio/honeyaws/state"
	"github.com/honeycombio/honeytail/event"
	"github.com/honeycombio/honeytail/parsers/nginx"
)

type CloudFrontEventParser struct {
	sampler dynsampler.Sampler
}

func NewCloudFrontEventParser(sampleRate int) *CloudFrontEventParser {
	ep := &CloudFrontEventParser{
		sampler: &dynsampler.AvgSampleRate{
			ClearFrequencySec: 300,
			GoalSampleRate:    sampleRate,
		},
	}

	if err := ep.sampler.Start(); err != nil {
		logrus.WithField("err", err).Fatal("Couldn't start dynamic sampler")
	}

	return ep
}

func (ep *CloudFrontEventParser) ParseEvents(obj state.DownloadedObject, out chan<- event.Event) error {
	np := &nginx.Parser{}
	err := np.Init(&nginx.Options{
		ConfigFile:      formatFileName,
		TimeFieldName:   "timestamp",
		TimeFieldFormat: "2006-01-02T15:04:05",
		LogFormatName:   AWSCloudFrontWebFormat,
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

		splitLine := strings.Fields(line)

		// Date and time are two separate fields instead of only one
		// timestamp field, so join them together..
		//
		// We join together the first two items with "T" in between as
		// a new first item and "delete" the second item.
		//
		// Yeah it's ugly, but we don't have many other options with
		// the nginx parser and Amazon's quirky format.
		//
		// Example of Amazon's format:
		// 2014-05-23 01:13:11
		splitLine = append([]string{splitLine[0] + "T" + splitLine[1]}, splitLine[2:]...)

		// nginx parser is fickle about whitespace, so the join ensures
		// that only one space exists between fields
		linesCh <- strings.Join(splitLine, " ")
	}

	close(linesCh)

	return nil
}

func (ep *CloudFrontEventParser) DynSample(in <-chan event.Event, out chan<- event.Event) {
	for ev := range in {
		var key string
		if backendStatusCode, ok := ev.Data["sc-status"]; ok {
			if bsc, ok := backendStatusCode.(int64); ok {
				key = fmt.Sprintf("%d", bsc)
			} else {
				key = "0"
				logrus.WithFields(logrus.Fields{
					"field":    "sc-status",
					"intended": "int64",
				}).Error("Did not cast field from access log correctly")
			}
		}

		// Make sure sample rate is per-distribution (cs is the domain
		// name of the CloudFront distribution)
		if distributionDomain, ok := ev.Data["cs"]; ok {
			if name, ok := distributionDomain.(string); ok {
				key = fmt.Sprintf("%s_%s", key, name)
			}
		}

		if edgeResultType, ok := ev.Data["x-edge-result-type"]; ok {
			if resultType, ok := edgeResultType.(string); ok {
				key = fmt.Sprintf("%s_%s", key, resultType)
			} else {
				key = "0"
				logrus.WithFields(logrus.Fields{
					"field":    "x-edge-result-type",
					"intended": "string",
				}).Error("Did not cast field from access log correctly")
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
