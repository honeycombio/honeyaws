## Honeycomb AWS Bundle

[![OSS Lifecycle](https://img.shields.io/osslifecycle/honeycombio/honeyaws?color=success)](https://github.com/honeycombio/home/blob/main/honeycomb-oss-lifecycle-and-practices.md)
[![CircleCI](https://circleci.com/gh/honeycombio/honeyaws.svg?style=svg)](https://circleci.com/gh/honeycombio/honeyaws)

`honeyaws` is a collection of programs to send events from your AWS
infrastructure into [Honeycomb](https://www.honeycomb.io/), a service
for debugging your software in production.

- `honeyelb` - A tool for ingesting Elastic Load Balancer access logs.
  ([docs](https://honeycomb.io/docs/connect/aws-elastic-load-balancer))
- `honeyalb` - A tool for ingesting Application Load Balancer access and connection logs.
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

### ALB connection logs

`honeyalb` ingests access logs by default. Use `--alb_log_type=connection` to
ingest only connection logs, or `--alb_log_type=both` to ingest both types:

```sh
honeyalb --alb_log_type=both --writekey=<writekey> ingest <alb-name>
```

Enable each selected log type on the ALB. Access and connection locations are
discovered independently; connection-only ingestion does not require access
logging. See the AWS documentation for
[access logs](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-access-logs.html)
and [connection logs](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-connection-logs.html).

To look up the log locations, honeyalb needs
`elasticloadbalancing:DescribeLoadBalancers` and
`elasticloadbalancing:DescribeLoadBalancerAttributes`. Give the AWS identity
running honeyalb the following permissions for each log location:

| IAM action | Resource |
| --- | --- |
| `s3:ListBucket` | The bucket ARN, such as `arn:aws:s3:::BUCKET`. |
| `s3:GetObject` | The selected log object ARNs, such as `arn:aws:s3:::BUCKET/PREFIX/AWSLogs/ACCOUNT_ID/elasticloadbalancing/REGION/*`. |

If `s3:ListBucket` is restricted using a `StringLike` condition on `s3:prefix`,
allow the date-based prefixes honeyalb lists under that log location. Within
each `YYYY/MM/DD/` directory, honeyalb selects filenames using these prefixes:

| Log type | Filename prefix |
| --- | --- |
| Access | `ACCOUNT_ID_elasticloadbalancing_REGION_app.LB_NAME.` |
| Connection | `conn_log_ACCOUNT_ID_elasticloadbalancing_REGION_app.LB_NAME.` |

The IAM actions are the same for both log types. Listing conditions and object
permissions must cover the selected filename prefixes; ingesting `both` requires
both. Omit `PREFIX/` from the example ARN when no log prefix is configured. See the
[S3 prefix policy examples](https://docs.aws.amazon.com/AmazonS3/latest/userguide/amazon-s3-policy-keys.html).

These are honeyalb's read permissions. The ALB log-delivery bucket policy is
configured separately when enabling logging.

Both types use `--dataset` (default `aws-elb-access`). Filter by `log_type=access`
for requests or `log_type=connection` for connection attempts. Connection fields
use AWS names; `tls_handshake_latency` is in seconds and unavailable `-` values
are omitted. Older connection records obtain `elb` from the S3 filename.

Filter by `conn_trace_id` and `elb` to group retained events from a connection,
including requests whose connection event was sampled out. Application trace IDs
are preserved separately. Older records without `conn_trace_id` are accepted;
new trailing fields are ignored. See [Sampling](#sampling) for retention controls.

## High Availability

There exists the option to run the Honeycomb AWS binaries in a high availability
mode. This is done using [DynamoDB](https://aws.amazon.com/dynamodb/)
for management of processed log files. There are a few things that must be
set up before running `highavail`.

First, a table must be created with the name `HoneyAWSAccessLogBuckets` with a
primary key named `S3Object`. We also require that TTL be
enabled (we don't want your table to grow infinitely!) with the attribute name
`TTL`. The TTL for objects is 7 days.

Conveniently, we provide you with a CloudFormation
template to do just this!

```
$ aws cloudformation create-stack --stack-name DynamoDBHoneyAWS \
    --template-body file://cloudformation/dynamoDB.yml
```

We also have a [terraform snippet](terraform/dynamodb_table.tf) that you can use.

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
Use `--sampler_type` to select one; the default is `simple`.
Complete details about these implementations can be found [here](https://github.com/honeycombio/dynsampler-go).

- `simple` looks at a single interval of traffic, defined by `--sampler_interval`
  (default `300` seconds), and computes sample rates based on counts of traffic
  categories seen in that interval. At every interval, the results of the previous
  interval are discarded.
- `ema` averages observations from each interval into a moving average of counts,
  and computes sample rates based on those counts. Older observations are phased
  out at a rate specified by `--sampler_decay` (default `0.5`, used only by `ema`).
  Larger decay values mean that sample rates are more heavily influenced by
  newer traffic.

`simple` is suitable for most types of traffic, but we recommend using `ema` if your traffic comes in in bursts.

### ALB sampling fields

`honeyalb` groups access events by backend status code, load balancer status code,
and load balancer name by default. To replace that key, repeat `--sampler_fields` for
each parsed ALB log field to include:

```sh
honeyalb --sampler_type=ema --samplerate=20 \
  --sampler_fields=elb --sampler_fields=domain_name \
  --sampler_fields=elb_status_code --writekey=<writekey> ingest <alb-name>
```

Omitting `--sampler_fields` preserves the existing access sampling key.
Explicit fields replace the entire key; include `elb` to keep load balancers in
separate groups. This option applies only to `honeyalb`, with either sampler type.

Fields are read before request shaping and trace enrichment. For example,
`domain_name`, `target_group_arn`, and `elb_status_code` are available, while derived
`request_shape` and `request_method` are not. Missing fields form a null key
component, distinct from an empty string. Check field names carefully: an unknown
field is also missing. Keys preserve field boundaries and numeric/string values.

Use fields with recurring values. A unique trace ID or raw request URL can make
nearly every event a rare group and reduce the effectiveness of dynamic sampling.
The `--samplerate` target applies across the configured groups, not independently
to each key. The sample rate attached to retained events continues to reflect
the sampler's actual per-key rate.

### ALB connection sampling

To sample connection logs with `honeyalb`, use `--connection_samplerate`. The
access and connection sample rates are independent. If you omit this flag, it
defaults to `1` and all parsed connection events are sent, even when
`--samplerate` is set to a higher value for access logs.

For instance, setting `--connection_samplerate=10` targets roughly one out of
every ten successful connections. The connection sampler uses the
[sampler type, interval, and decay described above](#sampler-type), with its own
sampling history:

```sh
honeyalb --alb_log_type=both --sampler_type=ema \
  --samplerate=20 --connection_samplerate=10 \
  --writekey=<writekey> ingest <alb-name>
```

Connection logs are grouped by `tls_verify_status`, `tls_protocol`, and `elb` by
default. To replace that key, repeat `--connection_sampler_fields` for each
connection field to include. The access-log `--sampler_fields` setting does not
affect connection logs. Keep in mind that sampling access logs gives you a
partial count of requests per connection.

### ALB error retention

With `honeyalb`, the following errors are always sent at sample rate `1`:

- Connections with `tls_verify_status` starting with `Failed:`.
- Requests with ALB or target 5xx status, a populated `error_reason`, or negative
  request, backend, or response processing time.
- Requests with ALB or target 4xx status when `--keep_4xx` is set.

Keeping errors can mean sending more events than the sampling target. This
applies to records honeyalb successfully reads and parses; delivery from AWS or
through the downloader is not guaranteed.

## Contributions

Features, bug fixes and other changes to the Honeycomb AWS Bundle are gladly
accepted. Please open issues or a pull request with your change. Remember to add
your name to the CONTRIBUTORS file!

All contributions will be released under the Apache License 2.0.
