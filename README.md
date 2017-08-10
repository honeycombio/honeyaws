## honeyelb

This is the home of the source code for the agent to ingest AWS Elastic Load
Balancer access logs into [Honeycomb](https://honeycomb.io/). For reference on
usage of the tool, please see [the
documentation](https://honeycomb.io/docs/connect/aws-elastic-load-balancer).

`honeyelb` is currently considered in beta. It is OK to use, but may have
occasional rough edges or bugs.

## Install

From source:

```
$ go get github.com/honeycombio/honeyelb
```

For an official build, see the
[docs](https://honeycomb.io/docs/connect/aws-elastic-load-balancer).

## Usage

Ensure that IAM credentials are properly provided (e.g., via environment
variables) and you have a Honeycomb write key. Additionally, access logs will
need to be enabled for whichever load balancer(s) you wish to ingest logs from.
The S3 bucket where they are kept will be looked up automatically.

List load balancers:

```
$ honeyelb ls
foo-lb
bar-lb
quux-lb
```

Ingest LB access logs to Honeycomb by name using `ingest`:

```
$ honeyelb --writekey=<writekey> ingest foo-lb
... ingesting ...
```

To ingest all LBs, use `honeyelb ingest` without any non-flag arguments.

## Contributions

Features, bug fixes and other changes to honeyelb are gladly accepted. Please
open issues or a pull request with your change. Remember to add your name to the
CONTRIBUTORS file!

All contributions will be released under the Apache License 2.0.
