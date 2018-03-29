package logbucket

import (
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/honeycombio/honeyaws/meta"
	"github.com/honeycombio/honeyaws/state"
)

const (
	AWSElasticLoadBalancing   = "elasticloadbalancing"
	AWSElasticLoadBalancingV2 = "elasticloadbalancingv2"
	AWSCloudFront             = "cloudfront"
	AWSCloudTrail             = "cloudtrail"
	alb                       = "alb"
	elb                       = "elb"
)

type ObjectDownloader interface {
	fmt.Stringer

	// ObjectPrefix allows the downloaded to efficiently lookup objects
	// based on prefix (unique to each AWS service).
	ObjectPrefix(day time.Time) string

	// Bucket will return the name of the bucket we are downloading the
	// objects from
	Bucket() string
}

// Wrapper struct used to unite the specific structs with common methods.
type Downloader struct {
	state.Stater
	ObjectDownloader
	Sess              *session.Session
	DownloadedObjects chan state.DownloadedObject
	ObjectsToDownload chan *s3.Object
	BackfillInterval  time.Duration
}

func NewDownloader(sess *session.Session, stater state.Stater, downloader ObjectDownloader, backfill int) *Downloader {
	return &Downloader{
		Stater:            stater,
		ObjectDownloader:  downloader,
		Sess:              sess,
		DownloadedObjects: make(chan state.DownloadedObject),
		ObjectsToDownload: make(chan *s3.Object),
		BackfillInterval:  time.Hour * time.Duration(backfill),
	}
}

type ELBDownloader struct {
	Prefix, BucketName, AccountID, Region, LBName, LBType string
}

type ALBDownloader struct {
	*ELBDownloader
}

type CloudFrontDownloader struct {
	Prefix, BucketName, DistributionID string
}

type CloudTrailDownloader struct {
	Prefix, BucketName, AccountID, Region, TrailID string
}

func NewCloudTrailDownloader(sess *session.Session, bucketName, bucketPrefix, trailID string) *CloudTrailDownloader {
	metadata := meta.Data(sess)

	// If the user specified a prefix for the access logs in the bucket,
	// set "/" as the prefix (otherwise the leading/root slash will be
	// mising).
	if bucketPrefix != "" {
		bucketPrefix += "/"
	}

	return &CloudTrailDownloader{
		AccountID:  metadata.AccountID,
		Region:     metadata.Region,
		BucketName: bucketName,
		Prefix:     bucketPrefix,
		TrailID:    trailID,
	}

}

func (d *CloudTrailDownloader) ObjectPrefix(day time.Time) string {
	dayPath := day.Format("2006/01/02")
	return d.Prefix + "AWSLogs/" + d.AccountID + "/" + "CloudTrail" + "/" + d.Region + "/" + dayPath + "/" + d.AccountID + "_CloudTrail_" + d.Region
}

func (d *CloudTrailDownloader) String() string {
	return d.TrailID
}

func (d *CloudTrailDownloader) Bucket() string {
	return d.BucketName
}

func NewCloudFrontDownloader(bucketName, bucketPrefix, distID string) *CloudFrontDownloader {
	if bucketPrefix != "" {
		bucketPrefix += "/"
	}
	return &CloudFrontDownloader{
		BucketName:     bucketName,
		Prefix:         bucketPrefix,
		DistributionID: distID,
	}
}

func (d *CloudFrontDownloader) ObjectPrefix(day time.Time) string {
	dayPath := day.Format("2006-01-02")
	return d.Prefix + d.DistributionID + "." + dayPath
}

func (d *CloudFrontDownloader) String() string {
	return d.DistributionID
}

func (d *CloudFrontDownloader) Bucket() string {
	return d.BucketName
}
func NewELBDownloader(sess *session.Session, bucketName, bucketPrefix, lbName string) *ELBDownloader {
	metadata := meta.Data(sess)

	// If the user specified a prefix for the access logs in the bucket,
	// set "/" as the prefix (otherwise the leading/root slash will be
	// mising).
	if bucketPrefix != "" {
		bucketPrefix += "/"
	}
	return &ELBDownloader{
		AccountID:  metadata.AccountID,
		Region:     metadata.Region,
		BucketName: bucketName,
		Prefix:     bucketPrefix,
		LBName:     lbName,
	}
}

// pass in time.Now().UTC()
func (d *ELBDownloader) ObjectPrefix(day time.Time) string {
	dayPath := day.Format("/2006/01/02")
	return d.Prefix + "AWSLogs/" + d.AccountID + "/" + AWSElasticLoadBalancing + "/" + d.Region + dayPath +
		"/" + d.AccountID + "_" + AWSElasticLoadBalancing + "_" + d.Region + "-" + d.LBName
}

func (d *ELBDownloader) String() string {
	return d.LBName
}

func (d *ELBDownloader) Bucket() string {
	return d.BucketName
}

func NewALBDownloader(sess *session.Session, bucketName, bucketPrefix, lbName string) *ALBDownloader {
	return &ALBDownloader{NewELBDownloader(sess, bucketName, bucketPrefix, lbName)}
}

func (d *ALBDownloader) ObjectPrefix(day time.Time) string {
	dayPath := day.Format("/2006/01/02")
	return d.Prefix + "AWSLogs/" + d.AccountID + "/" + AWSElasticLoadBalancing + "/" + d.Region + dayPath +
		"/" + d.AccountID + "_" + AWSElasticLoadBalancing + "_" + d.Region + "_app." + d.LBName
}

func (d *Downloader) downloadObject(obj *s3.Object) error {
	logrus.WithFields(logrus.Fields{
		"key":           *obj.Key,
		"size":          *obj.Size,
		"from_time_ago": time.Since(*obj.LastModified),
		"entity":        d.String(),
	}).Info("Downloading access logs from object")

	f, err := ioutil.TempFile("", "hc-entity-ingest")
	if err != nil {
		return fmt.Errorf("Error creating tmp file: %s", err)
	}

	downloader := s3manager.NewDownloader(d.Sess)

	nBytes, err := downloader.Download(f, &s3.GetObjectInput{
		Bucket: aws.String(d.Bucket()),
		Key:    aws.String(*obj.Key),
	})
	if err != nil {
		return fmt.Errorf("Error downloading object file: %s", err)
	}
	logrus.WithFields(logrus.Fields{
		"bytes":  nBytes,
		"file":   f.Name(),
		"entity": d.String(),
	}).Info("Successfully downloaded object")

	d.DownloadedObjects <- state.DownloadedObject{
		Filename: f.Name(),
		Object:   *obj.Key,
	}

	return nil
}

func (d *Downloader) downloadObjects() {
	for obj := range d.ObjectsToDownload {
		if err := d.downloadObject(obj); err != nil {
			logrus.Error(err)
		}
		// TODO: Should we sleep in between downloads here? Watching
		// many load balancers concurrently could potentially result in
		// many downloads attempting to go off at once, and
		// consequently getting rate limited by AWS.
	}
}

func (d *Downloader) accessLogBucketPageCallback(processedObjects map[string]time.Time, bucketResp *s3.ListObjectsOutput, lastPage bool) bool {
	logrus.WithFields(logrus.Fields{
		"objects":   len(bucketResp.Contents),
		"truncated": *bucketResp.IsTruncated,
	}).Debug("Start S3 bucket page")
	for _, obj := range bucketResp.Contents {
		_, ok := processedObjects[*obj.Key]

		if ok {
			logrus.WithField("object", *obj.Key).Debug("Already processed, skipping")
			continue
		}

		if time.Since(*obj.LastModified) < d.BackfillInterval {
			d.ObjectsToDownload <- obj
		}
	}

	logrus.WithField("lastPage", lastPage).Debug("End S3 bucket page")

	return true
}
func (d *Downloader) pollObjects() {
	// get new logs every 5 minutes
	ticker := time.NewTicker(5 * time.Minute).C

	s3svc := s3.New(d.Sess, nil)

	// Start the loop to continually ingest access logs.
	for {
		// For now, get objects for just today.
		totalPrefix := d.ObjectPrefix(time.Now().UTC())

		logrus.WithFields(logrus.Fields{
			"prefix": totalPrefix,
			"entity": d.String(),
		}).Info("Getting recent objects")

		processedObjects, err := d.ProcessedObjects()
		if err != nil {
			logrus.Error(err)
		}

		cb := func(bucketResp *s3.ListObjectsOutput, lastPage bool) bool {
			return d.accessLogBucketPageCallback(processedObjects, bucketResp, lastPage)
		}

		if err := s3svc.ListObjectsPages(&s3.ListObjectsInput{
			Bucket: aws.String(d.Bucket()),
			Prefix: aws.String(totalPrefix),
		}, cb); err != nil {
			fmt.Fprintln(os.Stderr, "Error listing/paging bucket objects: ", err)
			os.Exit(1)
		}
		logrus.WithField("entity", d.String()).Info("Bucket polling paused until the next set of logs are available")
		<-ticker
	}
}

func (d *Downloader) Download(downloadedObjects chan state.DownloadedObject) {
	d.DownloadedObjects = downloadedObjects
	go d.pollObjects()
	go d.downloadObjects()
}
