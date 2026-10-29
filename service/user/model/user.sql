CREATE TABLE users (
    id bigserial PRIMARY KEY,
    username varchar(64) NOT NULL UNIQUE,
    password varchar(128) NOT NULL,
    email varchar(128),
    status smallint NOT NULL DEFAULT 1,
    created_at timestamptz DEFAULT now(),
    updated_at timestamptz DEFAULT now()
);
CREATE TABLE user_quotas (
    id bigserial PRIMARY KEY,
    uid bigint NOT NULL REFERENCES users(id),
    total_quota bigint NOT NULL DEFAULT 1073741824,
    used bigint NOT NULL DEFAULT 0,
    created_at timestamptz DEFAULT now(),
    updated_at timestamptz DEFAULT now()
);
