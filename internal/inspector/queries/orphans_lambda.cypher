MATCH (f:Function:AWSResource)
WHERE NOT ()-[:TRIGGERS]->(f)
  AND NOT ()-[:NOTIFIES]->(f)
  AND ($accountId = "" OR f.accountId = $accountId)
  AND ($region = "" OR f.region = $region)
RETURN f.arn AS arn,
       f.name AS name,
       f.region AS region,
       f.accountId AS accountId,
       properties(f) AS properties