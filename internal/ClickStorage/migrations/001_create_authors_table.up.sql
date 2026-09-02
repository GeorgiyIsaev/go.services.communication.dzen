-- Создание таблицы авторов
CREATE TABLE IF NOT EXISTS authors (
    id         SERIAL  PRIMARY KEY,                  -- уникальный идентификатор (автоинкремент)
    email      TEXT      NOT NULL UNIQUE,            -- электронная почта (обязательно, уникально)
    first_name TEXT      NOT NULL,                   -- имя (обязательно)
    last_name  TEXT      NOT NULL,                   -- фамилия (обязательно)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW() -- дата создания (автоматически)
);