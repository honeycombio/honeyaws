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
					"elb":                     "app/foo-alb/1db0c9806095122a",
					"request_path":            "/down/stream",
				},
			},
			expected: map[string]interface{}{
				// old fields
				"request.headers.x-amzn-trace-id": "Root=1-5759e988-bd862e3fe1be46a994272793;Parent=53995c3f42cd8ad8;Self=1-67891234-12456789abcdef012345678;Sampled=1",
				"request_processing_time":         1.12,
				"elb":                             "app/foo-alb/1db0c9806095122a",
				"request_path":                    "/down/stream",
				"trace.trace_id":                  "bd862e3fe1be46a994272793",
				"trace.span_id":                   "12456789abcdef012345678",
				"trace.parent_id":                 "53995c3f42cd8ad8",
				"duration_ms":                     1.12,
				"sampled":                         "1",
				"service_name":                    "app/foo-alb/1db0c9806095122a",
				"name":                            "/down/stream",
			},
		},
		{
			ev: event.Event{
				Data: map[string]interface{}{
					// trace id contains some garbage - should not crash
					"trace_id": "Root=1-5759e988-bd862e3fe1be46a994272793;Parent=53995c3f42cd8ad8;Self=1-67891234-12456789abcdef012345678;Sampled=1;shouldnotcrash",
					// request time is a string, not a float - should not crash
					"request_processing_time": "1.12",
					// elb is not a string, should not crash
					"elb": 1,
					// request_path is the wrong type - should not crash
					"request_path": false,
				},
			},
			expected: map[string]interface{}{
				"request.headers.x-amzn-trace-id": "Root=1-5759e988-bd862e3fe1be46a994272793;Parent=53995c3f42cd8ad8;Self=1-67891234-12456789abcdef012345678;Sampled=1;shouldnotcrash",
				"request_processing_time":         "1.12",
				"elb":                             1,
				"request_path":                    false,
				"trace.trace_id":                  "bd862e3fe1be46a994272793",
				"trace.span_id":                   "12456789abcdef012345678",
				"trace.parent_id":                 "53995c3f42cd8ad8",
				"sampled":                         "1",
			},
		},
		{
			ev: event.Event{
				Data: map[string]interface{}{
					"trace_id": "Root=1-5759e988-bd862e3fe1be46a994272793;Parent=53995c3f42cd8ad8;Self=1-67891234-12456789abcdef012345678;Sampled=1",
					// request processing time, request path, and elb missing - should not crash
				},
			},
			expected: map[string]interface{}{
				// old fields
				"request.headers.x-amzn-trace-id": "Root=1-5759e988-bd862e3fe1be46a994272793;Parent=53995c3f42cd8ad8;Self=1-67891234-12456789abcdef012345678;Sampled=1",
				"trace.trace_id":                  "bd862e3fe1be46a994272793",
				"trace.span_id":                   "12456789abcdef012345678",
				"trace.parent_id":                 "53995c3f42cd8ad8",
				"sampled":                         "1",
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
