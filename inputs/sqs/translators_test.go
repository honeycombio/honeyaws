package sqs

import (
	"log"
	"reflect"
	"testing"

	aws_sqs "github.com/aws/aws-sdk-go/service/sqs"
	"github.com/davecgh/go-spew/spew"
)

func TestGenericSQSJSONTranslator(t *testing.T) {
	jsonString := string(`{"foo": 1, "bar": 2}`)
	message := aws_sqs.Message{
		Body: &jsonString,
	}

	event, err := genericSQSJSONTranslator(&message)
	if err != nil {
		t.Fatal(err)
	}
	expected := GenericJSONEvent{
		"foo": float64(1),
		"bar": float64(2),
	}
	if !reflect.DeepEqual(expected, event) {
		t.Error("translated event did not match expected value")
		log.Println("Actual: ", spew.Sdump(event))
		log.Println("Expected: ", spew.Sdump(expected))
	}
}

func TestGenericSNSJSONTranslator(t *testing.T) {
	jsonString := string(`{"MessageId":"483eae4c-4fb0-57e5-a5f9-ff9b08612bef","Signature":"Uy3tn/qAQg/sXARGk2DRddd31ZtyDE+B1IzRla/KA75BaerApJqN+H59q69z8H+pRx0AyUwOD1K0huBYdDRbAMVOUsMgZgdcNjj0gSfFg8uZvTuKaqTaWj4E0hmzoemHENWeuswuq3l6xoPcAJ9fHd2yFhX+792AV++i/8P4EKv/9I4j8Ejs3OxMRN49gkWefKbv4/avyHOdSaFTnXV0rGLmPb103dtjeY4K05PTKvUlPerN+MdRTvHrjApvqDvP0NEVyYBU4zFZQ6GnFcFnHtTk44c3NH/dVi6Gf9VrX8V1id5VSZICYiIG1iaUZ0b676IhRh8znzjMDWaczOBwkA==","Type":"Notification","TopicArn":"arn:aws:sns:us-west-2:123456789000:ses_messages","MessageAttributes":{},"SignatureVersion":"1","Timestamp":"2017-07-05T20:01:21.366Z","SigningCertUrl":"https://sns.us-west-2.amazonaws.com/SimpleNotificationService-b95095beb82e8f6a046b3aafc7f4149a.pem","Message":"{\"foo\": 1, \"bar\": 2}","UnsubscribeUrl":"https://sns.us-west-2.amazonaws.com/?Action=Unsubscribe&eifjccgihujihfhrchunfnglreichbrcljrnlvtbeked\n        SubscriptionArn=arn:aws:sns:us-west-2:123456789000:ses_messages:26a58451-3392-4ab6-a829-d65c2968421a","Subject":null}`)
	message := aws_sqs.Message{
		Body: &jsonString,
	}

	event, err := genericSNSJSONTranslator(&message)
	if err != nil {
		t.Fatal(err)
	}
	expected := GenericJSONEvent{
		"foo": float64(1),
		"bar": float64(2),
	}
	if !reflect.DeepEqual(expected, event) {
		t.Error("translated event did not match expected value")
		log.Println("Actual: ", spew.Sdump(event))
		log.Println("Expected: ", spew.Sdump(expected))
	}
}

func TestRDSEventTranslator(t *testing.T) {
	jsonString := string(`{"Type":"Notification","MessageId":"f298b518-c616-5ed3-b12e-e60341d25925","TopicArn":"arn:aws:sns:us-east-1:123456789:rds-events","Subject":"RDS Notification Message","Message":"{\"Event Source\":\"db-instance\",\"Event Time\":\"2018-03-08 18:47:46.562\",\"Identifier Link\":\"https://console.aws.amazon.com/rds/home?region=us-east-1#dbinstance:id=foobar-mysql\",\"Source ID\":\"foobar-mysql\",\"Event ID\":\"http://docs.amazonwebservices.com/AmazonRDS/latest/UserGuide/USER_Events.html#RDS-EVENT-0002\",\"Event Message\":\"Finished DB Instance backup\"}","Timestamp":"2018-03-08T18:48:36.255Z","SignatureVersion":"1","Signature":"","SigningCertURL":"","UnsubscribeURL":""}`)
	message := aws_sqs.Message{
		MessageId: &jsonString,
		Body:      &jsonString,
	}

	event, err := rdsTranslator(&message)
	if err != nil {
		t.Fatal(err)
	}
	expected := RdsEvent{
		EventSource:    "db-instance",
		EventTime:      "2018-03-08 18:47:46.562",
		IdentifierLink: "https://console.aws.amazon.com/rds/home?region=us-east-1#dbinstance:id=foobar-mysql",
		SourceID:       "foobar-mysql",
		EventID:        "http://docs.amazonwebservices.com/AmazonRDS/latest/UserGuide/USER_Events.html#RDS-EVENT-0002",
		EventMessage:   "Finished DB Instance backup",
	}
	if !reflect.DeepEqual(expected, event) {
		t.Error("translated event did not match expected value")
		log.Println("Actual: ", spew.Sdump(event))
		log.Println("Expected: ", spew.Sdump(expected))
	}
}

func TestTranslateMessage(t *testing.T) {
	jsonString := string(`{"foo": 1, "bar": 2}`)
	message := aws_sqs.Message{
		Body: &jsonString,
	}

	event, err := TranslateMessage(genericSQSJSON, &message, false)
	if err != nil {
		t.Fatal(err)
	}
	expected := GenericJSONEvent{
		"foo": float64(1),
		"bar": float64(2),
	}
	if !reflect.DeepEqual(expected, event) {
		t.Error("translated event did not match expected value")
		log.Println("Actual: ", spew.Sdump(event))
		log.Println("Expected: ", spew.Sdump(expected))
	}
}

// Tests when an unknown event type is passed as the first arg
func TestTranslateMessageUnknownEventType(t *testing.T) {
	jsonString := string(`{"foo": 1, "bar": 2}`)
	message := aws_sqs.Message{
		Body: &jsonString,
	}

	_, err := TranslateMessage("something new!", &message, false)
	if err == nil {
		t.Fatal("this should have returned an error")
	}
	if _, ok := err.(ErrUnsupportedType); !ok {
		t.Fatal("this should return an error of type ErrUnsupportedType")
	}
}
