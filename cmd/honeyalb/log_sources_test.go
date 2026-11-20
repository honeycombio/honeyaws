package main

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/honeycombio/honeyaws/options"
	flag "github.com/jessevdk/go-flags"
)

func logAttributes(values map[string]string) []*elbv2.LoadBalancerAttribute {
	var attributes []*elbv2.LoadBalancerAttribute
	for key, value := range values {
		attributes = append(attributes, &elbv2.LoadBalancerAttribute{Key: aws.String(key), Value: aws.String(value)})
	}
	return attributes
}

func TestALBLogSources(t *testing.T) {
	attributes := logAttributes(map[string]string{
		"access_logs.s3.enabled": "true", "access_logs.s3.bucket": "access-bucket", "access_logs.s3.prefix": "access",
		"connection_logs.s3.enabled": "true", "connection_logs.s3.bucket": "connection-bucket", "connection_logs.s3.prefix": "connection",
	})
	for _, tc := range []struct {
		kind string
		want []albLogSource
	}{
		{"access", []albLogSource{{"access", "access-bucket", "access"}}},
		{"connection", []albLogSource{{"connection", "connection-bucket", "connection"}}},
		{"both", []albLogSource{{"access", "access-bucket", "access"}, {"connection", "connection-bucket", "connection"}}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			got, err := albLogSources(attributes, tc.kind)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("sources = %#v, %v; want %#v", got, err, tc.want)
			}
		})
	}
	connectionOnly := logAttributes(map[string]string{"connection_logs.s3.enabled": "true", "connection_logs.s3.bucket": "bucket"})
	if _, err := albLogSources(connectionOnly, "connection"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"access", "both", "unknown"} {
		if _, err := albLogSources(connectionOnly, kind); err == nil {
			t.Errorf("accepted missing or unsupported log type %q", kind)
		}
	}
	if _, err := albLogSources(logAttributes(map[string]string{"connection_logs.s3.enabled": "true"}), "connection"); err == nil {
		t.Fatal("accepted missing bucket")
	}
	accessOnly := logAttributes(map[string]string{"access_logs.s3.enabled": "true", "access_logs.s3.bucket": "bucket", "connection_logs.s3.enabled": "false", "connection_logs.s3.bucket": "bucket"})
	for _, kind := range []string{"connection", "both"} {
		if _, err := albLogSources(accessOnly, kind); err == nil {
			t.Errorf("accepted disabled connection logging for %q", kind)
		}
	}
}

func TestALBLogFlags(t *testing.T) {
	for _, args := range [][]string{nil, {"--alb_log_type=both", "--samplerate=20", "--connection_samplerate=10", "--keep_4xx", "--connection_sampler_fields=elb"}} {
		opt := &options.Options{}
		if _, err := flag.NewParser(opt, flag.None).ParseArgs(args); err != nil {
			t.Fatal(err)
		}
		if err := validateALBOptions(opt); err != nil {
			t.Fatal(err)
		}
		if len(args) == 0 && (opt.ALBLogType != "access" || opt.ConnectionSampleRate != 1) {
			t.Fatalf("unexpected defaults: %#v", opt)
		}
		if len(args) > 0 && (opt.ALBLogType != "both" || opt.ConnectionSampleRate != 10 || !opt.Keep4xx || !reflect.DeepEqual(opt.ConnectionSamplerFields, []string{"elb"})) {
			t.Fatalf("flags not applied: %#v", opt)
		}
	}
	for _, args := range [][]string{{"--alb_log_type=unknown"}, {"--samplerate=0"}, {"--connection_samplerate=-1"}} {
		opt := &options.Options{}
		_, err := flag.NewParser(opt, flag.None).ParseArgs(args)
		if err == nil {
			err = validateALBOptions(opt)
		}
		if err == nil {
			t.Fatalf("accepted invalid flags %v", args)
		}
	}
}
