MATCH (s:Subnet:AWSResource)
WHERE NOT ()-[:USES_SUBNET]->(s)
  AND ($accountId = "" OR s.accountId = $accountId)
RETURN s.arn AS arn,
       s.subnetId AS name,
       s.accountId AS accountId,
       properties(s) AS properties