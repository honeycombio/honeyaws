package publisher

import (
	"log"
	"reflect"
	"testing"

	"github.com/honeycombio/honeytail/event"
)

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
