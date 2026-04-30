# Worker pipeline infrastructure

## Setup inicial

```bash
cd infra/terraform
terraform init
terraform plan
terraform apply
```

## Verificar que funciona

```bash
# Ver la URL de la queue
terraform output queue_url

# Generar un evento de prueba (crea y elimina una queue dummy)
aws sqs create-queue --queue-name terraform-test-queue
aws sqs delete-queue --queue-url $(aws sqs get-queue-url --queue-name terraform-test-queue --query QueueUrl --output text)

# Esperar 30 segundos y revisar la queue
aws sqs receive-message --queue-url $(terraform output -raw queue_url) --max-number-of-messages 10
```

## Tear down

```bash
terraform destroy
```

Esto **elimina todos los recursos creados**. El CloudTrail no se toca porque no fue creado por este Terraform.