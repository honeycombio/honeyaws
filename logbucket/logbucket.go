package logbucket

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/honeycombio/honeyelb/publisher"
	"compress/gzip"
)

const (
	// Somewhat arbitrary -- usually we see about 50 objects per hour in
	// Honeycomb dogfood data.
	//
	// Important thing is that the length is capped and does not grow
	// indefinitely, old objects will not need de-dupe protection as they
	// will not be returned in the list objects results once an hour has
	// elapsed.
	maxProcessedObjects = 2000

	AWSElasticLoadBalancing     = "elasticloadbalancing"
	AWSApplicationLoadBalancing = "elasticloadbalancingv2"
	AWSCloudFront               = ""
	AWSCloudTrail               = "CloudTrail"

	stateFileFormat = "%s-state-%s.json"
)

type ObjectDownloadParser struct {
	// The Publisher provides a way for the object downloaded parser to
	// publish the parsed events to Honeycomb.
	*publisher.HoneycombPublisher

	// The Service defines which AWS service we are downloading and parsing
	// objects for, e.g., 'elasticloadbalancing'. Provided by constants
	// starting with 'AWS' in this package.
	Service string

	// The Entity defines which specific sub-entity we are downloading and
	// parsing objects for -- e.g., CloudFront distribution name or ELB
	// load balancer name.
	Entity string

	// The directory in which to store files indicating the current state
	// of which objects have been processed.
	StateDir string
}

//TODO: write test and maybe return error also?
func userIDFromARN(arn string) string {
	splitARN := strings.Split(arn, ":")
	return splitARN[4]
}

func (o *ObjectDownloadParser) parseEvents(log string) error {
	// Open access log file for reading.
	logFile, err := os.Open(log)
	if err != nil {
		return err
	}

	gz, err := gzip.NewReader(logFile)
	if err != nil {
		return err
	}

	defer logFile.Close()
	defer gz.Close()

	// Publish will perform the scanning and send the events to Honeycomb.
	return o.Publish(gz)
}

func (o *ObjectDownloadParser) processObject(sess *session.Session, bucketName string, obj *s3.Object) error {
	// Backfill one hour backwards by default
	//
	// TODO(nathanleclaire): Make backfill interval configurable.
	if time.Since(*obj.LastModified) < time.Hour {
		objectRecord := strings.Replace(*obj.Key, "/", "_", -1)

		stateFile := filepath.Join(o.StateDir, fmt.Sprintf(stateFileFormat, o.Service, o.Entity))

		if _, err := os.Stat(stateFile); os.IsNotExist(err) {
			// make sure file exists first run
			if err := ioutil.WriteFile(stateFile, []byte(`[]`), 0644); err != nil {
				return fmt.Errorf("Error writing file: %s", err)
			}
		}

		// Accessing this file is safe, since we use
		// 1-goroutine-per-entity, therefore only one goroutine will be
		// in this critical section (per file) at a time.
		data, err := ioutil.ReadFile(stateFile)
		if err != nil {
			return fmt.Errorf("Error reading object cursor file: %s", err)
		}

		var processedObjects []string

		if err := json.Unmarshal(data, &processedObjects); err != nil {
			return fmt.Errorf("Unmarshalling state file JSON failed: %s", err)
		}

		for _, obj := range processedObjects {
			if obj == objectRecord {
				logrus.WithField("object", obj).Info("Already processed object, skipping.")
				return nil
			}
		}

		logrus.WithFields(logrus.Fields{
			"key":           *obj.Key,
			"size":          *obj.Size,
			"from_time_ago": time.Since(*obj.LastModified),
			"entity":        o.Entity,
		}).Info("Downloading access logs from object")

		f, err := ioutil.TempFile("", "hc-entity-ingest")
		if err != nil {
			return fmt.Errorf("Error creating tmp file: %s", err)
		}

		downloader := s3manager.NewDownloader(sess)

		nBytes, err := downloader.Download(f, &s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(*obj.Key),
		})
		if err != nil {
			return fmt.Errorf("Error downloading object file: %s", err)
		}

		if err := f.Close(); err != nil {
			return fmt.Errorf("Error closing downloaded object file: %s", err)
		}

		logrus.WithFields(logrus.Fields{
			"bytes":  nBytes,
			"file":   f.Name(),
			"entity": o.Entity,
		}).Info("Successfully downloaded object")

		if err := o.parseEvents(f.Name()); err != nil {
			return fmt.Errorf("Error parsing access log file: %s", err)
		}

		processedObjects = append(processedObjects, objectRecord)

		if len(processedObjects) > maxProcessedObjects {
			// "rotate" oldest remembered object out so state file
			// does not grow indefinitely
			processedObjects = processedObjects[1:]
		}

		processedData, err := json.Marshal(processedObjects)
		if err != nil {
			return fmt.Errorf("Marshalling JSON failed: %s", err)
		}

		if err := ioutil.WriteFile(stateFile, processedData, 0644); err != nil {
			return fmt.Errorf("Writing file failed: %s", err)
		}

		// Clean up the downloaded object.
		if err := os.Remove(f.Name()); err != nil {
			return fmt.Errorf("Error cleaning up downloaded object %s: %s", f.Name(), err)
		}
	}

	return nil
}

func (o *ObjectDownloadParser) accessLogBucketPageCallback(sess *session.Session, bucketName string, bucketResp *s3.ListObjectsOutput, lastPage bool) bool {
	logrus.WithFields(logrus.Fields{
		"bucket_name": bucketName,
		"num_objects": len(bucketResp.Contents),
	}).Debug("Executing bucket callback")

	// TODO: This sort doesn't work as originally intended if the paging
	// comes into play. Consider removing, or gathering all desired objects
	// as a result of the callback, _then_ sorting and iterating over them.
	sort.Slice(bucketResp.Contents, func(i, j int) bool {
		return (*bucketResp.Contents[i].LastModified).After(
			*bucketResp.Contents[j].LastModified,
		)
	})

	for _, obj := range bucketResp.Contents {
		if err := o.processObject(sess, bucketName, obj); err != nil {
			logrus.WithError(err).Error("Error processing bucket object")
		}
	}

	return !lastPage
}

func (o *ObjectDownloadParser) TotalPrefix(bucketPrefix, accountID, region string) string {
	if bucketPrefix != "" {
		// Add seperator slash so concatenation makes sense.
		bucketPrefix += "/"
	}

	// Converted into a string which also is used for the object prefix
	nowPath := time.Now().UTC().Format("/2006/01/02")

	// For now, get objects for just today.
	return bucketPrefix + "AWSLogs/" + accountID + "/" + o.Service + "/" + region + nowPath +
		"/" + accountID + "_" + o.Service + "_" + region + "_" + o.Entity
}

func (o *ObjectDownloadParser) Ingest(sess *session.Session, bucketName, bucketPrefix string) {
	// used to get account ID (needed to know the
	// bucket's object prefix)
	stsClient := sts.New(sess)
	req, userResp := stsClient.GetCallerIdentityRequest(&sts.GetCallerIdentityInput{})
	if err := req.Send(); err != nil {
		fmt.Fprintln(os.Stderr, "Error trying to get account ID: ", err)
		os.Exit(1)
	}

	accountID := userIDFromARN(*userResp.Arn)
	region := *sess.Config.Region

	// get new logs every 5 minutes
	ticker := time.NewTicker(5 * time.Minute).C
	// Start the loop to continually ingest access logs.
	for {
		s3svc := s3.New(sess, nil)

		totalPrefix := o.TotalPrefix(bucketPrefix, accountID, region)

		logrus.WithFields(logrus.Fields{
			"prefix": totalPrefix,
			"entity": o.Entity,
		}).Info("Getting recent objects")

		// Wrapper function used to satisfy the method signature of
		// ListObjectsPages and still pass additional parameters like
		// sess.
		cb := func(bucketResp *s3.ListObjectsOutput, lastPage bool) bool {
			return o.accessLogBucketPageCallback(sess, bucketName, bucketResp, lastPage)
		}

		if err := s3svc.ListObjectsPages(&s3.ListObjectsInput{
			Bucket: aws.String(bucketName),
			Prefix: aws.String(totalPrefix),
		}, cb); err != nil {
			fmt.Fprintln(os.Stderr, "Error listing/paging bucket objects: ", err)
			os.Exit(1)
		}
		logrus.Info("Pausing until the next set of logs are available")
		<-ticker
	}
}
