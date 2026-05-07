package crawler

import (
	"context"
	"fmt"
	"infra-diagram/internal/models"
	"log"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type SubnetCrawler struct {
	client *ec2.Client
}

func NewSubnetCrawler(client *ec2.Client) *SubnetCrawler {
	return &SubnetCrawler{client: client}
}

func (c *SubnetCrawler) Client() *ec2.Client {
	return c.client
}

// =============================================================================
// FASE 1 — Nodos
// =============================================================================

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
			if err := c.upsertNodeFromSubnet(ctx, session, subnet); err != nil {
				log.Printf("ingesting subnet: %v", err)
				continue
			}
		}
	}
	return nil
}

// IngestSingle ingiere SOLO el nodo de una sola subnet por su ID.
// No toca relaciones. Para eso usa RebuildRelations.
func (c *SubnetCrawler) IngestSingle(ctx context.Context, session neo4j.SessionWithContext, subnetId string) error {
	subnet, err := c.fetchSubnet(ctx, subnetId)
	if err != nil {
		return err
	}
	return c.upsertNodeFromSubnet(ctx, session, subnet)
}

func (c *SubnetCrawler) IngestSingleWithDriver(ctx context.Context, driver neo4j.DriverWithContext, subnetId string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	return c.IngestSingle(ctx, session, subnetId)
}

// upsertNodeFromSubnet hace SOLO el MERGE del nodo. No relaciones.
// Compartido entre IngestNodes (bulk) e IngestSingle (puntual).
func (c *SubnetCrawler) upsertNodeFromSubnet(ctx context.Context, session neo4j.SessionWithContext, subnet ec2types.Subnet) error {
	if subnet.SubnetArn == nil ||
		subnet.SubnetId == nil ||
		subnet.VpcId == nil ||
		subnet.CidrBlock == nil ||
		subnet.AvailabilityZone == nil {
		return fmt.Errorf("subnet missing required fields")
	}

	accountId := ""
	if subnet.OwnerId != nil {
		accountId = *subnet.OwnerId
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
		IsPublic:         false,
	}

	_, err := session.Run(ctx,
		`MERGE (s:Subnet:AWSResource {arn: $arn})
		 SET s.subnetId         = $subnetId,
		     s.vpcId            = $vpcId,
		     s.cidr             = $cidr,
		     s.availabilityZone = $availabilityZone,
		     s.isPublic         = $isPublic,
		     s.accountId        = $accountId`,
		map[string]any{
			"arn":              node.Arn,
			"subnetId":         node.SubnetId,
			"vpcId":            node.VpcId,
			"cidr":             node.Cidr,
			"availabilityZone": node.AvailabilityZone,
			"isPublic":         node.IsPublic,
			"accountId":        node.AccountId,
		},
	)
	if err != nil {
		return fmt.Errorf("merging subnet %s: %w", node.SubnetId, err)
	}
	return nil
}

// fetchSubnet hace un re-fetch puntual de una subnet desde AWS.
func (c *SubnetCrawler) fetchSubnet(ctx context.Context, subnetId string) (ec2types.Subnet, error) {
	resp, err := c.client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		SubnetIds: []string{subnetId},
	})
	if err != nil {
		return ec2types.Subnet{}, fmt.Errorf("describing subnet %s: %w", subnetId, err)
	}
	if len(resp.Subnets) == 0 {
		return ec2types.Subnet{}, fmt.Errorf("subnet %s not found", subnetId)
	}
	return resp.Subnets[0], nil
}

// =============================================================================
// FASE 2 — Relaciones
// =============================================================================

func (c *SubnetCrawler) IngestRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	paginator := ec2.NewDescribeSubnetsPaginator(c.client, &ec2.DescribeSubnetsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing subnets for relations: %w", err)
		}

		for _, subnet := range page.Subnets {
			if err := c.rebuildRelationsFromSubnet(ctx, session, subnet); err != nil {
				log.Printf("rebuilding subnet relations: %v", err)
				continue
			}
		}
	}
	return nil
}

// RebuildRelations recrea las relaciones salientes de una sola subnet.
// Usado por el worker tras IngestSingle.
func (c *SubnetCrawler) RebuildRelations(ctx context.Context, session neo4j.SessionWithContext, subnetId string) error {
	subnet, err := c.fetchSubnet(ctx, subnetId)
	if err != nil {
		return err
	}
	return c.rebuildRelationsFromSubnet(ctx, session, subnet)
}

func (c *SubnetCrawler) RebuildRelationsWithDriver(ctx context.Context, driver neo4j.DriverWithContext, subnetId string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	return c.RebuildRelations(ctx, session, subnetId)
}

// rebuildRelationsFromSubnet hace el rebuild de BELONGS_TO → VPC.
// Compartido entre IngestRelations (bulk) y RebuildRelations (puntual).
func (c *SubnetCrawler) rebuildRelationsFromSubnet(ctx context.Context, session neo4j.SessionWithContext, subnet ec2types.Subnet) error {
	if subnet.SubnetArn == nil || subnet.VpcId == nil {
		return nil
	}

	accountId := ""
	if subnet.OwnerId != nil {
		accountId = *subnet.OwnerId
	}

	region, err := GetRegionFromArn(*subnet.SubnetArn)
	if err != nil {
		return fmt.Errorf("parsing region: %w", err)
	}

	vpcArn := BuildArn("ec2", region, accountId, "vpc", *subnet.VpcId)
	return rebuildBelongsTo(ctx, session, *subnet.SubnetArn, vpcArn)
}

// =============================================================================
// DELETE
// =============================================================================

func (c *SubnetCrawler) DeleteByArn(ctx context.Context, driver neo4j.DriverWithContext, arn string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.Run(ctx,
		`MATCH (s:Subnet:AWSResource {arn: $arn})
		 DETACH DELETE s`,
		map[string]any{"arn": arn},
	)
	if err != nil {
		return fmt.Errorf("deleting subnet %s: %w", arn, err)
	}
	return nil
}
