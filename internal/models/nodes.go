package models

type AWSResource struct {
	Arn       string `neo4j:"arn"`
	AccountId string `neo4j:"accountId"`
}

type VPC struct {
	AWSResource
	VpcId     string `neo4j:"vpcId"`
	Cidr      string `neo4j:"cidr"`
	Name      string `neo4j:"name"`
	Region    string `neo4j:"region"`
	IsDefault bool   `neo4j:"isDefault"`
	State     string `neo4j:"state"`
}

type Subnet struct {
	AWSResource
	SubnetId         string `neo4j:"subnetId"`
	VpcId            string `neo4j:"vpcId"`
	Cidr             string `neo4j:"cidr"`
	AvailabilityZone string `neo4j:"availabilityZone"`
	IsPublic         bool   `neo4j:"isPublic"`
}

type Queue struct {
	AWSResource
	Name   string `neo4j:"name"`
	Url    string `neo4j:"url"`
	Region string `neo4j:"region"`
}

type Function struct {
	AWSResource
	Name            string `neo4j:"name"`
	Runtime         string `neo4j:"runtime"`
	Region          string `neo4j:"region"`
	VpcId           string `neo4j:"vpcId"`
	ExecutionRoleArn string `neo4j:"executionRoleArn"` // ARN del IAM role de ejecución
}

type S3Bucket struct {
	AWSResource
	Name   string `neo4j:"name"`
	Region string `neo4j:"region"`
}

type EC2Instance struct {
	AWSResource
	InstanceId            string `neo4j:"instanceId"`
	InstanceType          string `neo4j:"instanceType"`
	State                 string `neo4j:"state"`
	VpcId                 string `neo4j:"vpcId"`    // vacío si no está en VPC
	SubnetId              string `neo4j:"subnetId"` // vacío si no está en VPC
	Region                string `neo4j:"region"`
	LaunchTime            string `neo4j:"launchTime"` // como ISO 8601 string para Neo4j
	IamInstanceProfileArn string `neo4j:"iamInstanceProfileArn"` // vacío si no tiene instance profile
}

type IAMRole struct {
	AWSResource
	RoleName           string `neo4j:"roleName"`
	RoleId             string `neo4j:"roleId"`
	Path               string `neo4j:"path"`
	CreateDate         string `neo4j:"createDate"`         // ISO 8601
	Description        string `neo4j:"description"`        // vacío si nil en SDK
	MaxSessionDuration int32  `neo4j:"maxSessionDuration"` // segundos, 0 si nil
}

type IAMPolicy struct {
	AWSResource
	PolicyName      string `neo4j:"policyName"`
	PolicyId        string `neo4j:"policyId"`
	Path            string `neo4j:"path"`
	Description     string `neo4j:"description"`    // vacío en bulk; poblado en IngestSinglePolicy
	CreateDate      string `neo4j:"createDate"`     // ISO 8601
	UpdateDate      string `neo4j:"updateDate"`     // ISO 8601
	AttachmentCount int32  `neo4j:"attachmentCount"` // 0 si sin attachments
}
