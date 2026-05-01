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

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)

Licensed under the [GNU Affero General Public License, version 3 or later](LICENSE.md) (AGPL-3.0-or-later).

### Why AGPL?

Bridge runs as a network service — a Matrix bot orchestrating AI crew on
behalf of users. AGPL's network clause (Section 13) closes the "SaaS
loophole" that weaker copyleft licenses leave open: anyone running a
modified Bridge as a service must offer source to their users. That
matters here because Bridge *is* a service, and we want the share-alike
spirit to follow the code wherever it sails.

Klazomenai may relicense Bridge under any **OSI-approved open-source
license** in future, exercising the sublicensing rights granted by
contributors via the [Contributor License Agreement](CONTRIBUTING.md) —
but never under proprietary or source-available terms. Contributors
retain copyright in their contributions. See [STEWARDSHIP.md](STEWARDSHIP.md)
for the public commitments behind that.
