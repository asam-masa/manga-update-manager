ALTER TABLE works ADD COLUMN scheduled_date TEXT
    CHECK (scheduled_date IS NULL OR (
        length(scheduled_date) = 10
        AND scheduled_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
        AND substr(scheduled_date, 1, 4) BETWEEN '0001' AND '9999'
    ));
