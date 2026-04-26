package models

type AWSResource struct {
	Arn       string `neo4j:"arn"`
	AccountId string `neo4j:"accountId"`
}

type VPC struct {
	AWSResource
	VpcId  string `neo4j:"vpcId"`
	Cidr   string `neo4j:"cidr"`
	Name   string `neo4j:"name"`
	Region string `neo4j:"region"`
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
	Name    string `neo4j:"name"`
	Runtime string `neo4j:"runtime"`
	Region  string `neo4j:"region"`
	VpcId   string `neo4j:"vpcId"`
}

type S3Bucket struct {
	AWSResource
	Name   string `neo4j:"name"`
	Region string `neo4j:"region"`
}

type EC2Instance struct {
	AWSResource
	InstanceId   string `neo4j:"instanceId"`
	InstanceType string `neo4j:"instanceType"`
	State        string `neo4j:"state"`
	VpcId        string `neo4j:"vpcId"`    // vacío si no está en VPC
	SubnetId     string `neo4j:"subnetId"` // vacío si no está en VPC
	Region       string `neo4j:"region"`
	LaunchTime   string `neo4j:"launchTime"` // como ISO 8601 string para Neo4j
}
