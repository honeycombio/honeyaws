package logbucket

import (
	"log"
	"testing"
	"time"
)

func TestObjectPrefixes(t *testing.T) {
	testCases := []struct {
		downloader   ObjectDownloader
		lookupPrefix string
	}{
		{&ELBDownloader{
			AccountID:  "12345",
			Region:     "us-east-1",
			BucketName: "mylogs",
			Prefix:     "",
			LBName:     "service1",
		}, "AWSLogs/12345/elasticloadbalancing/us-east-1/2018/08/20/12345_elasticloadbalancing_us-east-1_service1"},
		{&ALBDownloader{
			ELBDownloader: &ELBDownloader{
				AccountID:  "12345",
				Region:     "us-east-1",
				BucketName: "mylogs",
				Prefix:     "noslash",
				LBName:     "service1",
			},
		}, "noslash/AWSLogs/12345/elasticloadbalancing/us-east-1/2018/08/20/12345_elasticloadbalancing_us-east-1_app.service1"},
		{&CloudFrontDownloader{
			BucketName:     "mylogs",
			Prefix:         "trailingslash/",
			DistributionID: "MADEUP8218912",
		}, "trailingslash/MADEUP8218912.2018-08-20"},
		{&CloudTrailDownloader{
			AccountID:  "12345",
			Region:     "us-east-1",
			BucketName: "mylogs",
			Prefix:     "",
			TrailID:    "MADEUP0",
		}, "AWSLogs/12345/CloudTrail/us-east-1/2018/08/20/12345_CloudTrail_us-east-1"},
		{&CloudTrailDownloader{
			AccountID:  "12345",
			Region:     "us-east-1",
			BucketName: "myorganizationlogs",
			Prefix:     "",
			TrailID:    "MADEUP0",
			OrgID:      "o-FakeOrgID",
		}, "AWSLogs/o-FakeOrgID/12345/CloudTrail/us-east-1/2018/08/20/12345_CloudTrail_us-east-1"},
	}

	for _, testCase := range testCases {
		prefix := testCase.downloader.ObjectPrefix(time.Date(2018, time.August, 20, 23, 0, 0, 0, time.UTC))
		if prefix != testCase.lookupPrefix {
			t.Errorf("lookupPrefix did not match:\n(expected)\t%s(actual)\t%s", testCase.lookupPrefix, prefix)
		}
		log.Print(prefix)
	}
}
