-- Создание таблицы статистики
CREATE TABLE IF NOT EXISTS stats (
    author_id INTEGER NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
    date DATE NOT NULL,
    clicks INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (author_id, date)
    );