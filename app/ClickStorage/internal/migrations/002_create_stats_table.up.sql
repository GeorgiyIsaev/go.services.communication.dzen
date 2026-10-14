-- Создание таблицы статистики
CREATE TABLE IF NOT EXISTS stats (
                                     id SERIAL PRIMARY KEY,
                                     author_id INTEGER NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
    date DATE NOT NULL,
    clicks INTEGER NOT NULL DEFAULT 0,
    UNIQUE(author_id, date)
    );