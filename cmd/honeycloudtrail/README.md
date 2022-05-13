# Overview

Honeycloudtrail ingests cloudtrail logs for single-region, multi-region, or Organization Cloud Trails.

## Env Vars

`HONEYCLOUDTRAIL_ROLE_ARN` - If you have created a special role for accessing your Cloud Trail, for example [per the AWS docs](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-sharing-logs.html), you can provide the arn as an environment variable, and honeycloudtrail will assume that role.

Organization Cloud Trails by default are multiregion, and write all account logs to the same bucket. You can control which regions and accounts are ingested with these env vars. By default only the session's region and account logs will be ingested.

`HONEYCLOUDTRAIL_COLLECT_REGIONS` - A comma-delimited list of regions used to specify which regions in an Organization trail should be ingested. If unset, the session's region will be used.
ex: `HONEYCLOUDTRAIL_COLLECT_REGIONS=ap-northeast-1,eu-central-1,eu-west-1,us-east-2,us-west-1,us-west-2`

`HONEYCLOUDTRAIL_COLLECT_ACCOUNTS` - A comma-delimited list of account ids, used to specify which accounts in an Organization trail should be ingested. If unset, the sessions account id will be used.
ex: `HONEYCLOUDTRAIL_COLLECT_ACCOUNTS=12345678,66654321,98765555`

## CLI Args

-   `organization_id` - As discussed in the [AWS docs on locating Cloud Trail logs](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-find-log-files.html), Organization Cloud Trail s3 Paths will have the organization id in them. You can pass this in via the `organization_id` flag so that S3 paths will be properly formatted.
-   `multiregion` - If you have multiregion cloudtrails or trails in regions other than the one honeycloudtrail is running in, pass in the `multiregion` flag to be able to see details on them.
