package state

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
)

const (
	stateFileFormat = "%s-state.json"
	DynamoTableName = "HoneyAWSAccessLogBuckets"
	TTLDefault      = time.Hour * 24 * 7
)

// Stater lets us gain insight into the current state of object processing. It
// could be backed by the local filesystem, cloud abstractions such as
// DynamoDB, consistent value stores like etcd, etc.
type Stater interface {
	// ProcessedObjects returns the full list of which objects have been
	// processed already.
	ProcessedObjects() (map[string]time.Time, error)

	// SetProcessed indicates that downloading, processing, and sending the
	// object to Honeycomb has been completed successfully.
	SetProcessed(object string) error
}

// Used to communicate between the various pieces which are relying on state
// information.
type DownloadedObject struct {
	Object, Filename string
}

type DynamoDBStater struct {
	Session          *session.Session
	BackfillInterval time.Duration
}

func NewDynamoDBStater(session *session.Session, backfillHrs int) (*DynamoDBStater, error) {
	stater := &DynamoDBStater{
		Session:          session,
		BackfillInterval: time.Hour * time.Duration(backfillHrs),
	}

	svc := dynamodb.New(session)
	input := &dynamodb.DescribeTableInput{
		TableName: aws.String(DynamoTableName),
	}
	_, err := svc.DescribeTable(input)
	if err != nil {
		// For some reason, we cannot write to
		// the table or access it
		return stater, err
	}

	return stater, nil
}

// Used for unmarshaling and adding objects to DynamoDB
type Record struct {
	S3Object string
	Time     time.Time
	TTL      int64 //future date formatted as unix seconds-since-epoch
}

// list of processed objects
func (d *DynamoDBStater) ProcessedObjects() (map[string]time.Time, error) {
	objs := make(map[string]time.Time)

	var records []Record

	svc := dynamodb.New(d.Session)
	err := svc.ScanPages(&dynamodb.ScanInput{
		TableName: aws.String(DynamoTableName),
	}, func(logs *dynamodb.ScanOutput, last bool) bool {
		recs := []Record{}

		err := dynamodbattribute.UnmarshalListOfMaps(logs.Items, &recs)

		// break out of function
		if err != nil {
			logrus.WithField("error", err).Debug("Failed to unmarshal DynamoDB Scan Items")
			return false
		}

		records = append(records, recs...)
		for _, rec := range records {
			// break out of the scan, we've reached the end of our
			// backfill interval
			if time.Since(rec.Time) > d.BackfillInterval {
				return false
			}
		}

		return true
	})

	if err != nil {
		return objs, fmt.Errorf("Error scanning DynamoDB, %v", err)
	}

	for _, record := range records {
		objs[record.S3Object] = record.Time
	}

	return objs, nil
}

func (d *DynamoDBStater) SetProcessed(s3object string) error {

	svc := dynamodb.New(d.Session)

	objMap := Record{
		S3Object: s3object,
		Time:     time.Now(),
		TTL:      time.Now().Add(TTLDefault).Unix(), //
	}

	obj, err := dynamodbattribute.MarshalMap(objMap)

	if err != nil {
		return fmt.Errorf("Marshalling DynamoDB object failed: %s", err)
	}

	// add object to dynamodb using conditional
	// if the object exists, no write happens
	input := &dynamodb.PutItemInput{
		Item:                obj,
		TableName:           aws.String(DynamoTableName),
		ConditionExpression: aws.String("attribute_not_exists(S3Object)"),
	}

	_, err = svc.PutItem(input)
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			// we want this to happen if object already exists
			if awsErr.Code() != dynamodb.ErrCodeConditionalCheckFailedException {
				return fmt.Errorf("PutItem failed: %s", err)
			}

		}
		// if it is the conditional check, we can just pop out and
		// ignore this object!
		return nil
	}

	return nil
}

// FileStater is an implementation for indicating processing state using the
// local filesystem for backing storage.
type FileStater struct {
	*sync.Mutex
	StateDir         string
	Service          string
	BackfillInterval time.Duration
}

func NewFileStater(stateDir, service string, backfillHrs int) *FileStater {
	return &FileStater{
		Mutex:            &sync.Mutex{},
		StateDir:         stateDir,
		Service:          service,
		BackfillInterval: time.Hour * time.Duration(backfillHrs),
	}
}

func (f *FileStater) stateFile() string {
	return filepath.Join(f.StateDir, fmt.Sprintf(stateFileFormat, f.Service))
}

func (f *FileStater) processedObjects() (map[string]time.Time, error) {
	objs := make(map[string]time.Time)

	if _, err := os.Stat(f.stateFile()); os.IsNotExist(err) {
		// make sure file exists first run
		if err := ioutil.WriteFile(f.stateFile(), []byte(`{}`), 0644); err != nil {
			return objs, fmt.Errorf("Error writing file: %s", err)
		}

		return objs, nil
	}

	data, err := ioutil.ReadFile(f.stateFile())
	if err != nil {
		return objs, fmt.Errorf("Error reading object cursor file: %s", err)
	}

	if err := json.Unmarshal(data, &objs); err != nil {
		return objs, fmt.Errorf("Unmarshalling state file JSON failed: %s", err)
	}

	return objs, nil
}

func (f *FileStater) ProcessedObjects() (map[string]time.Time, error) {
	f.Lock()
	defer f.Unlock()
	return f.processedObjects()
}

func (f *FileStater) SetProcessed(object string) error {
	f.Lock()
	defer f.Unlock()

	processedObjects, err := f.processedObjects()
	if err != nil {
		return err
	}

	// Reap old objects (outside of the "backfill interval"), otherwise the
	// state file will grow indefinitely.
	for k, v := range processedObjects {
		if time.Since(v) > f.BackfillInterval {
			delete(processedObjects, k)
		}
	}

	processedObjects[object] = time.Now()

	processedData, err := json.Marshal(processedObjects)
	if err != nil {
		return fmt.Errorf("Marshalling JSON failed: %s", err)
	}

	if err := ioutil.WriteFile(f.stateFile(), processedData, 0644); err != nil {
		return fmt.Errorf("Writing file failed: %s", err)
	}

	return nil
}
