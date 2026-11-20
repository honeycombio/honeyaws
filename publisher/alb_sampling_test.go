package publisher

import (
	"testing"

	dynsampler "github.com/honeycombio/dynsampler-go"
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
		{"connection", map[string]interface{}{"log_type": "connection", "tls_verify_status": "Success", "tls_protocol": "TLSv1.3", "elb": "app/example/id"}, `["Success","TLSv1.3","app/example/id"]`},
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

type albSamplingRecorder struct {
	dynsampler.Sampler
	keys []string
	rate int
}

func (s *albSamplingRecorder) GetSampleRate(key string) int {
	s.keys = append(s.keys, key)
	return s.rate
}

func TestALBIndependentSampling(t *testing.T) {
	access, connection := &albSamplingRecorder{rate: 1}, &albSamplingRecorder{rate: 1}
	parser := &ALBEventParser{sampler: access, connectionSampler: connection,
		samplerFields: []string{"elb_status_code"}, connectionSamplerFields: []string{"tls_protocol"}}
	in, out := make(chan event.Event, 2), make(chan event.Event, 2)
	in <- event.Event{Data: map[string]interface{}{"log_type": "access", "elb_status_code": int64(200), "conn_trace_id": "TID_shared"}}
	in <- event.Event{Data: map[string]interface{}{"log_type": "connection", "tls_protocol": "TLSv1.3", "tls_verify_status": "Success", "conn_trace_id": "TID_shared"}}
	close(in)
	parser.DynSample(in, out)
	if len(access.keys) != 1 || access.keys[0] != "[200]" || len(connection.keys) != 1 || connection.keys[0] != `["TLSv1.3"]` {
		t.Fatalf("wrong sampler routing: access=%v connection=%v", access.keys, connection.keys)
	}
	for range 2 {
		if ev := <-out; ev.SampleRate != 1 {
			t.Fatalf("sample rate = %d; want 1", ev.SampleRate)
		}
	}
}

func TestALBRetainsErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    map[string]interface{}
		keep4xx bool
		keep    bool
	}{
		{"ALB 5xx", map[string]interface{}{"elb_status_code": int64(503)}, false, true},
		{"target 5xx", map[string]interface{}{"backend_status_code": int64(500)}, false, true},
		{"reported error", map[string]interface{}{"error_reason": "AuthInvalidCookie"}, false, true},
		{"integer timing failure", map[string]interface{}{"backend_processing_time": int64(-1)}, false, true},
		{"float timing failure", map[string]interface{}{"response_processing_time": float64(-1)}, false, true},
		{"TLS failure", map[string]interface{}{"log_type": "connection", "tls_verify_status": "Failed:ClientCertUntrusted"}, false, true},
		{"ALB 4xx sampled", map[string]interface{}{"elb_status_code": int64(403)}, false, false},
		{"ALB 4xx retained", map[string]interface{}{"elb_status_code": int64(403)}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access, connection := &albSamplingRecorder{rate: 100}, &albSamplingRecorder{rate: 100}
			parser := &ALBEventParser{sampler: access, connectionSampler: connection, keep4xx: tc.keep4xx}
			in, out := make(chan event.Event, 1), make(chan event.Event, 1)
			in <- event.Event{Data: tc.data}
			close(in)
			parser.DynSample(in, out)
			if tc.keep {
				if len(out) != 1 {
					t.Fatalf("retained %d errors; want 1", len(out))
				}
				if ev := <-out; ev.SampleRate != 1 {
					t.Fatalf("error sample rate = %d; want 1", ev.SampleRate)
				}
			}

			calls := len(access.keys) + len(connection.keys)
			if tc.keep && calls != 0 || !tc.keep && calls != 1 {
				t.Fatalf("sampler calls = %d; bypass = %v", calls, tc.keep)
			}
		})
	}
}

func TestALBSamplerTargets(t *testing.T) {
	for _, kind := range []string{"simple", "ema"} {
		t.Run(kind, func(t *testing.T) {
			parser := NewALBEventParser(&options.Options{ALBLogType: "both", SamplerType: kind, SampleRate: 20, ConnectionSampleRate: 5})
			defer parser.sampler.Stop()
			defer parser.connectionSampler.Stop()
			if got := parser.sampler.GetSampleRate("access"); got != 20 {
				t.Fatalf("access sample rate = %d; want 20", got)
			}
			if got := parser.connectionSampler.GetSampleRate("connection"); got != 5 {
				t.Fatalf("connection sample rate = %d; want 5", got)
			}
		})
	}
}

func TestALBConnectionDefaultRetainsAll(t *testing.T) {
	parser := NewALBEventParser(&options.Options{ALBLogType: "both", SamplerType: "simple", SampleRate: 20, ConnectionSampleRate: 1})
	defer parser.sampler.Stop()
	in, out := make(chan event.Event, 2), make(chan event.Event, 2)
	in <- event.Event{Data: map[string]interface{}{"log_type": "connection", "tls_verify_status": "Success", "tls_protocol": "TLSv1.3"}}
	in <- event.Event{Data: map[string]interface{}{"log_type": "connection", "tls_verify_status": "Success", "tls_protocol": "TLSv1.2"}}
	close(in)
	parser.DynSample(in, out)
	if len(out) != 2 {
		t.Fatalf("retained %d connections; want 2", len(out))
	}
	for range 2 {
		if ev := <-out; ev.SampleRate != 1 {
			t.Fatalf("sample rate = %d; want 1", ev.SampleRate)
		}
	}
}

func TestALBConfiguredSamplingKey(t *testing.T) {
	for _, tc := range []struct {
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
		t.Run(tc.name, func(t *testing.T) {
			parser := &ALBEventParser{samplerFields: tc.fields}
			key, err := parser.samplingKey(event.Event{Data: tc.data})
			if err != nil || key != tc.want {
				t.Fatalf("key = %q, %v; want %q", key, err, tc.want)
			}
		})
	}
}
