// Package bfs implements BFS graph traversal for the knowledge graph service.
//
// Traversal is iterative (not recursive). At each depth level, a single batched
// SQL query fetches all neighbors for the current frontier, using the
// GetNeighbors helper in neighbors.go. No recursive CTEs are used.
package bfs

import (
	"context"
	"database/sql"
	"fmt"

	v1alpha1 "go.miloapis.com/knowledge/pkg/apis/knowledge/v1alpha1"
)

// GraphNode represents a single node discovered during BFS traversal.
type GraphNode struct {
	// ID is the unique identity key of this node: "{apiGroup}/{kind}/{namespace}/{name}".
	ID string
	// Endpoint identifies the Kubernetes resource this node represents.
	Endpoint v1alpha1.ResourceEndpoint
	// Depth is the number of hops from the root node to this node.
	Depth int
}

// GraphEdge represents a relationship edge discovered during BFS traversal.
type GraphEdge struct {
	// ID is the UID of the ResourceRelationship object.
	ID string
	// RelationshipType is the name of the RelationshipType for this edge.
	RelationshipType string
	// SubjectNodeID is the identity key of the subject node.
	SubjectNodeID string
	// ObjectNodeID is the identity key of the object node.
	ObjectNodeID string
}

// Traverse performs iterative BFS from root, calling GetNeighbors at each depth level.
//
// Returns nodes and edges up to maxDepth hops and maxNodes total nodes.
// If either limit is hit, truncated is set to true.
//
// direction must be one of "Outbound", "Inbound", or "Both".
// relationshipTypes restricts traversal to those type names; an empty slice means no filter.
func Traverse(
	ctx context.Context,
	db *sql.DB,
	root v1alpha1.ResourceEndpoint,
	maxDepth int,
	maxNodes int,
	relationshipTypes []string,
	direction string,
) (nodes []GraphNode, edges []GraphEdge, truncated bool, err error) {
	if maxDepth <= 0 {
		maxDepth = 5
	}
	if maxNodes <= 0 {
		maxNodes = 100
	}

	rootKey := endpointKey(root)
	visited := map[string]bool{rootKey: true}
	seenEdges := map[string]bool{}

	rootNode := GraphNode{
		ID:       rootKey,
		Endpoint: root,
		Depth:    0,
	}
	nodes = append(nodes, rootNode)

	// frontier holds the endpoint keys of the current depth level.
	frontier := []string{rootKey}
	// frontierEndpoints maps key -> endpoint for frontier members so we can
	// reconstruct neighbor endpoints when building GraphEdge.
	frontierEndpoints := map[string]v1alpha1.ResourceEndpoint{rootKey: root}

	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		neighbors, err := GetNeighbors(ctx, db, frontier, relationshipTypes, direction)
		if err != nil {
			return nodes, edges, truncated, fmt.Errorf("bfs: GetNeighbors at depth %d: %w", depth, err)
		}

		nextFrontier := []string{}
		nextFrontierEndpoints := map[string]v1alpha1.ResourceEndpoint{}

		for _, rr := range neighbors {
			// Skip edges already emitted on a previous depth — for direction=Both
			// the UNION can surface the same row twice, and even Outbound/Inbound
			// can rediscover an edge when both endpoints are in the frontier.
			edgeID := string(rr.UID)
			if seenEdges[edgeID] {
				continue
			}
			seenEdges[edgeID] = true

			subjectKey := endpointKey(rr.Spec.Subject)
			objectKey := endpointKey(rr.Spec.Object)

			// Determine which endpoint is the "new" neighbor relative to the frontier.
			// For Outbound: the object endpoint is new (frontier is subjects).
			// For Inbound:  the subject endpoint is new (frontier is objects).
			// For Both:     either could be new.
			var newKey string
			var newEndpoint v1alpha1.ResourceEndpoint

			switch direction {
			case "Inbound":
				newKey = subjectKey
				newEndpoint = rr.Spec.Subject
			case "Both":
				// The neighbor is whichever end is NOT in the frontier.
				if frontierEndpoints[subjectKey].Kind != "" {
					// Subject is in the frontier, so object is the new neighbor.
					newKey = objectKey
					newEndpoint = rr.Spec.Object
				} else {
					newKey = subjectKey
					newEndpoint = rr.Spec.Subject
				}
			default: // Outbound
				newKey = objectKey
				newEndpoint = rr.Spec.Object
			}

			// Record the edge regardless of whether the neighbor is new.
			edge := GraphEdge{
				ID:               string(rr.UID),
				RelationshipType: rr.Spec.RelationshipType.Name,
				SubjectNodeID:    subjectKey,
				ObjectNodeID:     objectKey,
			}
			edges = append(edges, edge)

			// Only add the new endpoint as a node if not yet visited.
			if !visited[newKey] {
				if len(nodes) >= maxNodes {
					truncated = true
					continue
				}
				visited[newKey] = true
				node := GraphNode{
					ID:       newKey,
					Endpoint: newEndpoint,
					Depth:    depth,
				}
				nodes = append(nodes, node)
				nextFrontier = append(nextFrontier, newKey)
				nextFrontierEndpoints[newKey] = newEndpoint
			}
		}

		if truncated {
			break
		}

		frontier = nextFrontier
		frontierEndpoints = nextFrontierEndpoints
	}

	return nodes, edges, truncated, nil
}
