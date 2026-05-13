package crawler

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// rebuildBelongsTo es un helper compartido: borra la relación BELONGS_TO
// saliente y crea la nueva. Útil en cualquier crawler que tenga BELONGS_TO.
func rebuildBelongsTo(ctx context.Context, session neo4j.SessionWithContext, childArn, parentArn string) error {
	// 1. Borrar relaciones BELONGS_TO existentes de este nodo
	_, err := session.Run(ctx,
		`MATCH (c:AWSResource {arn: $childArn})-[r:BELONGS_TO]->()
     DELETE r`,
		map[string]any{"childArn": childArn},
	)
	if err != nil {
		return fmt.Errorf("deleting old BELONGS_TO: %w", err)
	}
	// 2. Crear la nueva — solo si la VPC existe en el grafo.
	// Si no existe, MATCH falla en silencio y la relación no se crea.
	// El siguiente discovery o evento CreateVpc la creará.
	_, err = session.Run(ctx,
		`MATCH (c:AWSResource {arn: $childArn})
     MATCH (p:AWSResource {arn: $parentArn})
     MERGE (c)-[:BELONGS_TO]->(p)`,
		map[string]any{
			"childArn":  childArn,
			"parentArn": parentArn,
		},
	)
	if err != nil {
		return fmt.Errorf("creating BELONGS_TO: %w", err)
	}
	return nil
}

func rebuildUsesSubnet(ctx context.Context, session neo4j.SessionWithContext, childArn, subnetArn string) error {
	_, err := session.Run(ctx,
		`MATCH (c:AWSResource {arn: $childArn})-[r:USES_SUBNET]->()
		 DELETE r`,
		map[string]any{"childArn": childArn},
	)
	if err != nil {
		return fmt.Errorf("deleting old USES_SUBNET: %w", err)
	}

	_, err = session.Run(ctx,
		`MATCH (c:AWSResource {arn: $childArn})
		 MATCH (s:Subnet:AWSResource {arn: $subnetArn})
		 MERGE (c)-[:USES_SUBNET]->(s)`,
		map[string]any{
			"childArn":  childArn,
			"subnetArn": subnetArn,
		},
	)
	if err != nil {
		return fmt.Errorf("creating USES_SUBNET: %w", err)
	}
	return nil
}

// rebuildDeployedIn reemplaza la relación DEPLOYED_IN saliente.
// Se distingue de rebuildBelongsTo solo por el tipo de relación.
func rebuildDeployedIn(ctx context.Context, session neo4j.SessionWithContext, childArn, parentArn string) error {
	_, err := session.Run(ctx,
		`MATCH (c:AWSResource {arn: $childArn})-[r:DEPLOYED_IN]->()
		 DELETE r`,
		map[string]any{"childArn": childArn},
	)
	if err != nil {
		return fmt.Errorf("deleting old DEPLOYED_IN: %w", err)
	}

	_, err = session.Run(ctx,
		`MATCH (c:AWSResource {arn: $childArn})
		 MATCH (p:AWSResource {arn: $parentArn})
		 MERGE (c)-[:DEPLOYED_IN]->(p)`,
		map[string]any{
			"childArn":  childArn,
			"parentArn": parentArn,
		},
	)
	if err != nil {
		return fmt.Errorf("creating DEPLOYED_IN: %w", err)
	}
	return nil
}

// rebuildUsesSubnetMulti reemplaza TODAS las relaciones USES_SUBNET salientes
// con la lista provista. Si subnetArns está vacío, equivale a borrar todas.
//
// Diferencia con rebuildUsesSubnet (singular): este maneja N relaciones,
// necesario para Lambdas que pueden estar en varias subnets simultáneamente.
func rebuildUsesSubnetMulti(ctx context.Context, session neo4j.SessionWithContext, childArn string, subnetArns []string) error {
	// Borrar todas las USES_SUBNET previas
	_, err := session.Run(ctx,
		`MATCH (c:AWSResource {arn: $childArn})-[r:USES_SUBNET]->()
		 DELETE r`,
		map[string]any{"childArn": childArn},
	)
	if err != nil {
		return fmt.Errorf("deleting old USES_SUBNET: %w", err)
	}

	// Crear las nuevas
	for _, subnetArn := range subnetArns {
		_, err := session.Run(ctx,
			`MATCH (c:AWSResource {arn: $childArn})
			 MATCH (s:Subnet:AWSResource {arn: $subnetArn})
			 MERGE (c)-[:USES_SUBNET]->(s)`,
			map[string]any{
				"childArn":  childArn,
				"subnetArn": subnetArn,
			},
		)
		if err != nil {
			return fmt.Errorf("creating USES_SUBNET to %s: %w", subnetArn, err)
		}
	}
	return nil
}

// clearVPCRelations borra DEPLOYED_IN y USES_SUBNET de un recurso.
// Usado cuando una Lambda sale de VPC (UpdateFunctionConfiguration con VpcConfig vacío).
func clearVPCRelations(ctx context.Context, session neo4j.SessionWithContext, childArn string) error {
	_, err := session.Run(ctx,
		`MATCH (c:AWSResource {arn: $childArn})-[r:DEPLOYED_IN|USES_SUBNET]->()
		 DELETE r`,
		map[string]any{"childArn": childArn},
	)
	if err != nil {
		return fmt.Errorf("clearing vpc relations: %w", err)
	}
	return nil
}

// rebuildAttachedPolicies reemplaza TODAS las relaciones ATTACHED_POLICY salientes del rol.
// Si policyArns está vacío, equivale a borrar todas.
func rebuildAttachedPolicies(ctx context.Context, session neo4j.SessionWithContext, roleArn string, policyArns []string) error {
	_, err := session.Run(ctx,
		`MATCH (r:IAMRole:AWSResource {arn: $roleArn})-[rel:ATTACHED_POLICY]->()
		 DELETE rel`,
		map[string]any{"roleArn": roleArn},
	)
	if err != nil {
		return fmt.Errorf("deleting old ATTACHED_POLICY: %w", err)
	}

	for _, policyArn := range policyArns {
		_, err := session.Run(ctx,
			`MATCH (r:IAMRole:AWSResource {arn: $roleArn})
			 MATCH (p:IAMPolicy:AWSResource {arn: $policyArn})
			 MERGE (r)-[:ATTACHED_POLICY]->(p)`,
			map[string]any{
				"roleArn":   roleArn,
				"policyArn": policyArn,
			},
		)
		if err != nil {
			return fmt.Errorf("creating ATTACHED_POLICY to %s: %w", policyArn, err)
		}
	}
	return nil
}

// rebuildExecutes reemplaza TODAS las relaciones EXECUTES salientes del rol.
// Si functionArns está vacío, equivale a borrar todas.
func rebuildExecutes(ctx context.Context, session neo4j.SessionWithContext, roleArn string, functionArns []string) error {
	_, err := session.Run(ctx,
		`MATCH (r:IAMRole:AWSResource {arn: $roleArn})-[rel:EXECUTES]->()
		 DELETE rel`,
		map[string]any{"roleArn": roleArn},
	)
	if err != nil {
		return fmt.Errorf("deleting old EXECUTES: %w", err)
	}

	for _, funcArn := range functionArns {
		_, err := session.Run(ctx,
			`MATCH (r:IAMRole:AWSResource {arn: $roleArn})
			 MATCH (f:Function:AWSResource {arn: $funcArn})
			 MERGE (r)-[:EXECUTES]->(f)`,
			map[string]any{
				"roleArn": roleArn,
				"funcArn": funcArn,
			},
		)
		if err != nil {
			return fmt.Errorf("creating EXECUTES to %s: %w", funcArn, err)
		}
	}
	return nil
}

// RebuildInstanceProfileOf reemplaza TODAS las relaciones INSTANCE_PROFILE_OF salientes del rol.
// Exportada para que EC2Handler pueda llamarla directamente.
// Si instanceArns está vacío, equivale a borrar todas.
func RebuildInstanceProfileOf(ctx context.Context, session neo4j.SessionWithContext, roleArn string, instanceArns []string) error {
	_, err := session.Run(ctx,
		`MATCH (r:IAMRole:AWSResource {arn: $roleArn})-[rel:INSTANCE_PROFILE_OF]->()
		 DELETE rel`,
		map[string]any{"roleArn": roleArn},
	)
	if err != nil {
		return fmt.Errorf("deleting old INSTANCE_PROFILE_OF: %w", err)
	}

	for _, instanceArn := range instanceArns {
		_, err := session.Run(ctx,
			`MATCH (r:IAMRole:AWSResource {arn: $roleArn})
			 MATCH (e:EC2Instance:AWSResource {arn: $instanceArn})
			 MERGE (r)-[:INSTANCE_PROFILE_OF]->(e)`,
			map[string]any{
				"roleArn":     roleArn,
				"instanceArn": instanceArn,
			},
		)
		if err != nil {
			return fmt.Errorf("creating INSTANCE_PROFILE_OF to %s: %w", instanceArn, err)
		}
	}
	return nil
}
