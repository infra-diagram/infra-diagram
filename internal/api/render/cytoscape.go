package render

import (
	"fmt"

	"infra-diagram/internal/inspector"
)

// CytoscapeGraph is the format Cytoscape.js expects.
type CytoscapeGraph struct {
	Nodes []CytoscapeNode `json:"nodes"`
	Edges []CytoscapeEdge `json:"edges"`
}

type CytoscapeNode struct {
	Data CytoscapeNodeData `json:"data"`
}

type CytoscapeNodeData struct {
	ID         string         `json:"id"`
	Label      string         `json:"label"`
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
}

type CytoscapeEdge struct {
	Data CytoscapeEdgeData `json:"data"`
}

type CytoscapeEdgeData struct {
	ID         string         `json:"id"`
	Source     string         `json:"source"`
	Target     string         `json:"target"`
	Label      string         `json:"label"`
	Properties map[string]any `json:"properties,omitempty"`
}

// ToCytoscape transforms an inspector.Subgraph into Cytoscape.js format.
func ToCytoscape(sg inspector.Subgraph) CytoscapeGraph {
	nodes := make([]CytoscapeNode, 0, len(sg.Nodes))
	for _, n := range sg.Nodes {
		nodes = append(nodes, CytoscapeNode{
			Data: CytoscapeNodeData{
				ID:         n.ARN,
				Label:      nodeLabel(n),
				Type:       primaryLabel(n.Labels),
				Properties: n.Properties,
			},
		})
	}

	edges := make([]CytoscapeEdge, 0, len(sg.Edges))
	for i, e := range sg.Edges {
		edges = append(edges, CytoscapeEdge{
			Data: CytoscapeEdgeData{
				// Cytoscape requires unique edge IDs. ARNs can repeat
				// across multiple edges (e.g., same source-target with
				// different relation types), so we append an index.
				ID:         fmt.Sprintf("%s-%s-%d", e.FromARN, e.ToARN, i),
				Source:     e.FromARN,
				Target:     e.ToARN,
				Label:      e.Type,
				Properties: e.Properties,
			},
		})
	}

	return CytoscapeGraph{Nodes: nodes, Edges: edges}
}

// nodeLabel extracts a human-readable label from node properties.
// Falls back to the ARN if no name-like property is present.
func nodeLabel(n inspector.GraphNode) string {
	for _, key := range []string{"name", "roleName", "policyName", "subnetId", "vpcId"} {
		if v, ok := n.Properties[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return n.ARN
}

// primaryLabel returns the first non-"AWSResource" label.
func primaryLabel(labels []string) string {
	for _, l := range labels {
		if l != "AWSResource" {
			return l
		}
	}
	if len(labels) > 0 {
		return labels[0]
	}
	return "Unknown"
}
