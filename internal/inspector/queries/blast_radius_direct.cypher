// Direct relationships: what does this role currently touch?
//
// Returns the role, all resources that USE the role (EXECUTES, INSTANCE_PROFILE_OF),
// and the policies attached to the role.

MATCH (r:IAMRole:AWSResource {arn: $roleArn})

// Capture the role itself
WITH r

// Resources that use this role (Lambdas executing as it, EC2s with it)
OPTIONAL MATCH (r)-[execEdge:EXECUTES]->(executor:AWSResource)
WITH r, collect(DISTINCT {
    arn:        executor.arn,
    labels:     labels(executor),
    name:       coalesce(executor.name, executor.roleName, ""),
    vpcId:      coalesce(executor.vpcId, ""),
    region:     coalesce(executor.region, ""),
    properties: properties(executor),
    edgeType:   "EXECUTES"
}) AS executors

OPTIONAL MATCH (r)-[ipEdge:INSTANCE_PROFILE_OF]->(instance:AWSResource)
WITH r, executors, collect(DISTINCT {
    arn:        instance.arn,
    labels:     labels(instance),
    name:       coalesce(instance.name, ""),
    vpcId:      coalesce(instance.vpcId, ""),
    region:     coalesce(instance.region, ""),
    properties: properties(instance),
    edgeType:   "INSTANCE_PROFILE_OF"
}) AS instances

// Policies attached to this role
OPTIONAL MATCH (r)-[:ATTACHED_POLICY]->(policy:IAMPolicy:AWSResource)
WITH r, executors, instances, collect(DISTINCT {
    arn:             policy.arn,
    name:            policy.policyName,
    policyDocument:  coalesce(policy.policyDocument, ""),
    attachmentCount: coalesce(policy.attachmentCount, 0)
}) AS policies

RETURN r.arn        AS roleArn,
       r.roleName   AS roleName,
       r.accountId  AS accountId,
       properties(r) AS roleProperties,
       executors,
       instances,
       policies