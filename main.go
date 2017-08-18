package main

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
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/honeycombio/honeyelb/options"
	"github.com/honeycombio/honeyelb/publisher"
	libhoney "github.com/honeycombio/libhoney-go"
	flag "github.com/jessevdk/go-flags"
)

const (
	// Somewhat arbitrary -- usually we see about 1200 objects per day.
	//
	// Important thing is that the length is capped and does not grow
	// indefinitely, old objects will not need de-dupe protection as they
	// will not be returned in the list objects results once 24 hours have
	// elapsed.
	maxProcessedObjects = 2000
)

var (
	// 2017-07-31T20:30:57.975041Z spline_reticulation_lb 10.11.12.13:47882 10.3.47.87:8080 0.000021 0.010962 0.000016 200 200 766 17 "PUT https://api.simulation.io:443/reticulate/spline/1 HTTP/1.1" "libhoney-go/1.3.3" ECDHE-RSA-AES128-GCM-SHA256 TLSv1.2
	accessLogConfig = []byte(`log_format aws_elb '$timestamp $elb $client_authority $backend_authority $request_processing_time $backend_processing_time $response_processing_time $elb_status_code $backend_status_code $received_bytes $sent_bytes "$request" "$user_agent" $ssl_cipher $ssl_protocol';`)
	stateFileFormat = "honeyelb-state-%s.json"
	opt             = &options.Options{}
	formatFileName  string
	BuildID         string
)

func init() {
	// Bootstrap this config file when the program starts up for usage by
	// the nginx parser.
	formatFile, err := ioutil.TempFile("", "honeytail_elb_access_log_format")
	if err != nil {
		logrus.Fatal(err)
	}

	if _, err := formatFile.Write(accessLogConfig); err != nil {
		logrus.Fatal(err)
	}

	if err := formatFile.Close(); err != nil {
		logrus.Fatal(err)
	}

	formatFileName = formatFile.Name()

	// init libhoney user agent properly
	libhoney.UserAgentAddition = "honeyelb/" + BuildID
}

//TODO: Would it be better to parse from a sample object path directly?
//TODO: write test and maybe return error also?
func userIDFromARN(arn string) string {
	splitARN := strings.Split(arn, ":")
	return splitARN[4]
}

func parseELBAccessEvents(elbLog string) error {
	// Open access log file for reading.
	logFile, err := os.Open(elbLog)
	if err != nil {
		return err
	}

	// TODO(nathanleclaire): We could probably just use a singleton
	// Publisher instance and have a publisher.Publish() method.
	hp := publisher.NewHoneycombPublisher(opt, formatFileName)

	// Publish will perform the scanning and send the events to Honeycomb.
	return hp.Publish(logFile)
}

func processObject(sess *session.Session, lbName, bucketName string, obj *s3.Object) error {
	// Backfill one hour backwards by default
	//
	// TODO(nathanleclaire): Make backfill interval configurable.
	if time.Since(*obj.LastModified) < time.Hour {
		objectRecord := strings.Replace(*obj.Key, "/", "_", -1)

		// Using LB name in file name is OK -- LB name "must have a
		// maximum of 32 characters, must contain only alphanumeric
		// characters or hyphens, and cannot begin or end with a
		// hyphen".
		lbStateFile := filepath.Join(opt.StateDir, fmt.Sprintf(stateFileFormat, lbName))

		if _, err := os.Stat(lbStateFile); os.IsNotExist(err) {
			// make sure file exists first run
			if err := ioutil.WriteFile(lbStateFile, []byte(`[]`), 0644); err != nil {
				return fmt.Errorf("Error writing file: %s", err)
			}
		}

		// Additionally, accessing this file is safe, since we use
		// 1-goroutine-per-ELB, therefore only one goroutine will be in
		// this critical section (per file) at a time.
		data, err := ioutil.ReadFile(lbStateFile)
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
			"lbName":        lbName,
		}).Info("Downloading access logs from object")

		f, err := ioutil.TempFile("", "hc-elb-ingest")
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
			"lbName": lbName,
		}).Info("Successfully downloaded object")

		if err := parseELBAccessEvents(f.Name()); err != nil {
			return fmt.Errorf("Error parsing access log file: %s", err)
		}

		logrus.WithField("dataset", opt.Dataset).Info("Finished sending events from access log to Honeycomb")

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

		if err := ioutil.WriteFile(lbStateFile, processedData, 0644); err != nil {
			return fmt.Errorf("Writing file failed: %s", err)
		}

		// Clean up the downloaded object.
		if err := os.Remove(f.Name()); err != nil {
			return fmt.Errorf("Error cleaning up downloaded object %s: %s", f.Name(), err)
		}
	}

	return nil
}

func accessLogBucketPageCallback(sess *session.Session, lbName, bucketName string, bucketResp *s3.ListObjectsOutput, lastPage bool) bool {
	sort.Slice(bucketResp.Contents, func(i, j int) bool {
		return (*bucketResp.Contents[i].LastModified).After(
			*bucketResp.Contents[j].LastModified,
		)
	})

	for _, obj := range bucketResp.Contents {
		if err := processObject(sess, lbName, bucketName, obj); err != nil {
			logrus.WithError(err).Error("Error processing bucket object")
		}
	}

	return !lastPage
}

func ingestLBLogs(sess *session.Session, lbName string) {
	logrus.WithFields(logrus.Fields{
		"lbName": lbName,
	}).Info("Attempting to ingest LB")

	elbSvc := elb.New(sess, nil)

	lbResp, err := elbSvc.DescribeLoadBalancerAttributes(&elb.DescribeLoadBalancerAttributesInput{
		LoadBalancerName: aws.String(lbName),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error describing load balancers: ", err)
		os.Exit(1)
	}

	accessLog := lbResp.LoadBalancerAttributes.AccessLog

	if !*accessLog.Enabled {
		fmt.Fprintf(os.Stderr, `Access logs are not configured for ELB %q. Please enable them to use the ingest tool.

For reference see this link:

http://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-access-logs.html#enable-access-logging
`, lbName)
		os.Exit(1)
	}

	bucketName := *accessLog.S3BucketName
	bucketPrefix := *accessLog.S3BucketPrefix

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"lbName": lbName,
	}).Info("Access logs are enabled for ELB ♥")

	// used to get account ID (needed to know the
	// bucket's object prefix)
	iamSvc := iam.New(sess, nil)

	userResp, err := iamSvc.GetUser(&iam.GetUserInput{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error trying to get account ID: ", err)
		os.Exit(1)
	}

	accountID := userIDFromARN(*userResp.User.Arn)
	region := *sess.Config.Region

	// get new logs every 5 minutes
	ticker := time.NewTicker(5 * time.Minute).C
	// Start the loop to continually ingest access logs.
	for {
		select {
		case <-ticker:
			// Converted into a string which also is used
			// for the object prefix
			nowPath := time.Now().UTC().Format("/2006/01/02")

			s3svc := s3.New(sess, nil)

			// For now, get objects for just today.
			totalPrefix := bucketPrefix + "/AWSLogs/" + accountID + "/elasticloadbalancing/" + region + nowPath

			logrus.WithFields(logrus.Fields{
				"prefix": totalPrefix,
				"lbName": lbName,
			}).Info("Getting recent objects")

			// Wrapper function used to satisfy the method signature of
			// ListObjectsPages and still pass additional parameters like
			// sess.
			cb := func(bucketResp *s3.ListObjectsOutput, lastPage bool) bool {
				return accessLogBucketPageCallback(sess, lbName, bucketName, bucketResp, lastPage)
			}

			if err := s3svc.ListObjectsPages(&s3.ListObjectsInput{
				Bucket: aws.String(bucketName),
				Prefix: aws.String(totalPrefix),
			}, cb); err != nil {
				fmt.Fprintln(os.Stderr, "Error listing/paging bucket objects: ", err)
				os.Exit(1)
			}
			logrus.Info("Pausing until the next set of logs are available")
		}
	}

}

func cmdELB(args []string) error {
	// TODO: Would be nice to have this more highly configurable.
	//
	// Will just use environment config right now, e.g., default profile.
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	elbSvc := elb.New(sess, nil)

	describeLBResp, err := elbSvc.DescribeLoadBalancers(&elb.DescribeLoadBalancersInput{})
	if err != nil {
		return fmt.Errorf("Error describing LBs: ", err)
		os.Exit(1)
	}

	if len(args) > 0 {
		switch args[0] {
		case "ls":
			for _, lb := range describeLBResp.LoadBalancerDescriptions {
				fmt.Println(*lb.LoadBalancerName)
			}

			return nil

		case "ingest":
			if opt.WriteKey == "" {
				logrus.Fatal(`--writekey must be set to the proper write key for the Honeycomb team.
Your write key is available at https://ui.honeycomb.io/account`)
			}

			lbNames := args[1:]

			// Use all available load balancers by default if none
			// are provided.
			if len(lbNames) == 0 {
				for _, lb := range describeLBResp.LoadBalancerDescriptions {
					lbNames = append(lbNames, *lb.LoadBalancerName)
				}
			}

			// For now, just run one goroutine per-LB
			for _, lbName := range lbNames {
				go ingestLBLogs(sess, lbName)
			}

			signalCh := make(chan os.Signal)

			// block forever (until interrupt)
			select {
			case <-signalCh:
				logrus.Info("Exiting due to interrupt.")
				// TODO(nathanleclaire): Cleanup before exiting.
				//
				// Mostly, wait for existing in-flight object
				// parsing / sending to finish so that state of
				// parsing "cursor" can be written to the JSON
				// file.
				os.Exit(0)
			}
		}
	}

	return fmt.Errorf("Subcommand %q not recognized", args[0])
}

func main() {
	flagParser := flag.NewParser(opt, flag.Default)
	args, err := flagParser.Parse()
	if err != nil {
		os.Exit(1)
	}

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, `Usage: `+os.Args[0]+` [--flags] [ls|ingest] [ELB names...]

Use '`+os.Args[0]+` --help' to see available flags.`)
		os.Exit(1)
	}

	if err := cmdELB(args); err != nil {
		fmt.Fprintln(os.Stderr, "Error: ", err)
		os.Exit(1)
	}
}
