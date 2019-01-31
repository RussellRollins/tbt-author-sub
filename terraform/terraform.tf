variable "aws_access_key" {}
variable "aws_secret_key" {}
variable "sendgrid_key" {}

variable "aws_region" {
  default = "us-east-1"
}

provider "aws" {
  access_key = "${var.aws_access_key}"
  secret_key = "${var.aws_secret_key}"
  region     = "${var.aws_region}"
}

resource "aws_iam_role" "lambda-role" {
  name = "lambda-role"

  assume_role_policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "lambda.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
EOF
}

resource "aws_iam_policy" "lambda-dynamodb-tables-policy" {
  name        = "lambda-dynamodb-policy"
  description = "grants access to all tables"

  policy = <<EOF
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "dynamodb:BatchGetItem",
                "dynamodb:BatchWriteItem",
                "dynamodb:DeleteItem",
                "dynamodb:GetItem",
                "dynamodb:PutItem",
                "dynamodb:Query",
                "dynamodb:UpdateItem"
            ],
            "Resource": [
                "arn:aws:dynamodb:*:*:table/*"
            ]
        }
    ]
}
EOF
}

resource "aws_iam_policy" "lambda-xray-policy" {
  name        = "lambda-xray-policy"
  description = "lambda-xray-policy"

  policy = <<EOF
{
    "Version": "2012-10-17",
    "Statement": {
        "Effect": "Allow",
        "Action": [
            "xray:PutTraceSegments",
            "xray:PutTelemetryRecords"
        ],
        "Resource": [
            "*"
        ]
    }
}
EOF
}

resource "aws_iam_policy" "lambda-cloudwatch-policy" {
  name        = "lambda-cloudwatch-policy"
  description = "lambda-cloudwatch-policy"

  policy = <<EOF
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "logs:CreateLogStream",
                "logs:PutLogEvents"
            ],
            "Resource": "*"
        },
        {
            "Effect": "Allow",
            "Action": "logs:CreateLogGroup",
            "Resource": "*"
        }
      ]
}
EOF
}

resource "aws_iam_role_policy_attachment" "role-policy-attach-1" {
  role       = "${aws_iam_role.lambda-role.name}"
  policy_arn = "${aws_iam_policy.lambda-dynamodb-tables-policy.arn}"
}

resource "aws_iam_role_policy_attachment" "role-policy-attach-2" {
  role       = "${aws_iam_role.lambda-role.name}"
  policy_arn = "${aws_iam_policy.lambda-xray-policy.arn}"
}

resource "aws_iam_role_policy_attachment" "role-policy-attach-3" {
  role       = "${aws_iam_role.lambda-role.name}"
  policy_arn = "${aws_iam_policy.lambda-cloudwatch-policy.arn}"
}

resource "aws_dynamodb_table" "stories-table" {
  name           = "stories"
  billing_mode   = "PROVISIONED"
  read_capacity  = 20
  write_capacity = 20

  hash_key = "url"

  attribute {
    name = "url"
    type = "S"
  }
}

resource "aws_lambda_function" "tbt-author-sub" {
  filename         = "../build/tbt-author-sub.zip"
  role             = "${aws_iam_role.lambda-role.arn}"
  source_code_hash = "${base64sha256(file("../build/tbt-author-sub.zip"))}"
  function_name    = "tbt-author-sub"
  handler          = "tbt-author-sub"
  runtime          = "go1.x"
  memory_size      = 128
  timeout          = 30
  publish          = true

  tracing_config {
    mode = "Active"
  }

  environment {
    variables = {
      SENDGRID_KEY = "${var.sendgrid_key}"
    }
  }
}

resource "aws_cloudwatch_event_rule" "every-15-minutes" {
  name                = "every-15-minutes"
  description         = "Fires every 15 minutes"
  schedule_expression = "rate(15 minutes)"
}

resource "aws_cloudwatch_event_target" "run-every-15-minutes" {
  rule      = "${aws_cloudwatch_event_rule.every-15-minutes.name}"
  target_id = "tbt-author-sub"
  arn       = "${aws_lambda_function.tbt-author-sub.arn}"
}

resource "aws_lambda_permission" "allow-cloudwatch-to-call-tbt-author-sub" {
  statement_id  = "AllowExecutionFromCloudWatch"
  action        = "lambda:InvokeFunction"
  function_name = "${aws_lambda_function.tbt-author-sub.function_name}"
  principal     = "events.amazonaws.com"
  source_arn    = "${aws_cloudwatch_event_rule.every-15-minutes.arn}"
}
