package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudfront"
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
	libhoney.UserAgentAddition = "honeycloudfront/" + versionStr
}

func cmdCloudFront(args []string) error {
	// TODO: Would be nice to have this more highly configurable.
	//
	// Will just use environment config right now, e.g., default profile.
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	cloudfrontSvc := cloudfront.New(sess, nil)

	listDistributionsResp, err := cloudfrontSvc.ListDistributions(&cloudfront.ListDistributionsInput{})
	if err != nil {
		return err
	}

	if len(args) > 0 {
		switch args[0] {
		case "ls", "list":
			for _, distributionSummary := range listDistributionsResp.DistributionList.Items {
				fmt.Println(*distributionSummary.Id)
			}

			return nil

		case "ingest":
			if opt.WriteKey == "" {
				logrus.Fatal(`--writekey must be set to the proper write key for the Honeycomb team.
Your write key is available at https://ui.honeycomb.io/account`)
			}

			distIds := args[1:]

			// Use all available distributions by default if none
			// are provided.
			if len(distIds) == 0 {
				for _, distributionSummary := range listDistributionsResp.DistributionList.Items {
					distIds = append(distIds, *distributionSummary.Id)
				}
			}

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
				stater = state.NewFileStater(opt.StateDir, logbucket.AWSCloudFront, opt.BackfillHr)
				logrus.Info("State tracking enabled - using local file system.")
			}
			logrus.WithField("hours", time.Duration(opt.BackfillHr)*time.Hour).Debug("Backfill will be")

			downloadsCh := make(chan state.DownloadedObject)
			defaultPublisher := publisher.NewHoneycombPublisher(opt, stater, publisher.NewCloudFrontEventParser(opt))

			// For now, just run one goroutine per-distribution
			for _, id := range distIds {
				logrus.WithFields(logrus.Fields{
					"id": id,
				}).Info("Attempting to ingest CloudFront distribution")

				cloudfrontSvc := cloudfront.New(sess, nil)

				distConfigResp, err := cloudfrontSvc.GetDistributionConfig(&cloudfront.GetDistributionConfigInput{
					Id: aws.String(id),
				})
				if err != nil {
					fmt.Fprintln(os.Stderr, "Error getting distribution config: ", err)
					os.Exit(1)
				}

				loggingConfig := distConfigResp.DistributionConfig.Logging

				if !*loggingConfig.Enabled {
					fmt.Fprintf(os.Stderr, `Access logs are not configured for CloudFront distribution ID %q. Please enable them to use the ingest tool.

For reference see this link:

https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/AccessLogs.html
`, id)
					os.Exit(1)
				}

				// loggingConfig.Bucket returns a bucket URL
				// (e.g.,
				// nathanleclaire-cloudfront-test-access-logs.s3.amazonaws.com)
				// so strip the suffix from the bucket.
				//
				// TODO(nathanleclaire): Determine if this is
				// acceptably robust.
				bucket := strings.Replace(*loggingConfig.Bucket, ".s3.amazonaws.com", "", -1)

				logrus.WithFields(logrus.Fields{
					"bucket": bucket,
					"id":     id,
				}).Info("Access logs are enabled for CloudFront distribution ♥")

				cloudfrontDownloader := logbucket.NewCloudFrontDownloader(bucket, *loggingConfig.Prefix, id)
				downloader := logbucket.NewDownloader(sess, stater, cloudfrontDownloader, opt.BackfillHr)
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
		opt.Dataset = "aws-cloudfront-access"
	}

	if _, err := os.Stat(opt.StateDir); os.IsNotExist(err) {
		logrus.WithField("dir", opt.StateDir).Fatal("Specified state directory does not exist")
	}

	if opt.Version {
		fmt.Println("honeycloudfront version", versionStr)
		os.Exit(0)
	}

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, `Usage: `+os.Args[0]+` [--flags] [ls|ingest] [CloudFront distribution IDs...]

Use '`+os.Args[0]+` --help' to see available flags.`)
		os.Exit(1)
	}

	if err := cmdCloudFront(args); err != nil {
		fmt.Fprintln(os.Stderr, "Error: ", err)
		os.Exit(1)
	}
}
