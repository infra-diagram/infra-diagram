package graph

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func CreateIndexes(ctx context.Context, driver neo4j.DriverWithContext) error {
	indexes := []string{
		"CREATE INDEX idx_resource_arn  			IF NOT EXISTS FOR (n:AWSResource) ON (n.arn)",
		"CREATE INDEX idx_vpc_id        			IF NOT EXISTS FOR (n:VPC)         ON (n.vpcId)",
		"CREATE INDEX idx_subnet_id     			IF NOT EXISTS FOR (n:Subnet)      ON (n.subnetId)",
		"CREATE INDEX idx_ec2_instance_id			IF NOT EXIST FOR (n:EC2Instance) ON (n.id)",
		"CREATE TEXT INDEX idx_resource_name 	IF NOT EXISTS FOR (n:AWSResource) ON (n.name)",
	}

	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	for _, query := range indexes {
		if _, err := session.Run(ctx, query, nil); err != nil {
			return fmt.Errorf("creating index: %w", err)
		}
	}
	return nil
}
