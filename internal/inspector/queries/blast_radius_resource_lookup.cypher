// Lookup resources by ARN list — used to find graph nodes that match
// resources referenced in policy documents.
//
// $arns is a list of ARN patterns. We do prefix matching to handle
// wildcards like "arn:aws:s3:::bucket-name/*" matching "arn:aws:s3:::bucket-name".

UNWIND $arns AS arnPattern

// Direct exact match
OPTIONAL MATCH (n:AWSResource {arn: arnPattern})

// Prefix match for wildcards: "arn:aws:s3:::my-bucket/*" matches the bucket
WITH arnPattern, n,
     CASE
         WHEN arnPattern ENDS WITH "/*" THEN substring(arnPattern, 0, size(arnPattern) - 2)
         WHEN arnPattern ENDS WITH "*" THEN substring(arnPattern, 0, size(arnPattern) - 1)
         ELSE ""
     END AS prefix

OPTIONAL MATCH (m:AWSResource)
WHERE prefix <> "" AND m.arn STARTS WITH prefix

WITH arnPattern, coalesce(n, m) AS matched
WHERE matched IS NOT NULL

RETURN DISTINCT
    matched.arn         AS arn,
    labels(matched)     AS labels,
    coalesce(matched.name, matched.roleName, "") AS name,
    coalesce(matched.vpcId, "")  AS vpcId,
    coalesce(matched.region, "") AS region,
    properties(matched) AS properties,
    arnPattern          AS matchedPattern