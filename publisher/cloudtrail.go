package publisher

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"time"

	"github.com/Sirupsen/logrus"
	dynsampler "github.com/honeycombio/dynsampler-go"
	"github.com/honeycombio/honeyaws/options"
	"github.com/honeycombio/honeyaws/sampler"
	"github.com/honeycombio/honeyaws/state"
	"github.com/honeycombio/honeytail/event"
)

type CloudTrailResource struct {
	ResourceARN       string `json:ARN`
	ResourceAccountId string `json:accountId`
}

type CloudTrailUserIdentity struct {
	Type        string `json:type`
	PrincipleId string `json:principleId`
	ARN         string `json:arn`
	AccountId   string `json:accountId`
	AccessKeyId string `json:accessKeyId`
}

type CloudTrailRecords struct {
	Records []CloudTrailRecord `json:Records`
}

type CloudTrailRecord struct {
	UserIdentity      CloudTrailUserIdentity `json:"userIdentity"`
	EventTime         string                 `json:"eventTime"`
	EventSource       string                 `json:"eventSource"`
	EventName         string                 `json:"eventName"`
	AwsRegion         string                 `json:"awsRegion"`
	SourceIPAddress   string                 `json:"sourceIPAddress"`
	UserAgent         string                 `json:"userAgent"`
	Resources         []CloudTrailResource   `json:"resources"`
	EventType         string                 `json:"eventType"`
	RequestParameters map[string]interface{} `json:"requestParameters"`
}

type CloudTrailEventParser struct {
	sampler dynsampler.Sampler
}

// Helper function for flattening cloud trail records
// honeytail events are map[string]interface{}
func flattenCloudTrailRecord(r *CloudTrailRecord) map[string]interface{} {
	p := make(map[string]interface{}, 0)

	p["Type"] = r.UserIdentity.Type
	p["PrincipleId"] = r.UserIdentity.PrincipleId
	p["ARN"] = r.UserIdentity.ARN
	p["AccountId"] = r.UserIdentity.AccountId
	p["AccessKeyId"] = r.UserIdentity.AccessKeyId
	p["EventTime"] = r.EventTime
	p["EventName"] = r.EventName
	p["EventSource"] = r.EventSource
	p["AwsRegion"] = r.AwsRegion
	p["SourceIPAddress"] = r.SourceIPAddress
	p["UserAgent"] = r.UserAgent
	p["EventType"] = r.EventType
	p["Parameters"] = r.RequestParameters

	return p
}

func NewCloudTrailEventParser(opt *options.Options) *CloudTrailEventParser {
	s, err := sampler.NewSamplerFromOptions(opt)
	if err != nil {
		logrus.WithField("err", err).Fatal("couldn't build sampler from arguments")
	}
	ep := &CloudTrailEventParser{sampler: s}

	if err := ep.sampler.Start(); err != nil {
		logrus.WithField("err", err).Fatal("Couldn't start dynamic sampler")
	}

	return ep
}

// we have to wrap events ourselves due to there being no existing parsers
func (ep *CloudTrailEventParser) ParseEvents(obj state.DownloadedObject, out chan<- event.Event) error {

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

	if err != nil {
		logrus.WithFields(logrus.Fields{
			"object": obj,
			"err":    err,
		}).Debug("Error parsing")
		return err
	}

	dec := json.NewDecoder(r)
	var rec CloudTrailRecords
	for {
		if err := dec.Decode(&rec); err == io.EOF {
			break
		} else if err != nil {
			logrus.Fatal(err)
		}
	}

	timeFormat := "2006-01-02T15:04:05Z"

	// parse records one at a time
	// TODO: do we want to thread?

	for _, record := range rec.Records {
		t, err := time.Parse(timeFormat, record.EventTime)

		if err != nil {
			logrus.Fatal(err)
		}
		omap := flattenCloudTrailRecord(&record)
		e := event.Event{
			Timestamp: t,
			Data:      omap,
		}
		logrus.WithField("event", e).Info("Event parsing")
		out <- e
	}

	return nil
}

// samples every rate for event
// TODO: how can we additionally filter this down to potentially interesting
// bits?
func (ep *CloudTrailEventParser) DynSample(in <-chan event.Event, out chan<- event.Event) {
	for ev := range in {
		var key string
		if eventSource, ok := ev.Data["EventSource"]; ok {
			if evs, ok := eventSource.(string); ok {
				key = fmt.Sprintf("%s", evs)
			} else {
				key = "0"
				logrus.WithFields(logrus.Fields{
					"field":       "eventSource",
					"intended":    "string",
					"actual_val":  eventSource,
					"actual_type": fmt.Sprintf("%T", eventSource),
				}).Error("Did not cast field from access log correctly")

			}
		}
		if eventName, ok := ev.Data["EventName"]; ok {
			if evn, ok := eventName.(string); ok {
				key = fmt.Sprintf("%s_%s", key, evn)
			} else {
				key = "0"
				logrus.WithFields(logrus.Fields{
					"field":       "eventName",
					"intended":    "string",
					"actual_val":  eventName,
					"actual_type": fmt.Sprintf("%T", eventName),
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
