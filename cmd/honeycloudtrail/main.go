package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudtrail"
	"github.com/honeycombio/honeyaws/logbucket"
	"github.com/honeycombio/honeyaws/meta"
	"github.com/honeycombio/honeyaws/options"
	"github.com/honeycombio/honeyaws/publisher"
	"github.com/honeycombio/honeyaws/state"
	libhoney "github.com/honeycombio/libhoney-go"
	flag "github.com/jessevdk/go-flags"
	"github.com/sirupsen/logrus"
)

var (
	opt        = &options.Options{}
	BuildID    string
	versionStr string
)

func init() {
	// set the version string to our desired format
	if BuildID == "" {
		versionStr = "dev"
	} else {
		versionStr = BuildID
	}

	// init libhoney user agent properly
	libhoney.UserAgentAddition = "honeycloudtrail/" + versionStr
}

func cmdCloudTrail(args []string) error {
	// TODO: Would be nice to have this more highly configurable.
	//
	// Start with default profile.
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	// Assume role if set
	if roleArn := os.Getenv("HONEYCLOUDTRAIL_ROLE_ARN"); roleArn != "" {
		creds := stscreds.NewCredentials(sess, roleArn)
		logrus.Debugf("Running as role %s", roleArn)
		sess = sess.Copy(aws.NewConfig().WithCredentials(creds))
	}
	cloudtrailSvc := cloudtrail.New(sess, nil)

	listTrailsResp, err := cloudtrailSvc.DescribeTrails(&cloudtrail.DescribeTrailsInput{})

	if err != nil {
		return err
	}

	if len(args) > 0 {
		switch args[0] {
		case "ls", "list":
			for _, trailSummary := range listTrailsResp.TrailList {
				fmt.Println(*trailSummary.Name)
			}
			return nil

		case "lsa", "list-arn":
			for _, trailSummary := range listTrailsResp.TrailList {
				fmt.Println(*trailSummary.TrailARN)
			}
			return nil

		case "ingest":
			if opt.WriteKey == "" {
				logrus.Fatal(`--writekey must be set to the proper write key for the Honeycomb team.
Your write key is available at https://ui.honeycomb.io/account`)
			}

			trailNames := args[1:]

			if len(trailNames) == 0 {
				logrus.Info("No trail names provided; fetching all trails")
				for _, trail := range listTrailsResp.TrailList {
					var trailID string
					// ARN is required to describe Trails belonging to other regions
					// https://docs.aws.amazon.com/awscloudtrail/latest/APIReference/API_DescribeTrails.html
					if opt.MultiRegion {
						trailID = *trail.TrailARN
					} else {
						trailID = *trail.Name
					}
					trailNames = append(trailNames, trailID)
				}
			}

			trailListResp, err := cloudtrailSvc.DescribeTrails(&cloudtrail.DescribeTrailsInput{
				TrailNameList: aws.StringSlice(trailNames),
			})
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error getting trail descriptions: ", err)
				os.Exit(1)
			}

			if len(trailListResp.TrailList) == 0 {
				logrus.Fatal(`No valid trails listed. Try using ls to list available trails or refer to the README.`)
				os.Exit(1)
			}

			logrus.Debugf("Will attempt to ingest logs for %d trails", len(trailListResp.TrailList))

			var stater state.Stater

			if opt.BackfillHr < 1 || opt.BackfillHr > 168 {
				logrus.WithField("hours", opt.BackfillHr).Fatal("--backfill requires an hour input between 1 and 168")
			}

			if opt.HighAvail {
				stater, err = state.NewDynamoDBStater(sess, opt.BackfillHr)
				if err != nil {
					logrus.WithField("tableName", state.DynamoTableName).Fatal("--highavail requires an existing DynamoDB table named appropriately, please refer to the README.")
				}
				logrus.Info("High availability enabled - using DynamoDB")

			} else {
				stater = state.NewFileStater(opt.StateDir, logbucket.AWSCloudTrail, opt.BackfillHr)
				logrus.Info("State tracking enabled - using local file system.")
			}
			logrus.WithField("hours", time.Duration(opt.BackfillHr)*time.Hour).Debug("Backfill will be")

			downloadsCh := make(chan state.DownloadedObject)
			defaultPublisher := publisher.NewHoneycombPublisher(opt, stater, publisher.NewCloudTrailEventParser(opt))

			trlHandler := NewTrailHandler(sess, stater, downloadsCh)

			for _, trail := range trailListResp.TrailList {
				s3Bucket := trail.S3BucketName
				// we want to check if the field is null
				if s3Bucket == nil {

					fmt.Fprintf(os.Stderr, `%q does not currently have an S3 bucket that it is writing logs to. Please enable them to use the ingest tool.

For reference see this link:
https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-create-and-update-a-trail.html `, *trail.Name)

					os.Exit(1)
				}

				trlHandler.IngestCloudTrail(trail)
			}

			signalCh := make(chan os.Signal)
			signal.Notify(signalCh, os.Interrupt)
			go func() {
				<-signalCh
				logrus.Fatal("Exiting due to interrupt.")
			}()

			for {
				download := <-downloadsCh
				if err := defaultPublisher.Publish(download); err != nil {
					logrus.WithFields(logrus.Fields{
						"object": download,
						"error":  err,
					}).Error("Cannot properly publish downloaded object")
				}
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

	if opt.Debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	formatter := &logrus.TextFormatter{
		FullTimestamp: true,
	}
	logrus.SetFormatter(formatter)

	logrus.WithField("version", BuildID).Debug("Program starting")

	if opt.Dataset == "aws-$SERVICE-access" {
		opt.Dataset = "aws-cloudtrail-access"
	}
	if opt.OrganizationID != "" {
		logrus.Info("Organization ID provided, assuming Organization Cloud Trail")
	}

	if opt.MultiRegion {
		logrus.Info("Multiregion set, will find trails in all regions")
	}

	if _, err := os.Stat(opt.StateDir); os.IsNotExist(err) {
		logrus.WithField("dir", opt.StateDir).Fatal("Specified state directory does not exist")
	}

	if opt.Version {
		fmt.Println("honeycloudtrail version", versionStr)
		os.Exit(0)
	}

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, `Usage: `+os.Args[0]+` [--flags] [ls|lsa|ingest] [CloudTrail distribution IDs...]

Use '`+os.Args[0]+` --help' to see available flags.`)
		os.Exit(1)
	}

	if err := cmdCloudTrail(args); err != nil {
		fmt.Fprintln(os.Stderr, "Error: ", err)
		os.Exit(1)
	}
}

type TrailHandler struct {
	accountsOverride []string
	regionsOverride  []string
	sess             *session.Session
	stater           state.Stater
	downloadsCh      chan state.DownloadedObject
}

func NewTrailHandler(s *session.Session, st state.Stater, downloadsCh chan state.DownloadedObject) TrailHandler {
	ce := TrailHandler{
		sess:        s,
		stater:      st,
		downloadsCh: downloadsCh,
	}

	// For Organization or multiregion Trails, determine if trail logs for
	// specific account ids and regions outside the default session should be collected
	if rawRegions := os.Getenv("HONEYCLOUDTRAIL_COLLECT_REGIONS"); rawRegions != "" {
		ce.regionsOverride = strings.Split(rawRegions, ",")
	}
	if rawAccounts := os.Getenv("HONEYCLOUDTRAIL_COLLECT_ACCOUNTS"); rawAccounts != "" {
		ce.accountsOverride = strings.Split(rawAccounts, ",")
	}
	return ce
}

// AccountPathsToIngest handles what account(s) to look for in the s3 path
// when polling and downloading trail objects
func (c TrailHandler) AccountPathsToIngest(sess *session.Session) []string {
	// If no overriding accounts provided, use the session values
	sessionMetaData := meta.Data(sess)
	if len(c.accountsOverride) == 0 {
		logrus.Info("No accounts specified, using session account id for object download")
		return []string{sessionMetaData.AccountID}
	}
	return c.accountsOverride
}

// RegionPathsToIngest handles what region(s) to look for in the s3 path
// when polling and downloading trail objects
func (c TrailHandler) RegionPathsToIngest(sess *session.Session) []string {
	sessionMetaData := meta.Data(sess)
	if len(c.regionsOverride) == 0 {
		logrus.Info("No regions specified, using session region for object download")
		return []string{sessionMetaData.Region}
	}
	return c.regionsOverride
}

func (c TrailHandler) IngestCloudTrail(trail *cloudtrail.Trail) {
	var prefix string
	if trail.S3KeyPrefix == nil {
		prefix = ""
	} else {
		prefix = *trail.S3KeyPrefix
	}

	logrus.WithFields(logrus.Fields{
		"name":   *trail.Name,
		"prefix": prefix,
	}).Info("Access logs are enabled for CloudTrail trails")

	// The trail's region may differ from the session's region,
	// so use trail's HomeRegion for accessing s3
	awsConf := aws.NewConfig().WithRegion(*trail.HomeRegion)
	rsess := c.sess.Copy(awsConf)

	accounts := c.AccountPathsToIngest(rsess)
	logrus.Infof("Will fetch objects for account(s): %+v", accounts)
	regions := c.RegionPathsToIngest(rsess)
	logrus.Infof("Will fetch objects for region(s): %+v", regions)

	// Create a downloader for each account and region that needs to be collected
	for _, accountID := range accounts {
		for _, region := range regions {
			// Note: this is potentially a lot of concurrent requests to S3 if many accounts / regions are provided
			cloudtrailDownloader := logbucket.NewCloudTrailDownloader(accountID, region, *trail.S3BucketName, prefix, *trail.TrailARN, opt.OrganizationID)
			downloader := logbucket.NewDownloader(rsess, c.stater, cloudtrailDownloader, opt.BackfillHr)
			go downloader.Download(c.downloadsCh)
		}
	}
}
