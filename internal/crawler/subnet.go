package crawler

import (
	"context"
	"fmt"
	"infra-diagram/internal/models"
	"log"

	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type SubnetCrawler struct {
	client *ec2.Client
}

func NewSubnetCrawler(client *ec2.Client) *SubnetCrawler {
	return &SubnetCrawler{client: client}
}

func (c *SubnetCrawler) IngestNodes(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	paginator := ec2.NewDescribeSubnetsPaginator(c.client, &ec2.DescribeSubnetsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing subnets: %w", err)
		}

		for _, subnet := range page.Subnets {
			// nil check
			if subnet.SubnetId == nil ||
				subnet.SubnetArn == nil ||
				subnet.VpcId == nil ||
				subnet.CidrBlock == nil ||
				subnet.AvailabilityZone == nil {
				continue
			}

			accountId := ""
			if subnet.OwnerId != nil {
				accountId = *subnet.OwnerId
			}

			if err != nil {
				log.Printf("skipping %s: %v", *subnet.SubnetArn, err)
				continue
			}

			node := models.Subnet{
				AWSResource: models.AWSResource{
					Arn:       *subnet.SubnetArn,
					AccountId: accountId,
				},
				SubnetId:         *subnet.SubnetId,
				VpcId:            *subnet.VpcId,
				Cidr:             *subnet.CidrBlock,
				AvailabilityZone: *subnet.AvailabilityZone,
				IsPublic:         false, // TODO: inferir el valor de IsPublic revisando las route tables (la subnet que tenga una ruta hacia un Internet Gateway será pública)
			}

			_, err = session.Run(ctx,
				`MERGE (s:Subnet:AWSResource {arn: $arn})
				 SET s.accountId = $accountId,
						 s.subnetId = $subnetId,
						 s.vpcId = $vpcId,
						 s.cidr = $cidr,
						 s.availabilityZone = $availabilityZone,
						 s.isPublic = $isPublic`,
				map[string]any{
					"arn":              node.Arn,
					"accountId":        node.AccountId,
					"subnetId":         node.SubnetId,
					"vpcId":            node.VpcId,
					"cidr":             node.Cidr,
					"availabilityZone": node.AvailabilityZone,
					"isPublic":         node.IsPublic,
				},
			)

			if err != nil {
				log.Printf("neo4j error for %s: %v", node.SubnetId, err)
			}
		}
	}
	return nil
}

func (c *SubnetCrawler) IngestRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	if err := c.ingestVpcRelations(ctx, driver); err != nil {
		return err
	}

	// TODO: add the rest of ingestRelationships methods
	return nil
}

func (c *SubnetCrawler) ingestVpcRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	paginator := ec2.NewDescribeSubnetsPaginator(c.client, &ec2.DescribeSubnetsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)

		if err != nil {
			return fmt.Errorf("listing subnets for vpc relations: %w", err)
		}

		for _, subnet := range page.Subnets {
			// nil check
			if subnet.SubnetId == nil ||
				subnet.SubnetArn == nil ||
				subnet.VpcId == nil ||
				subnet.CidrBlock == nil ||
				subnet.AvailabilityZone == nil {
				continue
			}

			accountId := ""
			if subnet.OwnerId != nil {
				accountId = *subnet.OwnerId
			}

			if err != nil {
				log.Printf("skipping %s: %v", *subnet.SubnetArn, err)
				continue
			}

			region, err := GetRegionFromArn(*subnet.SubnetArn)

			if err != nil {
				log.Printf("skipping %s: %v", *subnet.SubnetArn, err)
				continue
			}

			vpcArn := BuildArn("ec2", region, accountId, "vpc", *subnet.VpcId)

			rel := models.BelongsToRelation{
				ChildArn:  *subnet.SubnetArn,
				ParentArn: vpcArn,
			}

			if _, err := session.Run(ctx,
				`MATCH (s:Subnet:AWSResource {arn: $childArn})
				 MATCH (v:VPC:AWSResource {arn: $parentArn})
			 	 MERGE (s)-[:BELONGS_TO]->(v)`,
				map[string]any{
					"childArn":  rel.ChildArn,
					"parentArn": rel.ParentArn,
				},
			); err != nil {
				log.Printf("BELONGS_TO error for %s: %v", *subnet.SubnetId, err)
			}
		}
	}
	return nil
}
