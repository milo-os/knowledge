package bfs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // Postgres driver (pgx)
	v1alpha1 "go.miloapis.com/knowledge/pkg/apis/knowledge/v1alpha1"
)

// nodeKeyParts is the count of slash-separated components in an endpoint key.
// Keys are "{apiGroup}/{kind}/{namespace}/{name}/{cpContextKind}/{cpContextName}".
const nodeKeyParts = 6

// GetNeighbors returns all ResourceRelationship edges where any of nodeKeys appears
// as the subject (direction=Outbound), object (direction=Inbound), or either (direction=Both).
// relationshipTypes filters by type name; an empty slice means no filter.
//
// nodeKeys are the 6-component endpoint identity strings produced by endpointKey().
func GetNeighbors(
	ctx context.Context,
	db *sql.DB,
	nodeKeys []string,
	relationshipTypes []string,
	direction string,
) ([]v1alpha1.ResourceRelationship, error) {
	if len(nodeKeys) == 0 {
		return nil, nil
	}

	// Decompose node keys into parallel arrays — one per endpoint component.
	// We then JOIN against UNNEST(...) so each row of the array becomes a tuple
	// that's matched against the typed subject/object columns. This stays
	// index-friendly (vs. concatenating columns into a synthetic key) and
	// scales with len(nodeKeys) as a single bound query.
	apiGroups := make([]string, 0, len(nodeKeys))
	kinds := make([]string, 0, len(nodeKeys))
	namespaces := make([]string, 0, len(nodeKeys))
	names := make([]string, 0, len(nodeKeys))
	cpKinds := make([]string, 0, len(nodeKeys))
	cpNames := make([]string, 0, len(nodeKeys))
	for _, k := range nodeKeys {
		parts := strings.SplitN(k, "/", nodeKeyParts)
		if len(parts) != nodeKeyParts {
			return nil, fmt.Errorf("GetNeighbors: malformed node key %q", k)
		}
		apiGroups = append(apiGroups, parts[0])
		kinds = append(kinds, parts[1])
		namespaces = append(namespaces, parts[2])
		names = append(names, parts[3])
		cpKinds = append(cpKinds, parts[4])
		cpNames = append(cpNames, parts[5])
	}

	args := []any{apiGroups, kinds, namespaces, names, cpKinds, cpNames}

	const subjectJoin = `
INNER JOIN UNNEST($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[])
  AS n(ag, k, ns, nm, cpk, cpn)
  ON rr.subject_api_group = n.ag
 AND rr.subject_kind      = n.k
 AND rr.subject_namespace = n.ns
 AND rr.subject_name      = n.nm
 AND rr.subject_cp_context_kind = n.cpk
 AND rr.subject_cp_context_name = n.cpn`

	const objectJoin = `
INNER JOIN UNNEST($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[])
  AS n(ag, k, ns, nm, cpk, cpn)
  ON rr.object_api_group = n.ag
 AND rr.object_kind      = n.k
 AND rr.object_namespace = n.ns
 AND rr.object_name      = n.nm
 AND rr.object_cp_context_kind = n.cpk
 AND rr.object_cp_context_name = n.cpn`

	var query string
	switch direction {
	case "Inbound":
		query = `SELECT rr.object_data FROM resource_relationships rr` + objectJoin
	case "Both":
		// Use UNION to cover subject-side and object-side matches without
		// degenerating into a full Cartesian filter.
		query = `SELECT rr.object_data FROM resource_relationships rr` + subjectJoin +
			` UNION ` +
			`SELECT rr.object_data FROM resource_relationships rr` + objectJoin
	default: // Outbound
		query = `SELECT rr.object_data FROM resource_relationships rr` + subjectJoin
	}

	if len(relationshipTypes) > 0 {
		args = append(args, relationshipTypes)
		if direction == "Both" {
			query = buildBothQueryWithTypeFilter(subjectJoin, objectJoin)
		} else {
			query += ` WHERE rr.relationship_type = ANY($7)`
		}
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("GetNeighbors: query failed: %w", err)
	}
	defer rows.Close()

	var results []v1alpha1.ResourceRelationship
	for rows.Next() {
		var dataJSON []byte
		if err := rows.Scan(&dataJSON); err != nil {
			return nil, fmt.Errorf("GetNeighbors: scan failed: %w", err)
		}
		var rr v1alpha1.ResourceRelationship
		if err := json.Unmarshal(dataJSON, &rr); err != nil {
			return nil, fmt.Errorf("GetNeighbors: unmarshal failed: %w", err)
		}
		results = append(results, rr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetNeighbors: rows error: %w", err)
	}
	return results, nil
}

// buildBothQueryWithTypeFilter constructs the Outbound∪Inbound query with a
// relationship_type filter applied to each side, so the per-side index on
// relationship_type can be used before the UNION rather than after.
func buildBothQueryWithTypeFilter(subjectJoin, objectJoin string) string {
	return `SELECT rr.object_data FROM resource_relationships rr` + subjectJoin +
		` WHERE rr.relationship_type = ANY($7)` +
		` UNION ` +
		`SELECT rr.object_data FROM resource_relationships rr` + objectJoin +
		` WHERE rr.relationship_type = ANY($7)`
}

// endpointKey returns the BFS identity key for a ResourceEndpoint in the form
// "{apiGroup}/{kind}/{namespace}/{name}/{cpContextKind}/{cpContextName}".
// Cluster-scoped resources use an empty string for namespace.
func endpointKey(ep v1alpha1.ResourceEndpoint) string {
	return strings.Join([]string{
		ep.APIGroup,
		ep.Kind,
		ep.Namespace,
		ep.Name,
		ep.ControlPlaneContextRef.Kind,
		ep.ControlPlaneContextRef.Name,
	}, "/")
}
