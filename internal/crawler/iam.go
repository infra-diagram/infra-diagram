package crawler

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/inspector/parsers"
	"infra-diagram/internal/models"
)

type IAMCrawler struct {
	iamClient *iam.Client

	// Cache de roles: usada por IngestNodes (Fase 1) e IngestRelations (Fase 2).
	// sync.Once garantiza una sola llamada a ListRoles aunque ambas fases corran.
	rolesOnce sync.Once
	roles     []iamtypes.Role
	rolesErr  error
}

func NewIAMCrawler(iamClient *iam.Client) *IAMCrawler {
	return &IAMCrawler{iamClient: iamClient}
}

func (c *IAMCrawler) loadRoles(ctx context.Context) ([]iamtypes.Role, error) {
	c.rolesOnce.Do(func() {
		paginator := iam.NewListRolesPaginator(c.iamClient, &iam.ListRolesInput{})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				c.rolesErr = fmt.Errorf("listing iam roles: %w", err)
				return
			}
			c.roles = append(c.roles, page.Roles...)
		}
	})
	return c.roles, c.rolesErr
}

// =============================================================================
// FASE 1 — Nodos
// =============================================================================

func (c *IAMCrawler) IngestNodes(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	if err := c.ingestRoleNodes(ctx, session); err != nil {
		return err
	}
	return c.ingestPolicyNodes(ctx, session)
}

func (c *IAMCrawler) IngestSingleRole(ctx context.Context, session neo4j.SessionWithContext, roleName string) error {
	role, err := c.fetchRole(ctx, roleName)
	if err != nil {
		return err
	}
	return c.upsertNodeFromRole(ctx, session, role)
}

func (c *IAMCrawler) IngestSingleRoleWithDriver(ctx context.Context, driver neo4j.DriverWithContext, roleName string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	return c.IngestSingleRole(ctx, session, roleName)
}

func (c *IAMCrawler) IngestSinglePolicy(ctx context.Context, session neo4j.SessionWithContext, policyArn string) error {
	policy, err := c.fetchPolicy(ctx, policyArn)
	if err != nil {
		return err
	}

	document, err := c.fetchPolicyDocument(ctx, policy)
	if err != nil {
		log.Printf("fetching policy document for %s: %v — continuing with empty doc",
			policyArn, err)
		document = ""
	}

	return c.upsertNodeFromPolicy(ctx, session, policy, document)
}

func (c *IAMCrawler) IngestSinglePolicyWithDriver(ctx context.Context, driver neo4j.DriverWithContext, policyArn string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	return c.IngestSinglePolicy(ctx, session, policyArn)
}

func (c *IAMCrawler) ingestRoleNodes(ctx context.Context, session neo4j.SessionWithContext) error {
	roles, err := c.loadRoles(ctx)
	if err != nil {
		return err
	}
	for _, role := range roles {
		if err := c.upsertNodeFromRole(ctx, session, role); err != nil {
			log.Printf("ingesting iam role: %v", err)
			continue
		}
	}
	return nil
}

func (c *IAMCrawler) ingestPolicyNodes(ctx context.Context, session neo4j.SessionWithContext) error {
	paginator := iam.NewListPoliciesPaginator(c.iamClient, &iam.ListPoliciesInput{
		Scope: iamtypes.PolicyScopeTypeLocal,
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing policies: %w", err)
		}

		for _, policy := range page.Policies {
			document, err := c.fetchPolicyDocument(ctx, policy)
			if err != nil {
				log.Printf("fetching policy document for %s: %v — continuing with empty doc",
					aws.ToString(policy.Arn), err)
				document = ""
			}

			if err := c.upsertNodeFromPolicy(ctx, session, policy, document); err != nil {
				log.Printf("upserting policy: %v", err)
				continue
			}
		}
	}
	return nil
}

func (c *IAMCrawler) upsertNodeFromRole(ctx context.Context, session neo4j.SessionWithContext, role iamtypes.Role) error {
	if role.Arn == nil || role.RoleName == nil {
		return fmt.Errorf("role missing required fields")
	}

	accountId, err := GetAccountIdFromArn(*role.Arn)
	if err != nil {
		return fmt.Errorf("parsing accountId: %w", err)
	}

	roleId := ""
	if role.RoleId != nil {
		roleId = *role.RoleId
	}
	path := ""
	if role.Path != nil {
		path = *role.Path
	}
	createDate := ""
	if role.CreateDate != nil {
		createDate = role.CreateDate.UTC().Format(time.RFC3339)
	}
	description := ""
	if role.Description != nil {
		description = *role.Description
	}
	var maxSessionDuration int32
	if role.MaxSessionDuration != nil {
		maxSessionDuration = *role.MaxSessionDuration
	}

	node := models.IAMRole{
		AWSResource: models.AWSResource{
			Arn:       *role.Arn,
			AccountId: accountId,
		},
		RoleName:           *role.RoleName,
		RoleId:             roleId,
		Path:               path,
		CreateDate:         createDate,
		Description:        description,
		MaxSessionDuration: maxSessionDuration,
	}

	_, err = session.Run(ctx,
		`MERGE (r:IAMRole:AWSResource {arn: $arn})
		 SET r.roleName           = $roleName,
		     r.roleId             = $roleId,
		     r.path               = $path,
		     r.createDate         = $createDate,
		     r.description        = $description,
		     r.maxSessionDuration = $maxSessionDuration,
		     r.accountId          = $accountId`,
		map[string]any{
			"arn":                node.Arn,
			"roleName":           node.RoleName,
			"roleId":             node.RoleId,
			"path":               node.Path,
			"createDate":         node.CreateDate,
			"description":        node.Description,
			"maxSessionDuration": node.MaxSessionDuration,
			"accountId":          node.AccountId,
		},
	)
	if err != nil {
		return fmt.Errorf("merging iam role %s: %w", node.RoleName, err)
	}
	return nil
}

func (c *IAMCrawler) upsertNodeFromPolicy(
	ctx context.Context,
	session neo4j.SessionWithContext,
	policy iamtypes.Policy,
	document string,
) error {
	if policy.Arn == nil || policy.PolicyName == nil {
		return fmt.Errorf("policy missing required fields")
	}

	accountId, err := GetAccountIdFromArn(*policy.Arn)
	if err != nil {
		return fmt.Errorf("parsing accountId: %w", err)
	}

	policyId := ""
	if policy.PolicyId != nil {
		policyId = *policy.PolicyId
	}
	path := ""
	if policy.Path != nil {
		path = *policy.Path
	}
	description := ""
	if policy.Description != nil {
		description = *policy.Description
	}
	createDate := ""
	if policy.CreateDate != nil {
		createDate = policy.CreateDate.UTC().Format(time.RFC3339)
	}
	updateDate := ""
	if policy.UpdateDate != nil {
		updateDate = policy.UpdateDate.UTC().Format(time.RFC3339)
	}
	var attachmentCount int32
	if policy.AttachmentCount != nil {
		attachmentCount = *policy.AttachmentCount
	}

	defaultVersionId := ""
	if policy.DefaultVersionId != nil {
		defaultVersionId = *policy.DefaultVersionId
	}

	node := models.IAMPolicy{
		AWSResource: models.AWSResource{
			Arn:       *policy.Arn,
			AccountId: accountId,
		},
		PolicyName:       *policy.PolicyName,
		PolicyId:         policyId,
		Path:             path,
		Description:      description,
		CreateDate:       createDate,
		UpdateDate:       updateDate,
		AttachmentCount:  attachmentCount,
		PolicyDocument:   document,
		DefaultVersionId: defaultVersionId,
	}

	_, err = session.Run(ctx,
		`MERGE (p:IAMPolicy:AWSResource {arn: $arn})
		 SET p.policyName      	= $policyName,
		     p.policyId        	= $policyId,
		     p.path            	= $path,
		     p.description     	= $description,
		     p.createDate      	= $createDate,
		     p.updateDate      	= $updateDate,
		     p.attachmentCount 	= $attachmentCount,
				 p.policyDocument  	= $policyDocument,
				 p.defaultVersionId = $defaultVersionId,
		     p.accountId       	= $accountId`,
		map[string]any{
			"arn":              node.Arn,
			"policyName":       node.PolicyName,
			"policyId":         node.PolicyId,
			"path":             node.Path,
			"description":      node.Description,
			"createDate":       node.CreateDate,
			"updateDate":       node.UpdateDate,
			"attachmentCount":  node.AttachmentCount,
			"policyDocument":   node.PolicyDocument,
			"defaultVersionId": node.DefaultVersionId,
			"accountId":        node.AccountId,
		},
	)
	if err != nil {
		return fmt.Errorf("merging iam policy %s: %w", node.PolicyName, err)
	}
	return nil
}

func (c *IAMCrawler) fetchRole(ctx context.Context, roleName string) (iamtypes.Role, error) {
	resp, err := c.iamClient.GetRole(ctx, &iam.GetRoleInput{RoleName: &roleName})
	if err != nil {
		return iamtypes.Role{}, fmt.Errorf("getting iam role %s: %w", roleName, err)
	}
	if resp.Role == nil {
		return iamtypes.Role{}, fmt.Errorf("iam role %s has no data", roleName)
	}
	return *resp.Role, nil
}

func (c *IAMCrawler) fetchPolicy(ctx context.Context, policyArn string) (iamtypes.Policy, error) {
	resp, err := c.iamClient.GetPolicy(ctx, &iam.GetPolicyInput{PolicyArn: &policyArn})
	if err != nil {
		return iamtypes.Policy{}, fmt.Errorf("getting iam policy %s: %w", policyArn, err)
	}
	if resp.Policy == nil {
		return iamtypes.Policy{}, fmt.Errorf("iam policy %s has no data", policyArn)
	}
	return *resp.Policy, nil
}

// fetchPolicyDocument obtiene el JSON del documento de la versión default de
// una policy. Maneja el URL-encoding que AWS aplica al campo.
//
// Retorna documento + DefaultVersionId. Si la policy no tiene versiones
// (no debería pasar pero es defensivo), retorna strings vacíos sin error.
func (c *IAMCrawler) fetchPolicyDocument(ctx context.Context, policy iamtypes.Policy) (string, error) {
	if policy.Arn == nil || policy.DefaultVersionId == nil {
		return "", nil
	}

	resp, err := c.iamClient.GetPolicyVersion(ctx, &iam.GetPolicyVersionInput{
		PolicyArn: policy.Arn,
		VersionId: policy.DefaultVersionId,
	})
	if err != nil {
		return "", fmt.Errorf("getting policy version: %w", err)
	}

	if resp.PolicyVersion == nil || resp.PolicyVersion.Document == nil {
		return "", nil
	}

	// AWS retorna el documento URL-encoded. Decodificamos antes de persistir.
	decoded, err := url.QueryUnescape(*resp.PolicyVersion.Document)
	if err != nil {
		return "", fmt.Errorf("decoding policy document: %w", err)
	}

	return decoded, nil
}

// =============================================================================
// FASE 2 — Relaciones
// =============================================================================

func (c *IAMCrawler) IngestRelations(ctx context.Context, driver neo4j.DriverWithContext) error {
	if err := c.ingestAttachedPolicyRelationsBulk(ctx, driver); err != nil {
		return err
	}
	if err := c.ingestInstanceProfileRelationsBulk(ctx, driver); err != nil {
		return err
	}
	if err := c.ingestExecutesRelationsBulk(ctx, driver); err != nil {
		return err
	}
	// materialize CAN_ACCESS relations (depends on ATTACHED_POLICY being done)
	if err := c.ingestCanAccessRelationsBulk(ctx, driver); err != nil {
		return err
	}
	return nil
}

// RebuildRelations recrea las relaciones ATTACHED_POLICY de UN rol.
// Llamado por IAMHandler después de AttachRolePolicy / DetachRolePolicy / etc.
func (c *IAMCrawler) RebuildRelations(ctx context.Context, session neo4j.SessionWithContext, roleName string) error {
	role, err := c.fetchRole(ctx, roleName)
	if err != nil {
		return err
	}
	return c.rebuildRoleRelations(ctx, session, role)
}

func (c *IAMCrawler) RebuildRelationsWithDriver(ctx context.Context, driver neo4j.DriverWithContext, roleName string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	return c.RebuildRelations(ctx, session, roleName)
}

func (c *IAMCrawler) rebuildRoleRelations(ctx context.Context, session neo4j.SessionWithContext, role iamtypes.Role) error {
	if role.Arn == nil || role.RoleName == nil {
		return nil
	}
	roleArn := *role.Arn
	roleName := *role.RoleName

	var policyArns []string
	paginator := iam.NewListAttachedRolePoliciesPaginator(c.iamClient, &iam.ListAttachedRolePoliciesInput{
		RoleName: &roleName,
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("listing attached policies for role %s: %w", roleName, err)
		}
		for _, p := range page.AttachedPolicies {
			if p.PolicyArn == nil {
				continue
			}
			// Filtrar AWS managed policies — no las modelamos como nodos
			if strings.HasPrefix(*p.PolicyArn, "arn:aws:iam::aws:policy/") {
				continue
			}
			policyArns = append(policyArns, *p.PolicyArn)
		}
	}

	return rebuildAttachedPolicies(ctx, session, roleArn, policyArns)
}

func (c *IAMCrawler) ingestAttachedPolicyRelationsBulk(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	roles, err := c.loadRoles(ctx)
	if err != nil {
		return err
	}

	for _, role := range roles {
		if err := c.rebuildRoleRelations(ctx, session, role); err != nil {
			log.Printf("rebuilding ATTACHED_POLICY for role %v: %v", role.RoleName, err)
			continue
		}
	}
	return nil
}

func (c *IAMCrawler) ingestInstanceProfileRelationsBulk(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		`MATCH (e:EC2Instance) WHERE e.iamInstanceProfileArn <> "" RETURN e.arn, e.iamInstanceProfileArn`,
		nil,
	)
	if err != nil {
		return fmt.Errorf("querying ec2 instances for instance profiles: %w", err)
	}

	// Agrupar instanceArns por roleArn
	roleToInstances := make(map[string][]string)
	for result.Next(ctx) {
		record := result.Record()
		instanceArnVal, _ := record.Get("e.arn")
		profileArnVal, _ := record.Get("e.iamInstanceProfileArn")
		instanceArn, ok1 := instanceArnVal.(string)
		profileArn, ok2 := profileArnVal.(string)
		if !ok1 || !ok2 || instanceArn == "" || profileArn == "" {
			continue
		}

		profileName, err := GetIAMResourceNameFromArn(profileArn)
		if err != nil {
			log.Printf("parsing instance profile name from ARN %s: %v", profileArn, err)
			continue
		}

		resp, err := c.iamClient.GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{
			InstanceProfileName: &profileName,
		})
		if err != nil {
			log.Printf("getting instance profile %s: %v", profileName, err)
			continue
		}
		if resp.InstanceProfile == nil || len(resp.InstanceProfile.Roles) == 0 {
			continue
		}
		if resp.InstanceProfile.Roles[0].Arn == nil {
			continue
		}
		roleArn := *resp.InstanceProfile.Roles[0].Arn
		roleToInstances[roleArn] = append(roleToInstances[roleArn], instanceArn)
	}
	if err := result.Err(); err != nil {
		return fmt.Errorf("iterating ec2 instance results: %w", err)
	}

	for roleArn, instanceArns := range roleToInstances {
		if err := RebuildInstanceProfileOf(ctx, session, roleArn, instanceArns); err != nil {
			log.Printf("rebuilding INSTANCE_PROFILE_OF for role %s: %v", roleArn, err)
		}
	}
	return nil
}

func (c *IAMCrawler) ingestExecutesRelationsBulk(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		`MATCH (f:Function) WHERE f.executionRoleArn <> "" RETURN f.arn, f.executionRoleArn`,
		nil,
	)
	if err != nil {
		return fmt.Errorf("querying functions for execution roles: %w", err)
	}

	// Agrupar functionArns por roleArn
	roleToFunctions := make(map[string][]string)
	for result.Next(ctx) {
		record := result.Record()
		funcArnVal, _ := record.Get("f.arn")
		roleArnVal, _ := record.Get("f.executionRoleArn")
		funcArn, ok1 := funcArnVal.(string)
		roleArn, ok2 := roleArnVal.(string)
		if !ok1 || !ok2 || funcArn == "" || roleArn == "" {
			continue
		}
		roleToFunctions[roleArn] = append(roleToFunctions[roleArn], funcArn)
	}
	if err := result.Err(); err != nil {
		return fmt.Errorf("iterating function results: %w", err)
	}

	for roleArn, funcArns := range roleToFunctions {
		if err := rebuildExecutes(ctx, session, roleArn, funcArns); err != nil {
			log.Printf("rebuilding EXECUTES for role %s: %v", roleArn, err)
		}
	}
	return nil
}

// =============================================================================
// DELETE
// =============================================================================

func (c *IAMCrawler) DeleteRoleByArn(ctx context.Context, driver neo4j.DriverWithContext, arn string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.Run(ctx,
		`MATCH (r:IAMRole:AWSResource {arn: $arn})
		 DETACH DELETE r`,
		map[string]any{"arn": arn},
	)
	if err != nil {
		return fmt.Errorf("deleting iam role %s: %w", arn, err)
	}
	return nil
}

func (c *IAMCrawler) DeletePolicyByArn(ctx context.Context, driver neo4j.DriverWithContext, arn string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.Run(ctx,
		`MATCH (p:IAMPolicy:AWSResource {arn: $arn})
		 DETACH DELETE p`,
		map[string]any{"arn": arn},
	)
	if err != nil {
		return fmt.Errorf("deleting iam policy %s: %w", arn, err)
	}
	return nil
}

// ingestCanAccessRelationsBulk crea relaciones (:IAMRole)-[:CAN_ACCESS]->(:Resource)
// para todos los roles del grafo, parseando las policies adjuntas.
//
// Llamado desde IngestRelations como tercera fase, después de ATTACHED_POLICY
// (que necesita estar materializada para que esta query funcione).
func (c *IAMCrawler) ingestCanAccessRelationsBulk(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	// Traer todos los roles con sus policies adjuntas y los documentos
	res, err := session.Run(ctx, `
		MATCH (r:IAMRole:AWSResource)-[:ATTACHED_POLICY]->(p:IAMPolicy:AWSResource)
		WHERE p.policyDocument <> ""
		RETURN r.arn AS roleArn, collect({arn: p.arn, document: p.policyDocument}) AS policies
	`, nil)
	if err != nil {
		return fmt.Errorf("fetching roles with policies: %w", err)
	}

	for res.Next(ctx) {
		record := res.Record()
		roleArnVal, _ := record.Get("roleArn")
		roleArn, _ := roleArnVal.(string)
		if roleArn == "" {
			continue
		}

		policiesVal, _ := record.Get("policies")
		policies, _ := policiesVal.([]any)

		if err := c.rebuildCanAccessForRole(ctx, session, roleArn, policies); err != nil {
			log.Printf("rebuilding CAN_ACCESS for %s: %v", roleArn, err)
			continue
		}
	}
	return res.Err()
}

// RebuildCanAccessForRole recrea las relaciones CAN_ACCESS salientes de un
// solo role. Usado por el worker cuando un evento afecta las policies del role.
//
// Hace su propio fetch de policies desde el grafo — espera que ATTACHED_POLICY
// ya esté actualizada cuando se llama.
func (c *IAMCrawler) RebuildCanAccessForRole(ctx context.Context, session neo4j.SessionWithContext, roleArn string) error {
	res, err := session.Run(ctx, `
		MATCH (r:IAMRole:AWSResource {arn: $roleArn})-[:ATTACHED_POLICY]->(p:IAMPolicy:AWSResource)
		WHERE p.policyDocument <> ""
		RETURN collect({arn: p.arn, document: p.policyDocument}) AS policies
	`, map[string]any{"roleArn": roleArn})
	if err != nil {
		return fmt.Errorf("fetching policies for role: %w", err)
	}

	if !res.Next(ctx) {
		// No policies attached — just clear any stale CAN_ACCESS
		return c.clearCanAccess(ctx, session, roleArn)
	}

	policiesVal, _ := res.Record().Get("policies")
	policies, _ := policiesVal.([]any)

	return c.rebuildCanAccessForRole(ctx, session, roleArn, policies)
}

// RebuildCanAccessForRoleWithDriver es el wrapper que abre su propia sesión.
func (c *IAMCrawler) RebuildCanAccessForRoleWithDriver(ctx context.Context, driver neo4j.DriverWithContext, roleArn string) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	return c.RebuildCanAccessForRole(ctx, session, roleArn)
}

// rebuildCanAccessForRole is the shared implementation. Receives the raw
// policies list (from Cypher result) and rebuilds all CAN_ACCESS relations.
func (c *IAMCrawler) rebuildCanAccessForRole(
	ctx context.Context,
	session neo4j.SessionWithContext,
	roleArn string,
	policies []any,
) error {
	// Step 1: clear existing CAN_ACCESS for this role
	if err := c.clearCanAccess(ctx, session, roleArn); err != nil {
		return err
	}

	// Step 2: for each policy, extract grants and create CAN_ACCESS relations
	for _, p := range policies {
		policyMap, ok := p.(map[string]any)
		if !ok {
			continue
		}
		policyArn, _ := policyMap["arn"].(string)
		document, _ := policyMap["document"].(string)
		if document == "" {
			continue
		}

		parsed, err := parsers.ParsePolicyDocument(document)
		if err != nil {
			log.Printf("parsing policy %s for CAN_ACCESS: %v", policyArn, err)
			continue
		}

		grants := parsers.ExtractAccessGrants(parsed)
		for _, grant := range grants {
			if !parsers.IsDataPlaneAction(grant.Action) {
				continue
			}
			if grant.Resource == "*" {
				// Wildcard resources are too broad to materialize as edges.
				// Handled by the overpermissive insight instead.
				continue
			}
			if err := c.createCanAccessEdge(ctx, session, roleArn, policyArn, grant); err != nil {
				// Log but continue — one bad grant doesn't kill the rest
				log.Printf("creating CAN_ACCESS for %s -> %s: %v", roleArn, grant.Resource, err)
			}
		}
	}
	return nil
}

// clearCanAccess removes all CAN_ACCESS edges outgoing from a role.
func (c *IAMCrawler) clearCanAccess(ctx context.Context, session neo4j.SessionWithContext, roleArn string) error {
	_, err := session.Run(ctx, `
		MATCH (r:IAMRole:AWSResource {arn: $roleArn})-[edge:CAN_ACCESS]->()
		DELETE edge
	`, map[string]any{"roleArn": roleArn})
	return err
}

// createCanAccessEdge creates one CAN_ACCESS edge if the target resource
// exists in the graph. Uses prefix matching for ARN wildcards.
func (c *IAMCrawler) createCanAccessEdge(
	ctx context.Context,
	session neo4j.SessionWithContext,
	roleArn, sourcePolicy string,
	grant parsers.AccessGrant,
) error {
	// Compute prefix for wildcard matching
	prefix := grant.Resource
	usePrefix := false
	if strings.HasSuffix(prefix, "/*") {
		prefix = prefix[:len(prefix)-2]
		usePrefix = true
	} else if strings.HasSuffix(prefix, "*") {
		prefix = prefix[:len(prefix)-1]
		usePrefix = true
	}

	var query string
	if usePrefix {
		query = `
			MATCH (r:IAMRole:AWSResource {arn: $roleArn})
			MATCH (target:AWSResource)
			WHERE target.arn STARTS WITH $prefix
			MERGE (r)-[edge:CAN_ACCESS {action: $action, sourcePolicy: $sourcePolicy}]->(target)
		`
	} else {
		query = `
			MATCH (r:IAMRole:AWSResource {arn: $roleArn})
			MATCH (target:AWSResource {arn: $resource})
			MERGE (r)-[edge:CAN_ACCESS {action: $action, sourcePolicy: $sourcePolicy}]->(target)
		`
	}

	_, err := session.Run(ctx, query, map[string]any{
		"roleArn":      roleArn,
		"resource":     grant.Resource,
		"prefix":       prefix,
		"action":       grant.Action,
		"sourcePolicy": sourcePolicy,
	})
	return err
}
