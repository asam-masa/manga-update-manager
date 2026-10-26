-- Rebuild only the preference table to remove the work_sort enum CHECK.
-- No application-owned indexes, triggers, views or foreign keys reference it.
CREATE TABLE new_display_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    registration_form_visible INTEGER NOT NULL CHECK (registration_form_visible IN (0, 1)),
    work_list_mode TEXT NOT NULL DEFAULT 'cards' CHECK (work_list_mode IN ('compact', 'rows', 'cards')),
    work_sort TEXT NOT NULL DEFAULT 'registration_asc',
    show_future_schedules INTEGER NOT NULL DEFAULT 1 CHECK (show_future_schedules IN (0, 1))
) STRICT;

INSERT INTO new_display_settings(id, registration_form_visible, work_list_mode, work_sort)
SELECT id, registration_form_visible, work_list_mode, work_sort FROM display_settings;
DROP TABLE display_settings;
ALTER TABLE new_display_settings RENAME TO display_settings;
