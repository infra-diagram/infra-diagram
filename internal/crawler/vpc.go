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

type VPCCrawler struct {
	client *ec2.Client
	region string
}

func NewVPCCrawler(client *ec2.Client, region string) *VPCCrawler {
	return &VPCCrawler{client: client, region: region}
}

// Client expone el cliente EC2 subyacente.
func (c *VPCCrawler) Client() *ec2.Client {
	return c.client
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
			if err := c.ingestSingleFromVpc(ctx, session, vpc); err != nil {
				log.Printf("ingesting vpc: %v", err)
				continue
			}
		}
	}
	return nil
}

// IngestSingle ingiere una sola VPC por su ID.
// Usado por el worker cuando llega un evento CreateVpc. Hace un re-fetch
// puntual contra DescribeVpcs filtrando por ese ID.
func (c *VPCCrawler) IngestSingle(ctx context.Context, session neo4j.SessionWithContext, vpcId string) error {
	resp, err := c.client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		VpcIds: []string{vpcId},
	})
	if err != nil {
		return fmt.Errorf("describing vpc %s: %w", vpcId, err)
	}
	if len(resp.Vpcs) == 0 {
		return fmt.Errorf("vpc %s not found", vpcId)
	}

	return c.ingestSingleFromVpc(ctx, session, resp.Vpcs[0])
}

// IngestSingleWithDriver es la variante de conveniencia para callers que no
// tienen una sesión activa (típicamente el worker).
func (c *VPCCrawler) IngestSingleWithDriver(ctx context.Context, driver neo4j.DriverWithContext, vpcId string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	return c.IngestSingle(ctx, session, vpcId)
}

// ingestSingleFromVpc es la lógica compartida entre IngestNodes (que ya
// tiene el ec2types.Vpc del paginador) e IngestSingle (que lo obtuvo via
// DescribeVpcs). Evita duplicar el upsert.
func (c *VPCCrawler) ingestSingleFromVpc(ctx context.Context, session neo4j.SessionWithContext, vpc ec2types.Vpc) error {
	if vpc.VpcId == nil || vpc.CidrBlock == nil {
		return fmt.Errorf("vpc missing required fields")
	}

	accountId := ""
	if vpc.OwnerId != nil {
		accountId = *vpc.OwnerId
	}

	isDefault := false
	if vpc.IsDefault != nil {
		isDefault = *vpc.IsDefault
	}

	vpcArn := BuildArn("ec2", c.region, accountId, "vpc", *vpc.VpcId)

	node := models.VPC{
		AWSResource: models.AWSResource{
			Arn:       vpcArn,
			AccountId: accountId,
		},
		VpcId:     *vpc.VpcId,
		Cidr:      *vpc.CidrBlock,
		Region:    c.region,
		Name:      GetNameFromTags(vpc.Tags),
		IsDefault: isDefault,
		State:     string(vpc.State),
	}

	_, err := session.Run(ctx,
		`MERGE (v:VPC:AWSResource {arn: $arn})
		 SET v.vpcId     = $vpcId,
		     v.cidr      = $cidr,
		     v.region    = $region,
		     v.name      = $name,
		     v.isDefault = $isDefault,
		     v.state     = $state,
		     v.accountId = $accountId`,
		map[string]any{
			"arn":       node.Arn,
			"vpcId":     node.VpcId,
			"cidr":      node.Cidr,
			"region":    node.Region,
			"name":      node.Name,
			"isDefault": node.IsDefault,
			"state":     node.State,
			"accountId": node.AccountId,
		},
	)
	if err != nil {
		return fmt.Errorf("merging vpc %s: %w", node.VpcId, err)
	}

	return nil
}

// DeleteByArn remueve un nodo VPC del grafo.
func (c *VPCCrawler) DeleteByArn(ctx context.Context, driver neo4j.DriverWithContext, arn string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.Run(ctx,
		`MATCH (v:VPC:AWSResource {arn: $arn})
		 DETACH DELETE v`,
		map[string]any{"arn": arn},
	)
	if err != nil {
		return fmt.Errorf("deleting vpc %s: %w", arn, err)
	}
	return nil
}

func (c *VPCCrawler) IngestRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	// VPC no tiene relaciones salientes en el MVP.
	// Las relaciones BELONGS_TO desde Subnets/EC2/Lambda se manejan en sus crawlers.
	return nil
}
