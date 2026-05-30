package dataqueries

import _ "embed"

//go:embed access_patterns_overpriviledge.cypher
var AccessPatternsOverprivilegedQuery string

//go:embed access_patterns_overshared.cypher
var AccessPatternsOversharedQuery string
