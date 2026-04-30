# CloudFormation alternativa

## Deploy

```bash
aws cloudformation deploy \
  --template-file worker-pipeline.yaml \
  --stack-name infra-diagram-worker \
  --parameter-overrides ProjectName=infra-diagram Environment=dev
```

## Verificar que funciona

```bash
# Ver outputs
aws cloudformation describe-stacks \
  --stack-name infra-diagram-worker \
  --query 'Stacks[0].Outputs'

# Test
aws sqs create-queue --queue-name cf-test-queue
aws sqs delete-queue --queue-url $(aws sqs get-queue-url --queue-name cf-test-queue --query QueueUrl --output text)

# Recibir mensaje (~30s después)
QUEUE_URL=$(aws cloudformation describe-stacks --stack-name infra-diagram-worker --query 'Stacks[0].Outputs[?OutputKey==`QueueUrl`].OutputValue' --output text)
aws sqs receive-message --queue-url $QUEUE_URL --max-number-of-messages 10
```

## Tear down

```bash
aws cloudformation delete-stack --stack-name infra-diagram-worker
```