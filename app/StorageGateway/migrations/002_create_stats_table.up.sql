CREATE TABLE IF NOT EXISTS stats (
    author_id INTEGER NOT NULL,
    date DATE NOT NULL,
    clicks INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (author_id, date)
    );
