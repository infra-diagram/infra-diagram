MATCH (p:IAMPolicy:AWSResource)
WHERE p.policyDocument <> ""
  AND ($accountId = "" OR p.accountId = $accountId)

// Encontrar roles ACTIVOS que tienen esta policy adjunta
OPTIONAL MATCH (p)<-[:ATTACHED_POLICY]-(r:IAMRole)
WHERE EXISTS {
  MATCH (r)-[:EXECUTES|INSTANCE_PROFILE_OF]->()
}

RETURN p.arn              AS policyArn,
       p.policyName        AS policyName,
       p.policyDocument    AS document,
       p.attachmentCount   AS attachmentCount,
       p.accountId         AS accountId,
       collect(DISTINCT {
         arn:  r.arn,
         name: r.roleName
       }) AS activeRoles