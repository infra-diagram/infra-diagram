MATCH (q:Queue:AWSResource)
WHERE NOT ()-[:TRIGGERS]->(q)
  AND ($accountId = "" OR q.accountId = $accountId)
  AND ($region = "" OR q.region = $region)
RETURN q.arn AS arn,
       q.name AS name,
       q.region AS region,
       q.accountId AS accountId,
       properties(q) AS properties