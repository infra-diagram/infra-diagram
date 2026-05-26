package queries

import _ "embed"

//go:embed orphans_queue.cypher
var OrphansQueue string

//go:embed orphans_lambda.cypher
var OrphansLambda string

//go:embed orphans_role.cypher
var OrphansRole string

//go:embed orphans_subnet.cypher
var OrphansSubnet string

//go:embed overpermissive.cypher
var OverPermissive string

//go:embed crossboundary.cypher
var CrossBoundary string

//go:embed blast_radius_direct.cypher
var BlastRadiusDirectQuery string

//go:embed blast_radius_resource_lookup.cypher
var BlastRadiusResourceLookupQuery string

//go:embed access_patterns_overpriviledge.cypher
var AccessPatternsOverprivilegedQuery string

//go:embed access_patterns_overshared.cypher
var AccessPatternsOversharedQuery string
