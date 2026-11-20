package main

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/honeycombio/honeyaws/options"
)

type albLogSource struct {
	logType, bucket, prefix string
}

func validateALBOptions(opt *options.Options) error {
	if opt.ALBLogType != "access" && opt.ALBLogType != "connection" && opt.ALBLogType != "both" {
		return fmt.Errorf("--alb_log_type must be access, connection, or both")
	}
	if opt.SampleRate < 1 || opt.ConnectionSampleRate < 1 {
		return fmt.Errorf("--samplerate and --connection_samplerate must be positive")
	}
	return nil
}

func albLogSources(attributes []*elbv2.LoadBalancerAttribute, logType string) ([]albLogSource, error) {
	values := make(map[string]string, len(attributes))
	for _, attribute := range attributes {
		if attribute != nil {
			values[aws.StringValue(attribute.Key)] = aws.StringValue(attribute.Value)
		}
	}
	var kinds []string
	switch logType {
	case "access", "connection":
		kinds = []string{logType}
	case "both":
		kinds = []string{"access", "connection"}
	default:
		return nil, fmt.Errorf("unsupported ALB log type %q", logType)
	}
	sources := make([]albLogSource, 0, len(kinds))
	for _, kind := range kinds {
		key := kind + "_logs.s3."
		if values[key+"enabled"] != "true" || values[key+"bucket"] == "" {
			return nil, fmt.Errorf("%s logs are not configured; enable %s_logs.s3 before ingesting them", kind, kind)
		}
		sources = append(sources, albLogSource{logType: kind, bucket: values[key+"bucket"], prefix: values[key+"prefix"]})
	}
	return sources, nil
}
