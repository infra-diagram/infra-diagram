package crawler

import (
	"context"
	"log"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type EC2Crawler struct {
	client *ec2.Client
	region string
}

func NewEC2Crawler(client *ec2.Client, region string) *EC2Crawler {
	return &EC2Crawler{client: client, region: region}
}

func (c *EC2Crawler) IngestNodes(ctx context.Context, session neo4j.SessionWithContext) error {
	paginator := ec2.NewDescribeInstancesPaginator(c.client, &ec2.DescribeInstancesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}

		for _, reservation := range page.Reservations {
			for _, i := range reservation.Instances {
				arn := BuildArn("ec2", c.region, *reservation.OwnerId, "instance", *i.InstanceId)
				_, err := session.Run(ctx,
					` Merge (i:EC2Instance {arn: $arn})
						Set i.id = $id, i.type = $type, i.state = $state, i.launchTime = $launchTime`,
					map[string]any{
						"arn":        arn,
						"id":         *i.InstanceId,
						"type":       string(i.InstanceType),
						"state":      string(i.State.Name),
						"launchTime": i.LaunchTime,
					},
				)

				if err != nil {
					log.Printf("neo4j error for: %s: %v", *i.InstanceId, err)
				}
			}
		}
	}
	return nil
}

func (c *EC2Crawler) IngestRelations(ctx context.Context, session neo4j.SessionWithContext) error {
	paginator := ec2.NewDescribeInstancesPaginator(c.client, &ec2.DescribeInstancesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)

		if err != nil {
			return err
		}

		for _, reservation := range page.Reservations {
			for _, i := range reservation.Instances {
				arn := BuildArn("ec2", c.region, *reservation.OwnerId, "instance", *i.InstanceId)

				// EC2 -> VPC
				if i.VpcId != nil {
					session.Run(ctx,
						`Match (i:EC2Instance {arn: $arn})
						Match (v:VPC {vpcId: $vpcId})
						Merge (i)-[BELONGS_TO]->(v)`,
						map[string]any{"arn": arn, "vpcId": *i.VpcId},
					)
				}

				// EC2 -> IAM Role
				if i.IamInstanceProfile != nil {
					session.Run(ctx,
						`Match(i:EC2Instance {arn: $arn})
						Match (r:IAMRole {arn: $roleArn})
						Merge (i)-[:ASSUMES_ROLE]->(r)`,
						map[string]any{"arn": arn, "roleArn": *i.IamInstanceProfile.Arn},
					)
				}

				// EC2 -> EBS Volumes (relación embedded en BlockedDevicedMappings)
				for _, bdm := range i.BlockDeviceMappings {
					if bdm.Ebs != nil {
						volArn := BuildArn("ec2", c.region, *reservation.OwnerId, "volume", *bdm.Ebs.VolumeId)
						session.Run(ctx,
							`Match (i:EC2Instance {arn: $arn})
							Match (v:EBSVolume {arn: $volArn})
							Merge (i)-[:HAS_VOLUME {device: $device}]->(v)`,
							map[string]any{
								"arn":    arn,
								"volArn": volArn,
								"device": *bdm.DeviceName,
							},
						)
					}
				}
			}
		}
	}
	return nil
}
