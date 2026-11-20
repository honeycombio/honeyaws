package main

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elbv2"
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
	libhoney.UserAgentAddition = "honeyalb/" + versionStr
}

func cmdALB(args []string) error {
	if len(args) > 0 && args[0] == "ingest" {
		if err := validateALBOptions(opt); err != nil {
			return err
		}
	}
	// TODO: Would be nice to have this more highly configurable.
	//
	// Will just use environment config right now, e.g., default profile.
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	elbSvc := elbv2.New(sess, nil)

	describeLBResp, err := elbSvc.DescribeLoadBalancers(&elbv2.DescribeLoadBalancersInput{})
	if err != nil {
		return err
	}

	if len(args) > 0 {
		switch args[0] {
		case "ls", "list":
			for _, lb := range describeLBResp.LoadBalancers {
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
				for _, lb := range describeLBResp.LoadBalancers {
					lbNames = append(lbNames, *lb.LoadBalancerName)
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
				logrus.Info("State tracking with high availability enabled - using DynamoDB")
			} else {
				stater = state.NewFileStater(opt.StateDir, logbucket.AWSElasticLoadBalancingV2, opt.BackfillHr)
				logrus.Info("State tracking enabled - using local file system.")
			}
			logrus.WithField("hours", time.Duration(opt.BackfillHr)*time.Hour).Debug("Backfill will be")

			defaultPublisher := publisher.NewHoneycombPublisher(opt, stater, publisher.NewALBEventParser(opt))
			downloadsCh := make(chan state.DownloadedObject)

			// For now, just run one goroutine per-LB
			for _, lbName := range lbNames {
				logrus.WithFields(logrus.Fields{
					"lbName": lbName,
				}).Info("Attempting to ingest ALB")

				elbSvc := elbv2.New(sess, nil)

				lbNameResp, err := elbSvc.DescribeLoadBalancers(&elbv2.DescribeLoadBalancersInput{
					Names: []*string{
						aws.String(lbName),
					},
				})
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}

				lbArn := lbNameResp.LoadBalancers[0].LoadBalancerArn
				lbArnResp, err := elbSvc.DescribeLoadBalancerAttributes(&elbv2.DescribeLoadBalancerAttributesInput{
					LoadBalancerArn: lbArn,
				})
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}

				sources, err := albLogSources(lbArnResp.Attributes, opt.ALBLogType)
				if err != nil {
					return fmt.Errorf("ALB %q: %w", lbName, err)
				}
				for _, source := range sources {
					logrus.WithFields(logrus.Fields{
						"bucket":   source.bucket,
						"lbName":   lbName,
						"log_type": source.logType,
					}).Info("Ingesting ALB logs")
					albDownloader := logbucket.NewALBDownloader(sess, source.bucket, source.prefix, lbName)
					albDownloader.ConnectionLogs = source.logType == "connection"
					downloader := logbucket.NewDownloader(sess, stater, albDownloader, opt.BackfillHr)
					go downloader.Download(downloadsCh)
				}
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
		opt.Dataset = "aws-elb-access"
	}

	if _, err := os.Stat(opt.StateDir); os.IsNotExist(err) {
		logrus.WithField("dir", opt.StateDir).Fatal("Specified state directory does not exist")
	}

	if opt.Version {
		fmt.Println("honeyalb version", versionStr)
		os.Exit(0)
	}

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, `Usage: `+os.Args[0]+` [--flags] [ls|ingest] [ALB names...]

Use '`+os.Args[0]+` --help' to see available flags.`)
		os.Exit(1)
	}

	if err := cmdALB(args); err != nil {
		fmt.Fprintln(os.Stderr, "Error: ", err)
		os.Exit(1)
	}
}
