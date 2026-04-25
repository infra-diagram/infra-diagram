package crawler

import (
	"fmt"
	"strings"
)

// BuildArn construye ARNs con el formato estándar:
// arn:aws:{service}:{region}:{accountId}:{resource}/{id}
//
// Nota: S3 usa un formato distinto (sin región ni accountId).
// Para S3 usar: "arn:aws:s3:::" + bucketName
func BuildArn(service, region, accountId, resource, id string) string {
	return fmt.Sprintf("arn:aws:%s:%s:%s:%s/%s",
		service,
		region,
		accountId,
		resource,
		id,
	)
}

func GetAccountIdFromArn(arn string) (string, error) {
	parts := strings.Split(arn, ":")

	if len(parts) < 6 {
		return "", fmt.Errorf("invalid ARN: %s", arn)
	}

	accountId := parts[4]
	if accountId == "" {
		return "", fmt.Errorf("account ID not found in ARN: %s", arn)
	}
	return accountId, nil
}

func GetResourceNameFromArn(arn string) (string, error) {
	parts := strings.Split(arn, ":")

	if len(parts) < 6 {
		return "", fmt.Errorf("invalid ARN: %s", arn)
	}

	resourceName := parts[5]
	if resourceName == "" {
		return "", fmt.Errorf("resource name not found in ARN: %s", arn)
	}
	return resourceName, nil
}

func GetRegionFromArn(arn string) (string, error) {
	parts := strings.Split(arn, ":")

	if len(parts) < 6 {
		return "", fmt.Errorf("invalid ARN: %s", arn)
	}

	region := parts[3]
	if region == "" {
		return "", fmt.Errorf("region ID not found in ARN: %s", arn)
	}
	return region, nil
}
