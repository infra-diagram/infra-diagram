package crawler

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/models"
)

type EC2Crawler struct {
	client *ec2.Client
	region string
}

func NewEC2Crawler(client *ec2.Client, region string) *EC2Crawler {
	return &EC2Crawler{client: client, region: region}
}

func (c *EC2Crawler) Client() *ec2.Client {
	return c.client
}

// =============================================================================
// FASE 1 — Nodos
// =============================================================================

func (c *EC2Crawler) IngestNodes(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	paginator := ec2.NewDescribeInstancesPaginator(c.client, &ec2.DescribeInstancesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing ec2 instances: %w", err)
		}

		for _, reservation := range page.Reservations {
			if reservation.OwnerId == nil {
				continue
			}
			accountId := *reservation.OwnerId

			for _, instance := range reservation.Instances {
				if err := c.upsertNodeFromInstance(ctx, session, instance, accountId); err != nil {
					log.Printf("ingesting ec2 instance: %v", err)
					continue
				}
			}
		}
	}
	return nil
}

func (c *EC2Crawler) IngestSingle(ctx context.Context, session neo4j.SessionWithContext, instanceId string) error {
	instance, accountId, err := c.fetchInstance(ctx, instanceId)
	if err != nil {
		return err
	}
	return c.upsertNodeFromInstance(ctx, session, instance, accountId)
}

func (c *EC2Crawler) IngestSingleWithDriver(ctx context.Context, driver neo4j.DriverWithContext, instanceId string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	return c.IngestSingle(ctx, session, instanceId)
}

func (c *EC2Crawler) upsertNodeFromInstance(ctx context.Context, session neo4j.SessionWithContext, instance ec2types.Instance, accountId string) error {
	if instance.InstanceId == nil {
		return fmt.Errorf("instance missing InstanceId")
	}

	arn := BuildArn("ec2", c.region, accountId, "instance", *instance.InstanceId)

	state := ""
	if instance.State != nil {
		state = string(instance.State.Name)
	}

	vpcId := ""
	if instance.VpcId != nil {
		vpcId = *instance.VpcId
	}

	subnetId := ""
	if instance.SubnetId != nil {
		subnetId = *instance.SubnetId
	}

	launchTime := ""
	if instance.LaunchTime != nil {
		launchTime = instance.LaunchTime.UTC().Format(time.RFC3339)
	}

	node := models.EC2Instance{
		AWSResource: models.AWSResource{
			Arn:       arn,
			AccountId: accountId,
		},
		InstanceId:   *instance.InstanceId,
		InstanceType: string(instance.InstanceType),
		State:        state,
		VpcId:        vpcId,
		SubnetId:     subnetId,
		Region:       c.region,
		LaunchTime:   launchTime,
	}

	_, err := session.Run(ctx,
		`MERGE (e:EC2Instance:AWSResource {arn: $arn})
		 SET e.instanceId   = $instanceId,
		     e.instanceType = $instanceType,
		     e.state        = $state,
		     e.vpcId        = $vpcId,
		     e.subnetId     = $subnetId,
		     e.region       = $region,
		     e.accountId    = $accountId,
		     e.launchTime   = $launchTime`,
		map[string]any{
			"arn":          node.Arn,
			"instanceId":   node.InstanceId,
			"instanceType": node.InstanceType,
			"state":        node.State,
			"vpcId":        node.VpcId,
			"subnetId":     node.SubnetId,
			"region":       node.Region,
			"accountId":    node.AccountId,
			"launchTime":   node.LaunchTime,
		},
	)
	if err != nil {
		return fmt.Errorf("merging instance %s: %w", node.InstanceId, err)
	}
	return nil
}

// fetchInstance hace re-fetch puntual y retorna también el accountId del
// reservation, que necesitamos para construir el ARN.
func (c *EC2Crawler) fetchInstance(ctx context.Context, instanceId string) (ec2types.Instance, string, error) {
	resp, err := c.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceId},
	})
	if err != nil {
		return ec2types.Instance{}, "", fmt.Errorf("describing instance %s: %w", instanceId, err)
	}
	if len(resp.Reservations) == 0 || len(resp.Reservations[0].Instances) == 0 {
		return ec2types.Instance{}, "", fmt.Errorf("instance %s not found", instanceId)
	}

	reservation := resp.Reservations[0]
	if reservation.OwnerId == nil {
		return ec2types.Instance{}, "", fmt.Errorf("instance %s reservation missing OwnerId", instanceId)
	}

	return reservation.Instances[0], *reservation.OwnerId, nil
}

// =============================================================================
// FASE 2 — Relaciones
// =============================================================================

func (c *EC2Crawler) IngestRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	paginator := ec2.NewDescribeInstancesPaginator(c.client, &ec2.DescribeInstancesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing ec2 instances for relations: %w", err)
		}

		for _, reservation := range page.Reservations {
			if reservation.OwnerId == nil {
				continue
			}
			accountId := *reservation.OwnerId

			for _, instance := range reservation.Instances {
				if err := c.rebuildRelationsFromInstance(ctx, session, instance, accountId); err != nil {
					log.Printf("rebuilding ec2 relations: %v", err)
					continue
				}
			}
		}
	}
	return nil
}

func (c *EC2Crawler) RebuildRelations(ctx context.Context, session neo4j.SessionWithContext, instanceId string) error {
	instance, accountId, err := c.fetchInstance(ctx, instanceId)
	if err != nil {
		return err
	}
	return c.rebuildRelationsFromInstance(ctx, session, instance, accountId)
}

func (c *EC2Crawler) RebuildRelationsWithDriver(ctx context.Context, driver neo4j.DriverWithContext, instanceId string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	return c.RebuildRelations(ctx, session, instanceId)
}

func (c *EC2Crawler) rebuildRelationsFromInstance(ctx context.Context, session neo4j.SessionWithContext, instance ec2types.Instance, accountId string) error {
	if instance.InstanceId == nil {
		return nil
	}
	instanceArn := BuildArn("ec2", c.region, accountId, "instance", *instance.InstanceId)

	// EC2 → VPC
	if instance.VpcId != nil {
		vpcArn := BuildArn("ec2", c.region, accountId, "vpc", *instance.VpcId)
		if err := rebuildBelongsTo(ctx, session, instanceArn, vpcArn); err != nil {
			return fmt.Errorf("rebuilding BELONGS_TO: %w", err)
		}
	}

	// EC2 → Subnet
	if instance.SubnetId != nil {
		subnetArn := BuildArn("ec2", c.region, accountId, "subnet", *instance.SubnetId)
		if err := rebuildUsesSubnet(ctx, session, instanceArn, subnetArn); err != nil {
			return fmt.Errorf("rebuilding USES_SUBNET: %w", err)
		}
	}

	return nil
}

// =============================================================================
// DELETE
// =============================================================================

func (c *EC2Crawler) DeleteByArn(ctx context.Context, driver neo4j.DriverWithContext, arn string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.Run(ctx,
		`MATCH (e:EC2Instance:AWSResource {arn: $arn})
		 DETACH DELETE e`,
		map[string]any{"arn": arn},
	)
	if err != nil {
		return fmt.Errorf("deleting ec2 instance %s: %w", arn, err)
	}
	return nil
}
