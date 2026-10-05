CREATE TABLE works (
    id INTEGER PRIMARY KEY,
    url TEXT NOT NULL UNIQUE CHECK (length(url) > 0),
    title TEXT NOT NULL CHECK (length(title) > 0),
    site_name TEXT NOT NULL DEFAULT '',
    thumbnail_path TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (updated_at >= created_at)
) STRICT;
