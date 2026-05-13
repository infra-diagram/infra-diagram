terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# ----------------------------------------------------------------------
# SQS — Dead Letter Queue
# Mensajes que fallan despues de N intentos en la queue principal terminan aqui.
# ----------------------------------------------------------------------
resource "aws_sqs_queue" "worker_dlq" {
  name                       = "${var.project_name}-worker-dlq"
  message_retention_seconds  = 1209600  # 14 días — mantenemos los fallos para debugging
  visibility_timeout_seconds = 60

  tags = local.common_tags
}

# ----------------------------------------------------------------------
# SQS — Queue principal
# Recibe eventos de EventBridge. Tu worker hace polling de aqui.
# ----------------------------------------------------------------------
resource "aws_sqs_queue" "worker_queue" {
  name = "${var.project_name}-worker-queue"

  # Para desarrollo: 1 día. En producción subiríamos a 4-14 días.
  message_retention_seconds = 86400

  # Tiempo que un mensaje queda invisible despues de ReceiveMessage.
  # Si el worker no llama DeleteMessage en este tiempo, el mensaje
  # vuelve a estar disponible (otro consumer lo agarra).
  visibility_timeout_seconds = 60

  # Long polling: ReceiveMessage espera hasta 20s si no hay mensajes,
  # en vez de devolver vacio inmediatamente. Reduce costos y latencia.
  receive_wait_time_seconds = 20

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.worker_dlq.arn
    maxReceiveCount     = 5  # despues de 5 intentos fallidos, va a DLQ
  })

  tags = local.common_tags
}

# ----------------------------------------------------------------------
# SQS Queue Policy — permite a EventBridge enviar mensajes
# Sin esta policy, EventBridge no puede escribir a la queue.
# ----------------------------------------------------------------------
resource "aws_sqs_queue_policy" "worker_queue_policy" {
  queue_url = aws_sqs_queue.worker_queue.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = { Service = "events.amazonaws.com" }
        Action    = "sqs:SendMessage"
        Resource  = aws_sqs_queue.worker_queue.arn
        Condition = {
          ArnEquals = {
            "aws:SourceArn" = aws_cloudwatch_event_rule.worker_rule.arn
          }
        }
      }
    ]
  })
}

# ----------------------------------------------------------------------
# EventBridge Rule
# Filtra eventos de CloudTrail que cambian recursos.
# Solo eventos en aws.ec2, aws.lambda, aws.s3, aws.sqs.
# ----------------------------------------------------------------------
resource "aws_cloudwatch_event_rule" "worker_rule" {
  name        = "${var.project_name}-resource-changes"
  description = "Captura eventos que crean, modifican o eliminan recursos AWS"

  event_pattern = jsonencode({
    source      = ["aws.ec2", "aws.lambda", "aws.s3", "aws.sqs", "aws.iam"]
    detail-type = ["AWS API Call via CloudTrail"]
    detail = {
      eventName = [
        # EC2 / VPC / Subnet
        "RunInstances", "TerminateInstances",
        "CreateVpc", "DeleteVpc",
        "CreateSubnet", "DeleteSubnet",
        # EC2 — instance profile (EC2Handler reconstruye INSTANCE_PROFILE_OF)
        "AssociateIamInstanceProfile", "DisassociateIamInstanceProfile",

        # Lambda
        "CreateFunction20150331", "DeleteFunction20150331",
        "UpdateFunctionConfiguration20150331v2",
        "CreateEventSourceMapping", "DeleteEventSourceMapping",
        "UpdateEventSourceMapping",

        # S3
        "CreateBucket", "DeleteBucket",
        "PutBucketNotificationConfiguration",

        # SQS
        "CreateQueue", "DeleteQueue",

        # IAM Roles
        "CreateRole", "UpdateRole",
        "UpdateAssumeRolePolicy",
        "AttachRolePolicy", "DetachRolePolicy",
        "PutRolePolicy",
        "DeleteRole",

        # IAM Policies
        "CreatePolicy",
        "CreatePolicyVersion", "SetDefaultPolicyVersion",
        "DeletePolicy"
      ]
    }
  })

  tags = local.common_tags
}

# ----------------------------------------------------------------------
# EventBridge Target — apunta la rule a la SQS queue
# ----------------------------------------------------------------------
resource "aws_cloudwatch_event_target" "worker_target" {
  rule = aws_cloudwatch_event_rule.worker_rule.name
  arn  = aws_sqs_queue.worker_queue.arn
}

# ----------------------------------------------------------------------
# Tags comunes para todos los recursos
# ----------------------------------------------------------------------
locals {
  common_tags = {
    Project     = var.project_name
    Environment = var.environment
    ManagedBy   = "terraform"
  }
}