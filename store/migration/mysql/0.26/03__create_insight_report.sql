CREATE TABLE `insight_report` (
  `id` INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `creator_id` INT NOT NULL,
  `created_ts` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_ts` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `source_type` VARCHAR(64) NOT NULL,
  `source_filter` TEXT NOT NULL DEFAULT '',
  `source_memo_names` JSON NOT NULL,
  `resolved_memo_names` JSON NOT NULL,
  `resolved_memo_count` INT NOT NULL DEFAULT 0,
  `perspective` VARCHAR(128) NOT NULL DEFAULT '',
  `summary` TEXT NOT NULL,
  `insight` LONGTEXT NOT NULL,
  `citations` JSON NOT NULL,
  `model` VARCHAR(256) NOT NULL DEFAULT ''
);

CREATE INDEX `idx_insight_report_creator_created_ts` ON `insight_report` (`creator_id`, `created_ts`);
