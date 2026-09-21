package schema

// ETF objects have their own identity; never insert them into index_series.
// The execution code shares the existing lease/fence rules through a fixed
// table namespace. This migration only creates tables and is replayable.
var etfSyncMigration = sqlMigration(
	`CREATE TABLE IF NOT EXISTS etf_sync_object (
 ts_code VARCHAR(20) PRIMARY KEY, fence BIGINT NOT NULL DEFAULT 0, active_run VARCHAR(32) NULL
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS etf_sync_run (
 sequence BIGINT NOT NULL AUTO_INCREMENT UNIQUE,
 id VARCHAR(32) PRIMARY KEY, ts_code VARCHAR(20) NOT NULL, mode VARCHAR(16) NOT NULL,
 start_date CHAR(8) NOT NULL, end_date CHAR(8) NOT NULL, effective_start CHAR(8) NOT NULL DEFAULT '',
 state VARCHAR(16) NOT NULL DEFAULT 'queued', stage VARCHAR(32) NOT NULL DEFAULT 'queued',
 processed_rows BIGINT NOT NULL DEFAULT 0, completed_segments INT NOT NULL DEFAULT 0,
 total_segments INT NOT NULL DEFAULT 0, checkpoint CHAR(8) NOT NULL DEFAULT '',
 history_evidence TEXT NOT NULL, error_code VARCHAR(40) NOT NULL DEFAULT '', message VARCHAR(300) NOT NULL DEFAULT '',
 owner BIGINT NOT NULL DEFAULT 0, lease_until DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 KEY object_queue(ts_code,state,sequence),FOREIGN KEY(ts_code) REFERENCES etf_sync_object(ts_code)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS etf_sync_event (
 sequence BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,run_id VARCHAR(32) NOT NULL,kind VARCHAR(32) NOT NULL,
 checkpoint CHAR(8) NOT NULL,message VARCHAR(300) NOT NULL,created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 KEY run_events(run_id,sequence),FOREIGN KEY(run_id) REFERENCES etf_sync_run(id) ON DELETE CASCADE
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS etf_sync_batch (
 id VARCHAR(32) PRIMARY KEY,parent_id VARCHAR(32) NULL UNIQUE,request_key CHAR(64) NOT NULL,end_date CHAR(8) NOT NULL,
 created_at DATETIME(6) NOT NULL, KEY batch_request(request_key)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS etf_sync_item (
 batch_id VARCHAR(32) NOT NULL,ts_code VARCHAR(20) NOT NULL,run_id VARCHAR(32) NOT NULL,
 PRIMARY KEY(batch_id,ts_code),KEY run_batch(run_id),
 FOREIGN KEY(batch_id) REFERENCES etf_sync_batch(id) ON DELETE CASCADE,
 FOREIGN KEY(run_id) REFERENCES etf_sync_run(id) ON DELETE CASCADE
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS etf_sync_progress (
 run_id VARCHAR(32) PRIMARY KEY,daily_start CHAR(8) NOT NULL,adj_start CHAR(8) NOT NULL,
 daily_checkpoint CHAR(8) NOT NULL DEFAULT '',adj_checkpoint CHAR(8) NOT NULL DEFAULT '',
 daily_rows BIGINT NOT NULL DEFAULT 0,adj_rows BIGINT NOT NULL DEFAULT 0,chunk_days INT NOT NULL,parent_run VARCHAR(32) NOT NULL DEFAULT '',
 FOREIGN KEY(run_id) REFERENCES etf_sync_run(id) ON DELETE CASCADE
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
)
