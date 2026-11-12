package publisher

import (
	"reflect"
	"testing"

	"github.com/honeycombio/honeyaws/options"
	"github.com/honeycombio/honeytail/event"
)

func TestALBDefaultSamplingKey(t *testing.T) {
	ep := &ALBEventParser{}
	for _, tc := range []struct {
		name string
		data map[string]interface{}
		want string
	}{
		{"complete", map[string]interface{}{"backend_status_code": int64(200), "elb_status_code": int64(200), "elb": "app/example/id"}, "200_200_app/example/id"},
		{"missing backend", map[string]interface{}{"elb_status_code": int64(503), "elb": "lb"}, "_503_lb"},
		{"missing statuses", map[string]interface{}{"elb": "lb"}, "_lb"},
		{"empty", map[string]interface{}{}, ""},
		{"unexpected backend type", map[string]interface{}{"backend_status_code": "200", "elb_status_code": int64(200), "elb": "lb"}, "0_200_lb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ep.samplingKey(event.Event{Data: tc.data})
			if err != nil || got != tc.want {
				t.Fatalf("key = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestALBConfiguredSamplingKey(t *testing.T) {
	ep := &ALBEventParser{samplerFields: []string{"domain_name", "elb_status_code"}}
	key, err := ep.samplingKey(event.Event{Data: map[string]interface{}{"domain_name": "example.com", "elb_status_code": int64(200), "elb": "ignored"}})
	if err != nil || key != `["example.com",200]` {
		t.Fatalf("configured key = %q, %v", key, err)
	}

	ep.samplerFields = []string{"a", "b"}
	keys := map[string]bool{}
	for _, data := range []map[string]interface{}{
		{"a": "a_b", "b": "c"},
		{"a": "a", "b": "b_c"},
		{"a": "a", "b": ""},
		{"a": "a"},
		{"a": int64(200)},
		{"a": "200"},
	} {
		key, err := ep.samplingKey(event.Event{Data: data})
		if err != nil {
			t.Fatal(err)
		}
		if keys[key] {
			t.Fatalf("sampling key collision: %q", key)
		}
		keys[key] = true
	}
}

func TestALBSamplingFieldsCopiedFromOptions(t *testing.T) {
	opt := &options.Options{SampleRate: 1, SamplerType: "simple", SamplerFields: []string{"domain_name"}}
	ep := NewALBEventParser(opt)
	defer ep.sampler.Stop()
	opt.SamplerFields[0] = "elb"
	if ep.samplerFields[0] != "domain_name" {
		t.Fatal("sampler fields share mutable options storage")
	}
}

type recordingALBSampler struct{ keys []string }

func (s *recordingALBSampler) Start() error { return nil }
func (s *recordingALBSampler) Stop() error  { return nil }
func (s *recordingALBSampler) GetSampleRate(key string) int {
	s.keys = append(s.keys, key)
	return 1
}
func (s *recordingALBSampler) GetSampleRateMulti(key string, count int) int {
	return s.GetSampleRate(key)
}
func (s *recordingALBSampler) SaveState() ([]byte, error)         { return nil, nil }
func (s *recordingALBSampler) LoadState([]byte) error             { return nil }
func (s *recordingALBSampler) GetMetrics(string) map[string]int64 { return nil }

func TestALBDynSampleUsesConfiguredKey(t *testing.T) {
	sampler := &recordingALBSampler{}
	ep := &ALBEventParser{sampler: sampler, samplerFields: []string{"domain_name"}}
	in, out := make(chan event.Event, 1), make(chan event.Event, 1)
	in <- event.Event{Data: map[string]interface{}{"domain_name": "example.com"}}
	close(in)
	ep.DynSample(in, out)
	if !reflect.DeepEqual(sampler.keys, []string{`["example.com"]`}) {
		t.Fatalf("unexpected sampler keys: %#v", sampler.keys)
	}
	if len(out) != 1 || (<-out).SampleRate != 1 {
		t.Fatal("sampled event lost or sample weight changed")
	}
}

func TestALBDynSampleRetainsUnencodableKey(t *testing.T) {
	sampler := &recordingALBSampler{}
	ep := &ALBEventParser{sampler: sampler, samplerFields: []string{"invalid"}}
	in, out := make(chan event.Event, 1), make(chan event.Event, 1)
	in <- event.Event{Data: map[string]interface{}{"invalid": make(chan int)}}
	close(in)
	ep.DynSample(in, out)
	if len(sampler.keys) != 0 || len(out) != 1 || (<-out).SampleRate != 1 {
		t.Fatal("unencodable key was sampled or dropped")
	}
}
