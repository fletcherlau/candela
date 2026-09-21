package schema

// Recovery attempts are execution records, not archived versions of results.
var rotationRecoveryMigration = sqlMigration(`CREATE TABLE IF NOT EXISTS rotation_recovery_run (
 sequence BIGINT NOT NULL AUTO_INCREMENT UNIQUE,id VARCHAR(32) PRIMARY KEY,
 request_key CHAR(64) NOT NULL UNIQUE,parent_id VARCHAR(32) NOT NULL DEFAULT '',
 origin_id VARCHAR(32) NOT NULL,basis VARCHAR(24) NOT NULL,trade_date CHAR(8) NOT NULL,target_at VARCHAR(35) NOT NULL,
 state VARCHAR(24) NOT NULL DEFAULT 'queued',stage VARCHAR(32) NOT NULL DEFAULT 'accepted',
 message VARCHAR(300) NOT NULL DEFAULT '',sync_batch_id VARCHAR(32) NOT NULL DEFAULT '',
 owner BIGINT NOT NULL DEFAULT 0,lease_until DATETIME(6) NULL,recoveries INT NOT NULL DEFAULT 0,
 created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,
 KEY recovery_queue(state,sequence),KEY recovery_origin(basis,origin_id)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
