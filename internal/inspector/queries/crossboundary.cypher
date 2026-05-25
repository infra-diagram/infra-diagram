// Detecta edges TRIGGERS y NOTIFIES donde source y target están en VPCs
// distintas, o donde uno está en VPC y el otro no.
//
// La VPC de un recurso se obtiene de:
//   - Lambda (Function): property `vpcId` (puede estar vacío si no está en VPC)
//   - EC2Instance: property `vpcId`
//   - Queue, S3Bucket: no tienen VPC (recursos "públicos")
//
// El parámetro $accountId filtra por cuenta si se provee.

MATCH (source:AWSResource)-[edge:TRIGGERS|NOTIFIES]->(target:AWSResource)
WHERE ($accountId = "" OR (source.accountId = $accountId AND target.accountId = $accountId))

// Extraer vpcId de source y target. Si el nodo no tiene la property,
// coalesce devuelve "" (recurso sin VPC).
WITH source, target, edge,
     coalesce(source.vpcId, "") AS sourceVpc,
     coalesce(target.vpcId, "") AS targetVpc

// Solo nos interesan edges donde las VPCs difieren.
// Casos: VPC-A → VPC-B, VPC-A → "", "" → VPC-A
WHERE sourceVpc <> targetVpc

RETURN source.arn       AS sourceArn,
       labels(source)   AS sourceLabels,
       source.name      AS sourceName,
       sourceVpc        AS sourceVpc,
       source.region    AS sourceRegion,
       source.accountId AS sourceAccountId,
       target.arn       AS targetArn,
       labels(target)   AS targetLabels,
       target.name      AS targetName,
       targetVpc        AS targetVpc,
       target.region    AS targetRegion,
       target.accountId AS targetAccountId,
       type(edge)       AS edgeType,
       properties(edge) AS edgeProperties