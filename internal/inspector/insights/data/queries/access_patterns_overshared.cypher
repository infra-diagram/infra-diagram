// Find resources accessed by many distinct roles.
// Indicates potentially over-shared resources (data leak risk).

MATCH (r:IAMRole:AWSResource)-[edge:CAN_ACCESS]->(target:AWSResource)
WHERE ($accountId = "" OR target.accountId = $accountId)
WITH target,
     count(DISTINCT r) AS roleCount,
     collect(DISTINCT {
         arn:      r.arn,
         roleName: r.roleName,
         action:   edge.action
     }) AS accessingRoles
WHERE roleCount >= $threshold
RETURN target.arn       AS targetArn,
       labels(target)   AS targetLabels,
       coalesce(target.name, "") AS targetName,
       coalesce(target.region, "") AS targetRegion,
       target.accountId AS accountId,
       roleCount,
       accessingRoles,
       properties(target) AS targetProperties
ORDER BY roleCount DESC