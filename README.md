## Honeycomb AWS Bundle

[![OSS Lifecycle](https://img.shields.io/osslifecycle/honeycombio/honeyaws?color=success)](https://github.com/honeycombio/home/blob/main/honeycomb-oss-lifecycle-and-practices.md)
[![CircleCI](https://circleci.com/gh/honeycombio/honeyaws.svg?style=svg)](https://circleci.com/gh/honeycombio/honeyaws)

`honeyaws` is a collection of programs to send events from your AWS
infrastructure into [Honeycomb](https://www.honeycomb.io/), a service
for debugging your software in production.

- `honeyelb` - A tool for ingesting Elastic Load Balancer access logs.
  ([docs](https://honeycomb.io/docs/connect/aws-elastic-load-balancer))
- `honeyalb` - A tool for ingesting Application Load Balancer access logs.
- `honeycloudfront` - A tool for ingesting CloudFront access logs.
  ([docs](https://honeycomb.io/docs/connect/aws-cloudfront/))
- `honeycloudtrail` - A tool for ingesting CloudTrail logs.

[Usage & Examples](https://docs.honeycomb.io/getting-data-in/integrations/aws/aws-elastic-load-balancer/)

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
sample rate while using one of the Honeycomb AWS tools, use the `--samplerate`
flag. While the tools run, this base rate will be automatically adjusted by the
Honeycomb AWS tools using dynamic sampling to keep more interesting traffic at a
higher rate.

For instance, setting the sample flag to 20 will send 1 out of every 20 requests
processed to Honeycomb by default. Fields such as `elb_status_code` are used to
lower this ratio for rarer, but relevant, events such as HTTP 500-level errors.

```
$ honeyelb --samplerate 20 ...  ingest ...
```

### Sampler Type

You can choose between two implementations of dynamic sampling: `simple` or `ema`.
Complete details about these implementations can be found [here](https://github.com/honeycombio/dynsampler-go).

- `simple` looks at a single interval of traffic, defined by the `sampler_interval` arg, and computes sample rates
based on counts of traffic categories seen in that interval. At every interval, the results of the previous interval
are discarded.
- `ema` averages observations from each interval into a moving average of counts, and computes sample rates based
on those counts. Older observations are phased out at a rate specified by `sampler_decay`. Larger decay values mean that
sample rates are more heavily influenced by newer traffic

`simple` is suitable for most types of traffic, but we recommend using `ema` if your traffic comes in in bursts.

## Contributions

Features, bug fixes and other changes to the Honeycomb AWS Bundle are gladly
accepted. Please open issues or a pull request with your change. Remember to add
your name to the CONTRIBUTORS file!

All contributions will be released under the Apache License 2.0.
