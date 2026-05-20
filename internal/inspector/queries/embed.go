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
