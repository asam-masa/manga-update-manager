ALTER TABLE display_settings ADD COLUMN work_list_mode TEXT NOT NULL
    DEFAULT 'cards' CHECK (work_list_mode IN ('compact', 'rows', 'cards'));
