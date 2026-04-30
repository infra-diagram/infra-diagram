output "queue_url" {
  description = "URL de la SQS queue principal — el worker lee de aquí"
  value       = aws_sqs_queue.worker_queue.url
}

output "queue_arn" {
  description = "ARN de la queue principal"
  value       = aws_sqs_queue.worker_queue.arn
}

output "dlq_url" {
  description = "URL del dead letter queue"
  value       = aws_sqs_queue.worker_dlq.url
}

output "rule_arn" {
  description = "ARN de la EventBridge rule"
  value       = aws_cloudwatch_event_rule.worker_rule.arn
}