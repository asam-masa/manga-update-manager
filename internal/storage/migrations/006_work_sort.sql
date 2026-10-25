ALTER TABLE display_settings ADD COLUMN work_sort TEXT NOT NULL
    DEFAULT 'registration_asc' CHECK (work_sort IN (
        'registration_asc', 'registration_desc', 'title_asc', 'title_desc',
        'last_access_asc', 'last_access_desc'
    ));
