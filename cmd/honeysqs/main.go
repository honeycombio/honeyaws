package main

import (
	"os"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	aws_sqs "github.com/aws/aws-sdk-go/service/sqs"
	"github.com/honeycombio/honeyaws/sqs"
	libhoney "github.com/honeycombio/libhoney-go"
	flags "github.com/jessevdk/go-flags"
)

type options struct {
	Dataset    string `short:"d" long:"dataset" description:"Honeycomb dataset for events" default:"honeycomb-sns-events"`
	WriteKey   string `short:"k" long:"writekey" description:"Your honeycomb write key" required:"true"`
	APIHost    string `long:"apihost" description:"Honeycomb API host" default:"https://api.honeycomb.io"`
	SNSQueue   string `short:"u" long:"queueurl" description:"URL of queue receiving SNS events" required:"true"`
	EventType  string `short:"t" long:"eventtype" description:"Type of event to handle. (see docs)" required:"true"`
	SampleRate int    `short:"s" long:"samplerate" description:"Honeycomb sample rate" default:"1"`
}

var opt options
var parser = flags.NewParser(&opt, flags.Default)
var usage = "Honeycomb adapter for SQS events."

func main() {
	parser.Usage = usage
	_, err := parser.Parse()
	if err != nil {
		os.Exit(1)
	}

	libhoney.Init(libhoney.Config{
		WriteKey:   opt.WriteKey,
		Dataset:    opt.Dataset,
		APIHost:    opt.APIHost,
		SampleRate: uint(opt.SampleRate),
	})
	defer libhoney.Close()

	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	svc := aws_sqs.New(sess)

	for {
		result, err := svc.ReceiveMessage(&aws_sqs.ReceiveMessageInput{
			AttributeNames: []*string{
				aws.String(aws_sqs.MessageSystemAttributeNameSentTimestamp),
			},
			MessageAttributeNames: []*string{
				aws.String(aws_sqs.QueueAttributeNameAll),
			},
			QueueUrl:            &opt.SNSQueue,
			MaxNumberOfMessages: aws.Int64(10),
			VisibilityTimeout:   aws.Int64(60),
			WaitTimeSeconds:     aws.Int64(0),
		})

		if err != nil {
			log.WithError(err).Error("error getting messages")
			time.Sleep(time.Second)
			continue
		}

		if len(result.Messages) == 0 {
			log.Info("no events found - nothing to do!")
			time.Sleep(time.Second * 60)
			continue
		}

		for _, message := range result.Messages {
			translatedEvent, err := sqs.TranslateMessage(opt.EventType, message, false)
			if err != nil {
				if _, ok := err.(sqs.ErrUnsupportedType); ok {
					log.WithError(err).
						WithField("EventType", opt.EventType).
						Fatal("unsupported event type requested, we shouldn't continue")
					os.Exit(1)
				}
				log.WithError(err).
					WithFields(log.Fields{
						"EventType": opt.EventType,
						"MessageId": *message.MessageId,
					}).
					Error("error extracting event type from message")
				continue
			}
			event := libhoney.NewEvent()
			event.Add(translatedEvent)
			err = event.Send()
			// if we sent the event to honeycomb, we can delete it from the queue
			if err == nil {
				_, err = svc.DeleteMessage(&aws_sqs.DeleteMessageInput{
					QueueUrl:      &opt.SNSQueue,
					ReceiptHandle: message.ReceiptHandle,
				})

				if err != nil {
					log.WithError(err).
						WithField("MessageId", *message.MessageId).
						Error("sqs deletion failed for message")
					continue
				}
			}

			log.WithField("MessageId", *message.MessageId).
				Info("translated and sent message to honeycomb")
		}
	}
}
