# Alfred Server

FastAPI backend with LangGraph orchestration for Alfred.

## Structure

- **app/brain/**: Manager/Talker LangGraph graphs
- **app/agents/**: Specialized worker agents (scheduler, comms, productivity)
- **app/tools/**: Broker for external APIs (calendar, gmail, etc.)
- **app/events/**: Google Calendar/Gmail ingest handlers
- **app/whiteboard/**: Redis stream writer
- **app/state/**: Supabase/pgvector models for cloud memories
- **app/metrics/**: Prometheus metrics collection
- **infra/**: Managed Redis and Supabase configurations

## Requirements

- Python 3.11+
- Redis 7.0+
- PostgreSQL 15+ with pgvector extension

## Development

```bash
cd server
python -m venv venv
source venv/bin/activate
pip install -r requirements.txt
uvicorn app.main:app --reload --host 0.0.0.0 --port 8000
```