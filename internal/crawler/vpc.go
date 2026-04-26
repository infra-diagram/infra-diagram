package crawler

import (
	"context"
	"fmt"
	"infra-diagram/internal/models"
	"log"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type VPCCrawler struct {
	client *ec2.Client
	region string
}

func NewVPCCrawler(client *ec2.Client, region string) *VPCCrawler {
	return &VPCCrawler{client: client, region: region}
}

func (c *VPCCrawler) IngestNodes(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	paginator := ec2.NewDescribeVpcsPaginator(c.client, &ec2.DescribeVpcsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing vpcs: %w", err)
		}

		for _, vpc := range page.Vpcs {
			//nil check
			if vpc.CidrBlock == nil ||
				vpc.IsDefault == nil {
				continue
			}

			accountId := *vpc.OwnerId

			vpcArn := BuildArn("ec2", c.region, accountId, "vpc", *vpc.VpcId)
			name := GetNameFromTags(vpc.Tags)
			isDefault := false

			if vpc.IsDefault != nil {
				isDefault = *vpc.IsDefault
			}

			node := models.VPC{
				AWSResource: models.AWSResource{
					Arn:       vpcArn,
					AccountId: accountId,
				},
				VpcId:     *vpc.VpcId,
				Cidr:      *vpc.CidrBlock,
				Region:    c.region,
				Name:      name,
				IsDefault: isDefault,
				State:     string(vpc.State),
			}

			_, err := session.Run(ctx,
				`MERGE (v:VPC:AWSResource {arn: $arn})
				SET v.vpcId = $vpcId,
						v.cidr = $cidr,
						v.region = $region,
						v.name = $name,
						v.isDefault = $isDefault,
						v.state = $state`,
				map[string]any{
					"arn":       node.Arn,
					"vpcId":     node.VpcId,
					"cidr":      node.Cidr,
					"region":    node.Region,
					"name":      node.Name,
					"isDefault": node.IsDefault,
					"state":     node.State,
				},
			)

			if err != nil {
				log.Printf("neo4j error for %s: %v", node.Name, err)
			}
		}
	}
	return nil
}

func (c *VPCCrawler) IngestRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	return nil
}
