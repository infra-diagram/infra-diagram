// internal/api/service/findings.go
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"

	"github.com/google/uuid"

	"infra-diagram/internal/inspector"
)

// --- Wire types (response shapes) ---

// FindingView is the lightweight row used in list responses. It deliberately
// omits evidence, remediation, and related_resources — those live in the detail.
type FindingView struct {
	ID           string                `json:"id"`  // stable hash, URL-safe
	Key          string                `json:"key"` // canonical key (the original verbose ID)
	Resource     inspector.ResourceRef `json:"resource"`
	Severity     inspector.Severity    `json:"severity"`
	Title        string                `json:"title"`
	RelatedCount int                   `json:"related_count"`
}

// FindingDetail is the full payload for a single finding.
type FindingDetail struct {
	ID               string                  `json:"id"`
	Key              string                  `json:"key"`
	InsightID        string                  `json:"insight_id"`
	InsightVersion   string                  `json:"insight_version"`
	Resource         inspector.ResourceRef   `json:"resource"`
	Severity         inspector.Severity      `json:"severity"`
	Title            string                  `json:"title"`
	Description      string                  `json:"description"`
	Evidence         map[string]any          `json:"evidence,omitempty"`
	Remediation      string                  `json:"remediation,omitempty"`
	RelatedResources []inspector.ResourceRef `json:"related_resources,omitempty"`
}

// FindingGraphResult carries the scoped subgraph for one finding plus meta.
// The handler converts Subgraph to Cytoscape before serializing.
type FindingGraphResult struct {
	InsightID      string
	InsightVersion string
	FindingID      string
	ExecutedAt     time.Time
	ExecutionMS    int64
	Subgraph       inspector.Subgraph
}

// SeverityCounts always serializes all five keys so the frontend badges
// render even at zero.
type SeverityCounts struct {
	Critical int `json:"CRITICAL"`
	High     int `json:"HIGH"`
	Medium   int `json:"MEDIUM"`
	Low      int `json:"LOW"`
	Info     int `json:"INFO"`
}

type Pagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// FindingsPage is the response for the list endpoint.
type FindingsPage struct {
	InsightID      string         `json:"insight_id"`
	InsightVersion string         `json:"insight_version"`
	AccountID      string         `json:"account_id"`
	ExecutedAt     time.Time      `json:"executed_at"`
	ExecutionMS    int64          `json:"execution_ms"`
	Pagination     Pagination     `json:"pagination"`
	SeverityCounts SeverityCounts `json:"severity_counts"`
	Findings       []FindingView  `json:"findings"`
}

// FindingsSummary is the dashboard response: counts only, no findings.
type FindingsSummary struct {
	InsightID      string         `json:"insight_id"`
	InsightVersion string         `json:"insight_version"`
	AccountID      string         `json:"account_id"`
	ExecutedAt     time.Time      `json:"executed_at"`
	ExecutionMS    int64          `json:"execution_ms"`
	Total          int            `json:"total"`
	SeverityCounts SeverityCounts `json:"severity_counts"`
}

// ListFindingsOptions holds filter + pagination params for ListFindings.
type ListFindingsOptions struct {
	Severity inspector.Severity // empty = all severities
	Page     int
	PageSize int
}

// --- Service methods ---

// ListFindings runs the insight and returns a paginated, severity-filtered
// list of lightweight finding views. severity_counts are computed over ALL
// findings (pre-filter) so badges show totals regardless of the active filter.
func (s *Service) ListFindings(
	ctx context.Context,
	accountID uuid.UUID,
	insightType string,
	extraParams map[string]any,
	opts ListFindingsOptions,
) (*FindingsPage, error) {
	result, prepared, err := s.preparedFindings(ctx, accountID, insightType, extraParams)
	if err != nil {
		return nil, err
	}

	counts := countSeverities(prepared)

	filtered := prepared
	if opts.Severity != "" {
		filtered = filterBySeverity(prepared, opts.Severity)
	}

	total := len(filtered)
	start, end := pageBounds(opts.Page, opts.PageSize, total)

	views := make([]FindingView, 0, end-start)
	for _, pf := range filtered[start:end] {
		views = append(views, toView(pf))
	}

	return &FindingsPage{
		InsightID:      result.InsightID,
		InsightVersion: result.InsightVersion,
		AccountID:      accountID.String(),
		ExecutedAt:     result.ExecutedAt,
		ExecutionMS:    result.ExecutionMS,
		Pagination: Pagination{
			Page:       opts.Page,
			PageSize:   opts.PageSize,
			Total:      total,
			TotalPages: totalPages(total, opts.PageSize),
		},
		SeverityCounts: counts,
		Findings:       views,
	}, nil
}

// GetFindingsSummary returns counts-only for a dashboard/overview view.
func (s *Service) GetFindingsSummary(
	ctx context.Context,
	accountID uuid.UUID,
	insightType string,
	extraParams map[string]any,
) (*FindingsSummary, error) {
	result, prepared, err := s.preparedFindings(ctx, accountID, insightType, extraParams)
	if err != nil {
		return nil, err
	}
	return &FindingsSummary{
		InsightID:      result.InsightID,
		InsightVersion: result.InsightVersion,
		AccountID:      accountID.String(),
		ExecutedAt:     result.ExecutedAt,
		ExecutionMS:    result.ExecutionMS,
		Total:          len(prepared),
		SeverityCounts: countSeverities(prepared),
	}, nil
}

// GetFinding returns the full detail for a single finding by its hash ID.
func (s *Service) GetFinding(
	ctx context.Context,
	accountID uuid.UUID,
	insightType string,
	findingID string,
	extraParams map[string]any,
) (*FindingDetail, error) {
	result, prepared, err := s.preparedFindings(ctx, accountID, insightType, extraParams)
	if err != nil {
		return nil, err
	}
	for _, pf := range prepared {
		if pf.hash == findingID {
			f := pf.finding
			return &FindingDetail{
				ID:               pf.hash,
				Key:              f.ID,
				InsightID:        result.InsightID,
				InsightVersion:   result.InsightVersion,
				Resource:         f.Resource,
				Severity:         f.Severity,
				Title:            f.Title,
				Description:      f.Description,
				Evidence:         f.Evidence,
				Remediation:      f.Remediation,
				RelatedResources: f.RelatedResources, // already deduped
			}, nil
		}
	}
	return nil, ErrFindingNotFound
}

// GetFindingGraph returns the subgraph scoped to a single finding.
//
// Fase 1: derives the subgraph by filtering the insight's global Subgraph
// down to the finding's resource + related resources. This preserves the
// real edge types/properties from Neo4j while producing a small, legible
// star graph instead of the merged hairball.
func (s *Service) GetFindingGraph(
	ctx context.Context,
	accountID uuid.UUID,
	insightType string,
	findingID string,
	extraParams map[string]any,
) (*FindingGraphResult, error) {
	result, prepared, err := s.preparedFindings(ctx, accountID, insightType, extraParams)
	if err != nil {
		return nil, err
	}
	for _, pf := range prepared {
		if pf.hash == findingID {
			return &FindingGraphResult{
				InsightID:      result.InsightID,
				InsightVersion: result.InsightVersion,
				FindingID:      pf.hash,
				ExecutedAt:     result.ExecutedAt,
				ExecutionMS:    result.ExecutionMS,
				Subgraph:       subgraphForFinding(result.Subgraph, pf.finding),
			}, nil
		}
	}
	return nil, ErrFindingNotFound
}

// --- internals ---

// preparedFinding bundles a finding with its stable hash. RelatedResources
// are already deduplicated here.
type preparedFinding struct {
	hash    string
	finding inspector.Finding
}

// preparedFindings runs the insight (via RunInsight, which resolves the
// account and translates validation errors) and returns findings prepared
// for serving: deduped, hashed, and sorted by severity desc then hash asc.
//
// Note: each finding-scoped endpoint re-runs the insight. At ~50ms this is
// fine for the MVP; once Persist-on-Discovery lands these become DB lookups.
func (s *Service) preparedFindings(
	ctx context.Context,
	accountID uuid.UUID,
	insightType string,
	extraParams map[string]any,
) (*inspector.Result, []preparedFinding, error) {
	result, err := s.RunInsight(ctx, accountID, insightType, extraParams)
	if err != nil {
		return nil, nil, err
	}
	return result, prepareFindings(result.Findings), nil
}

func prepareFindings(findings []inspector.Finding) []preparedFinding {
	out := make([]preparedFinding, 0, len(findings))
	for _, f := range findings {
		f.RelatedResources = dedupResources(f.RelatedResources) // local copy, original untouched
		out = append(out, preparedFinding{
			hash:    findingHash(f.ID),
			finding: f,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := severityRank(out[i].finding.Severity), severityRank(out[j].finding.Severity)
		if ri != rj {
			return ri > rj // higher severity first
		}
		return out[i].hash < out[j].hash // deterministic tiebreak
	})
	return out
}

func toView(pf preparedFinding) FindingView {
	return FindingView{
		ID:           pf.hash,
		Key:          pf.finding.ID,
		Resource:     pf.finding.Resource,
		Severity:     pf.finding.Severity,
		Title:        pf.finding.Title,
		RelatedCount: len(pf.finding.RelatedResources),
	}
}

// findingHash derives a short, URL-safe, deterministic ID from the canonical
// key (the insight's own Finding.ID). 16 hex chars = 64 bits — collision-safe
// for our volume. Bump to the full 64 if mathematical guarantees are ever needed.
func findingHash(canonicalKey string) string {
	h := sha256.Sum256([]byte(canonicalKey))
	return hex.EncodeToString(h[:])[:16]
}

func dedupResources(refs []inspector.ResourceRef) []inspector.ResourceRef {
	if len(refs) == 0 {
		return refs
	}
	seen := make(map[string]struct{}, len(refs))
	out := make([]inspector.ResourceRef, 0, len(refs))
	for _, r := range refs {
		if _, ok := seen[r.ARN]; ok {
			continue
		}
		seen[r.ARN] = struct{}{}
		out = append(out, r)
	}
	return out
}

func countSeverities(pfs []preparedFinding) SeverityCounts {
	var c SeverityCounts
	for _, pf := range pfs {
		switch pf.finding.Severity {
		case inspector.SeverityCritical:
			c.Critical++
		case inspector.SeverityHigh:
			c.High++
		case inspector.SeverityMedium:
			c.Medium++
		case inspector.SeverityLow:
			c.Low++
		case inspector.SeverityInfo:
			c.Info++
		}
	}
	return c
}

func filterBySeverity(pfs []preparedFinding, sev inspector.Severity) []preparedFinding {
	out := make([]preparedFinding, 0, len(pfs))
	for _, pf := range pfs {
		if pf.finding.Severity == sev {
			out = append(out, pf)
		}
	}
	return out
}

func severityRank(s inspector.Severity) int {
	switch s {
	case inspector.SeverityCritical:
		return 5
	case inspector.SeverityHigh:
		return 4
	case inspector.SeverityMedium:
		return 3
	case inspector.SeverityLow:
		return 2
	case inspector.SeverityInfo:
		return 1
	default:
		return 0
	}
}

func pageBounds(page, pageSize, total int) (start, end int) {
	start = (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	if start >= total {
		return total, total
	}
	end = start + pageSize
	if end > total {
		end = total
	}
	return start, end
}

func totalPages(total, pageSize int) int {
	if total == 0 || pageSize <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}

// subgraphForFinding scopes the insight's global subgraph to one finding:
// keeps the finding's resource + related resources as nodes, and only edges
// whose both endpoints fall inside that set. Nodes present in the global
// subgraph are reused as-is (real Neo4j properties); missing ones are
// synthesized from the ResourceRef so the node always renders.
func subgraphForFinding(global inspector.Subgraph, f inspector.Finding) inspector.Subgraph {
	related := f.RelatedResources // already deduped upstream

	keep := make(map[string]struct{}, len(related)+1)
	keep[f.Resource.ARN] = struct{}{}
	for _, r := range related {
		keep[r.ARN] = struct{}{}
	}

	globalNodes := make(map[string]inspector.GraphNode, len(global.Nodes))
	for _, n := range global.Nodes {
		globalNodes[n.ARN] = n
	}

	// Ordered: anchor first, then related — stable output.
	ordered := make([]inspector.ResourceRef, 0, len(related)+1)
	ordered = append(ordered, f.Resource)
	ordered = append(ordered, related...)

	nodes := make([]inspector.GraphNode, 0, len(keep))
	emitted := make(map[string]struct{}, len(ordered))
	for _, ref := range ordered {
		if _, done := emitted[ref.ARN]; done {
			continue
		}
		emitted[ref.ARN] = struct{}{}
		if gn, ok := globalNodes[ref.ARN]; ok {
			nodes = append(nodes, gn)
		} else {
			nodes = append(nodes, synthNodeFromRef(ref))
		}
	}

	edges := make([]inspector.GraphEdge, 0)
	for _, e := range global.Edges {
		if _, ok := keep[e.FromARN]; !ok {
			continue
		}
		if _, ok := keep[e.ToARN]; !ok {
			continue
		}
		edges = append(edges, e)
	}

	return inspector.Subgraph{Nodes: nodes, Edges: edges}
}

func synthNodeFromRef(ref inspector.ResourceRef) inspector.GraphNode {
	props := map[string]any{}
	if ref.Name != "" {
		props["name"] = ref.Name
	}
	if ref.Region != "" {
		props["region"] = ref.Region
	}
	labels := []string{"AWSResource"}
	if ref.Type != "" {
		labels = []string{ref.Type, "AWSResource"}
	}
	return inspector.GraphNode{
		ARN:        ref.ARN,
		Labels:     labels,
		Properties: props,
	}
}
