CREATE TABLE managed_covers (
    id TEXT PRIMARY KEY NOT NULL CHECK (
        length(id) = 32 AND instr(id, char(0)) = 0 AND id NOT GLOB '*[^0-9a-f]*'
    ),
    relative_path TEXT GENERATED ALWAYS AS ('covers/' || id || '.png') STORED NOT NULL UNIQUE,
    state TEXT NOT NULL CHECK (state IN ('reserved', 'ready')),
    volume_serial TEXT,
    file_id TEXT,
    byte_size INTEGER,
    sha256 BLOB,
    CHECK (
        (state = 'reserved' AND volume_serial IS NULL AND file_id IS NULL AND byte_size IS NULL AND sha256 IS NULL)
        OR
        (state = 'ready'
            AND volume_serial IS NOT NULL AND length(volume_serial) = 16
            AND instr(volume_serial, char(0)) = 0 AND volume_serial NOT GLOB '*[^0-9a-f]*'
            AND file_id IS NOT NULL AND length(file_id) = 32
            AND instr(file_id, char(0)) = 0 AND file_id NOT GLOB '*[^0-9a-f]*'
            AND byte_size IS NOT NULL AND byte_size > 0
            AND sha256 IS NOT NULL AND length(sha256) = 32)
    )
) STRICT;
