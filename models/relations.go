package models

// Cada struct de relación tiene exactamente lo que necesita
// el Cypher query para hacer el MATCH de los dos extremos
// y el MERGE de la relación entre ellos.

// (:Queue)-[:TRIGGERS {state}]->(:Function)
// Fuente: lambda.ListEventSourceMappings
type TriggerRelation struct {
	SourceArn string `neo4j:"sourceArn"` // ARN de la Queue (o Kinesis, DynamoDB, etc.)
	TargetArn string `neo4j:"targetArn"` // ARN de la Function
	State     string `neo4j:"state"`     // "Enabled", "Disabled"
}

// (:Subnet)-[:BELONGS_TO]->(:VPC)
// Fuente: ec2.DescribeSubnets → campo VpcId embebido en la respuesta
type BelongsToRelation struct {
	ChildArn  string `neo4j:"childArn"`  // ARN de la Subnet
	ParentArn string `neo4j:"parentArn"` // ARN del VPC
}

// (:S3Bucket)-[:NOTIFIES {events}]->(:Function)
// Fuente: s3.GetBucketNotificationConfiguration
type NotifiesRelation struct {
	SourceArn string   `neo4j:"sourceArn"` // ARN del S3 Bucket
	TargetArn string   `neo4j:"targetArn"` // ARN de la Function
	Events    []string `neo4j:"events"`    // ["s3:ObjectCreated:*", ...]
}
