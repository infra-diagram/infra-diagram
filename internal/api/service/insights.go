package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"infra-diagram/internal/inspector"
	"infra-diagram/internal/store"
)

// InsightSummary is the lightweight representation of an insight for listing.
type InsightSummary struct {
	ID          string                    `json:"id"`
	Version     string                    `json:"version"`
	Title       string                    `json:"title"`
	Description string                    `json:"description"`
	Category    string                    `json:"category"`
	Severity    inspector.Severity        `json:"severity"`
	Parameters  []inspector.ParameterSpec `json:"parameters,omitempty"`
}

// ListInsights returns the catalog of available insights.
func (s *Service) ListInsights(ctx context.Context) []InsightSummary {
	all := s.registry.List()
	out := make([]InsightSummary, 0, len(all))
	for _, ins := range all {
		meta := ins.Metadata()
		out = append(out, InsightSummary{
			ID:          ins.ID(),
			Version:     ins.Version(),
			Title:       meta.Title,
			Description: meta.Description,
			Category:    meta.Category,
			Severity:    meta.Severity,
			Parameters:  meta.Parameters,
		})
	}
	return out
}

// RunInsight executes an insight scoped to the given account and returns
// the full Result (findings + subgraph).
//
// accountID is the internal UUID. The service resolves it to the AWS
// account ID and passes it to the insight as a StandardFilter.
//
// extraParams contains any insight-specific parameters (e.g., role_arn
// for blast_radius). The service does not validate them — that is the
// insight's responsibility.
func (s *Service) RunInsight(
	ctx context.Context,
	accountID uuid.UUID,
	insightType string,
	extraParams map[string]any,
) (*inspector.Result, error) {
	// Resolve account
	account, err := s.store.GetAccount(ctx, accountID)
	if err != nil {
		if errors.Is(err, store.ErrAccountNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}

	// Resolve insight
	insight, ok := s.registry.Get(insightType)
	if !ok {
		return nil, ErrInsightNotFound
	}

	// Build filters with the AWS account ID
	filters := inspector.StandardFilters{
		AccountID: account.AWSAccountID,
	}

	// Execute
	return s.runner.Run(ctx, insight, filters, extraParams)
}
