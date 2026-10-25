# go.services.communication.dzen

Учебный стенд для сквозной демонстрации путешествия события через распределённую систему: клик читателя по автору рождается в генераторе, проходит через Kafka, накапливается в агрегаторе, сохраняется в PostgreSQL через шлюз и отображается таблицей в веб-дашборде.

**Цель проекта** — показать, как событие, клик читателя по автору,
проходит путь от генерации до отображения на экране. На каждом шаге событие
трансформируется, обогащается и превращается в полезные данные.

--------------------------------------------------------

## Краткая схема

```
ClickImitator → Kafka → ClickCounter → StorageGateway ← ClickDashboard → UI
                                            ↓
                                        PostgreSQL
```


## Сервисы

| Сервис | Назначение | Транспорт | Порт |
|--------|-----------|-----------|------|
| **ClickImitator** | Генератор кликов, две стратегии симуляции (`random`, `scenario`) | Kafka producer | — |
| **ClickCounter** | Читает топик, считает уникальных читателей по авторам за день, отправляет статистику по расписанию | Kafka consumer + HTTP client | 8080 |
| **StorageGateway** | Единственная точка доступа к PostgreSQL: приём статистики, миграции, выдача данных | HTTP-сервер + gRPC-сервер | 8084 (HTTP), 9084 (gRPC) |
| **ClickDashboard** | Держит снапшот в памяти и отдаёт таблицу статистики за N дней | HTTP-сервер + gRPC client | 8085 |

Расписание ClickCounter: отправка в **00:05** и **01:05** UTC (страхующее окно), очистка старых дней в Tracker — в **00:06** UTC.

--------------------------------------------------------

## Быстрый старт

**Требования:** Docker с compose v2

```bash
# 1. Конфигурация
cp .env.template .env
# в .env заполнить DB_USER, DB_PASSWORD, DB_NAME (значения для Postgres и compose)

# 2. Запуск инфраструктуры и сервисов
docker compose up -d --build

# 3.1 Генерация кликов в режиме сценария
docker compose run --rm --build `
  -e CLICK_IMITATOR_GENERATOR_MODE=scenario `
  -e CLICK_IMITATOR_DELAY_BETWEEN_READS_SEC=1 `
  click-imitator
  
# 3.2 Рандомизированная генерация с управляемой нагрузкой
docker compose run --rm --build `
  -e CLICK_IMITATOR_AUTHOR_ID_START=1 `
  -e CLICK_IMITATOR_AUTHOR_ID_END=100 `
  -e CLICK_IMITATOR_READER_ID_START=1 `
  -e CLICK_IMITATOR_READER_ID_END=1000 `
  click-imitator

# 4. Генерация кликов в режиме по умолчанию
docker compose run --rm --build click-imitator

# 5. Ручная отправка данных без ожидания расписания
curl -X POST "http://localhost:8080/flush/today"
curl -X POST "http://localhost:8080/flush/yesterday"
curl -s http://localhost:8080/stats/today

# 6. Проверка
curl http://localhost:8080/healthz   # ClickCounter
curl http://localhost:8084/healthz   # StorageGateway
curl http://localhost:8085/healthz   # ClickDashboard
open http://localhost:8085/          # UI таблица статистики
```
-------------------------------------------------

## Контракты

<details>
    <summary> Kafka: топик clicks
</summary>

```json
{
  "user_id": 42,
  "author_id": 7,
  "timestamp": "2024-06-15T10:30:00Z"
}
```

- Ключ сообщения — `author_id`: клики одного автора попадают в одну партицию.
- **Дату события определяет имитатор** и пишет её в `timestamp`; каунтер использует именно это поле. Сообщение без `timestamp` — нарушение контракта — отбрасывается с предупреждением в логе.
- Чтение с `FirstOffset` при каждом старте, retention 48 часа.
</details> 


<details>
    <summary> HTTP: ClickCounter (:8080)
</summary>

- `POST /flush/yesterday` - Ручная отправка за вчера
- `POST /flush/today` -  Ручная отправка за сегодня 
- `GET /stats/yesterday`  - Посмотреть что накопилось в Tracker за вчера
- `GET /stats/today` - Посмотреть что накопилось в Tracker за сегодня
- `GET /healthz` - Здоровье 


</details>


<details>
    <summary> HTTP: StorageGateway (:8084)
</summary>

- `POST /stats` — приём статистики от `ClickCounter`.
  Тело: `{ "date": "YYYY-MM-DD", "stats": [{ "author_id": 1, "count": 10 }] }`
- `GET /dashboard?date=YYYY-MM-DD` — данные для дашборда за указанную дату.
- `GET /authors` — список всех авторов.
- `GET /healthz` - Здоровье

</details>


<details>
    <summary> gRPC: StorageGateway (:9084) -> ClickDashboard 
</summary>

Протокол — `app/StorageGateway/proto/storagegateway.proto` (клиентская копия — в `app/ClickDashboard/pkg/pb`). Режим — **unary**: один запрос, один ответ.

| Метод | Запрос | Ответ |
|---|---|---|
| `GetAuthors` | `GetAuthorsRequest{}` | `GetAuthorsResponse{authors[]}` |
| `GetStatsRange` | `GetStatsRangeRequest{from_date, to_date}` (YYYY-MM-DD) | `GetStatsRangeResponse{days[]}` |


</details>

<details>
    <summary> HTTP: ClickDashboard (:8085)
</summary>

- `GET /` - Статическая страница с таблицей 
- `GET /stats?days=N` - Данные для таблицы из снапшота 
- `GET /config` -`{ "max_days": 30 }` 
- `GET /healthz` - Здоровье 
</details>

-------------------------------------------------------------------------------------------

## Особенности реализации

- **Две стратегии симуляции.** 
`random` — каждый «читатель» делает случайное число чтений случайным авторам; 
`scenario` — готовые сценарии для отладки (всегда детерминированные).
- **Консьюмер без consumer group.**
`StartOffset = FirstOffset`, offset'ы нигде не сохраняются
При перезапуске читаем всегда сначала. Сообщения в кафке хранятся за 48 часов.
На каждую партицию свой `kafka.Reader`.
- **Push-модель раз в сутки.**
  ClickCounter не шлёт данные в момент клика — он накапливает их в памяти
  (только сегодня и вчера) и отправляет по расписанию.
- **StorageGateway — единственная точка доступа к БД.** 
Только он знает про PostgreSQL, применяет миграции. 
Остальные сервисы общаются с ним по HTTP и gRPC .
- **gRPC unary.** Один запрос и один ответ request/response. 
ClickDashboard запрашивает у StorageGateway авторов и статистику за диапазон дат.
- **Снапшот дашборда в памяти.** ClickDashboard периодически опрашивает шлюз и держит иммутабельный снапшот
- **Упрощенная UI таблица** для отображения статистики из ClickDashboard
- **Graceful shutdown.** Сервисы ловят SIGINT/SIGTERM, дожидаются незавершённых отправок, закрывают kafka-ридеры и останавливают серверы по таймауту

--------------------------------------------------------

## Заключение

Проект показывает путь одного клика целиком: имитатор генерирует событие, пишет его в Kafka с ключом по автору, счётчик читает поток и агрегирует, раз в сутки отправляет накопленное через шлюз в PostgreSQL, а дашборд забирает данные обратно и рисует таблицу за N дней.

По этому маршруту видно, как работают партиционирование по ключу, ежедневная агрегация по расписанию, ретраи с настраиваемыми паузами, единая точка доступа к БД и развязка сервисов через шлюз. Всё это можно руками поломать и починить: остановить брокера, сломать контракт сообщения, выключить шлюз и посмотреть, как остальные сервисы это переживают.
