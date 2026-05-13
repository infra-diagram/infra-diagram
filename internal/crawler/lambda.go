package crawler

import (
	"context"
	"fmt"
	"infra-diagram/internal/models"
	"log"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type LambdaCrawler struct {
	client *lambda.Client

	functionsOnce sync.Once
	functions     []lambdatypes.FunctionConfiguration
	functionsErr  error
}

func NewLambdaCrawler(client *lambda.Client) *LambdaCrawler {
	return &LambdaCrawler{client: client}
}

func (c *LambdaCrawler) Client() *lambda.Client {
	return c.client
}

func (c *LambdaCrawler) loadFunctions(ctx context.Context) ([]lambdatypes.FunctionConfiguration, error) {
	c.functionsOnce.Do(func() {
		paginator := lambda.NewListFunctionsPaginator(c.client, &lambda.ListFunctionsInput{})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				c.functionsErr = fmt.Errorf("listing lambdas: %w", err)
				return
			}
			c.functions = append(c.functions, page.Functions...)
		}
	})
	return c.functions, c.functionsErr
}

// =============================================================================
// FASE 1 — Nodos
// =============================================================================

func (c *LambdaCrawler) IngestNodes(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	functions, err := c.loadFunctions(ctx)
	if err != nil {
		return err
	}

	for _, fn := range functions {
		if err := c.upsertNodeFromFunction(ctx, session, fn); err != nil {
			log.Printf("ingesting lambda: %v", err)
			continue
		}
	}
	return nil
}

func (c *LambdaCrawler) IngestSingle(ctx context.Context, session neo4j.SessionWithContext, functionName string) error {
	fn, err := c.fetchFunction(ctx, functionName)
	if err != nil {
		return err
	}
	return c.upsertNodeFromFunction(ctx, session, fn)
}

func (c *LambdaCrawler) IngestSingleWithDriver(ctx context.Context, driver neo4j.DriverWithContext, functionName string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	return c.IngestSingle(ctx, session, functionName)
}

func (c *LambdaCrawler) upsertNodeFromFunction(ctx context.Context, session neo4j.SessionWithContext, fn lambdatypes.FunctionConfiguration) error {
	if fn.FunctionArn == nil || fn.FunctionName == nil {
		return fmt.Errorf("function missing required fields")
	}

	region, err := GetRegionFromArn(*fn.FunctionArn)
	if err != nil {
		return fmt.Errorf("parsing region: %w", err)
	}

	accountId, err := GetAccountIdFromArn(*fn.FunctionArn)
	if err != nil {
		return fmt.Errorf("parsing accountId: %w", err)
	}

	vpcId := ""
	if fn.VpcConfig != nil && fn.VpcConfig.VpcId != nil {
		vpcId = *fn.VpcConfig.VpcId
	}

	executionRoleArn := ""
	if fn.Role != nil {
		executionRoleArn = *fn.Role
	}

	node := models.Function{
		AWSResource: models.AWSResource{
			Arn:       *fn.FunctionArn,
			AccountId: accountId,
		},
		Name:             *fn.FunctionName,
		Runtime:          string(fn.Runtime),
		Region:           region,
		VpcId:            vpcId,
		ExecutionRoleArn: executionRoleArn,
	}

	_, err = session.Run(ctx,
		`MERGE (f:Function:AWSResource {arn: $arn})
		 SET f.name             = $name,
		     f.runtime          = $runtime,
		     f.region           = $region,
		     f.accountId        = $accountId,
		     f.vpcId            = $vpcId,
		     f.executionRoleArn = $executionRoleArn`,
		map[string]any{
			"arn":              node.Arn,
			"name":             node.Name,
			"runtime":          node.Runtime,
			"region":           node.Region,
			"accountId":        node.AccountId,
			"vpcId":            node.VpcId,
			"executionRoleArn": node.ExecutionRoleArn,
		},
	)
	if err != nil {
		return fmt.Errorf("merging function %s: %w", node.Name, err)
	}
	return nil
}

// fetchFunction hace re-fetch puntual via GetFunction. Recibe nombre o ARN.
func (c *LambdaCrawler) fetchFunction(ctx context.Context, functionName string) (lambdatypes.FunctionConfiguration, error) {
	resp, err := c.client.GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: &functionName,
	})
	if err != nil {
		return lambdatypes.FunctionConfiguration{}, fmt.Errorf("getting function %s: %w", functionName, err)
	}
	if resp.Configuration == nil {
		return lambdatypes.FunctionConfiguration{}, fmt.Errorf("function %s has no configuration", functionName)
	}
	return *resp.Configuration, nil
}

// =============================================================================
// FASE 2 — Relaciones
// =============================================================================

func (c *LambdaCrawler) IngestRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	if err := c.ingestVPCRelationsBulk(ctx, driver); err != nil {
		return err
	}
	if err := c.ingestEventSourceMappings(ctx, driver); err != nil {
		return err
	}
	return nil
}

// RebuildRelations recrea las relaciones VPC/Subnet de UNA función.
//
// Importante: NO recrea TRIGGERS (event source mappings). Esas se
// manejan en handlers específicos para CreateEventSourceMapping /
// DeleteEventSourceMapping porque conceptualmente pertenecen al
// recurso "EventSourceMapping", no a la función.
func (c *LambdaCrawler) RebuildRelations(ctx context.Context, session neo4j.SessionWithContext, functionName string) error {
	fn, err := c.fetchFunction(ctx, functionName)
	if err != nil {
		return err
	}
	if err := c.rebuildVPCRelationsFromFunction(ctx, session, fn); err != nil {
		return err
	}
	return c.rebuildExecutesFromFunction(ctx, session, fn)
}

// rebuildExecutesFromFunction recrea la relación EXECUTES desde el IAMRole hacia esta función.
func (c *LambdaCrawler) rebuildExecutesFromFunction(ctx context.Context, session neo4j.SessionWithContext, fn lambdatypes.FunctionConfiguration) error {
	if fn.FunctionArn == nil || fn.Role == nil || *fn.Role == "" {
		return nil
	}
	return rebuildExecutes(ctx, session, *fn.Role, []string{*fn.FunctionArn})
}

func (c *LambdaCrawler) RebuildRelationsWithDriver(ctx context.Context, driver neo4j.DriverWithContext, functionName string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	return c.RebuildRelations(ctx, session, functionName)
}

func (c *LambdaCrawler) ingestVPCRelationsBulk(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	functions, err := c.loadFunctions(ctx)
	if err != nil {
		return err
	}

	for _, fn := range functions {
		if err := c.rebuildVPCRelationsFromFunction(ctx, session, fn); err != nil {
			log.Printf("rebuilding lambda vpc relations: %v", err)
			continue
		}
	}
	return nil
}

// rebuildVPCRelationsFromFunction maneja DEPLOYED_IN (1 VPC) y USES_SUBNET (N).
// USES_SUBNET es especial porque Lambda puede tener varias subnets simultáneamente.
func (c *LambdaCrawler) rebuildVPCRelationsFromFunction(ctx context.Context, session neo4j.SessionWithContext, fn lambdatypes.FunctionConfiguration) error {
	if fn.FunctionArn == nil {
		return nil
	}
	fnArn := *fn.FunctionArn

	// Si la lambda no está en VPC, borrar cualquier relación previa
	// (puede haber dejado de estar en VPC vía UpdateFunctionConfiguration).
	if fn.VpcConfig == nil || fn.VpcConfig.VpcId == nil || *fn.VpcConfig.VpcId == "" {
		return clearVPCRelations(ctx, session, fnArn)
	}

	region, err := GetRegionFromArn(fnArn)
	if err != nil {
		return fmt.Errorf("parsing region: %w", err)
	}
	accountId, err := GetAccountIdFromArn(fnArn)
	if err != nil {
		return fmt.Errorf("parsing accountId: %w", err)
	}

	// DEPLOYED_IN → VPC (1 a 1)
	vpcArn := BuildArn("ec2", region, accountId, "vpc", *fn.VpcConfig.VpcId)
	if err := rebuildDeployedIn(ctx, session, fnArn, vpcArn); err != nil {
		return fmt.Errorf("rebuilding DEPLOYED_IN: %w", err)
	}

	// USES_SUBNET → Subnet (1 a N — Lambda puede tener varias subnets)
	subnetArns := make([]string, 0, len(fn.VpcConfig.SubnetIds))
	for _, subnetId := range fn.VpcConfig.SubnetIds {
		subnetArns = append(subnetArns, BuildArn("ec2", region, accountId, "subnet", subnetId))
	}
	if err := rebuildUsesSubnetMulti(ctx, session, fnArn, subnetArns); err != nil {
		return fmt.Errorf("rebuilding USES_SUBNET: %w", err)
	}

	return nil
}

// ingestEventSourceMappings es la lógica original — crea relaciones
// TRIGGERS desde Queue/Kinesis/etc hacia Lambda. Se mantiene como bulk
// para discovery; el worker maneja eventos individuales con un handler
// dedicado a CreateEventSourceMapping.
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

// =============================================================================
// DELETE
// =============================================================================

func (c *LambdaCrawler) DeleteByArn(ctx context.Context, driver neo4j.DriverWithContext, arn string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.Run(ctx,
		`MATCH (f:Function:AWSResource {arn: $arn})
		 DETACH DELETE f`,
		map[string]any{"arn": arn},
	)
	if err != nil {
		return fmt.Errorf("deleting function %s: %w", arn, err)
	}
	return nil
}
