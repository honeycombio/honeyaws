package publisher

import (
	"log"
	"reflect"
	"testing"

	"github.com/honeycombio/honeytail/event"
)

func TestParseTraceData(t *testing.T) {
	testCases := []struct {
		ev       event.Event
		expected map[string]interface{}
	}{
		{
			ev: event.Event{
				Data: map[string]interface{}{
					"trace_id":                "Root=1-5759e988-bd862e3fe1be46a994272793;Parent=53995c3f42cd8ad8;Self=1-67891234-12456789abcdef012345678;Sampled=1",
					"request_processing_time": 1.12,
					"elb":          "fooservice",
					"request_path": "/down/stream",
				},
			},
			expected: map[string]interface{}{
				// old fields
				"trace_id":                "Root=1-5759e988-bd862e3fe1be46a994272793;Parent=53995c3f42cd8ad8;Self=1-67891234-12456789abcdef012345678;Sampled=1",
				"request_processing_time": 1.12,
				"elb":          "fooservice",
				"request_path": "/down/stream",

				"traceId":     "bd862e3fe1be46a994272793",
				"id":          "12456789abcdef012345678",
				"parentId":    "53995c3f42cd8ad8",
				"durationMs":  1.12,
				"sampled":     "1",
				"serviceName": "fooservice",
				"name":        "/down/stream",
			},
		},
	}

	for _, tc := range testCases {
		addTraceData(&tc.ev)
		if !reflect.DeepEqual(tc.ev.Data, tc.expected) {
			t.Error("Output did not match expected:")
			for k, v := range tc.ev.Data {
				log.Print("actual: ", k, "\t(", reflect.TypeOf(v), ") ", v)
				log.Print("expected: ", k, "\t(", reflect.TypeOf(tc.expected[k]), ") ", tc.expected[k])
			}
			t.Fatal()
		}
	}
}

func TestDropNegativeTimes(t *testing.T) {
	testCases := []struct {
		ev       event.Event
		expected map[string]interface{}
	}{
		{
			ev: event.Event{
				Data: map[string]interface{}{
					"response_processing_time": int64(-1),
				},
			},
			expected: map[string]interface{}{
				"error": "response_processing_time was -1 -- upstream server timed out, disconnected, or sent malformed response",
			},
		},
		{
			ev: event.Event{
				Data: map[string]interface{}{
					"request_processing_time": float64(-1),
				},
			},
			expected: map[string]interface{}{
				"error": "request_processing_time was -1 -- upstream server timed out, disconnected, or sent malformed response",
			},
		},
		{
			ev: event.Event{
				Data: map[string]interface{}{
					"backend_processing_time": int64(-1),
				},
			},
			expected: map[string]interface{}{
				"error": "backend_processing_time was -1 -- upstream server timed out, disconnected, or sent malformed response",
			},
		},
	}

	for _, tc := range testCases {
		dropNegativeTimes(&tc.ev)
		if !reflect.DeepEqual(tc.ev.Data, tc.expected) {
			t.Error("Output did not match expected:")
			for k, v := range tc.ev.Data {
				log.Print("actual: ", k, "\t(", reflect.TypeOf(v), ") ", v)
				log.Print("expected: ", k, "\t(", reflect.TypeOf(tc.expected[k]), ") ", tc.expected[k])
			}
			t.Fatal()
		}
	}
}
