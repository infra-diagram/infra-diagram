package events

import (
	"testing"
)

const sampleCreateQueue = `{
  "version": "0",
  "id": "8b675ce3-c971-2c7f-aeb5-3ae349692a79",
  "detail-type": "AWS API Call via CloudTrail",
  "source": "aws.sqs",
  "account": "110727845776",
  "time": "2026-04-30T20:39:49Z",
  "region": "us-east-1",
  "resources": [],
  "detail": {
    "eventVersion": "1.11",
    "eventTime": "2026-04-30T20:39:49Z",
    "eventSource": "sqs.amazonaws.com",
    "eventName": "CreateQueue",
    "awsRegion": "us-east-1",
    "userIdentity": {
      "type": "IAMUser",
      "arn": "arn:aws:iam::110727845776:user/Engineering/EydenVillanuevaAlpuche",
      "accountId": "110727845776",
      "userName": "EydenVillanuevaAlpuche"
    },
    "requestParameters": {"queueName": "terraform-test-trigger"},
    "responseElements": {"queueUrl": "https://sqs.us-east-1.amazonaws.com/110727845776/terraform-test-trigger"},
    "resources": [
      {
        "accountId": "110727845776",
        "type": "AWS::SQS::Queue",
        "ARN": "arn:aws:sqs:us-east-1:110727845776:terraform-test-trigger"
      }
    ]
  }
}`

func TestParseCreateQueue(t *testing.T) {
	msg, err := ParseMessage(sampleCreateQueue)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if msg.Source != "aws.sqs" {
		t.Errorf("expected source aws.sqs, got %s", msg.Source)
	}
	if msg.Detail.EventName != "CreateQueue" {
		t.Errorf("expected eventName CreateQueue, got %s", msg.Detail.EventName)
	}
	expectedARN := "arn:aws:sqs:us-east-1:110727845776:terraform-test-trigger"
	if got := msg.Detail.PrimaryResourceARN(); got != expectedARN {
		t.Errorf("expected ARN %s, got %s", expectedARN, got)
	}
}
