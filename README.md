# bridge

Voice assistant bridge — Matrix bot + AI crew orchestrator for the ship at sea.

## Overview

`bridge` is the server-side orchestrator for the Crew at Sea voice assistant system.
It runs as a Matrix bot, routes messages to AI crew members, and manages session context.

## Architecture

- **Bot**: mautrix-python Matrix client (E2EE)
- **Orchestrator**: Routes messages to crew members, manages per-room session context
- **Crew**: AI personas backed by Claude (Anthropic SDK)
- **Crest**: IMAP/SMTP service for email bootstrap

## License

Apache-2.0
