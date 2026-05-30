// Find roles with CAN_ACCESS to many distinct resources.
// Indicates potentially overprivileged roles.

MATCH (r:IAMRole:AWSResource)-[edge:CAN_ACCESS]->(target:AWSResource)
WHERE ($accountId = "" OR r.accountId = $accountId)
WITH r,
     count(DISTINCT target) AS resourceCount,
     count(DISTINCT target.arn) AS uniqueResources,
     collect(DISTINCT {
         arn:    target.arn,
         labels: labels(target),
         name:   coalesce(target.name, target.roleName, ""),
         action: edge.action
     }) AS targets
WHERE resourceCount >= $threshold
RETURN r.arn       AS roleArn,
       r.roleName  AS roleName,
       r.accountId AS accountId,
       resourceCount,
       targets
ORDER BY resourceCount DESC