# honeysqs

Simple utility to poll an SQS queue events and forward them to honeycomb. Supported event types right now are:

- **generic-sqs-json** - simple JSON-formatted messages in SQS
- **generic-sns-json** - simple JSON-formatted messages sent by SNS to SQS
- **rds** - AWS RDS events, sent to SQS via SNS

The JSON types only look at the first level of keys, so if you have nested objects you might be better off flattening your JSON events, or adding a new event type and translator (we accept pull requests!).

## Usage

```bash
# Your honeycomb write key
WRITE_KEY=CHANGEME
# Desired Dataset
DATASET=honeycomb-sqs-events
# Type of event
EVENT_TYPE=generic-sqs-json
# SQS Queue URL
QUEUE_URL=https://sqs.us-east-1.amazonaws.com/1234578900/my-events
# Sample rate, defaults to 1
SAMPLE_RATE=1
./honeysqs -k ${WRITE_KEY} -u ${QUEUE_URL} -d ${DATASET} -s ${SAMPLE_RATE} -t ${EVENT_TYPE}
```

### Minimum AWS Permissions

Your AWS user or service account only needs the following permissions to run honeysqs:

```
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Action": [
                "sqs:ReceiveMessage",
                "sqs:DeleteMessage"
            ],
            "Effect": "Allow",
            "Resource": "*"
        }
    ]
}
```

### Multiple queues and event types

For now, the best practice is to run one instance of the process per queue, and have one event type per queue. You can send events from different queues and types to the same dataset, but if the event types are significantly different or unrelated it's recommended that you use different datasets.

## Running in Kubernetes

We run honeysqs in Kubernetes at Honeycomb. Here's a sample spec to get you started.

```yaml
---
# sends SQS events into honeycomb
apiVersion: apps/v1 # for versions before 1.9.0 use apps/v1beta2
kind: Deployment
metadata:
  name: honeycomb-honeysqs
spec:
  selector:
    matchLabels:
      app: honeycomb-honeysqs
  replicas: 1
  template:
    metadata:
      labels:
        app: honeycomb-honeysqs
    spec:
      containers:
      - name: honeyaws
        image: honeycombio/honeyaws:latest
        command: ["honeysqs"]
        args:
          - -k
          - $(WRITE_KEY)
          - -d
          - $(DATASET)
          - -u
          - $(QUEUE_URL)
          - -t
          - $(EVENT_TYPE)
        resources:
          requests:
            cpu: 100m
            memory: 100Mi
        env:
        - name: WRITE_KEY
          valueFrom:
            secretKeyRef:
              name: honeycomb-write-keys
              key: write_key
        - name: DATASET
          value: honeycomb-sqs-events
        - name: QUEUE_URL
          value: https://sqs.us-east-1.amazonaws.com/1234578900/my-events
        - name: EVENT_TYPE
          value: generic-sqs-json
        - name: AWS_DEFAULT_REGION
          value: us-east-1
        - name: AWS_ACCESS_KEY_ID
          valueFrom:
            secretKeyRef:
              name: aws-sqs-svc-user
              key: aws_access_key_id
        - name: AWS_SECRET_ACCESS_KEY
          valueFrom:
            secretKeyRef:
              name: aws-sqs-svc-user
              key: aws_secret_access_key
...

```


