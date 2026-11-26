package publisher

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/honeycombio/honeyaws/options"
	"github.com/honeycombio/honeyaws/state"
	"github.com/honeycombio/honeytail/event"
)

func TestFlattenCloudTrailRecordErrorFields(t *testing.T) {
	failed := &CloudTrailRecord{
		EventName:    "AssumeRole",
		ErrorCode:    "AccessDenied",
		ErrorMessage: "User is not authorized to perform: sts:AssumeRole",
	}
	got := flattenCloudTrailRecord(failed)
	if got["ErrorCode"] != "AccessDenied" {
		t.Errorf("ErrorCode = %v, want AccessDenied", got["ErrorCode"])
	}
	if got["ErrorMessage"] != "User is not authorized to perform: sts:AssumeRole" {
		t.Errorf("ErrorMessage = %v", got["ErrorMessage"])
	}

	ok := flattenCloudTrailRecord(&CloudTrailRecord{EventName: "AssumeRole"})
	for _, k := range []string{"ErrorCode", "ErrorMessage"} {
		if _, present := ok[k]; present {
			t.Errorf("%s should not be set on a successful call", k)
		}
	}
}

func TestCloudTrailParseEventsErrorFields(t *testing.T) {
	const body = `{"Records":[
{"eventTime":"2023-01-02T03:04:05Z","eventSource":"s3.amazonaws.com","eventName":"GetObject","awsRegion":"us-east-1","errorCode":"NoSuchKey","errorMessage":"The specified key does not exist."},
{"eventTime":"2023-01-02T03:04:06Z","eventSource":"s3.amazonaws.com","eventName":"PutObject","awsRegion":"us-east-1"}
]}`

	path := filepath.Join(t.TempDir(), "trail.json.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(f)
	if _, err := zw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	parser := NewCloudTrailEventParser(&options.Options{SampleRate: 1, SamplerType: "simple"})
	out := make(chan event.Event, 2)
	if err := parser.ParseEvents(state.DownloadedObject{Object: "trail", Filename: path}, out); err != nil {
		t.Fatalf("ParseEvents: %v", err)
	}
	close(out)

	var events []event.Event
	for ev := range out {
		events = append(events, ev)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}

	if events[0].Data["ErrorCode"] != "NoSuchKey" || events[0].Data["ErrorMessage"] != "The specified key does not exist." {
		t.Errorf("failed call lost its error fields: %v", events[0].Data)
	}
	if _, present := events[1].Data["ErrorCode"]; present {
		t.Errorf("successful call should not carry ErrorCode: %v", events[1].Data)
	}
	if events[1].Data["EventName"] != "PutObject" {
		t.Errorf("unexpected second event: %v", events[1].Data)
	}
}
