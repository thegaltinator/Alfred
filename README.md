# Alfred - Personal AI Butler

A voice-first AI assistant project designed as a personal butler with calendar, email, and productivity management capabilities.

## Architecture

This project follows a modular, microservices architecture:

- **Client**: macOS menubar application (Swift)
- **Server**: FastAPI backend with LangGraph orchestration
- **Services**: Separate TTS, embedding, and connector services
- **Infrastructure**: Redis (streams/pub-sub), Supabase (pgvector), Docker compose

## Getting Started

Refer to the individual component READMEs for specific setup instructions.

## License

Proprietary - All rights reserved.