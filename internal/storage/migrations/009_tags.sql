CREATE TABLE tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT CHECK (id > 0),
    name TEXT COLLATE BINARY NOT NULL UNIQUE CHECK (
        length(name) BETWEEN 1 AND 50 AND instr(name, char(0)) = 0
    )
) STRICT;

CREATE TABLE work_tags (
    work_id INTEGER NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (work_id, tag_id)
) STRICT;

CREATE INDEX work_tags_by_tag ON work_tags(tag_id, work_id);
