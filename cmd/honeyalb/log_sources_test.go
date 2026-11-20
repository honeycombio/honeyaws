package main

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/honeycombio/honeyaws/options"
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

func TestValidateALBOptions(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		logType                    string
		accessRate, connectionRate int
		wantErr                    bool
	}{
		{"valid", "access", 1, 1, false},
		{"unknown log type", "unknown", 1, 1, true},
		{"zero access rate", "access", 0, 1, true},
		{"negative connection rate", "both", 20, -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateALBOptions(&options.Options{ALBLogType: tc.logType, SampleRate: tc.accessRate, ConnectionSampleRate: tc.connectionRate})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v; want error = %v", err, tc.wantErr)
			}
		})
	}
}
