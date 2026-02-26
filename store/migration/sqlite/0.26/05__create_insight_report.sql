CREATE TABLE insight_report (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  creator_id INTEGER NOT NULL,
  created_ts BIGINT NOT NULL DEFAULT (strftime('%s', 'now')),
  updated_ts BIGINT NOT NULL DEFAULT (strftime('%s', 'now')),
  source_type TEXT NOT NULL,
  source_filter TEXT NOT NULL DEFAULT '',
  source_memo_names TEXT NOT NULL DEFAULT '[]',
  resolved_memo_names TEXT NOT NULL DEFAULT '[]',
  resolved_memo_count INTEGER NOT NULL DEFAULT 0,
  perspective TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  insight TEXT NOT NULL DEFAULT '',
  citations TEXT NOT NULL DEFAULT '[]',
  model TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_insight_report_creator_created_ts ON insight_report (creator_id, created_ts DESC);
