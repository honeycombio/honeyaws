package sqs

import (
	"encoding/json"
	"fmt"

	log "github.com/Sirupsen/logrus"
	aws_sqs "github.com/aws/aws-sdk-go/service/sqs"
)

// ErrUnsupportedType indicates that the user has specified
// a type we are unprepared to translate
type ErrUnsupportedType string

func (e ErrUnsupportedType) Error() string {
	return fmt.Sprintf("unsupported type %s", e)
}

type eventTranslatorFn func(*aws_sqs.Message) (interface{}, error)

var translatorMap map[string]eventTranslatorFn

func extractSNSEntity(message *aws_sqs.Message) (*snsEntity, error) {
	var sns snsEntity
	err := json.Unmarshal([]byte(*message.Body), &sns)
	if err != nil || sns.Message == "" {
		log.WithError(err).WithField("MessageId", *message.MessageId).
			Error("error parsing message, did this come from SNS?")
		return nil, err
	}

	return &sns, nil
}

func rdsTranslator(message *aws_sqs.Message) (interface{}, error) {
	// RDS messages to SQS are delivered through SNS, so we need to pluck
	// out the SNS struct from the message
	snsEntity, err := extractSNSEntity(message)
	if err != nil {
		return nil, err
	}
	var rdsEvent RdsEvent
	// Now that we have an SNS entity, we parse out the RDS event
	if err := json.Unmarshal([]byte(snsEntity.Message), &rdsEvent); err != nil {
		log.WithError(err).WithFields(log.Fields{
			"MessageID": snsEntity.MessageID,
			"Message":   snsEntity.Message,
		}).Error("unable to extract rds event from message, " +
			"is this really an RDS event?")
		return nil, err
	}

	return rdsEvent, nil
}

func genericSNSJSONTranslator(message *aws_sqs.Message) (interface{}, error) {
	// First we have to get the SNS event out of SQS
	snsEntity, err := extractSNSEntity(message)
	if err != nil {
		return nil, err
	}
	var event GenericJSONEvent
	if err := json.Unmarshal([]byte(snsEntity.Message), &event); err != nil {
		return nil, err
	}

	return event, nil
}

func genericSQSJSONTranslator(message *aws_sqs.Message) (interface{}, error) {
	var event GenericJSONEvent
	if err := json.Unmarshal([]byte(*message.Body), &event); err != nil {
		return nil, err
	}

	return event, nil
}

// TranslateMessage accepts an eventTypeName and an SQS message and attempts
// to find a translator for that event type and convert it.
// If strict is true, will return an error if the translator fails
// Otherwise, it will return an UnknownEvent with just the Message
func TranslateMessage(eventTypeName string, message *aws_sqs.Message, strict bool) (interface{}, error) {
	if translator, ok := translatorMap[eventTypeName]; ok {
		translatedEvent, err := translator(message)
		if err != nil && strict {
			return nil, err
		} else if err != nil {
			return UnknownEvent{
				Message: *message.Body,
			}, nil
		}

		return translatedEvent, nil
	}

	return nil, ErrUnsupportedType(eventTypeName)
}

func init() {
	translatorMap = make(map[string]eventTranslatorFn)
	translatorMap[rdsEvent] = rdsTranslator
	translatorMap[genericSNSJSON] = genericSNSJSONTranslator
	translatorMap[genericSQSJSON] = genericSQSJSONTranslator
}
