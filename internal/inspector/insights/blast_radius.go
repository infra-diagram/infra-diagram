package insights

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/inspector"
	"infra-diagram/internal/inspector/cypherutil"
	"infra-diagram/internal/inspector/parsers"
	"infra-diagram/internal/inspector/queries"
)

// BlastRadiusInsight computes the blast radius of an IAM Role: the set of
// resources potentially or actually accessible if the role were compromised.
//
// For MVP, covers:
//   - Layer 1: resources granted via attached policies (potential access)
//   - Layer 2: resources currently using the role (EXECUTES, INSTANCE_PROFILE_OF)
//
// Future work (backlog):
//   - Layer 3: transitive escalation via AssumeRole
//   - Layer 4: compute hijacking (Lambda/EC2 RCE implications)
type BlastRadiusInsight struct{}

func NewBlastRadiusInsight() *BlastRadiusInsight {
	return &BlastRadiusInsight{}
}

func (b *BlastRadiusInsight) ID() string {
	return "iam.blast_radius"
}

func (b *BlastRadiusInsight) Version() string {
	return "1.0.0"
}

func (b *BlastRadiusInsight) Metadata() inspector.Metadata {
	return inspector.Metadata{
		Title:       "IAM Role blast radius",
		Description: "Computes the set of resources accessible by a given IAM role, combining direct usage (EXECUTES, INSTANCE_PROFILE_OF) with policy-granted access.",
		Category:    "iam",
		Severity:    inspector.SeverityHigh,
		Parameters: []inspector.ParameterSpec{
			{
				Name:        "role_arn",
				Type:        "arn",
				Required:    true,
				Description: "ARN of the IAM role to analyze",
			},
		},
	}
}

func (b *BlastRadiusInsight) Execute(
	ctx context.Context,
	session neo4j.SessionWithContext,
	filters inspector.StandardFilters,
	params map[string]any,
) (*inspector.Result, error) {
	// === Param validation ===
	roleArn, ok := params["role_arn"].(string)
	if !ok || roleArn == "" {
		return nil, fmt.Errorf("parameter 'role_arn' is required and must be a non-empty string")
	}
	if !strings.HasPrefix(roleArn, "arn:aws:iam::") || !strings.Contains(roleArn, ":role/") {
		return nil, fmt.Errorf("parameter 'role_arn' must be a valid IAM role ARN, got: %s", roleArn)
	}

	result := &inspector.Result{
		Findings: []inspector.Finding{},
		Subgraph: inspector.Subgraph{
			Nodes: []inspector.GraphNode{},
			Edges: []inspector.GraphEdge{},
		},
	}

	// === Layer 2: direct relationships ===
	roleData, err := b.fetchDirectRelationships(ctx, session, roleArn)
	if err != nil {
		return nil, fmt.Errorf("fetching direct relationships: %w", err)
	}
	if roleData == nil {
		// Role not found in graph
		return nil, fmt.Errorf("role not found in graph: %s", roleArn)
	}

	// Add role itself to subgraph
	result.Subgraph.Nodes = append(result.Subgraph.Nodes, inspector.GraphNode{
		ARN:        roleData.roleArn,
		Labels:     []string{"IAMRole", "AWSResource"},
		Properties: roleData.roleProperties,
	})

	// Process direct executors and instances → findings + subgraph
	directTargets := append(roleData.executors, roleData.instances...)
	for _, target := range directTargets {
		if target.arn == "" {
			continue
		}
		finding := b.buildDirectFinding(roleData.roleArn, roleData.roleName, target)
		result.Findings = append(result.Findings, finding)

		result.Subgraph.Nodes = append(result.Subgraph.Nodes, inspector.GraphNode{
			ARN:        target.arn,
			Labels:     target.labels,
			Properties: target.properties,
		})
		result.Subgraph.Edges = append(result.Subgraph.Edges, inspector.GraphEdge{
			FromARN: roleData.roleArn,
			ToARN:   target.arn,
			Type:    target.edgeType,
		})
	}

	// === Layer 1: policy-granted access ===
	for _, policy := range roleData.policies {
		if policy.arn == "" || policy.policyDocument == "" {
			continue
		}

		// Add policy to subgraph as context
		result.Subgraph.Nodes = append(result.Subgraph.Nodes, inspector.GraphNode{
			ARN:    policy.arn,
			Labels: []string{"IAMPolicy", "AWSResource"},
			Properties: map[string]any{
				"policyName":      policy.name,
				"attachmentCount": policy.attachmentCount,
			},
		})
		result.Subgraph.Edges = append(result.Subgraph.Edges, inspector.GraphEdge{
			FromARN: roleData.roleArn,
			ToARN:   policy.arn,
			Type:    "ATTACHED_POLICY",
		})

		// Parse policy and extract resource ARNs
		parsed, err := parsers.ParsePolicyDocument(policy.policyDocument)
		if err != nil {
			// Skip unparseable policies but continue
			continue
		}

		referencedArns := extractReferencedARNs(parsed)
		if len(referencedArns) == 0 {
			continue
		}

		// Lookup which referenced ARNs exist in our graph
		matchedResources, err := b.lookupResources(ctx, session, referencedArns)
		if err != nil {
			return nil, fmt.Errorf("looking up policy resources: %w", err)
		}

		// Generate findings for matched resources
		for _, resource := range matchedResources {
			finding := b.buildPolicyFinding(
				roleData.roleArn, roleData.roleName,
				policy.name, policy.arn,
				resource,
			)
			result.Findings = append(result.Findings, finding)

			// Add resource to subgraph
			result.Subgraph.Nodes = append(result.Subgraph.Nodes, inspector.GraphNode{
				ARN:        resource.arn,
				Labels:     resource.labels,
				Properties: resource.properties,
			})
			result.Subgraph.Edges = append(result.Subgraph.Edges, inspector.GraphEdge{
				FromARN: policy.arn,
				ToARN:   resource.arn,
				Type:    "GRANTS_ACCESS_TO",
				Properties: map[string]any{
					"matchedPattern": resource.matchedPattern,
				},
			})
		}
	}

	// Deduplicate subgraph nodes
	result.Subgraph.Nodes = deduplicateNodes(result.Subgraph.Nodes)

	return result, nil
}

// === Internal data structures ===

type roleBlastData struct {
	roleArn        string
	roleName       string
	accountId      string
	roleProperties map[string]any
	executors      []targetResource // EXECUTES targets
	instances      []targetResource // INSTANCE_PROFILE_OF targets
	policies       []attachedPolicy
}

type targetResource struct {
	arn        string
	labels     []string
	name       string
	vpcId      string
	region     string
	properties map[string]any
	edgeType   string
}

type attachedPolicy struct {
	arn             string
	name            string
	policyDocument  string
	attachmentCount int64
}

type matchedResource struct {
	arn            string
	labels         []string
	name           string
	vpcId          string
	region         string
	properties     map[string]any
	matchedPattern string
}

// === Query execution ===

func (b *BlastRadiusInsight) fetchDirectRelationships(
	ctx context.Context,
	session neo4j.SessionWithContext,
	roleArn string,
) (*roleBlastData, error) {
	res, err := session.Run(ctx, queries.BlastRadiusDirectQuery, map[string]any{
		"roleArn": roleArn,
	})
	if err != nil {
		return nil, err
	}

	if !res.Next(ctx) {
		// No record means role not found
		return nil, nil
	}

	record := res.Record()
	data := &roleBlastData{
		roleArn:        cypherutil.GetString(record, "roleArn"),
		roleName:       cypherutil.GetString(record, "roleName"),
		accountId:      cypherutil.GetString(record, "accountId"),
		roleProperties: cypherutil.GetMap(record, "roleProperties"),
	}

	data.executors = parseTargetList(record, "executors")
	data.instances = parseTargetList(record, "instances")
	data.policies = parsePolicyList(record, "policies")

	if err := res.Err(); err != nil {
		return nil, err
	}

	return data, nil
}

func (b *BlastRadiusInsight) lookupResources(
	ctx context.Context,
	session neo4j.SessionWithContext,
	arns []string,
) ([]matchedResource, error) {
	res, err := session.Run(ctx, queries.BlastRadiusResourceLookupQuery, map[string]any{
		"arns": arns,
	})
	if err != nil {
		return nil, err
	}

	var matches []matchedResource
	for res.Next(ctx) {
		record := res.Record()
		matches = append(matches, matchedResource{
			arn:            cypherutil.GetString(record, "arn"),
			labels:         cypherutil.GetStringSlice(record, "labels"),
			name:           cypherutil.GetString(record, "name"),
			vpcId:          cypherutil.GetString(record, "vpcId"),
			region:         cypherutil.GetString(record, "region"),
			properties:     cypherutil.GetMap(record, "properties"),
			matchedPattern: cypherutil.GetString(record, "matchedPattern"),
		})
	}

	if err := res.Err(); err != nil {
		return nil, err
	}

	return matches, nil
}

// === Finding builders ===

func (b *BlastRadiusInsight) buildDirectFinding(
	roleArn, roleName string,
	target targetResource,
) inspector.Finding {
	targetType := cypherutil.PrimaryLabel(target.labels)

	title := fmt.Sprintf("Role %s currently used by %s '%s'", roleName, targetType, target.name)
	description := fmt.Sprintf(
		"This role is actively used by %s '%s' via relationship %s. Compromising the role exposes this resource and all credentials available to it.",
		targetType, target.name, target.edgeType,
	)

	return inspector.Finding{
		ID: fmt.Sprintf("iam.blast_radius/%s/direct/%s", roleArn, target.arn),
		Resource: inspector.ResourceRef{
			ARN:    target.arn,
			Type:   targetType,
			Name:   target.name,
			Region: target.region,
		},
		Severity:    inspector.SeverityHigh,
		Title:       title,
		Description: description,
		Evidence: map[string]any{
			"access_layer": "direct_usage",
			"edge_type":    target.edgeType,
			"role_arn":     roleArn,
			"role_name":    roleName,
		},
		Remediation: "Verify the principle of least privilege. If this resource does not require the full set of permissions granted to the role, split into a dedicated role with reduced scope.",
	}
}

func (b *BlastRadiusInsight) buildPolicyFinding(
	roleArn, roleName, policyName, policyArn string,
	resource matchedResource,
) inspector.Finding {
	targetType := cypherutil.PrimaryLabel(resource.labels)

	title := fmt.Sprintf("Role %s can access %s '%s' via policy %s", roleName, targetType, resource.name, policyName)
	description := fmt.Sprintf(
		"The role has policy '%s' attached, which grants access to %s '%s' (matched pattern: %s). Compromising the role grants attacker the permissions defined in this policy against this resource.",
		policyName, targetType, resource.name, resource.matchedPattern,
	)

	return inspector.Finding{
		ID: fmt.Sprintf("iam.blast_radius/%s/policy/%s/%s", roleArn, policyArn, resource.arn),
		Resource: inspector.ResourceRef{
			ARN:    resource.arn,
			Type:   targetType,
			Name:   resource.name,
			Region: resource.region,
		},
		Severity:    inspector.SeverityMedium,
		Title:       title,
		Description: description,
		Evidence: map[string]any{
			"access_layer":    "policy_granted",
			"policy_arn":      policyArn,
			"policy_name":     policyName,
			"matched_pattern": resource.matchedPattern,
			"role_arn":        roleArn,
			"role_name":       roleName,
		},
		Remediation: "Review whether the policy grants access too broadly. Consider restricting the Resource field of the relevant statement to specific ARNs instead of wildcards.",
		RelatedResources: []inspector.ResourceRef{
			{ARN: policyArn, Type: "IAMPolicy", Name: policyName},
		},
	}
}

// === Helpers ===

// extractReferencedARNs walks the parsed policy and returns all unique
// resource ARNs referenced in Allow statements. Filters out "*" (matches
// everything — those resources are handled by overpermissive insight).
func extractReferencedARNs(doc *parsers.PolicyDocument) []string {
	seen := make(map[string]bool)
	for _, stmt := range doc.Statements {
		if stmt.Effect != "Allow" {
			continue
		}
		for _, resource := range stmt.Resources {
			if resource == "" || resource == "*" {
				continue
			}
			if !strings.HasPrefix(resource, "arn:") {
				continue
			}
			seen[resource] = true
		}
	}
	out := make([]string, 0, len(seen))
	for arn := range seen {
		out = append(out, arn)
	}
	return out
}

// parseTargetList extracts a list of targetResource from a Neo4j record key
// that contains an array of maps (output of collect(DISTINCT {...})).
func parseTargetList(record *neo4j.Record, key string) []targetResource {
	val, ok := record.Get(key)
	if !ok {
		return nil
	}
	items, ok := val.([]any)
	if !ok {
		return nil
	}

	out := make([]targetResource, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		arnStr, _ := m["arn"].(string)
		if arnStr == "" {
			continue
		}

		labels := []string{}
		if rawLabels, ok := m["labels"].([]any); ok {
			for _, l := range rawLabels {
				if s, ok := l.(string); ok {
					labels = append(labels, s)
				}
			}
		}

		properties, _ := m["properties"].(map[string]any)
		nameStr, _ := m["name"].(string)
		vpcStr, _ := m["vpcId"].(string)
		regionStr, _ := m["region"].(string)
		edgeStr, _ := m["edgeType"].(string)

		out = append(out, targetResource{
			arn:        arnStr,
			labels:     labels,
			name:       nameStr,
			vpcId:      vpcStr,
			region:     regionStr,
			properties: properties,
			edgeType:   edgeStr,
		})
	}
	return out
}

// parsePolicyList extracts attached policies from a Neo4j record.
func parsePolicyList(record *neo4j.Record, key string) []attachedPolicy {
	val, ok := record.Get(key)
	if !ok {
		return nil
	}
	items, ok := val.([]any)
	if !ok {
		return nil
	}

	out := make([]attachedPolicy, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		arnStr, _ := m["arn"].(string)
		if arnStr == "" {
			continue
		}
		nameStr, _ := m["name"].(string)
		docStr, _ := m["policyDocument"].(string)
		count, _ := m["attachmentCount"].(int64)

		out = append(out, attachedPolicy{
			arn:             arnStr,
			name:            nameStr,
			policyDocument:  docStr,
			attachmentCount: count,
		})
	}
	return out
}

// deduplicateNodes removes duplicate nodes from the subgraph by ARN.
func deduplicateNodes(nodes []inspector.GraphNode) []inspector.GraphNode {
	seen := make(map[string]bool)
	out := make([]inspector.GraphNode, 0, len(nodes))
	for _, n := range nodes {
		if seen[n.ARN] {
			continue
		}
		seen[n.ARN] = true
		out = append(out, n)
	}
	return out
}
