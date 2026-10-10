CREATE TABLE display_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    registration_form_visible INTEGER NOT NULL CHECK (registration_form_visible IN (0, 1))
) STRICT;
