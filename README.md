## Honeycomb AWS Bundle

`honeyaws` is a collection of programs to enable observability in your AWS
infrastructure. They get information from AWS and publish it to Honeycomb as
events for later querying and debugging.

- `honeyelb` - A tool for ingesting Elastic Load Balancer access logs.
  ([docs](https://honeycomb.io/docs/connect/aws-elastic-load-balancer))
- `honeycloudfront` - A tool for ingesting CloudFront access logs.
  ([docs](https://honeycomb.io/docs/connect/aws-cloudfront/))
- `honeycloudtrail` - A tool for ingesting CloudTrail logs

The Honeycomb AWS Bundle is currently considered in beta. It is OK to use, but
may have occasional rough edges or bugs.

## Install

To install a tool from the Honeycomb AWS Bundle, `go get` or `go install` from
the properly directory in `cmd/` like so:

```
$ go get github.com/honeycombio/honeyaws/cmd/honeyelb
```

For an official build, see the docs for the tool you are interested in (linked
above).

## Usage

Ensure that IAM credentials are properly provided where you are invoking the
tools (e.g., via environment variables) and you have a Honeycomb write key.
Additionally, you may need to enable access logs, etc., for whichever service
you wish to ingest information from.  The S3 bucket where they are kept will be
looked up automatically.

Most commands can list the targets for observation (`ls`), as well as invoke
`ingest` to publish the information (access log lines, etc.) as events to
Honeycomb.

For instance, let's take a look at `honeyelb`.

To list load balancers:

```
$ honeyelb ls
foo-lb
bar-lb
quux-lb
```

To ingest LB access logs to Honeycomb by name using `ingest`, specify the
name(s) as an argument:

```
$ honeyelb --writekey=<writekey> ingest foo-lb
... ingesting ...
```

To ingest all LBs, use `honeyelb ingest` without any non-flag arguments.

## High Availability

There exists the option to run the Honeycomb AWS binaries in a high availability
mode. This is done using [DynamoDB](https://aws.amazon.com/dynamodb/)
for management of processed log files. There are a few things that must be
set up before running `highavail`.

First, a table must be created with the name `HoneyAWSAccessLogBuckets` with a
primary key named `S3Object` and a sort key named `Time`. We also require that TTL be 
enabled (we don't want your table to grow infinitely!) with the attribute name
`TTL`. The TTL for objects is 7 days.

Conveniently, we provide you with a CloudFormation
template to do just this!

```
$ aws cloudformation create-stack --stack-name DynamoDBHoneyAWS \
    --template-body file://cloudformation/dynamoDB.yml
```

Once this table is created, you can simply add the `--highavail` flag to
`honeyelb` or `honeycloudfront`.

```
$ honeyelb --highavail --writekey=<writekey> ingest foo-lb
```

Now you can have multiple EC2 instances ingesting logs!

## Sampling

Sampling is a great way to send fewer events (thereby keeping more history and
reducing costs) while still preserving most relevant information. To set a
sample rate while using one of the Honeycomb AWS tools, use the `--sample-rate`
flag. While the tools run, this base rate will be automatically adjusted by the
Honeycomb AWS tools using dynamic sampling to keep more interesting traffic at a
higher rate.

For instance, setting the sample flag to 20 will send 1 out of every 20 requests
processed to Honeycomb by default. Fields such as `elb_status_code` are used to
lower this ratio for rarer, but relevant, events such as HTTP 500-level errors.

```
$ honeyelb --sample-rate 20 ...  ingest ...
```

## Contributions

Features, bug fixes and other changes to the Honeycomb AWS Bundle are gladly
accepted. Please open issues or a pull request with your change. Remember to add
your name to the CONTRIBUTORS file!

All contributions will be released under the Apache License 2.0.
