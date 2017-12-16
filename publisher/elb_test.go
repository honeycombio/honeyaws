package publisher

import (
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"testing"

	"github.com/honeycombio/honeyaws/state"
	"github.com/honeycombio/honeytail/event"
)

func TestNginxParseEvents(t *testing.T) {
	elbPubisher := NewELBEventParser(1)
	outCh := make(chan event.Event)
	tmpFile, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal("Shouldn't have err but did: ", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(`2017-07-31T20:30:57.975041Z spline_reticulation_lb 10.11.12.13:47882 10.3.47.87:8080 0.000021 0.010962 -1 504 504 766 17 "PUT https://api.simulation.io:443/reticulate/spline/1 HTTP/1.1" "libhoney-go/1.3.3" ECDHE-RSA-AES128-GCM-SHA256 TLSv1.2`)); err != nil {
		t.Fatal("Shouldn't have err but did: ", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatal("Shouldn't have err but did: ", err)
	}
	obj := state.DownloadedObject{
		Object:   "foo",
		Filename: tmpFile.Name(),
	}
	if err := elbPubisher.ParseEvents(obj, outCh); err != nil {
		t.Fatal("Shouldn't have err but did: ", err)
	}
	expected := map[string]interface{}{
		"client_authority":        "10.11.12.13:47882",
		"backend_authority":       "10.3.47.87:8080",
		"sent_bytes":              int64(17),
		"ssl_protocol":            "TLSv1.2",
		"request_processing_time": 2.1e-05,
		"request":                 "PUT https://api.simulation.io:443/reticulate/spline/1 HTTP/1.1",
		"user_agent":              "libhoney-go/1.3.3",
		"backend_processing_time": 0.010962,
		"elb": "spline_reticulation_lb",
		"backend_status_code":      int64(504),
		"elb_status_code":          int64(504),
		"response_processing_time": int64(-1),
		"received_bytes":           int64(766),
		"ssl_cipher":               "ECDHE-RSA-AES128-GCM-SHA256",
	}
	evData := (<-outCh).Data
	close(outCh)
	if !reflect.DeepEqual(evData, expected) {
		t.Error("Output did not match expected:")
		for k, v := range evData {
			log.Print("actual: ", k, "\t(", reflect.TypeOf(v), ") ", v)
			log.Print("expected: ", k, "\t(", reflect.TypeOf(expected[k]), ") ", expected[k])
		}
		t.Fatal()
	}
}
