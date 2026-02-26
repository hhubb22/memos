package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/usememos/memos/store"
)

func (d *DB) CreateInsightReport(ctx context.Context, create *store.InsightReport) (*store.InsightReport, error) {
	sourceMemoNames := "[]"
	if create.SourceMemoNames != nil {
		v, err := json.Marshal(create.SourceMemoNames)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal source memo names")
		}
		sourceMemoNames = string(v)
	}

	resolvedMemoNames := "[]"
	if create.ResolvedMemoNames != nil {
		v, err := json.Marshal(create.ResolvedMemoNames)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal resolved memo names")
		}
		resolvedMemoNames = string(v)
	}

	citations := "[]"
	if create.Citations != nil {
		v, err := json.Marshal(create.Citations)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal citations")
		}
		citations = string(v)
	}

	fields := []string{
		"`creator_id`",
		"`source_type`",
		"`source_filter`",
		"`source_memo_names`",
		"`resolved_memo_names`",
		"`resolved_memo_count`",
		"`perspective`",
		"`summary`",
		"`insight`",
		"`citations`",
		"`model`",
	}
	placeholder := []string{"?", "?", "?", "?", "?", "?", "?", "?", "?", "?", "?"}
	args := []any{
		create.CreatorID,
		create.SourceType,
		create.SourceFilter,
		sourceMemoNames,
		resolvedMemoNames,
		create.ResolvedMemoCount,
		create.Perspective,
		create.Summary,
		create.Insight,
		citations,
		create.Model,
	}

	stmt := "INSERT INTO `insight_report` (" + strings.Join(fields, ", ") + ") VALUES (" + strings.Join(placeholder, ", ") + ")"
	result, err := d.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	id32 := int32(id)

	list, err := d.ListInsightReports(ctx, &store.FindInsightReport{ID: &id32})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("insight report not found after insert")
	}

	return list[0], nil
}

func (d *DB) ListInsightReports(ctx context.Context, find *store.FindInsightReport) ([]*store.InsightReport, error) {
	where, args := []string{"1 = 1"}, []any{}

	if find.ID != nil {
		where, args = append(where, "`id` = ?"), append(args, *find.ID)
	}
	if find.CreatorID != nil {
		where, args = append(where, "`creator_id` = ?"), append(args, *find.CreatorID)
	}

	query := "SELECT `id`, `creator_id`, UNIX_TIMESTAMP(`created_ts`), UNIX_TIMESTAMP(`updated_ts`), `source_type`, `source_filter`, `source_memo_names`, `resolved_memo_names`, `resolved_memo_count`, `perspective`, `summary`, `insight`, `citations`, `model` FROM `insight_report` WHERE " + strings.Join(where, " AND ") + " ORDER BY `created_ts` DESC, `id` DESC"
	if find.Limit != nil {
		query = fmt.Sprintf("%s LIMIT %d", query, *find.Limit)
		if find.Offset != nil {
			query = fmt.Sprintf("%s OFFSET %d", query, *find.Offset)
		}
	}

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []*store.InsightReport{}
	for rows.Next() {
		report := &store.InsightReport{}
		var sourceMemoNamesBytes []byte
		var resolvedMemoNamesBytes []byte
		var citationsBytes []byte
		if err := rows.Scan(
			&report.ID,
			&report.CreatorID,
			&report.CreatedTs,
			&report.UpdatedTs,
			&report.SourceType,
			&report.SourceFilter,
			&sourceMemoNamesBytes,
			&resolvedMemoNamesBytes,
			&report.ResolvedMemoCount,
			&report.Perspective,
			&report.Summary,
			&report.Insight,
			&citationsBytes,
			&report.Model,
		); err != nil {
			return nil, err
		}

		if err := json.Unmarshal(sourceMemoNamesBytes, &report.SourceMemoNames); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal source memo names")
		}
		if err := json.Unmarshal(resolvedMemoNamesBytes, &report.ResolvedMemoNames); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal resolved memo names")
		}
		if err := json.Unmarshal(citationsBytes, &report.Citations); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal citations")
		}
		list = append(list, report)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return list, nil
}

func (d *DB) DeleteInsightReport(ctx context.Context, delete *store.DeleteInsightReport) error {
	result, err := d.db.ExecContext(ctx, "DELETE FROM `insight_report` WHERE `id` = ?", delete.ID)
	if err != nil {
		return errors.Wrap(err, "failed to delete insight report")
	}
	if _, err := result.RowsAffected(); err != nil {
		return err
	}
	return nil
}
