resource "aws_dynamodb_table" "honey_aws_access_log_bucket" {
  name = "HoneyAWSAccessLogBuckets"

  hash_key  = "S3Object"

  attribute {
    name = "S3Object"
    type = "S"
  }

  read_capacity  = 10
  write_capacity = 10

  ttl {
    attribute_name = "TTL"
    enabled        = true
  }
}
