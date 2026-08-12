-- Создание таблицы авторов
CREATE TABLE IF NOT EXISTS authors (
                                       id INTEGER PRIMARY KEY,
                                       created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
    );