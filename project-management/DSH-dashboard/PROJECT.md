# Dashboard

- **Code:** DSH
- **Status:** Implementation (Iteration 028) — fix Tailscale phone passkey enrollment (awaiting review)
- **Priority:** Q2 — Important, Not Urgent
- **Lead:** Developer
- **Created:** 2026-05-27
- **Last updated:** 2026-06-15
- **Current phase started:** 2026-06-15

## Overview
A self-hosted web dashboard that aggregates idea backlog, project management progress, and agent outputs into a single UI — accessible from any machine Tomas uses via SSH port-forward.

## Architecture

```mermaid
graph LR
    Browser["Browser\n(SSH tunnel / Tailscale)"] -->|HTTP/HTTPS| GoServer["Go HTTP Server\n(HTMX + templates)"]
    GoServer --> Auth["Auth Module\n(session, Passkey/WebAuthn,\nOAuth2.1 tokens)"]
    GoServer --> DB["SQLite\n(named volume)"]
    GoServer --> Files["Files\n(bind mounts)\ntodo.txt\nproject-management/"]
    Agent["Agent / project"] -->|"OAuth2.1\nclient_credentials"| GoServer
    GoServer -->|"Web Push\n(VAPID)"| FCM["Browser Push Service\n(FCM/APNs)"]
    FCM -->|"Encrypted push"| Phone["Phone Browser\n(Service Worker)"]
    Tailscale["Tailscale Sidecar"] -.->|"HTTPS proxy\nLet's Encrypt"| GoServer
```

## Current State
Iteration 028 — **Fix Tailscale phone passkey enrollment** (branch `dsh-taila-fix`, awaiting Tomas review). Phone couldn't get in: the only on-demand path to register a phone passkey (the boot-time setup token printed by `run.sh`) had a 10-min TTL anchored to container start, so on a days-up container it was always expired and `run.sh` couldn't re-mint it without a restart. Added an authenticated on-demand enrollment flow — **Passkeys → "Add a new device"** mints a fresh, generation-anchored, one-time token and shows a **QR** pointing at the Tailscale origin; scan it on the phone to register a passkey bound to the correct RPID. Also hardened `waForRequest` to a deterministic RP fallback. Full unit + integration tests green; live-binary smoke verified end-to-end (production container untouched). See iteration 028, [DSH-031], [DSH-032].

Previous — Iteration 026 — **Threads** shipped: durable M:N discussions attachable to notifications/plans/projects; agents post via JWT API with authenticated authorship, Tomas replies in the UI; `[discuss]`/💬 links on notification rows; nav badge counts open threads. This is the re-scoped L32+L33 (ideation 024): herdr/MND own the *live* agent-direction channel, DSH owns the *durable* one. First consumer: GML processed-tracking (`?ref_type=notification&ref_id=N&status=resolved` ⇒ skip re-distilling) — GML-side adoption is a `[GML]` backlog entry. Real-data acceptance test ready, pending a live-DB copy.

Previous: Iteration 022 — todo.txt filters/sort/badges/bulk actions. Iteration 020 — backlog batch (todo delete, project drill-down, favicon, notif display, Compose Watch). Iteration 019 — LLP tab over secure handshake. Iteration 016 — push notifications.
