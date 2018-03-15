package main

import (
	"os"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	aws_sqs "github.com/aws/aws-sdk-go/service/sqs"
	"github.com/honeycombio/honeyaws/inputs/sqs"
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

type eventMetadata struct {
	messageID     string
	receiptHandle string
}

var opt options
var parser = flags.NewParser(&opt, flags.Default)
var usage = "Honeycomb adapter for SQS events."

// responseHandler watches the libhoney responses channel and deletes sqs
// messages that have successfully been ingested into honeycomb
func responseHandler(session *session.Session) {
	responses := libhoney.Responses()

	svc := aws_sqs.New(session)

	// batched deletes may be nice here in the future. This isn't really
	// written with high throughput SQS queues in mind
	for {
		response, more := <-responses
		// libhoney should close this channel when Close() is called
		if !more {
			return
		}

		metadata, ok := response.Metadata.(*eventMetadata)
		if !ok {
			log.Error("got unexpected type in event metadata")
			continue
		}
		if response.Err != nil {
			log.WithError(response.Err).
				WithField("MessageId", metadata.messageID).
				Warn("failed to submit event for sqs message")
			continue
		}
		// Note that it is possible for deletion to fail because the
		// receiptHandle is no longer valid. This could happen if it takes
		// longer than VisibilityTimeout (60s right now) for the message
		// to make it through the response queue
		// https://docs.aws.amazon.com/AWSSimpleQueueService/latest/APIReference/API_DeleteMessage.html
		_, err := svc.DeleteMessage(&aws_sqs.DeleteMessageInput{
			QueueUrl:      &opt.SNSQueue,
			ReceiptHandle: &metadata.receiptHandle,
		})
		if err != nil {
			log.WithError(err).
				WithField("MessageId", metadata.messageID).
				Warn("sqs deletion failed for message")
			continue
		}
	}
}

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

	session := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	svc := aws_sqs.New(session)

	go responseHandler(session)

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
			// pass the sqs receipt handle in the event metadata,
			// so that we can delete the message upon successful event
			// submission
			event.Metadata = &eventMetadata{
				receiptHandle: *message.ReceiptHandle,
				messageID:     *message.MessageId,
			}
			err = event.Send()
			// if we fail to enqueue the message, just move on.
			// We will eventually pick up the message from SQS again when it
			// becomes visible
			if err != nil {
				log.WithError(err).
					WithField("MessageId", *message.MessageId).
					Warn("failed to enqueue message to honeycomb")
				continue
			}

			log.WithField("MessageId", *message.MessageId).
				Info("translated and sent message to honeycomb")
		}
	}
}
