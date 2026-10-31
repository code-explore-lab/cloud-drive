
CREATE TABLE IF NOT EXISTS `upload_tasks` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `upload_id` VARCHAR(64) NOT NULL,
    `user_id` BIGINT UNSIGNED NOT NULL,
    `file_id` BIGINT UNSIGNED DEFAULT NULL,
    `file_name` VARCHAR(255) NOT NULL,
    `total_size` BIGINT UNSIGNED NOT NULL,
    `content_type` VARCHAR(255) NOT NULL DEFAULT 'application/octet-stream',
    `expected_hash` CHAR(64) NOT NULL DEFAULT '',
    `object_key` VARCHAR(512) NOT NULL DEFAULT '' COMMENT 'Final object key, decided at init time',
    `chunk_size` BIGINT UNSIGNED NOT NULL,
    `total_chunks` INT UNSIGNED NOT NULL,
    `uploaded_chunks` INT UNSIGNED NOT NULL DEFAULT 0,
    `uploaded_bytes` BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `status` VARCHAR(20) NOT NULL DEFAULT 'initiated' COMMENT 'initiated, uploading, completing, completed, failed, aborted',
    `last_error` VARCHAR(512) NOT NULL DEFAULT '',
    `expires_at` DATETIME NOT NULL,
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_upload_tasks_upload_id` (`upload_id`),
    KEY `idx_upload_tasks_user_status` (`user_id`, `status`),
    KEY `idx_upload_tasks_expires_at` (`expires_at`),
    KEY `idx_upload_tasks_file_id` (`file_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `upload_parts` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `upload_id` VARCHAR(64) NOT NULL,
    `part_number` INT UNSIGNED NOT NULL,
    `size` BIGINT UNSIGNED NOT NULL,
    `content_hash` CHAR(64) NOT NULL DEFAULT '' COMMENT 'SHA-256 of this single part',
    `etag` VARCHAR(255) NOT NULL DEFAULT '',
    `object_part_id` VARCHAR(512) NOT NULL DEFAULT '' COMMENT 'Temporary object key of this part',
    `status` TINYINT NOT NULL DEFAULT 1 COMMENT '1=uploaded, 0=invalid',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_upload_parts_upload_part` (`upload_id`, `part_number`),
    KEY `idx_upload_parts_status` (`upload_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `storage_objects` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `object_key` VARCHAR(512) NOT NULL,
    `backend` VARCHAR(32) NOT NULL DEFAULT 'minio' COMMENT 'minio, s3, local',
    `bucket_name` VARCHAR(255) NOT NULL DEFAULT '',
    `size` BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `content_type` VARCHAR(255) NOT NULL DEFAULT 'application/octet-stream',
    `content_hash` CHAR(64) NOT NULL DEFAULT '',
    `hash_verified` TINYINT NOT NULL DEFAULT 0 COMMENT '1=digest verified against the uploaded bytes',
    `status` VARCHAR(20) NOT NULL DEFAULT 'available' COMMENT 'uploading, available, deleting, deleted, failed',
    `gc_attempts` INT UNSIGNED NOT NULL DEFAULT 0,
    `last_error` VARCHAR(512) NOT NULL DEFAULT '',
    `deleted_at` DATETIME DEFAULT NULL,
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_storage_objects_object_key` (`object_key`(191)),
    KEY `idx_storage_objects_hash` (`content_hash`),
    KEY `idx_storage_objects_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
