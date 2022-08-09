package logbucket

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/honeycombio/honeyaws/meta"
	"github.com/honeycombio/honeyaws/state"
	"github.com/sirupsen/logrus"
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

	// ObjectPrefix allows the downloader to efficiently lookup objects
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
	Sess               *session.Session
	DownloadedObjects  chan state.DownloadedObject
	ObjectsToDownload  chan *s3.Object
	BackfillInterval   time.Duration
	ConcurrencyLimiter ConcurrencyLimiter
}

func NewDownloader(sess *session.Session, stater state.Stater, downloader ObjectDownloader, backfill int) *Downloader {
	return &Downloader{
		Stater:            stater,
		ObjectDownloader:  downloader,
		Sess:              sess,
		DownloadedObjects: make(chan state.DownloadedObject),
		ObjectsToDownload: make(chan *s3.Object),
		BackfillInterval:  time.Hour * time.Duration(backfill),
		// retain unlimited request concurrency by default
		ConcurrencyLimiter: NoLimits{},
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
	Prefix, BucketName, AccountID, Region, TrailID, OrgID string
}

func NewCloudTrailDownloader(accountID, region, bucketName, bucketPrefix, trailID, orgID string) *CloudTrailDownloader {
	return &CloudTrailDownloader{
		AccountID:  accountID,
		Region:     region,
		BucketName: bucketName,
		Prefix:     bucketPrefix,
		TrailID:    trailID,
		OrgID:      orgID,
	}

}

// ObjectPrefix handles formatting the Trail log s3 path properly,
// including the optional user-created prefix, and the organization
// unit ID used for Organization Cloud Trail logs if provided:
// https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-find-log-files.html
func (d *CloudTrailDownloader) ObjectPrefix(day time.Time) string {
	dayPath := day.Format("2006/01/02")
	return filepath.Join(d.Prefix, "AWSLogs", d.OrgID, d.AccountID, "CloudTrail",
		d.Region, dayPath, d.AccountID+"_CloudTrail_"+d.Region)
}

func (d *CloudTrailDownloader) String() string {
	return d.TrailID
}

func (d *CloudTrailDownloader) Bucket() string {
	return d.BucketName
}

func NewCloudFrontDownloader(bucketName, bucketPrefix, distID string) *CloudFrontDownloader {
	return &CloudFrontDownloader{
		BucketName:     bucketName,
		Prefix:         bucketPrefix,
		DistributionID: distID,
	}
}

func (d *CloudFrontDownloader) ObjectPrefix(day time.Time) string {
	dayPath := day.Format("2006-01-02")
	return filepath.Join(d.Prefix, d.DistributionID+"."+dayPath)
}

func (d *CloudFrontDownloader) String() string {
	return d.DistributionID
}

func (d *CloudFrontDownloader) Bucket() string {
	return d.BucketName
}
func NewELBDownloader(sess *session.Session, bucketName, bucketPrefix, lbName string) *ELBDownloader {
	metadata := meta.Data(sess)
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
	return filepath.Join(d.Prefix, "AWSLogs", d.AccountID, AWSElasticLoadBalancing, d.Region, dayPath,
		d.AccountID+"_"+AWSElasticLoadBalancing+"_"+d.Region+"_"+d.LBName)
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
	return filepath.Join(d.Prefix, "AWSLogs/", d.AccountID, AWSElasticLoadBalancing, d.Region+dayPath,
		d.AccountID+"_"+AWSElasticLoadBalancing+"_"+d.Region+"_app."+d.LBName)
}

// UseConcurrencyLimiting sets a concurrency limiter for the downloader
// to optionally protect the requestee service from highly concurrent requests,
// which could trigger rate-limiting
func (d *Downloader) UseConcurrencyLimiting(limiter ConcurrencyLimiter) {
	d.ConcurrencyLimiter = limiter
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
		// TODO(2022.08.08): at time of writing only
		// honeycloudtrail passes through a concurrency limiter
		d.ConcurrencyLimiter.Acquire()
		if err := d.downloadObject(obj); err != nil {
			logrus.Error(err)
		}
		d.ConcurrencyLimiter.Release()
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
			logrus.Debugf("Consuming object of age %s", time.Since(*obj.LastModified))
			// Note: this marks an object as processed even if the download fails
			if err := d.SetProcessed(*obj.Key); err != nil {
				logrus.Debug("Error setting state of object as processed: ", *obj.Key)
				continue
			}
			// we want to set the object as processed as
			// soon as it's ready to downloaded
			// to avoid duplicates in downloading
			d.ObjectsToDownload <- obj
		}
	}

	logrus.WithField("lastPage", lastPage).Debug("End S3 bucket page")

	return true
}
func (d *Downloader) pollObjects() {
	// get new logs every 5 minutes
	ticker := time.NewTicker(5 * time.Minute).C

	logrus.Debugf("Polling objects in region %s for %s", *d.Sess.Config.Region, d.ObjectDownloader.ObjectPrefix(time.Now().UTC()))
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

type ConcurrencyLimit struct {
	concurrency      int
	requestSemaphore chan interface{}
}

// NewConcurrencyLimit creates a new concurrency limiter
// with the provided limit
func NewConcurrencyLimit(limit int) *ConcurrencyLimit {
	requestSem := make(chan interface{}, limit)
	return &ConcurrencyLimit{
		limit,
		requestSem,
	}
}

// Acquire waits for a slot in the request semaphore to free up
func (c *ConcurrencyLimit) Acquire() {
	start := time.Now()
	c.requestSemaphore <- struct{}{}
	waited := time.Since(start)
	logrus.WithField("concurrency_limit", c.concurrency).
		Debugf("Waited %d ms to make request", waited.Milliseconds())
}

// Release releases the claim to the semaphore
func (c *ConcurrencyLimit) Release() {
	<-c.requestSemaphore
}

// NoLimits is a no-op that does no concurrency limiting
type NoLimits struct{}

func (n NoLimits) Acquire() {}
func (n NoLimits) Release() {}

type ConcurrencyLimiter interface {
	Acquire()
	Release()
}
