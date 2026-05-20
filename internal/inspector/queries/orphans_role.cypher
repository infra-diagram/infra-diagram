MATCH (r:IAMRole:AWSResource)
WHERE NOT (r)-[:EXECUTES]->()
  AND NOT (r)-[:INSTANCE_PROFILE_OF]->()
  AND ($accountId = "" OR r.accountId = $accountId)
RETURN r.arn AS arn,
       r.roleName AS name,
       r.accountId AS accountId,
       properties(r) AS properties