package main

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudtrail"
	"github.com/honeycombio/honeyaws/logbucket"
	"github.com/honeycombio/honeyaws/options"
	"github.com/honeycombio/honeyaws/publisher"
	"github.com/honeycombio/honeyaws/state"
	libhoney "github.com/honeycombio/libhoney-go"
	flag "github.com/jessevdk/go-flags"
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
	// Will just use environment config right now, e.g., default profile.
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	cloudtrailSvc := cloudtrail.New(sess, nil)

	listTrailsResp, err := cloudtrailSvc.DescribeTrails(&cloudtrail.DescribeTrailsInput{})

	if err != nil {
		return err
		os.Exit(1)
	}

	if len(args) > 0 {
		switch args[0] {
		case "ls", "list":
			for _, trailSummary := range listTrailsResp.TrailList {
				fmt.Println(*trailSummary.Name)
			}
			return nil

		case "ingest":
			if opt.WriteKey == "" {
				logrus.Fatal(`--writekey must be set to the proper write key for the Honeycomb team.
Your write key is available at https://ui.honeycomb.io/account`)
			}

			trailNames := args[1:]

			if len(trailNames) == 0 {
				for _, trail := range listTrailsResp.TrailList {
					trailNames = append(trailNames, *trail.Name)
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

			var stater state.Stater

			if opt.BackfillHr < 1 || opt.BackfillHr > 168 {
				logrus.WithField("hours", opt.BackfillHr).Fatal("--backfill requires an hour input between 1 and 168")
			}

			if opt.HighAvail {
				stater, err = state.NewDynamoDBStater(sess, logbucket.AWSCloudTrail, opt.BackfillHr)
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
			defaultPublisher := publisher.NewHoneycombPublisher(opt, stater, publisher.NewCloudTrailEventParser(opt.SampleRate))

			for _, trail := range trailListResp.TrailList {

				var prefix string

				s3Bucket := trail.S3BucketName
				// we want to check if the field is null
				if s3Bucket == nil {

					fmt.Fprintf(os.Stderr, `%q does not currently have an S3 bucket that it is writing logs to. Please enable them to use the ingest tool. 

For reference see this link:
https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-create-and-update-a-trail.html `, *trail.Name)

					os.Exit(1)
				}
				if trail.S3KeyPrefix == nil {
					prefix = ""
				} else {
					prefix = *trail.S3KeyPrefix
				}
				logrus.WithFields(logrus.Fields{
					"name":   *trail.Name,
					"prefix": prefix,
				}).Info("Access logs are enabled for CloudTrail trails")

				cloudtrailDownloader := logbucket.NewCloudTrailDownloader(sess, *s3Bucket, prefix, *trail.TrailARN)
				downloader := logbucket.NewDownloader(sess, stater, cloudtrailDownloader, opt.BackfillHr)
				go downloader.Download(downloadsCh)
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
					logrus.WithField("object", download).Error("Cannot properly publish downloaded object")
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

	if _, err := os.Stat(opt.StateDir); os.IsNotExist(err) {
		logrus.WithField("dir", opt.StateDir).Fatal("Specified state directory does not exist")
	}

	if opt.Version {
		fmt.Println("honeycloudtrail version", versionStr)
		os.Exit(0)
	}

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, `Usage: `+os.Args[0]+` [--flags] [ls|ingest] [CloudTrail distribution IDs...]

Use '`+os.Args[0]+` --help' to see available flags.`)
		os.Exit(1)
	}

	if err := cmdCloudTrail(args); err != nil {
		fmt.Fprintln(os.Stderr, "Error: ", err)
		os.Exit(1)
	}
}
