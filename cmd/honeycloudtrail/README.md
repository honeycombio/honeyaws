# Overview

Honeycloudtrail ingests cloudtrail logs for single-region, multi-region, or Organization Cloud Trails.

## Env Vars

### IAM

`HONEYCLOUDTRAIL_ROLE_ARN` - If you have created a special role for accessing your Cloud Trail [per the AWS docs](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-sharing-logs.html), you can provide the arn as an environment variable, and honeycloudtrail will assume that role.

### Multi-region and Organization Trails

Organization Cloud Trails by default are multiregion, and write all account logs to the same bucket. You can control which regions and accounts are ingested with these env vars. By default only logs for the Trail's region and the session's account id will be ingested.

`HONEYCLOUDTRAIL_COLLECT_REGIONS` - A comma-delimited list of regions used to specify which regions in an Organization trail should be ingested. If unset, the Trail's region will be used. If set, no other regions will be collected.
ex: `HONEYCLOUDTRAIL_COLLECT_REGIONS=ap-northeast-1,eu-central-1,eu-west-1,us-east-2,us-west-1,us-west-2`

`HONEYCLOUDTRAIL_COLLECT_ACCOUNTS` - A comma-delimited list of account ids, used to specify which accounts in an Organization trail should be ingested. If unset, the sessions account id will be used. If set, no other accounts will be collected.
ex: `HONEYCLOUDTRAIL_COLLECT_ACCOUNTS=12345678,66654321,98765555`

## CLI Args

### Multi-region and Organization Trails

-   `organization_id` - As discussed in the [AWS docs on locating Cloud Trail logs](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-find-log-files.html), Organization Cloud Trail s3 Paths will have the organization id in them. You can pass this in via the `organization_id` flag so that S3 paths will be properly formatted.
-   `find_trails_in_all_regions` - If you have multiregion cloudtrails or trails in regions other than the one honeycloudtrail is running in, pass in the `find_trails_in_all_regions` flag to be able to ingest them.
-   `lsa` - Works like `ls`, but lists the cloudtrail ARN instead of the name. The ARN is required for describing and ingesting Trails outside the session region.
