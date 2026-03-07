# Web Dashboard

React + TypeScript frontend for the trading engine.

## Stack

- React 18
- TypeScript
- Vite
- Tailwind CSS
- SWR
- Recharts

## Run locally

```bash
npm install
npm run dev
```

Default dev URL: `http://localhost:3000`

## Build

```bash
npm run build
```

## Backend dependency

The dashboard expects the backend API at `http://localhost:8080`.

Main endpoints used:

- `GET /api/traders`
- `GET /api/competition`
- `GET /api/status`
- `GET /api/account`
- `GET /api/positions`
- `GET /api/decisions`
- `GET /api/decisions/latest`
- `GET /api/statistics`
- `GET /api/equity-history`
- `GET /api/performance`
- `GET /api/candles`
- `GET /ws`

## Directory overview

```text
web/
  src/
    components/   UI views/widgets
    contexts/     app contexts
    i18n/         translations/resources
    lib/          API client
    types/        TypeScript models
  index.html
  vite.config.ts
  package.json
```
