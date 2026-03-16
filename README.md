# subscription-service

REST API для управления подписками.

## Запуск через Docker
docker-compose -p subapp up

Ручки API

- POST /subscriptions — создать подписку
- GET /subscriptions — список всех
- GET /subscriptions/{id} — получить одну
- PUT /subscriptions/{id} — обновить
- DELETE /subscriptions/{id} — удалить
- GET /calc?from=01-2025&to=12-2025&user=&name= — подсчёт стоимости

Пример запроса

curl -X POST http://localhost:8080/subscriptions \
  -H "Content-Type: application/json" \
  -d '{"service_name":"Yandex Plus","price":400,"user_id":"123e4567-e89b-12d3-a456-426614174000","start_date":"07-2025"}'

