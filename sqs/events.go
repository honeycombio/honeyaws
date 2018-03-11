package sqs

import "time"

const (
	rdsEvent       = "rds"
	genericSNSJSON = "generic-sns-json"
	genericSQSJSON = "generic-sqs-json"
)

// UnknownEvent is optionally returned if the event type handler
// fails to extrac it. It just returns the Message text
type UnknownEvent struct {
	Message string `json:"Message"`
}

// GenericJSONEvent is used when the SNS/SQS Message contains JSON
type GenericJSONEvent map[string]interface{}

// RdsEvent represents an RDS event delivered by RDS to SQS via SNS
// This spec doesn't seem to be documented anywhere, so this is a best-effort
// based on data that has come in
type RdsEvent struct {
	EventSource    string `json:"Event Source"`
	EventTime      string `json:"Event Time"`
	IdentifierLink string `json:"Identifier Link"`
	SourceID       string `json:"Source ID"`
	EventID        string `json:"Event ID"`
	EventMessage   string `json:"Event Message"`
}

// There doesn't seem to be a type for this in the AWS SDK
type snsEntity struct {
	Signature         string                 `json:"Signature"`
	MessageID         string                 `json:"MessageId"`
	Type              string                 `json:"Type"`
	TopicArn          string                 `json:"TopicArn"`
	MessageAttributes map[string]interface{} `json:"MessageAttributes"`
	SignatureVersion  string                 `json:"SignatureVersion"`
	Timestamp         time.Time              `json:"Timestamp"`
	SigningCertURL    string                 `json:"SigningCertUrl"`
	Message           string                 `json:"Message"`
	UnsubscribeURL    string                 `json:"UnsubscribeUrl"`
	Subject           string                 `json:"Subject"`
}
