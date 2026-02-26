CREATE TABLE insight_report (
  id SERIAL PRIMARY KEY,
  creator_id INTEGER NOT NULL,
  created_ts BIGINT NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()),
  updated_ts BIGINT NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()),
  source_type TEXT NOT NULL,
  source_filter TEXT NOT NULL DEFAULT '',
  source_memo_names JSONB NOT NULL DEFAULT '[]'::jsonb,
  resolved_memo_names JSONB NOT NULL DEFAULT '[]'::jsonb,
  resolved_memo_count INTEGER NOT NULL DEFAULT 0,
  perspective TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  insight TEXT NOT NULL DEFAULT '',
  citations JSONB NOT NULL DEFAULT '[]'::jsonb,
  model TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_insight_report_creator_created_ts ON insight_report (creator_id, created_ts DESC);
