# Jira REST API v2 

Все запросы выполняются к базовому URL: `{jira_base_url}/rest/api/2`.

## 1. Получение списка проектов

Получение списка всех доступных проектов для отображения в пользовательском интерфейсе.

### Запрос
GET /rest/api/2/project

## 2. Достать конкретный проект

### Запрос
http
GET /rest/api/2/project/{projectIdOrKey}

## 3. Поиск задач по проекту
http
GET /rest/api/2/search

параметры - jql