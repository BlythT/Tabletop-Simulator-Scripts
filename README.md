# Tabletop Simulator Card Importer & Proxy

[![Go Proxy CI](https://github.com/BlythT/Tabletop-Simulator-Scripts/actions/workflows/go.yml/badge.svg)](https://github.com/BlythT/Tabletop-Simulator-Scripts/actions/workflows/go.yml)
![Go Test Coverage](https://img.shields.io/badge/coverage-81.1%25-brightgreen)

This repository contains custom Tabletop Simulator (TTS) Lua scripts for automated card importing and game modules, alongside a high-performance, zero-CGO local Go proxy server with SQLite caching to optimize Scryfall API queries.

---

## Go Proxy Server (`/proxy`)

A local REST proxy that intercepts Scryfall API endpoints queried by Tabletop Simulator. It serves lookups, fuzzy matching, and deck lists locally using a pre-populated SQLite database, protecting Scryfall's APIs from rate-limits (429s).

### Key Features
* **Offline Caching:** Streams and parses Scryfall's `default-cards` bulk JSON file, importing 115,000+ cards into a B-Tree indexed SQLite database in transactions.
* **Fast Batch Resolving:** Exposes a `/batch` POST endpoint that resolves entire 60-card decks in a single HTTP request in **~2ms**.
* **Index-Only Name Matching:** Utilizes exact and prefix range matching on cleaned names for sub-microsecond fuzzy lookups (no full-table scans).
* **Cross-Platform CGO-Free Compilation:** Written in pure Go with zero C-library compiler dependencies.

### Running Locally

1. **Rebuild the Database:**
   ```bash
   cd proxy
   go run . --update
   ```
2. **Launch the Proxy Server:**
   ```bash
   go run .
   ```
   *The server binds to port `8000` by default (http://localhost:8000).*

3. **Run Tests & Benchmarks:**
   ```bash
   go test -v -coverprofile=coverage.out
   go test -bench="." -benchmem
   ```

---

## Tabletop Simulator Importer (`/Magic`)

The Lua script integrates directly into Tabletop Simulator custom objects:
* **Batch Spawning:** Resolves decks synchronously in a single frame, building deck structures instantly on the table without staggered timers.
* **Redirection:** Dynamically proxies all Named, Set/Collector, Search, Random, Legalities, and Ruling requests through the local proxy.
