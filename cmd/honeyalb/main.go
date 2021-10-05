package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/honeycombio/honeyaws/logbucket"
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
	logbuckets map[string]logbucket.LogbucketData
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

	logbuckets = make(map[string]logbucket.LogbucketData)
}

func addMatchingALBsToMap(elbSvc *elbv2.ELBV2, regexStrs []string) {
	regexes := []*regexp.Regexp{}
	for _, regex := range regexStrs {
		regexes = append(regexes, regexp.MustCompile(fmt.Sprintf("^%s$", regex)))
	}

	// paginate and regex
	elbSvc.DescribeLoadBalancersPages(&elbv2.DescribeLoadBalancersInput{},
		func(page *elbv2.DescribeLoadBalancersOutput, lastPage bool) bool {
			for _, lb := range page.LoadBalancers {
				name := *lb.LoadBalancerName
				for _, regex := range regexes {
					if _, ok := logbuckets[name]; !ok && regex.MatchString(name) {
						lbArnResp, err := elbSvc.DescribeLoadBalancerAttributes(&elbv2.DescribeLoadBalancerAttributesInput{
							LoadBalancerArn: lb.LoadBalancerArn,
						})
						if err != nil {
							fmt.Fprintln(os.Stderr, err)
							os.Exit(1)
						}

						var enabled = false
						var bucketName string
						var bucketPrefix string
						for _, element := range lbArnResp.Attributes {
							if *element.Key == "access_logs.s3.enabled" && *element.Value == "true" {

								// We're appending lbNames that:
								// - match one of the given regexes and
								// - has access_logs.s3.enabled
								enabled = true
							}
							if *element.Key == "access_logs.s3.bucket" {
								bucketName = *element.Value
							}
							if *element.Key == "access_logs.s3.prefix" {
								bucketPrefix = *element.Value
							}
						}

						if enabled {
							logbuckets[name] = logbucket.LogbucketData{
								BucketName:   bucketName,
								BucketPrefix: bucketPrefix,
								CancelFn:     nil,
							}
						} else {
							fmt.Fprintf(os.Stderr, `Access logs are not configured for ALB %q. Please enable them to use the ingest tool.

For reference see this link:

http://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-access-logs.html#enable-access-logging
`, name)
						}
						break
					}
				}
			}
			return !lastPage
		})
}

func startDownloaders(sess *session.Session, stater state.Stater, downloadsCh chan (state.DownloadedObject)) {
	for name, data := range logbuckets {
		if data.CancelFn == nil {
			albDownloader := logbucket.NewALBDownloader(sess, data.BucketName, data.BucketPrefix, name)
			downloader := logbucket.NewDownloader(sess, stater, albDownloader, opt.BackfillHr)

			ctx, cancelFn := context.WithCancel(context.Background())
			data = logbuckets[name]
			data.CancelFn = cancelFn
			go downloader.Download(ctx, downloadsCh)
		}
	}
}

func removeALBsFromMap(elbSvc *elbv2.ELBV2) {
	for name, _ := range logbuckets {
		lbNameResp, err := elbSvc.DescribeLoadBalancers(&elbv2.DescribeLoadBalancersInput{
			Names: []*string{
				aws.String(name),
			},
		})

		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue // hopefully it's a transient error?
		}

		remove := false

		if len(lbNameResp.LoadBalancers) == 0 {
			remove = true
		} else {
			lbArn := lbNameResp.LoadBalancers[0].LoadBalancerArn
			lbArnResp, err := elbSvc.DescribeLoadBalancerAttributes(&elbv2.DescribeLoadBalancerAttributesInput{
				LoadBalancerArn: lbArn,
			})
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				continue // hopefully it's a transient error?
			}

			var enabled = false
			var bucketName string
			var bucketPrefix string
			for _, element := range lbArnResp.Attributes {
				if *element.Key == "access_logs.s3.enabled" && *element.Value == "true" {

					// We're appending lbNames that:
					// - match one of the given regexes and
					// - has access_logs.s3.enabled
					enabled = true
				}
				if *element.Key == "access_logs.s3.bucket" {
					bucketName = *element.Value
				}
				if *element.Key == "access_logs.s3.prefix" {
					bucketPrefix = *element.Value
				}
			}

			// if logs are no longer enabled, _or_ they're being sent somewhere else
			// now, remove it from the map.
			remove = !enabled || bucketName != logbuckets[name].BucketName ||
				bucketPrefix != logbuckets[name].BucketPrefix
		}

		if remove {
			if logbucketData, ok := logbuckets[name]; ok {
				logbucketData.CancelFn()
				delete(logbuckets, name)
			}
		}
	}
}

func cmdALB(args []string) error {
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

			lbNameRegexes := args[1:]
			// no args? Slurp it all.
			if len(lbNameRegexes) == 0 {
				lbNameRegexes = []string{".*"}
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

			signalCh := make(chan os.Signal)
			signal.Notify(signalCh, os.Interrupt)

			go func() {
				<-signalCh
				logrus.Fatal("Exiting due to interrupt.")
			}()

			var tickerChan (<-chan (time.Time))
			// if this value is 0, time.NewTicker would panic - but then the desired
			// behavior is not to re-poll, so a channel that never receives is fine.
			if opt.PollNewSourcesIntervalSeconds == 0 {
				tickerChan = make(<-chan (time.Time))
			} else {
				ticker := time.NewTicker(time.Duration(opt.PollNewSourcesIntervalSeconds) * time.Second)
				tickerChan = ticker.C
			}

			// initial run, no delay
			removeALBsFromMap(elbSvc)
			addMatchingALBsToMap(elbSvc, lbNameRegexes)
			startDownloaders(sess, stater, downloadsCh)

			for {
				select {
				case <-tickerChan:
					removeALBsFromMap(elbSvc)
					addMatchingALBsToMap(elbSvc, lbNameRegexes)
					startDownloaders(sess, stater, downloadsCh)
				case download := <-downloadsCh:
					if err := defaultPublisher.Publish(download); err != nil {
						logrus.WithFields(logrus.Fields{
							"object": download,
							"error":  err,
						}).Error("Cannot properly publish downloaded object")
					}
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
