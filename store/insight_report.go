package store

import "context"

// InsightSourceType indicates how source memos are selected.
type InsightSourceType string

const (
	// InsightSourceTypeMemoNames means source memos are provided explicitly.
	InsightSourceTypeMemoNames InsightSourceType = "MEMO_NAMES"
	// InsightSourceTypeFilter means source memos are selected by filter.
	InsightSourceTypeFilter InsightSourceType = "FILTER"
)

// InsightCitation represents a source citation used in an insight report.
type InsightCitation struct {
	Memo   string `json:"memo"`
	Quote  string `json:"quote"`
	Reason string `json:"reason"`
}

// InsightReport stores the generated AI insight output and metadata.
type InsightReport struct {
	ID int32

	CreatorID int32
	CreatedTs int64
	UpdatedTs int64

	SourceType InsightSourceType

	SourceFilter      string
	SourceMemoNames   []string
	ResolvedMemoNames []string
	ResolvedMemoCount int32

	Perspective string
	Summary     string
	Insight     string
	Citations   []InsightCitation

	Model string
}

// FindInsightReport finds insight reports by criteria.
type FindInsightReport struct {
	ID        *int32
	CreatorID *int32

	// Pagination.
	Limit  *int
	Offset *int
}

// DeleteInsightReport deletes an insight report.
type DeleteInsightReport struct {
	ID int32
}

// CreateInsightReport creates a new insight report.
func (s *Store) CreateInsightReport(ctx context.Context, create *InsightReport) (*InsightReport, error) {
	return s.driver.CreateInsightReport(ctx, create)
}

// ListInsightReports lists insight reports by criteria.
func (s *Store) ListInsightReports(ctx context.Context, find *FindInsightReport) ([]*InsightReport, error) {
	return s.driver.ListInsightReports(ctx, find)
}

// GetInsightReport gets one insight report by criteria.
func (s *Store) GetInsightReport(ctx context.Context, find *FindInsightReport) (*InsightReport, error) {
	list, err := s.ListInsightReports(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list[0], nil
}

// DeleteInsightReport deletes one insight report by ID.
func (s *Store) DeleteInsightReport(ctx context.Context, delete *DeleteInsightReport) error {
	return s.driver.DeleteInsightReport(ctx, delete)
}
