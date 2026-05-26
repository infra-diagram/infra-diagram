package iamqueries

import _ "embed"

//go:embed overpermissive.cypher
var OverPermissive string

//go:embed blast_radius_direct.cypher
var BlastRadiusDirectQuery string

//go:embed blast_radius_resource_lookup.cypher
var BlastRadiusResourceLookupQuery string
