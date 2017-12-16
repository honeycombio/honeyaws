package publisher

import (
	"log"
	"reflect"
	"testing"

	"github.com/honeycombio/honeytail/event"
)

func TestDropNegativeTimes(t *testing.T) {
	ev := event.Event{
		Data: map[string]interface{}{
			"response_processing_time": int64(-1),
			"request_processing_time":  float64(-1),
			"backend_processing_time":  int64(-1),
		},
	}
	expected := map[string]interface{}{}
	dropNegativeTimes(&ev)
	if !reflect.DeepEqual(ev.Data, expected) {
		t.Error("Output did not match expected:")
		for k, v := range ev.Data {
			log.Print("actual: ", k, "\t(", reflect.TypeOf(v), ") ", v)
			log.Print("expected: ", k, "\t(", reflect.TypeOf(expected[k]), ") ", expected[k])
		}
		t.Fatal()
	}
}
