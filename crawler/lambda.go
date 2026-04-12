package crawler

import (
	"context"
	"fmt"
	"infra-diagram/models"
	"log"

	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type LambdaCrawler struct {
	client *lambda.Client
}

func NewLambdaCrawler(client *lambda.Client) *LambdaCrawler {
	return &LambdaCrawler{client: client}
}

func (c *LambdaCrawler) IngestNodes(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	paginator := lambda.NewListFunctionsPaginator(c.client, &lambda.ListFunctionsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing lambdas: %w", err)
		}

		for _, fn := range page.Functions {
			region, err := GetRegionFromArn(*fn.FunctionArn)
			if err != nil {
				log.Printf("skipping %s: %v", *fn.FunctionArn, err)
				continue
			}

			accountId, err := GetAccountIdFromArn(*fn.FunctionArn)
			if err != nil {
				log.Printf("skipping %s: %v", *fn.FunctionArn, err)
				continue
			}

			vpcId := ""
			if fn.VpcConfig != nil && fn.VpcConfig.VpcId != nil {
				vpcId = *fn.VpcConfig.VpcId
			}

			node := models.Function{
				AWSResource: models.AWSResource{
					Arn:       *fn.FunctionArn,
					AccountId: accountId,
				},
				Name:    *fn.FunctionName,
				Runtime: string(fn.Runtime),
				Region:  region,
				VpcId:   vpcId,
			}

			_, err = session.Run(ctx,
				`MERGE (f:Function:AWSResource {arn: $arn})
				 SET f.name      = $name,
				     f.runtime   = $runtime,
				     f.region    = $region,
				     f.accountId = $accountId,
				     f.vpcId     = $vpcId`,
				map[string]any{
					"arn":       node.Arn,
					"name":      node.Name,
					"runtime":   node.Runtime,
					"region":    node.Region,
					"accountId": node.AccountId,
					"vpcId":     node.VpcId,
				},
			)
			if err != nil {
				log.Printf("neo4j error for %s: %v", node.Name, err)
			}
		}
	}
	return nil
}

func (c *LambdaCrawler) IngestRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	if err := c.ingestEventSourceMappings(ctx, driver); err != nil {
		return err
	}
	if err := c.ingestVPCRelations(ctx, driver); err != nil {
		return err
	}
	return nil
}

func (c *LambdaCrawler) ingestEventSourceMappings(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	paginator := lambda.NewListEventSourceMappingsPaginator(c.client, &lambda.ListEventSourceMappingsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing event source mappings: %w", err)
		}

		for _, m := range page.EventSourceMappings {
			if m.State == nil || (*m.State != "Enabled" && *m.State != "Creating") {
				continue
			}

			rel := models.TriggerRelation{
				SourceArn: *m.EventSourceArn,
				TargetArn: *m.FunctionArn,
				State:     *m.State,
			}

			_, err := session.Run(ctx,
				`MATCH (src:AWSResource {arn: $sourceArn})
				 MATCH (fn:Function:AWSResource {arn: $targetArn})
				 MERGE (src)-[:TRIGGERS {state: $state}]->(fn)`,
				map[string]any{
					"sourceArn": rel.SourceArn,
					"targetArn": rel.TargetArn,
					"state":     rel.State,
				},
			)
			if err != nil {
				log.Printf("TRIGGERS relation error: %v", err)
			}
		}
	}
	return nil
}

func (c *LambdaCrawler) ingestVPCRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	paginator := lambda.NewListFunctionsPaginator(c.client, &lambda.ListFunctionsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing lambdas for vpc relations: %w", err)
		}

		for _, fn := range page.Functions {
			if fn.VpcConfig == nil || fn.VpcConfig.VpcId == nil {
				continue
			}

			region, err := GetRegionFromArn(*fn.FunctionArn)
			if err != nil {
				log.Printf("skipping vpc relation for %s: %v", *fn.FunctionArn, err)
				continue
			}

			accountId, err := GetAccountIdFromArn(*fn.FunctionArn)
			if err != nil {
				log.Printf("skipping vpc relation for %s: %v", *fn.FunctionArn, err)
				continue
			}

			vpcArn := BuildArn("ec2", region, accountId, "vpc", *fn.VpcConfig.VpcId)

			rel := models.BelongsToRelation{
				ChildArn:  *fn.FunctionArn,
				ParentArn: vpcArn,
			}

			if _, err := session.Run(ctx,
				`MATCH (f:Function:AWSResource {arn: $childArn})
				 MATCH (v:VPC:AWSResource {arn: $parentArn})
				 MERGE (f)-[:DEPLOYED_IN]->(v)`,
				map[string]any{
					"childArn":  rel.ChildArn,
					"parentArn": rel.ParentArn,
				},
			); err != nil {
				log.Printf("DEPLOYED_IN error for %s: %v", *fn.FunctionName, err)
			}

			for _, subnetId := range fn.VpcConfig.SubnetIds {
				subnetArn := BuildArn("ec2", region, accountId, "subnet", subnetId)

				if _, err := session.Run(ctx,
					`MATCH (f:Function:AWSResource {arn: $fnArn})
					 MATCH (s:Subnet:AWSResource {arn: $subnetArn})
					 MERGE (f)-[:USES_SUBNET]->(s)`,
					map[string]any{
						"fnArn":     *fn.FunctionArn,
						"subnetArn": subnetArn,
					},
				); err != nil {
					log.Printf("USES_SUBNET error for %s/%s: %v", *fn.FunctionName, subnetId, err)
				}
			}
		}
	}
	return nil
}
