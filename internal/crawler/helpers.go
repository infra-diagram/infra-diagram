package crawler

import (
	"fmt"
	"strings"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
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

func GetNameFromTags(tags []ec2types.Tag) string {
	for _, tag := range tags {
		if tag.Key == nil || tag.Value == nil {
			continue
		}
		if *tag.Key == "Name" {
			return *tag.Value
		}
	}
	return ""
}

func BuildS3Arn(bucketName string) string {
	return "arn:aws:s3:::" + bucketName
}

// GetIAMResourceNameFromArn extrae el nombre del recurso IAM desde su ARN.
// Maneja paths con múltiples segmentos (service-linked roles, paths organizacionales):
//
//	arn:aws:iam::123:role/my-role                                    → "my-role"
//	arn:aws:iam::123:role/aws-service-role/elb.amazonaws.com/my-role → "my-role"
//	arn:aws:iam::123:policy/my-policy                                → "my-policy"
//	arn:aws:iam::123:instance-profile/my-prof                        → "my-prof"
//
// NOTA: no llamar GetRegionFromArn para ARNs IAM — el campo región está vacío.
func GetIAMResourceNameFromArn(arn string) (string, error) {
	parts := strings.Split(arn, ":")
	if len(parts) < 6 {
		return "", fmt.Errorf("invalid IAM ARN: %s", arn)
	}
	// parts[5] = "role/path/to/name" o "instance-profile/name"
	segments := strings.Split(parts[5], "/")
	if len(segments) < 2 {
		return "", fmt.Errorf("invalid IAM resource segment in ARN: %s", arn)
	}
	return segments[len(segments)-1], nil
}
