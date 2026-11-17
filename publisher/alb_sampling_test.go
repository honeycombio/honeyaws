package publisher

import (
	"testing"

	"github.com/honeycombio/honeyaws/options"
	"github.com/honeycombio/honeytail/event"
)

func TestALBDefaultSamplingKey(t *testing.T) {
	parser := &ALBEventParser{}
	for _, testCase := range []struct {
		name string
		data map[string]interface{}
		want string
	}{
		{"complete", map[string]interface{}{"backend_status_code": int64(200), "elb_status_code": int64(200), "elb": "app/example/id"}, "200_200_app/example/id"},
		{"missing backend", map[string]interface{}{"elb_status_code": int64(503), "elb": "lb"}, "_503_lb"},
		{"unavailable backend", map[string]interface{}{"backend_status_code": "-", "elb_status_code": int64(503), "elb": "lb"}, "0_503_lb"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			key, err := parser.samplingKey(event.Event{Data: testCase.data})
			if err != nil || key != testCase.want {
				t.Fatalf("key = %q, %v; want %q", key, err, testCase.want)
			}
		})
	}
}

func TestALBConfiguredSamplingKey(t *testing.T) {
	for _, samplerType := range []string{"simple", "ema"} {
		t.Run(samplerType, func(t *testing.T) {
			for _, testCase := range []struct {
				name   string
				fields []string
				data   map[string]interface{}
				want   string
			}{
				{"selected fields", []string{"domain_name", "elb_status_code"}, map[string]interface{}{"domain_name": "example.com", "elb_status_code": int64(200), "elb": "app/example/id"}, `["example.com",200]`},
				{"field order", []string{"elb_status_code", "domain_name"}, map[string]interface{}{"domain_name": "example.com", "elb_status_code": int64(200)}, `[200,"example.com"]`},
				{"missing field", []string{"domain_name", "elb_status_code"}, map[string]interface{}{"elb_status_code": int64(200)}, `[null,200]`},
				{"empty field", []string{"domain_name", "elb_status_code"}, map[string]interface{}{"domain_name": "", "elb_status_code": int64(200)}, `["",200]`},
			} {
				t.Run(testCase.name, func(t *testing.T) {
					parser := NewALBEventParser(&options.Options{
						SampleRate:    1,
						SamplerType:   samplerType,
						SamplerFields: testCase.fields,
					})
					defer parser.sampler.Stop()
					key, err := parser.samplingKey(event.Event{Data: testCase.data})
					if err != nil || key != testCase.want {
						t.Fatalf("key = %q, %v; want %q", key, err, testCase.want)
					}
				})
			}
		})
	}
}
