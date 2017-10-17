package main

import (
	"fmt"
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/honeycombio/honeyelb/logbucket"
	"github.com/honeycombio/honeyelb/options"
	"github.com/honeycombio/honeyelb/publisher"
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
		versionStr = "1." + BuildID
	}

	// init libhoney user agent properly
	libhoney.UserAgentAddition = "honeyelb/" + versionStr
}

func cmdELB(args []string) error {
	// TODO: Would be nice to have this more highly configurable.
	//
	// Will just use environment config right now, e.g., default profile.
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	elbSvc := elbv2.New(sess)

	describeLBResp, err := elbSvc.DescribeLoadBalancers(&elbv2.DescribeLoadBalancersInput{})
	if err != nil {
		return fmt.Errorf("Error describing LBs: ", err)
		os.Exit(1)
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

			// Use this one publisher instance for all ObjectDownloadParsers.
			defaultPublisher := publisher.NewHoneycombPublisher(opt, publisher.AWSElasticLoadBalancerFormatV2)

			// For now, just run one goroutine per-LB
			for _, lbName := range lbNames {
				logrus.WithFields(logrus.Fields{
					"lbName": lbName,
				}).Info("Attempting to ingest LB")

				elbSvc := elbv2.New(sess, nil)

				describeLBResp, err := elbSvc.DescribeLoadBalancers(&elbv2.DescribeLoadBalancersInput{
					Names: []*string{
						aws.String(lbName),
					},
				}) // not walking token because there should be only one
				if err != nil {
					fmt.Fprintln(os.Stderr, "Error describing LBs: ", err)
					os.Exit(1)
				}
				if len(describeLBResp.LoadBalancers) == 0 {
					fmt.Fprintln(os.Stderr, "Couldn't find load balancer named", lbName)
					os.Exit(1)
				}
				lbArn := describeLBResp.LoadBalancers[0].LoadBalancerArn
				lbResp, err := elbSvc.DescribeLoadBalancerAttributes(&elbv2.DescribeLoadBalancerAttributesInput{
					LoadBalancerArn: lbArn,
				})
				if err != nil {
					fmt.Fprintln(os.Stderr, "Error describing load balancers: ", err)
					os.Exit(1)
				}
				enabled := false
				bucketName := ""
				bucketPrefix := ""
				for _, element := range lbResp.Attributes {
					fmt.Fprintln(os.Stderr, *element.Key, *element.Value)
					if *element.Key == "access_logs.s3.enabled" && *element.Value == "true" {
						enabled = true
					}
					if *element.Key == "access_logs.s3.bucket" {
						bucketName = *element.Value
					}
					if *element.Key == "access_logs.s3.prefix" {
						bucketPrefix = *element.Value
					}
				}

				if !enabled {
					fmt.Fprintf(os.Stderr, `Access logs are not configured for ELB %q. Please enable them to use the ingest tool.

For reference see this link:

http://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-access-logs.html#enable-access-logging
`, lbName)
					os.Exit(1)
				}
				logrus.WithFields(logrus.Fields{
					"bucket":       bucketName,
					"bucketPrefix": bucketPrefix,
					"lbName":       lbName,
				}).Info("Access logs are enabled for ELB ♥")

				downloadParser := logbucket.ObjectDownloadParser{
					Service:            logbucket.AWSElasticLoadBalancing,
					Entity:             "app."+lbName,
					HoneycombPublisher: defaultPublisher,
					StateDir:           opt.StateDir,
				}

				// TODO: One-goroutine-per-LB is a bit silly.
				//
				// Finish implementing a proper 'pipeline'
				// instead using channels:
				//
				// (Query Objects to Process) => (Download Objects) => (Parse Objects) => (Send to HC)
				go downloadParser.Ingest(sess, bucketName, bucketPrefix)
			}

			signalCh := make(chan os.Signal)

			// block forever (until interrupt)
			select {
			case <-signalCh:
				logrus.Info("Exiting due to interrupt.")
				// TODO(nathanleclaire): Cleanup before
				// exiting.
				//
				// 1. Delete format file, even
				//    though it's in /tmp.
				// 2. Also, wait for existing in-flight object
				//    parsing / sending to finish so that state of
				//    parsing "cursor" can be written to the JSON
				//    file.
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

	if opt.Debug {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.WithField("version", versionStr).Debug("Starting honeyelb")
	}

	if _, err := os.Stat(opt.StateDir); os.IsNotExist(err) {
		logrus.WithField("dir", opt.StateDir).Fatal("Specified state directory does not exist")
	}

	if opt.Version {
		fmt.Println("honeyelb version", versionStr)
		os.Exit(0)
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
