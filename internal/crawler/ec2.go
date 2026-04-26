package crawler

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
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

			for _, i := range reservation.Instances {
				if i.InstanceId == nil {
					continue
				}

				arn := BuildArn("ec2", c.region, accountId, "instance", *i.InstanceId)

				// Defensivos: cualquiera de estos campos puede venir nil.
				state := ""
				if i.State != nil {
					state = string(i.State.Name)
				}

				vpcId := ""
				if i.VpcId != nil {
					vpcId = *i.VpcId
				}

				subnetId := ""
				if i.SubnetId != nil {
					subnetId = *i.SubnetId
				}

				launchTime := ""
				if i.LaunchTime != nil {
					launchTime = i.LaunchTime.UTC().Format(time.RFC3339)
				}

				node := models.EC2Instance{
					AWSResource: models.AWSResource{
						Arn:       arn,
						AccountId: accountId,
					},
					InstanceId:   *i.InstanceId,
					InstanceType: string(i.InstanceType),
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
					log.Printf("neo4j error for %s: %v", node.InstanceId, err)
				}
			}
		}
	}
	return nil
}

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

			for _, i := range reservation.Instances {
				if i.InstanceId == nil {
					continue
				}
				instanceArn := BuildArn("ec2", c.region, accountId, "instance", *i.InstanceId)

				// EC2 -> VPC
				if i.VpcId != nil {
					vpcArn := BuildArn("ec2", c.region, accountId, "vpc", *i.VpcId)

					rel := models.BelongsToRelation{
						ChildArn:  instanceArn,
						ParentArn: vpcArn,
					}

					if _, err := session.Run(ctx,
						`MATCH (e:EC2Instance:AWSResource {arn: $childArn})
		 					MATCH (v:VPC:AWSResource {arn: $parentArn})
		 					MERGE (e)-[:BELONGS_TO]->(v)`,
						map[string]any{
							"childArn":  rel.ChildArn,
							"parentArn": rel.ParentArn,
						},
					); err != nil {
						log.Printf("BELONGS_TO error for %s: %v", *i.InstanceId, err)
					}
				}

				// EC2 -> Subnet
				if i.SubnetId != nil {
					subnetArn := BuildArn("ec2", c.region, accountId, "subnet", *i.SubnetId)

					if _, err := session.Run(ctx,
						`MATCH (e:EC2Instance:AWSResource {arn: $instanceArn})
		 					MATCH (s:Subnet:AWSResource {arn: $subnetArn})
		 					MERGE (e)-[:USES_SUBNET]->(s)`,
						map[string]any{
							"instanceArn": instanceArn,
							"subnetArn":   subnetArn,
						},
					); err != nil {
						log.Printf("USES_SUBNET error for %s: %v", *i.InstanceId, err)
					}
				}

				// TODO post-MVP: EC2 -> IAM Role (necesita IAMCrawler)
				// TODO post-MVP: EC2 -> EBS Volume (necesita EBSCrawler)
				// TODO post-MVP: EC2 -> SecurityGroup (necesita SecurityGroupCrawler)
			}
		}
	}
	return nil
}
