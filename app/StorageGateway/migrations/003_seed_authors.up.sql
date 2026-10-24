-- Заполнение таблицы authors тестовыми данными (100 записей).
-- Используем генерацию случайных имён и фамилий из заранее заданных списков,
-- а также уникальный email на основе имени, фамилии и порядкового номера.

INSERT INTO authors (id, email, first_name, last_name)
SELECT
    seq,
    -- Формируем email: имя.фамилия_номер@example.com (в нижнем регистре, без пробелов)
    LOWER(s.first_name) || '.' || LOWER(s.last_name) || '_' || seq || '@example.com' AS email,
    s.first_name,
    s.last_name
FROM (
         SELECT
             seq,
             -- Случайное имя из списка 30 популярных имён
             (ARRAY[
                  'John', 'Jane', 'Michael', 'Sarah', 'David', 'Laura',
              'Robert', 'Emma', 'James', 'Olivia', 'William', 'Mary',
              'Joseph', 'Patricia', 'Thomas', 'Linda', 'Charles', 'Barbara',
              'Christopher', 'Elizabeth', 'Daniel', 'Jennifer', 'Matthew', 'Maria',
              'Anthony', 'Susan', 'Donald', 'Margaret', 'Mark', 'Dorothy'
                  ])[floor(random() * 30) + 1] AS first_name,
             -- Случайная фамилия из списка 30 популярных фамилий
             (ARRAY[
                 'Smith', 'Johnson', 'Williams', 'Brown', 'Jones', 'Garcia',
                 'Miller', 'Davis', 'Rodriguez', 'Martinez', 'Hernandez', 'Lopez',
                 'Wilson', 'Anderson', 'Thomas', 'Taylor', 'Moore', 'Jackson',
                 'Martin', 'Lee', 'Perez', 'Thompson', 'White', 'Harris',
                 'Sanchez', 'Clark', 'Ramirez', 'Lewis', 'Robinson', 'Walker'
             ])[floor(random() * 30) + 1] AS last_name
         FROM generate_series(1, 100) AS seq
     ) s
-- Идемпотентность: при повторном применении просто пропускаем существующие id.
    ON CONFLICT (id) DO NOTHING;
