
CREATE TABLE IF NOT EXISTS `folders` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `owner_id` BIGINT UNSIGNED NOT NULL,
    `parent_id` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=root directory',
    `name` VARCHAR(255) NOT NULL,
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at` DATETIME DEFAULT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_folders_owner_parent` (`owner_id`, `parent_id`),
    KEY `idx_folders_owner_deleted` (`owner_id`, `deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `files` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `owner_id` BIGINT UNSIGNED NOT NULL,
    `parent_id` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'Parent folder ID; 0=root',
    `name` VARCHAR(255) NOT NULL,
    `size` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'Size of the current version',
    `content_type` VARCHAR(255) NOT NULL DEFAULT 'application/octet-stream',
    `content_hash` CHAR(64) NOT NULL DEFAULT '' COMMENT 'SHA-256 hex digest of the current version',
    `object_key` VARCHAR(512) NOT NULL DEFAULT '',
    `version` INT UNSIGNED NOT NULL DEFAULT 1 COMMENT 'Current version number',
    `status` VARCHAR(20) NOT NULL DEFAULT 'pending' COMMENT 'pending, uploading, processing, available, failed, deleting',
    `request_id` VARCHAR(64) DEFAULT NULL COMMENT 'Idempotency key of the create-upload request',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at` DATETIME DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_files_owner_request` (`owner_id`, `request_id`),
    KEY `idx_files_owner_parent` (`owner_id`, `parent_id`),
    KEY `idx_files_owner_status` (`owner_id`, `status`),
    KEY `idx_files_owner_updated` (`owner_id`, `updated_at`),
    KEY `idx_files_content_hash` (`content_hash`),
    KEY `idx_files_object_key` (`object_key`(191))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `file_versions` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `file_id` BIGINT UNSIGNED NOT NULL,
    `owner_id` BIGINT UNSIGNED NOT NULL,
    `version` INT UNSIGNED NOT NULL DEFAULT 1,
    `size` BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `content_type` VARCHAR(255) NOT NULL DEFAULT 'application/octet-stream',
    `content_hash` CHAR(64) NOT NULL DEFAULT '',
    `object_key` VARCHAR(512) NOT NULL,
    `request_id` VARCHAR(64) DEFAULT NULL COMMENT 'Idempotency key, e.g. the upload id',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_file_versions_file_version` (`file_id`, `version`),
    UNIQUE KEY `uk_file_versions_file_request` (`file_id`, `request_id`),
    KEY `idx_file_versions_owner` (`owner_id`),
    KEY `idx_file_versions_object_key` (`object_key`(191))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `shares` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `file_id` BIGINT UNSIGNED NOT NULL,
    `owner_id` BIGINT UNSIGNED NOT NULL,
    `token_hash` CHAR(64) NOT NULL,
    `password_hash` VARCHAR(255) DEFAULT NULL,
    `expires_at` DATETIME DEFAULT NULL,
    `max_access_count` INT UNSIGNED DEFAULT NULL,
    `access_count` INT UNSIGNED NOT NULL DEFAULT 0,
    `last_access_at` DATETIME DEFAULT NULL,
    `status` TINYINT NOT NULL DEFAULT 1 COMMENT '1=active, 0=disabled',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_shares_token_hash` (`token_hash`),
    KEY `idx_shares_file_owner` (`file_id`, `owner_id`),
    KEY `idx_shares_expires_at` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
